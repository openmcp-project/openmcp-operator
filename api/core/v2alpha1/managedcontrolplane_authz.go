package v2alpha1

import (
	rbacv1 "k8s.io/api/rbac/v1"
)

type AuthorizationConfiguration struct {
	// Members is a list of members with their assigned roles.
	Members []Member `json:"members,omitempty"`
}

type Member struct {
	Subject `json:",inline"`

	// Roles is a list of roles assigned to the subject.
	Roles []string `json:"roles,omitempty"`
}

// Subject contains a reference to the object or user identities a role binding applies to. This can either hold a direct API object reference,
// or a value for non-objects such as user and group names.
// +kubebuilder:validation:XValidation:rule="self.kind == 'ServiceAccount' || !has(self.__namespace__)",message="Namespace must not be specified if Kind is User or Group"
// +kubebuilder:validation:XValidation:rule="self.kind != 'ServiceAccount' || has(self.__namespace__)",message="Namespace is required for ServiceAccount"
type Subject struct {
	// Kind of object being referenced. Can be "User", "Group", or "ServiceAccount".
	// +kubebuilder:validation:Enum=User;Group;ServiceAccount
	Kind string `json:"kind"`

	// Name of the object being referenced.
	Name string `json:"name"`

	// Namespace of the referenced object. Required if Kind is "ServiceAccount". Must not be specified if Kind is "User" or "Group".
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// RbacV1 converts the Subject to a Kubernetes RBAC v1 Subject.
func (s Subject) RbacV1() rbacv1.Subject {
	rs := rbacv1.Subject{
		Kind:      s.Kind,
		Name:      s.Name,
		Namespace: s.Namespace,
	}
	if s.Kind != rbacv1.ServiceAccountKind {
		rs.APIGroup = rbacv1.GroupName
	}
	return rs
}
