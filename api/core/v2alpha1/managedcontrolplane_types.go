package v2alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"

	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
)

type ManagedControlPlaneV2Spec struct {
	// IAM contains the access management configuration for the ManagedControlPlaneV2.
	IAM IAMConfig `json:"iam"`
}

type IAMConfig struct {
	Tokens []TokenConfig `json:"tokens,omitempty"`
	OIDC   *OIDCConfig   `json:"oidc,omitempty"`
}

type OIDCConfig struct {
	DefaultProvider DefaultProviderConfig `json:"defaultProvider,omitempty"`
	// ExtraProviders is a list of OIDC providers that should be configured for the ManagedControlPlaneV2.
	// They are independent of the standard OIDC provider and in addition to it, unless it has been disabled by not specifying any role bindings.
	// +optional
	ExtraProviders []commonapi.OIDCProviderConfig `json:"extraProviders,omitempty"`
}

type DefaultProviderConfig struct {
	// RoleBindings is a list of subjects with (cluster) role bindings that should be created for them.
	// These bindings refer to the standard OIDC provider. If empty, the standard OIDC provider is disabled.
	// Note that the username prefix is added automatically to the subjects' names, it must not be explicitly specified here.
	// +optional
	RoleBindings []commonapi.RoleBindings `json:"roleBindings,omitempty"`
}

type TokenConfig struct {
	//+kubebuilder:validation:minLength=1
	Name                         string `json:"name"`
	clustersv1alpha1.TokenConfig `json:",inline"`
}

type ManagedControlPlaneV2Status struct {
	commonapi.Status `json:",inline"`

	// Access is a mapping from OIDC provider names to secret references.
	// Each referenced secret is expected to contain a 'kubeconfig' key with the kubeconfig that was generated for the respective OIDC provider for the ManagedControlPlaneV2.
	// The default OIDC provider, if configured, uses the name "default" in this mapping.
	// The "default" key is also used if the ClusterProvider does not support OIDC-based access and created a serviceaccount with a token instead.
	// +optional
	Access map[string]commonapi.LocalObjectReference `json:"access,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=mcpv2
// +kubebuilder:metadata:labels="openmcp.cloud/cluster=onboarding"
// +kubebuilder:selectablefield:JSONPath=".status.phase"
// +kubebuilder:printcolumn:JSONPath=".status.phase",name="Phase",type=string

type ManagedControlPlaneV2 struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ManagedControlPlaneV2Spec   `json:"spec,omitempty"`
	Status            ManagedControlPlaneV2Status `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

type ManagedControlPlaneV2List struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ManagedControlPlaneV2 `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ManagedControlPlaneV2{}, &ManagedControlPlaneV2List{})
}
