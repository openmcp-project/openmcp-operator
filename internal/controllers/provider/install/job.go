package install

import (
	"fmt"

	"github.com/openmcp-project/controller-utils/pkg/resources"
	v1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openmcp-project/openmcp-operator/api/provider/v1alpha1"
)

type jobMutator struct {
	name           string
	namespace      string
	deploymentSpec *v1alpha1.DeploymentSpec
	meta           resources.Mutator[client.Object]
}

var _ resources.Mutator[*v1.Job] = &jobMutator{}

func newJobMutator(
	name string,
	namespace string,
	deploymentSpec *v1alpha1.DeploymentSpec,
	labels map[string]string,
	annotations map[string]string,
) resources.Mutator[*v1.Job] {
	return &jobMutator{
		name:           name,
		namespace:      namespace,
		deploymentSpec: deploymentSpec,
		meta:           resources.NewMetadataMutator(labels, annotations),
	}
}

func (m *jobMutator) String() string {
	return fmt.Sprintf("job %s/%s", m.namespace, m.name)
}

func (m *jobMutator) Empty() *v1.Job {
	return &v1.Job{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Job",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.name,
			Namespace: m.namespace,
		},
	}
}

func (m *jobMutator) Mutate(j *v1.Job) error {
	j.Spec = v1.JobSpec{
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Name: m.name,
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:            "init",
						Image:           m.deploymentSpec.Image,
						ImagePullPolicy: corev1.PullIfNotPresent,
						Command: []string{
							"/service-provider-landscaper",
							"init",
							"--verbosity=info",
						},
					},
				},
				ImagePullSecrets: m.imagePullSecrets(),
				RestartPolicy:    corev1.RestartPolicyNever,
			},
		},
	}
	return m.meta.Mutate(j)
}

func (m *jobMutator) imagePullSecrets() []corev1.LocalObjectReference {
	secrets := make([]corev1.LocalObjectReference, len(m.deploymentSpec.ImagePullSecrets))
	for i, s := range m.deploymentSpec.ImagePullSecrets {
		secrets[i] = corev1.LocalObjectReference{
			Name: s.Name,
		}
	}
	return secrets
}
