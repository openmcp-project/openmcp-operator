package install

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/openmcp-project/controller-utils/pkg/resources"

	"github.com/openmcp-project/openmcp-operator/api/install"
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

	d.Spec = appsv1.DeploymentSpec{
		Replicas: ptr.To[int32](1),
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
						Args: []string{
							"run",
							"--environment=" + m.values.Environment(),
							"--verbosity=" + m.values.Verbosity(),
							"--provider-name=" + m.values.provider.GetName(),
						},
						Env:          env,
						VolumeMounts: m.values.deploymentSpec.ExtraVolumeMounts,
					},
				},
				ImagePullSecrets:   m.values.ImagePullSecrets(),
				ServiceAccountName: m.values.NamespacedDefaultResourceName(),
				Volumes:            m.values.deploymentSpec.ExtraVolumes,
			},
		},
	}

	// Set the provider as owner of the deployment, so that the provider controller gets an event if the deployment changes.
	if err := controllerutil.SetControllerReference(m.values.provider, d, install.InstallOperatorAPIs(runtime.NewScheme())); err != nil {
		return fmt.Errorf("failed to set deployment controller as owner of deployment: %w", err)
	}

	return m.meta.Mutate(d)
}
