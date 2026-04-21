package v2alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"

	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
)

type ControlPlaneSpec struct {
	// IAM contains the access management configuration for the ControlPlane.
	IAM IAMConfig `json:"iam"`
}

type IAMConfig struct {
	// Tokens is a list of token-based access configurations.
	// +optional
	Tokens []TokenConfig `json:"tokens,omitempty"`
	// OIDC is the OIDC-based access configuration.
	OIDC *OIDCConfig `json:"oidc,omitempty"`
}

type OIDCConfig struct {
	// DefaultProvider is the standard OIDC provider that is enabled for all ControlPlane resources.
	DefaultProvider DefaultProviderConfig `json:"defaultProvider,omitempty"`
	// ExtraProviders is a list of OIDC providers that should be configured for the ControlPlane.
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
	// Name is the name of this token configuration.
	// It is used to generate a secret name and must be unique among all token configurations in the same ControlPlane.
	// +kubebuilder:validation:minLength=1
	Name                         string `json:"name"`
	clustersv1alpha1.TokenConfig `json:",inline"`
}

type ControlPlaneStatus struct {
	commonapi.Status `json:",inline"`

	// Access is a mapping from OIDC provider names to secret references.
	// Each referenced secret is expected to contain a 'kubeconfig' key with the kubeconfig that was generated for the respective OIDC provider for the ControlPlane.
	// The default OIDC provider, if configured, uses the name "default" in this mapping.
	// The "default" key is also used if the ClusterProvider does not support OIDC-based access and created a serviceaccount with a token instead.
	// +optional
	Access map[string]commonapi.LocalObjectReference `json:"access,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=cp;ctrlp
// +kubebuilder:metadata:labels="openmcp.cloud/cluster=onboarding"
// +kubebuilder:selectablefield:JSONPath=".status.phase"
// +kubebuilder:printcolumn:JSONPath=".status.phase",name="Phase",type=string

type ControlPlane struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ControlPlaneSpec   `json:"spec,omitempty"`
	Status            ControlPlaneStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

type ControlPlaneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ControlPlane `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ControlPlane{}, &ControlPlaneList{})
}
