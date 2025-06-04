package install

import (
	"fmt"

	"github.com/openmcp-project/controller-utils/pkg/resources"
	v1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/openmcp-project/openmcp-operator/api/install"
	"github.com/openmcp-project/openmcp-operator/api/provider/v1alpha1"
)

type jobMutator struct {
	values         *Values
	deploymentSpec *v1alpha1.DeploymentSpec
	meta           resources.MetadataMutator
}

var _ resources.Mutator[*v1.Job] = &jobMutator{}

func NewJobMutator(values *Values, deploymentSpec *v1alpha1.DeploymentSpec, annotations map[string]string) resources.Mutator[*v1.Job] {
	res := &jobMutator{
		values:         values,
		deploymentSpec: deploymentSpec,
		meta:           resources.NewMetadataMutator(),
	}
	res.meta.WithLabels(values.LabelsInitJob()).WithAnnotations(annotations)
	return res
}

func (m *jobMutator) MetadataMutator() resources.MetadataMutator {
	return m.meta
}

func (m *jobMutator) String() string {
	return fmt.Sprintf("job %s/%s", m.values.Namespace(), m.values.NamespacedResourceName(initPrefix))
}

func (m *jobMutator) Empty() *v1.Job {
	return &v1.Job{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Job",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.values.NamespacedResourceName(initPrefix),
			Namespace: m.values.Namespace(),
		},
	}
}

func (m *jobMutator) Mutate(j *v1.Job) error {
	env, err := m.values.EnvironmentVariables()
	if err != nil {
		return err
	}

	j.Spec = v1.JobSpec{
		BackoffLimit: ptr.To[int32](4),
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: m.values.LabelsInitJob(),
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:            "init",
						Image:           m.values.Image(),
						ImagePullPolicy: corev1.PullIfNotPresent,
						Args: []string{
							"init",
							"--environment=" + m.values.Environment(),
							"--verbosity=" + m.values.Verbosity(),
						},
						Env: env,
					},
				},
				ServiceAccountName: m.values.NamespacedResourceName(initPrefix),
				ImagePullSecrets:   m.values.ImagePullSecrets(),
				RestartPolicy:      corev1.RestartPolicyNever,
			},
		},
	}

	// Set the provider as owner of the job, so that the provider controller gets an event if the job changes.
	if err := controllerutil.SetControllerReference(m.values.provider, j, install.InstallOperatorAPIs(runtime.NewScheme())); err != nil {
		return fmt.Errorf("failed to set deployment controller as owner of init job: %w", err)
	}

	return m.meta.Mutate(j)
}
