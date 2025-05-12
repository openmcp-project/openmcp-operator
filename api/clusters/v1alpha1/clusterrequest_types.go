package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec is immutable"
type ClusterRequestSpec struct {
	// Purpose is the purpose of the requested cluster.
	// +kubebuilder:validation:MinLength=1
	Purpose string `json:"purpose"`
}

// +kubebuilder:validation:XValidation:rule="!has(oldSelf.clusterRef) || has(self.clusterRef)", message="clusterRef may not be removed once set"
type ClusterRequestStatus struct {
	CommonStatus `json:",inline"`

	// Phase is the current phase of the request.
	Phase RequestPhase `json:"phase"`

	// ClusterRef is the reference to the Cluster that was returned as a result of a granted request.
	// Note that this information needs to be recoverable in case this status is lost, e.g. by adding a back reference in form of a finalizer to the Cluster resource.
	// +optional
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="clusterRef is immutable"
	ClusterRef *NamespacedObjectReference `json:"clusterRef,omitempty"`
}

type RequestPhase string

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="openmcp.cloud/cluster=onboarding"
// +kubebuilder:selectablefield:JSONPath=".spec.purpose"
// +kubebuilder:selectablefield:JSONPath=".status.phase"
// +kubebuilder:printcolumn:JSONPath=".spec.purpose",name="Purpose",type=string
// +kubebuilder:printcolumn:JSONPath=".status.phase",name="Phase",type=string

// ClusterRequest is the Schema for the clusters API
type ClusterRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterRequestSpec   `json:"spec,omitempty"`
	Status ClusterRequestStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterRequestList contains a list of Cluster
type ClusterRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Cluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterRequest{}, &ClusterRequestList{})
}

// FinalizerForCluster returns the finalizer that is used to mark that a specific request has pointed to a specific cluster.
// Apart from preventing the Cluster's deletion, this information is used to recover the Cluster if the status of the ClusterRequest ever gets lost.
func (cr *ClusterRequest) FinalizerForCluster() string {
	return RequestFinalizerOnClusterPrefix + string(cr.UID)
}
