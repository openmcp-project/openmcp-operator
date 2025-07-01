package v1alpha1

import (
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:validation:XValidation:rule="!has(oldSelf.clusterRef) || has(self.clusterRef)", message="clusterRef may not be removed once set"
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.requestRef) || has(self.requestRef)", message="requestRef may not be removed once set"
type AccessRequestSpec struct {
	// ClusterRef is the reference to the Cluster for which access is requested.
	// If set, requestRef will be ignored.
	// This value is immutable.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="clusterRef is immutable"
	// +optional
	ClusterRef *NamespacedObjectReference `json:"clusterRef,omitempty"`

	// RequestRef is the reference to the ClusterRequest for whose Cluster access is requested.
	// Is ignored if clusterRef is set.
	// This value is immutable.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="requestRef is immutable"
	// +optional
	RequestRef *NamespacedObjectReference `json:"requestRef,omitempty"`

	// Permissions are the requested permissions.
	Permissions []PermissionsRequest `json:"permissions"`
}

type PermissionsRequest struct {
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
	// ObservedGeneration is the generation of this resource that was last reconciled by the controller.
	ObservedGeneration int64 `json:"observedGeneration"`

	// LastReconcileTime is the time when the resource was last reconciled by the controller.
	LastReconcileTime metav1.Time `json:"lastReconcileTime"`

	// Reason is expected to contain a CamelCased string that provides further information in a machine-readable format.
	// +optional
	Reason string `json:"reason,omitempty"`

	// Message contains further details in a human-readable format.
	// +optional
	Message string `json:"message,omitempty"`

	// Conditions contains the conditions of this resource using the standard Kubernetes condition format.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Phase is the current phase of the request.
	// +kubebuilder:default=Pending
	// +kubebuilder:validation:Enum=Pending;Granted;Denied
	Phase RequestPhase `json:"phase"`

	// SecretRef holds the reference to the secret that contains the actual credentials.
	SecretRef *NamespacedObjectReference `json:"secretRef,omitempty"`
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
