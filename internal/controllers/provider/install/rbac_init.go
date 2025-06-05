package install

import (
	"github.com/openmcp-project/controller-utils/pkg/resources"
	corev1 "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
)

func newInitServiceAccountMutator(values *Values) resources.Mutator[*corev1.ServiceAccount] {
	res := resources.NewServiceAccountMutator(values.NamespacedResourceName(initPrefix), values.Namespace())
	res.MetadataMutator().WithLabels(values.LabelsInitJob())
	return res
}

func newInitClusterRoleBindingMutator(values *Values) resources.Mutator[*rbac.ClusterRoleBinding] {
	clusterRoleName := values.ClusterScopedResourceName(initPrefix)
	res := resources.NewClusterRoleBindingMutator(
		clusterRoleName,
		[]rbac.Subject{
			{
				Kind:      rbac.ServiceAccountKind,
				Name:      values.NamespacedResourceName(initPrefix),
				Namespace: values.Namespace(),
			},
		},
		resources.NewClusterRoleRef(clusterRoleName),
	)
	res.MetadataMutator().WithLabels(values.LabelsInitJob())
	return res
}

func newInitClusterRoleMutator(values *Values) resources.Mutator[*rbac.ClusterRole] {
	res := resources.NewClusterRoleMutator(
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
				APIGroups: []string{""},
				Resources: []string{"configmaps"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{"clusters.openmcp.cloud"},
				Resources: []string{"accessrequests", "clusterrequests"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
		},
	)
	res.MetadataMutator().WithLabels(values.LabelsInitJob())
	return res
}

func newInitRoleBindingMutator(values *Values) resources.Mutator[*rbac.RoleBinding] {
	roleName := values.NamespacedResourceName(initPrefix)
	res := resources.NewRoleBindingMutator(
		roleName,
		values.Namespace(),
		[]rbac.Subject{
			{
				Kind:      rbac.ServiceAccountKind,
				Name:      values.NamespacedResourceName(initPrefix),
				Namespace: values.Namespace(),
			},
		},
		resources.NewRoleRef(roleName),
	)
	res.MetadataMutator().WithLabels(values.LabelsInitJob())
	return res
}

func newInitRoleMutator(values *Values) resources.Mutator[*rbac.Role] {
	roleName := values.NamespacedResourceName(initPrefix)
	res := resources.NewRoleMutator(
		roleName,
		values.Namespace(),
		[]rbac.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"secrets"},
				Verbs:     []string{"create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"configmaps"},
				Verbs:     []string{"create", "update", "patch", "delete"},
			},
		})

	res.MetadataMutator().WithLabels(values.LabelsInitJob())
	return res
}
