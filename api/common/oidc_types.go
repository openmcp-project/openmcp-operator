package common

import (
	rbacv1 "k8s.io/api/rbac/v1"
)

type OIDCProviderConfig struct {
	// Name is the name of the OIDC provider.
	// May be used in k8s resources, therefore has to be a valid k8s name.
	// It is also used (with a ':' suffix) as prefix in k8s resources referencing users or groups from this OIDC provider.
	// E.g. if the name is 'example', the username 'alice' from this provider will be referenced as 'example:alice' in k8s resources.
	// Must be unique among all OIDC providers configured in the same environment.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	// +kubebuilder:validation:XValidation:rule=`self != "system"`, message="'system' is a reserved string and may not be used as OIDC provider name"
	Name string `json:"name"`

	// Issuer is the issuer URL of the OIDC provider.
	// Must be a valid URL.
	// +kubebuilder:validation:Pattern=`^https?://[^\s/$.?#].[^\s]*$`
	// +kubebuilder:validation:MinLength=1
	Issuer string `json:"issuer"`

	// ClientID is the client ID to use for the OIDC provider.
	// +kubebuilder:validation:MinLength=1
	ClientID string `json:"clientID"`

	// GroupsClaim is the claim in the OIDC token that contains the groups.
	// If empty, the default claim "groups" will be used.
	// +kubebuilder:default="groups"
	// +optional
	GroupsClaim string `json:"groupsClaim"`

	// UsernameClaim is the claim in the OIDC token that contains the username.
	// If empty, the default claim "sub" will be used.
	// +kubebuilder:default="sub"
	// +optional
	UsernameClaim string `json:"usernameClaim"`

	// ExtraScopes is a list of extra scopes that should be requested from the OIDC provider.
	// +optional
	ExtraScopes []string `json:"extraScopes,omitempty"`

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

// RoleRef defines a reference to a (cluster) role that should be bound to the subjects.
// TODO: Validate that Namespace is set if Kind is 'Role' and not set if Kind is 'ClusterRole'.
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
	if o.UsernameClaim == "" {
		o.UsernameClaim = "sub"
	}
	return o
}

// UsernameGroupsPrefix returns the prefix for usernames and groups for this OIDC provider.
// It is equivalent to <provider_name> + ":".
func (o *OIDCProviderConfig) UsernameGroupsPrefix() string {
	return o.Name + ":"
}
