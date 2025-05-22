package install

import (
	"github.com/openmcp-project/controller-utils/pkg/resources"
	corev1 "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
)

func newProviderServiceAccountMutator(values *Values) resources.Mutator[*corev1.ServiceAccount] {
	res := resources.NewServiceAccountMutator(
		values.NamespacedDefaultResourceName(),
		values.Namespace(),
	)
	res.MetadataMutator().WithLabels(values.LabelsController())
	return res
}

func newProviderClusterRoleBindingMutator(values *Values) resources.Mutator[*rbac.ClusterRoleBinding] {
	res := resources.NewClusterRoleBindingMutator(
		values.ClusterScopedDefaultResourceName(),
		[]rbac.Subject{
			{
				Kind:      rbac.ServiceAccountKind,
				Name:      values.NamespacedDefaultResourceName(),
				Namespace: values.Namespace(),
			},
		},
		resources.NewClusterRoleRef("cluster-admin"),
	)
	res.MetadataMutator().WithLabels(values.LabelsController())
	return res
}
