package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type PreemptiveClusterRequestSpec struct {
	// Purpose is the purpose of the requested cluster.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="purpose is immutable"
	// +kubebuilder:validation:MinLength=1
	Purpose string `json:"purpose"`

	// Workload specifies for how many workloads this preemptive cluster request should account.
	// Must be greater than 0.
	// +kubebuilder:validation:Minimum=1
	Workload int `json:"workload"`
}

type PreemptiveClusterRequestStatus struct {
	CommonStatus `json:",inline"`

	// Phase is the current phase of the request.
	// +kubebuilder:default=Pending
	// +kubebuilder:validation:Enum=Pending;Granted;Denied
	Phase RequestPhase `json:"phase"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=pcr;pcreq
// +kubebuilder:metadata:labels="openmcp.cloud/cluster=platform"
// +kubebuilder:selectablefield:JSONPath=".spec.purpose"
// +kubebuilder:selectablefield:JSONPath=".status.phase"
// +kubebuilder:printcolumn:JSONPath=".spec.purpose",name="Purpose",type=string
// +kubebuilder:printcolumn:JSONPath=".spec.workload",name="Workload",type=string
// +kubebuilder:printcolumn:JSONPath=".status.phase",name="Phase",type=string

type PreemptiveClusterRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PreemptiveClusterRequestSpec   `json:"spec,omitempty"`
	Status PreemptiveClusterRequestStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PreemptiveClusterRequestList contains a list of Cluster
type PreemptiveClusterRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PreemptiveClusterRequest `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PreemptiveClusterRequest{}, &PreemptiveClusterRequestList{})
}

// FinalizerForCluster returns the finalizer that is used to mark that a specific request has pointed to a specific cluster.
// Apart from preventing the Cluster's deletion, this information is used to recover the Cluster if the status of the PreemptiveClusterRequest ever gets lost.
func (cr *PreemptiveClusterRequest) FinalizerForCluster() string {
	return PreemptiveRequestFinalizerOnClusterPrefix + string(cr.UID)
}
