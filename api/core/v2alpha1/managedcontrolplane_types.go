package v2alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
)

type ManagedControlPlaneSpec struct {
	// Authentication contains the configuration for the enabled OpenID Connect identity providers
	Authentication *AuthenticationConfiguration `json:"authentication,omitempty"`

	// Authorization contains the configuration of the subjects assigned to control plane roles
	Authorization *AuthorizationConfiguration `json:"authorization,omitempty"`
}

type ManagedControlPlaneStatus struct {
	commonapi.Status `json:",inline"`

	// Access contains a reference to a secret holding the kubeconfig for the ManagedControlPlane.
	Access *commonapi.LocalSecretReference `json:"access,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=mcp
// +kubebuilder:metadata:labels="openmcp.cloud/cluster=onboarding"
// +kubebuilder:selectablefield:JSONPath=".status.phase"
// +kubebuilder:printcolumn:JSONPath=".status.phase",name="Phase",type=string

type ManagedControlPlane struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ManagedControlPlaneSpec   `json:"spec,omitempty"`
	Status            ManagedControlPlaneStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

type ManagedControlPlaneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ManagedControlPlane `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ManagedControlPlane{}, &ManagedControlPlaneList{})
}
