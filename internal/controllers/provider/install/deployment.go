package install

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openmcp-project/controller-utils/pkg/resources"
)

const (
	deploymentSelectorLabelKey   = "app"
	deploymentSelectorLabelValue = "openmcp.cloud/deployment"
)

type deploymentMutator struct {
	values *Values
	meta   resources.Mutator[client.Object]
}

var _ resources.Mutator[*appsv1.Deployment] = &deploymentMutator{}

func newDeploymentMutator(values *Values) resources.Mutator[*appsv1.Deployment] {
	return &deploymentMutator{
		values: values,
		meta:   resources.NewMetadataMutator(values.LabelsController(), nil),
	}
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

func (m *deploymentMutator) Mutate(r *appsv1.Deployment) error {
	r.Spec = appsv1.DeploymentSpec{
		Replicas: ptr.To[int32](1),
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				deploymentSelectorLabelKey: deploymentSelectorLabelValue,
			},
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
						},
					},
				},
				ImagePullSecrets:   m.values.ImagePullSecrets(),
				ServiceAccountName: m.values.NamespacedDefaultResourceName(),
			},
		},
	}
	return m.meta.Mutate(r)
}
