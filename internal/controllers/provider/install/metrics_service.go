package install

import (
	"fmt"

	"github.com/openmcp-project/controller-utils/pkg/resources"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/openmcp-project/openmcp-operator/api/constants"
)

type metricsServiceMutator struct {
	values *Values
	meta   resources.MetadataMutator
	name   string
}

var _ resources.Mutator[*corev1.Service] = &metricsServiceMutator{}

func NewMetricsServiceMutator(values *Values) resources.Mutator[*corev1.Service] {
	suffix := "-metrics"
	res := &metricsServiceMutator{
		values: values,
		meta:   resources.NewMetadataMutator(),
		name:   fmt.Sprintf("%s%s", ctrlutils.ShortenToXCharactersUnsafe(values.NamespacedDefaultResourceName(), ctrlutils.K8sMaxNameLength-len(suffix)), suffix)
	}
	res.meta.WithLabels(values.LabelsController())
	return res
}

func (m *metricsServiceMutator) MetadataMutator() resources.MetadataMutator {
	return m.meta
}

func (m *metricsServiceMutator) String() string {
	return fmt.Sprintf("metricsService %s/%s", m.values.Namespace(), m.name)
}

func (m *metricsServiceMutator) Empty() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.name,
			Namespace: m.values.Namespace(),
		},
	}
}

func (m *metricsServiceMutator) Mutate(s *corev1.Service) error {
	s.Spec = corev1.ServiceSpec{
		Type:     corev1.ServiceTypeClusterIP,
		Selector: m.values.LabelsController(),
		Ports: []corev1.ServicePort{
			{
				Name:       constants.MetricsPortName,
				Port:       m.values.deploymentSpec.Metrics.GetPort(),
				Protocol:   corev1.ProtocolTCP,
				TargetPort: intstr.FromString(constants.MetricsPortName),
			},
		},
	}
	return m.meta.Mutate(s)
}
