package install

import (
	"github.com/openmcp-project/controller-utils/pkg/resources"
	corev1 "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
)

func newProviderServiceAccountMutator(values *Values) resources.Mutator[*corev1.ServiceAccount] {
	return resources.NewServiceAccountMutator(
		values.NamespacedDefaultResourceName(),
		values.Namespace(),
		values.LabelsController(),
		nil)
}

func newProviderClusterRoleBindingMutator(values *Values) resources.Mutator[*rbac.ClusterRoleBinding] {
	return resources.NewClusterRoleBindingMutator(
		values.ClusterScopedDefaultResourceName(),
		[]rbac.Subject{
			{
				Kind:      rbac.ServiceAccountKind,
				Name:      values.NamespacedDefaultResourceName(),
				Namespace: values.Namespace(),
			},
		},
		resources.NewClusterRoleRef("cluster-admin"),
		values.LabelsController(),
		nil)
}
