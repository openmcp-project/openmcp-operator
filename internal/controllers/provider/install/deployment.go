package install

import (
	"fmt"
	"slices"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/openmcp-project/openmcp-operator/api/constants"

	"github.com/openmcp-project/controller-utils/pkg/resources"

	"github.com/openmcp-project/openmcp-operator/api/install"
	libutils "github.com/openmcp-project/openmcp-operator/lib/utils"
)

type deploymentMutator struct {
	values *Values
	meta   resources.MetadataMutator
}

var _ resources.Mutator[*appsv1.Deployment] = &deploymentMutator{}

func NewDeploymentMutator(values *Values) resources.Mutator[*appsv1.Deployment] {
	res := &deploymentMutator{
		values: values,
		meta:   resources.NewMetadataMutator(),
	}
	res.meta.WithLabels(values.LabelsController())
	return res
}

func (m *deploymentMutator) MetadataMutator() resources.MetadataMutator {
	return m.meta
}

func (m *deploymentMutator) String() string {
	return fmt.Sprintf("deployment %s/%s", m.values.Namespace(), m.values.NamespacedDefaultResourceName())
}

func (m *deploymentMutator) Empty() *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.values.NamespacedDefaultResourceName(),
			Namespace: m.values.Namespace(),
		},
	}
}

func (m *deploymentMutator) Mutate(d *appsv1.Deployment) error {
	env, err := m.values.EnvironmentVariables()
	if err != nil {
		return err
	}

	volumes := m.values.deploymentSpec.ExtraVolumes
	volumeMounts := m.values.deploymentSpec.ExtraVolumeMounts
	if m.values.deploymentSpec.Webhook != nil && m.values.deploymentSpec.Webhook.Enabled {
		whSecretName, err := libutils.WebhookSecretName(m.values.provider.GetName())
		if err != nil {
			return err
		}
		volumes = append(volumes, corev1.Volume{
			Name: "webhook-tls",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: whSecretName,
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "webhook-tls",
			MountPath: "/tmp/k8s-webhook-server/serving-certs",
			ReadOnly:  true,
		})
	}

	runCmd := slices.Clone(m.values.deploymentSpec.RunCommand)
	if len(runCmd) == 0 {
		runCmd = []string{"run"}
	}
	runCmd = append(runCmd,
		"--environment="+m.values.Environment(),
		"--verbosity="+m.values.Verbosity(),
		"--provider-name="+m.values.provider.GetName(),
	)
	if m.values.deploymentSpec.RunReplicas > 1 {
		runCmd = append(runCmd, "--leader-elect=true")
	}
	d.Spec = appsv1.DeploymentSpec{
		Replicas: ptr.To(m.values.deploymentSpec.RunReplicas),
		Selector: &metav1.LabelSelector{
			MatchLabels: m.values.LabelsController(),
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: m.values.LabelsController(),
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:            m.values.NamespacedDefaultResourceName(),
						Image:           m.values.Image(),
						ImagePullPolicy: corev1.PullIfNotPresent,
						Args:            runCmd,
						Env:             env,
						VolumeMounts:    volumeMounts,
					},
				},
				ImagePullSecrets:          m.values.ImagePullSecrets(),
				ServiceAccountName:        m.values.NamespacedDefaultResourceName(),
				Volumes:                   volumes,
				TopologySpreadConstraints: m.values.deploymentSpec.TopologySpreadConstraints,
			},
		},
	}

	if len(m.values.deploymentSpec.TopologySpreadConstraints) > 0 {
		for i := range d.Spec.Template.Spec.TopologySpreadConstraints {
			labelSelector := &metav1.LabelSelector{
				MatchLabels: map[string]string{
					constants.TopologyLabel:          m.values.NamespacedDefaultResourceName(),
					constants.TopologyNamespaceLabel: m.values.Namespace(),
				},
			}
			d.Spec.Template.Spec.TopologySpreadConstraints[i].LabelSelector = labelSelector
		}

		d.Spec.Template.Labels[constants.TopologyLabel] = m.values.NamespacedDefaultResourceName()
		d.Spec.Template.Labels[constants.TopologyNamespaceLabel] = m.values.Namespace()
	}

	// Set the provider as owner of the deployment, so that the provider controller gets an event if the deployment changes.
	if err := controllerutil.SetControllerReference(m.values.provider, d, install.InstallOperatorAPIsPlatform(runtime.NewScheme())); err != nil {
		return fmt.Errorf("failed to set deployment controller as owner of deployment: %w", err)
	}

	return m.meta.Mutate(d)
}
