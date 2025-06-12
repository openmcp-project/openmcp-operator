package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec is immutable"
type ClusterRequestSpec struct {
	// Purpose is the purpose of the requested cluster.
	// +kubebuilder:validation:MinLength=1
	Purpose string `json:"purpose"`

	// Preemptive determines whether the request is preemptive.
	// Preemptive requests are used to create clusters in advance, so that they are ready when needed.
	// AccessRequests for preemptive clusters are not allowed.
	// +optional
	// +kubebuilder:default=false
	Preemptive bool `json:"preemptive,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="!has(oldSelf.cluster) || has(self.cluster)", message="cluster may not be removed once set"
type ClusterRequestStatus struct {
	CommonStatus `json:",inline"`

	// Phase is the current phase of the request.
	// +kubebuilder:default=Pending
	// +kubebuilder:validation:Enum=Pending;Granted;Denied
	Phase RequestPhase `json:"phase"`

	// Cluster is the reference to the Cluster that was returned as a result of a granted request.
	// Note that this information needs to be recoverable in case this status is lost, e.g. by adding a back reference in form of a finalizer to the Cluster resource.
	// +optional
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="cluster is immutable"
	Cluster *NamespacedObjectReference `json:"cluster,omitempty"`
}

type RequestPhase string

func (p RequestPhase) IsGranted() bool {
	return p == REQUEST_GRANTED
}

func (p RequestPhase) IsDenied() bool {
	return p == REQUEST_DENIED
}

func (p RequestPhase) IsPending() bool {
	return p == "" || p == REQUEST_PENDING
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=cr;creq
// +kubebuilder:metadata:labels="openmcp.cloud/cluster=platform"
// +kubebuilder:selectablefield:JSONPath=".spec.purpose"
// +kubebuilder:selectablefield:JSONPath=".spec.preemptive"
// +kubebuilder:selectablefield:JSONPath=".status.phase"
// +kubebuilder:printcolumn:JSONPath=".spec.purpose",name="Purpose",type=string
// +kubebuilder:printcolumn:JSONPath=".status.phase",name="Phase",type=string
// +kubebuilder:printcolumn:JSONPath=".spec.preemptive",name="Preemptive",type=string
// +kubebuilder:printcolumn:JSONPath=".status.cluster.name",name="Cluster",type=string
// +kubebuilder:printcolumn:JSONPath=".status.cluster.namespace",name="Cluster-NS",type=string

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
	Items           []ClusterRequest `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterRequest{}, &ClusterRequestList{})
}

// FinalizerForCluster returns the finalizer that is used to mark that a specific request has pointed to a specific cluster.
// Apart from preventing the Cluster's deletion, this information is used to recover the Cluster if the status of the ClusterRequest ever gets lost.
func (cr *ClusterRequest) FinalizerForCluster() string {
	prefix := RequestFinalizerOnClusterPrefix
	if cr.Spec.Preemptive {
		prefix = PreemptiveRequestFinalizerOnClusterPrefix
	}
	return prefix + string(cr.UID)
}
