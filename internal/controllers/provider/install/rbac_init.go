package install

import (
	"github.com/openmcp-project/controller-utils/pkg/resources"
	corev1 "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
)

func newInitServiceAccountMutator(values *Values) resources.Mutator[*corev1.ServiceAccount] {
	return resources.NewServiceAccountMutator(
		values.NamespacedResourceName(initPrefix),
		values.Namespace(),
		values.LabelsInitJob(),
		nil)
}

func newInitClusterRoleBindingMutator(values *Values) resources.Mutator[*rbac.ClusterRoleBinding] {
	clusterRoleName := values.ClusterScopedResourceName(initPrefix)
	return resources.NewClusterRoleBindingMutator(
		clusterRoleName,
		[]rbac.Subject{
			{
				Kind:      rbac.ServiceAccountKind,
				Name:      values.NamespacedResourceName(initPrefix),
				Namespace: values.Namespace(),
			},
		},
		resources.NewClusterRoleRef(clusterRoleName),
		values.LabelsInitJob(),
		nil)
}

func newInitClusterRoleMutator(values *Values) resources.Mutator[*rbac.ClusterRole] {
	return resources.NewClusterRoleMutator(
		values.ClusterScopedResourceName(initPrefix),
		[]rbac.PolicyRule{
			{
				APIGroups: []string{"apiextensions.k8s.io"},
				Resources: []string{"customresourcedefinitions"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"secrets"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{"clusters.openmcp.cloud"},
				Resources: []string{"accessrequests", "clusterrequests"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
		},
		values.LabelsInitJob(),
		nil)
}
