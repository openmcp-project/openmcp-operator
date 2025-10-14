package v1alpha1

import (
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
)

const (
	// AccessRequestPending is the phase if the AccessRequest has not been scheduled yet.
	AccessRequestPending = "Pending"
	// AccessRequestGranted is the phase if the AccessRequest has been granted.
	AccessRequestGranted = "Granted"
)

// +kubebuilder:validation:XValidation:rule="!has(oldSelf.clusterRef) || has(self.clusterRef)", message="clusterRef may not be removed once set"
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.requestRef) || has(self.requestRef)", message="requestRef may not be removed once set"
// +kubebuilder:validation:XValidation:rule="(has(self.token) && !has(self.oidc)) || (!has(self.token) && has(self.oidc))",message="exactly one of spec.token or spec.oidc must be set"
type AccessRequestSpec struct {
	// ClusterRef is the reference to the Cluster for which access is requested.
	// If set, requestRef will be ignored.
	// This value is immutable.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="clusterRef is immutable"
	// +optional
	ClusterRef *commonapi.ObjectReference `json:"clusterRef,omitempty"`

	// RequestRef is the reference to the ClusterRequest for whose Cluster access is requested.
	// Is ignored if clusterRef is set.
	// This value is immutable.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="requestRef is immutable"
	// +optional
	RequestRef *commonapi.ObjectReference `json:"requestRef,omitempty"`

	// Token is the configuration for token-based access.
	// Exactly one of Token or OIDC must be set.
	// +optional
	Token *TokenConfig `json:"token,omitempty"`

	// OIDC is the configuration for OIDC-based access.
	// Exactly one of Token or OIDC must be set.
	// +optional
	OIDC *OIDCConfig `json:"oidc,omitempty"`
}

type TokenConfig struct {
	// Permissions are the requested permissions.
	// If not empty, corresponding Roles and ClusterRoles will be created in the target cluster.
	// The created serviceaccount will be bound to the created Roles and ClusterRoles.
	// +optional
	Permissions []PermissionsRequest `json:"permissions,omitempty"`

	// RoleRefs are references to existing (Cluster)Roles that should be bound to the created serviceaccount.
	// +optional
	RoleRefs []commonapi.RoleRef `json:"roleRefs,omitempty"`
}

type OIDCConfig struct {
	commonapi.OIDCProviderConfig `json:",inline"`

	// Roles are additional (Cluster)Roles that should be created.
	// Note that they are not automatically bound to any user.
	// It is strongly recommended to set the name field so that the created (Cluster)Roles can be referenced in the RoleBindings field.
	// +optional
	Roles []PermissionsRequest `json:"roles,omitempty"`
}

type PermissionsRequest struct {
	// Name is an optional name for the (Cluster)Role that will be created for the requested permissions.
	// If not set, a randomized name that is unique in the cluster will be generated.
	// Note that the AccessRequest will not be granted if the to-be-created (Cluster)Role already exists, but is not managed by the AccessRequest, so choose this name carefully.
	// +optional
	Name string `json:"name,omitempty"`

	// Namespace is the namespace for which the permissions are requested.
	// If empty, this will result in a ClusterRole, otherwise in a Role in the respective namespace.
	// Note that for a Role, the namespace needs to either exist or a permission to create it must be included in the requested permissions (it will be created automatically then), otherwise the request will be rejected.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Rules are the requested RBAC rules.
	Rules []rbacv1.PolicyRule `json:"rules"`
}

// AccessRequestStatus defines the observed state of AccessRequest
type AccessRequestStatus struct {
	commonapi.Status `json:",inline"`

	// SecretRef holds the reference to the secret that contains the actual credentials.
	// The secret is in the same namespace as the AccessRequest.
	// +optional
	SecretRef *commonapi.LocalObjectReference `json:"secretRef,omitempty"`
}

func (ars AccessRequestStatus) IsGranted() bool {
	return ars.Phase == REQUEST_GRANTED
}

func (ars AccessRequestStatus) IsDenied() bool {
	return ars.Phase == REQUEST_DENIED
}

func (ars AccessRequestStatus) IsPending() bool {
	return ars.Phase == "" || ars.Phase == REQUEST_PENDING
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ar;areq
// +kubebuilder:metadata:labels="openmcp.cloud/cluster=platform"
// +kubebuilder:selectablefield:JSONPath=".status.phase"
// +kubebuilder:printcolumn:JSONPath=".status.phase",name="Phase",type=string

// AccessRequest is the Schema for the accessrequests API
type AccessRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AccessRequestSpec   `json:"spec,omitempty"`
	Status AccessRequestStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AccessRequestList contains a list of AccessRequest
type AccessRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AccessRequest `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AccessRequest{}, &AccessRequestList{})
}
