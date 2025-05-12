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
	values         *Values
	deploymentSpec *v1alpha1.DeploymentSpec
	meta           resources.Mutator[client.Object]
}

var _ resources.Mutator[*v1.Job] = &jobMutator{}

func newJobMutator(
	values *Values,
	deploymentSpec *v1alpha1.DeploymentSpec,
	annotations map[string]string,
) resources.Mutator[*v1.Job] {
	return &jobMutator{
		values:         values,
		deploymentSpec: deploymentSpec,
		meta:           resources.NewMetadataMutator(values.LabelsInitJob(), annotations),
	}
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
	j.Spec = v1.JobSpec{
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
						},
					},
				},
				ServiceAccountName: m.values.NamespacedResourceName(initPrefix),
				ImagePullSecrets:   m.values.ImagePullSecrets(),
				RestartPolicy:      corev1.RestartPolicyNever,
			},
		},
	}
	return m.meta.Mutate(j)
}
