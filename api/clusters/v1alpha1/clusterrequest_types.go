package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
)

const (
	// ClusterRequestPending is the phase if the ClusterRequest has not been scheduled yet.
	ClusterRequestPending = "Pending"
	// ClusterRequestScheduled is the phase if the ClusterRequest has been scheduled.
	ClusterRequestScheduled = "Scheduled"
)

// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec is immutable"
type ClusterRequestSpec struct {
	// Purpose is the purpose of the requested cluster.
	// +kubebuilder:validation:MinLength=1
	Purpose string `json:"purpose"`

	// WaitForClusterDeletion specifies whether the ClusterProvider should remove its finalizer from the ClusterRequest only after the corresponding Cluster has been deleted.
	// 'true' means that the finalizer stays until the Cluster is gone, 'false' means that the finalizer can be removed before the Cluster has been deleted.
	// If not specified, this defaults to 'true' if the cluster's tenancy is 'Exclusive' and to 'false' otherwise.
	// Note that the delayed finalizer removal only occurs if the deletion of the ClusterRequest actually triggers the deletion of the Cluster.
	// If the cluster is shared with further ClusterRequests using it or if it does not have the 'clusters.openmcp.cloud/delete-without-requests' label set to 'true',
	// the finalizer will be removed without waiting for the Cluster deletion, independently of this setting.
	// +optional
	WaitForClusterDeletion *bool `json:"waitForClusterDeletion,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="!has(oldSelf.cluster) || has(self.cluster)", message="cluster may not be removed once set"
type ClusterRequestStatus struct {
	commonapi.Status `json:",inline"`

	// Cluster is the reference to the Cluster that was returned as a result of a granted request.
	// Note that this information needs to be recoverable in case this status is lost, e.g. by adding a back reference in form of a finalizer to the Cluster resource.
	// +optional
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="cluster is immutable"
	Cluster *commonapi.ObjectReference `json:"cluster,omitempty"`
}

func (crs ClusterRequestStatus) IsGranted() bool {
	return crs.Phase == REQUEST_GRANTED
}

func (crs ClusterRequestStatus) IsDenied() bool {
	return crs.Phase == REQUEST_DENIED
}

func (crs ClusterRequestStatus) IsPending() bool {
	return crs.Phase == "" || crs.Phase == REQUEST_PENDING
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=cr;creq
// +kubebuilder:metadata:labels="openmcp.cloud/cluster=platform"
// +kubebuilder:selectablefield:JSONPath=".spec.purpose"
// +kubebuilder:selectablefield:JSONPath=".status.phase"
// +kubebuilder:printcolumn:JSONPath=".spec.purpose",name="Purpose",type=string
// +kubebuilder:printcolumn:JSONPath=".status.phase",name="Phase",type=string
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
	return RequestFinalizerOnClusterPrefix + string(cr.UID)
}
