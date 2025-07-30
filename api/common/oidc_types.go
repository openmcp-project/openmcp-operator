package common

import (
	"strings"

	rbacv1 "k8s.io/api/rbac/v1"
)

type OIDCProviderConfig struct {
	// Name is the name of the OIDC provider.
	// May be used in k8s resources, therefore has to be a valid k8s name.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*`
	Name string `json:"name"`

	// Issuer is the issuer URL of the OIDC provider.
	Issuer string `json:"issuer"`

	// ClientID is the client ID to use for the OIDC provider.
	ClientID string `json:"clientID"`

	// GroupsClaim is the claim in the OIDC token that contains the groups.
	// If empty, the default claim "groups" will be used.
	// +kubebuilder:default="groups"
	// +optional
	GroupsClaim string `json:"groupsClaim"`

	// GroupsPrefix is a prefix that will be added to all group names when referenced in RBAC rules.
	// This is required to avoid conflicts with Kubernetes built-in groups.
	// If the prefix does not end with a colon (:), it will be added automatically.
	// +kubebuilder:validation:MinLength=1
	GroupsPrefix string `json:"groupsPrefix"`

	// UsernameClaim is the claim in the OIDC token that contains the username.
	// If empty, the default claim "sub" will be used.
	// +kubebuilder:default="sub"
	// +optional
	UsernameClaim string `json:"usernameClaim"`

	// UsernamePrefix is a prefix that will be added to all usernames when referenced in RBAC rules.
	// This is required to avoid conflicts with Kubernetes built-in users.
	// If the prefix does not end with a colon (:), it will be added automatically.
	// +kubebuilder:validation:MinLength=1
	UsernamePrefix string `json:"usernamePrefix"`

	// RoleBindings is a list of subjects with (cluster) role bindings that should be created for them.
	// Note that the username prefix is added automatically to the subjects' names, it must not be explicitly specified here.
	RoleBindings []RoleBindings `json:"roleBindings"`
}

type RoleBindings struct {
	// Subjects is a list of subjects that should be bound to the specified roles.
	// The subjects' names will be prefixed with the username prefix of the OIDC provider.
	Subjects []rbacv1.Subject `json:"subjects"`

	// RoleRefs is a list of (cluster) role references that the subjects should be bound to.
	// Note that existence of the roles is not checked and missing (cluster) roles will result in ineffective (cluster) role bindings.
	RoleRefs []RoleRef `json:"roleRefs"`
}

// +kubebuilder:validation:XValidation:rule="self.kind == 'Role' && has(self.namespace) && self.namespace != ”", message="namespace must be set if kind is 'Role'"
// +kubebuilder:validation:XValidation:rule="self.kind == 'ClusterRole' && (!has(self.namespace) || self.namespace == ”)", message="namespace must not be set if kind is 'ClusterRole'"
type RoleRef struct {
	// Name is the name of the role or cluster role to bind to the subjects.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Namespace is the namespace of the role to bind to the subjects.
	// It must be set if the kind is 'Role' and may not be set if the kind is 'ClusterRole'.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Kind is the kind of the role to bind to the subjects.
	// It must be 'Role' or 'ClusterRole'.
	// +kubebuilder:validation:Enum=Role;ClusterRole
	Kind string `json:"kind"`
}

// Default sets default values for the OIDCProviderConfig.
// Modifies in-place and returns the receiver for chaining.
func (o *OIDCProviderConfig) Default() *OIDCProviderConfig {
	if o == nil {
		return nil
	}
	if o.GroupsClaim == "" {
		o.GroupsClaim = "groups"
	}
	if !strings.HasSuffix(o.GroupsPrefix, ":") {
		o.GroupsPrefix += ":"
	}
	if o.UsernameClaim == "" {
		o.UsernameClaim = "sub"
	}
	if !strings.HasSuffix(o.UsernamePrefix, ":") {
		o.UsernamePrefix += ":"
	}
	return o
}
