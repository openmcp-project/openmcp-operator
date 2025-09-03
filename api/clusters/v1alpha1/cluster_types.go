package v1alpha1

import (
	"encoding/json"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"

	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
)

// ClusterSpec defines the desired state of Cluster
type ClusterSpec struct {
	// Profile is a reference to the cluster provider.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="profile is immutable"
	Profile string `json:"profile"`

	// ClusterConfigs allows to reference any amount of provider-specific cluster configuration objects.
	// The k8s resource kind that is referenced by this depends on the provider (which is defined by the profile).
	// +optional
	ClusterConfigs []commonapi.LocalObjectReference `json:"clusterConfigs,omitempty"`

	// Kubernetes configuration for the cluster.
	Kubernetes K8sConfiguration `json:"kubernetes,omitempty"`

	// Purposes lists the purposes this cluster is intended for.
	// +kubebuilder:validation:MinItems=1
	Purposes []string `json:"purposes,omitempty"`

	// Tenancy is the tenancy model of the cluster.
	// +kubebuilder:validation:Enum=Exclusive;Shared
	Tenancy Tenancy `json:"tenancy"`
}

type K8sConfiguration struct {
	// Version is the k8s version of the cluster.
	Version string `json:"version,omitempty"`
}

// ClusterStatus defines the observed state of Cluster
type ClusterStatus struct {
	commonapi.Status `json:",inline"`

	// APIServer is the API server endpoint of the cluster.
	// +optional
	APIServer string `json:"apiServer,omitempty"`

	// ProviderStatus is the provider-specific status of the cluster.
	// x-kubernetes-preserve-unknown-fields: true
	// +optional
	ProviderStatus *runtime.RawExtension `json:"providerStatus,omitempty"`
}

const (
	// CLUSTER_PHASE_UNKNOWN represents an unknown status for the cluster.
	CLUSTER_PHASE_UNKNOWN string = "Unknown"
	// CLUSTER_PHASE_READY represents a cluster that is ready.
	CLUSTER_PHASE_READY string = "Ready"
	// CLUSTER_PHASE_NOT_READY represents a cluster that is not ready.
	CLUSTER_PHASE_NOT_READY string = "Not Ready"
	// CLUSTER_PHASE_ERROR represents a cluster that could not be reconciled successfully.
	CLUSTER_PHASE_ERROR string = "Error"
	// CLUSTER_PHASE_DELETING represents a cluster that is being deleted.
	CLUSTER_PHASE_DELETING string = "In Deletion"
	// CLUSTER_PHASE_DELETING_ERROR represents a cluster that could not be reconciled successfully while being in deletion.
	CLUSTER_PHASE_DELETING_ERROR string = "Error In Deletion"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="openmcp.cloud/cluster=platform"
// +kubebuilder:selectablefield:JSONPath=".spec.profile"
// +kubebuilder:printcolumn:JSONPath=".spec.purposes",name="Purposes",type=string
// +kubebuilder:printcolumn:JSONPath=`.status.phase`,name="Phase",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.labels.clusters\.openmcp\.cloud/k8sversion`,name="Version",type=string
// +kubebuilder:printcolumn:JSONPath=`.metadata.labels.clusters\.openmcp\.cloud/provider`,name="Provider",type=string
// +kubebuilder:printcolumn:JSONPath=".spec.profile",name="Profile",type=string,priority=10
// +kubebuilder:printcolumn:JSONPath=`.metadata.annotations.clusters\.openmcp\.cloud/providerinfo`,name="Info",type=string,priority=10
// +kubebuilder:printcolumn:JSONPath=".status.apiServer",name="APIServer",type=string,priority=10
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// Cluster is the Schema for the clusters API
type Cluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterSpec   `json:"spec,omitempty"`
	Status ClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterList contains a list of Cluster
type ClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Cluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Cluster{}, &ClusterList{})
}

// GetProviderStatus tries to unmarshal the provider status into the given variable.
func (cs *ClusterStatus) GetProviderStatus(into any) error {
	return json.Unmarshal(cs.ProviderStatus.Raw, into)
}

// SetProviderStatus marshals the given variable into the provider status.
func (cs *ClusterStatus) SetProviderStatus(from any) error {
	b, err := json.Marshal(from)
	if err != nil {
		return err
	}
	cs.ProviderStatus = &runtime.RawExtension{Raw: b}
	return nil
}

// GetTenancyCount returns the number of ClusterRequests currently pointing to this cluster.
// This is determined by counting the finalizers that have the corresponding prefix.
// Note that only unique finalizers are counted, so if there are multiple identical request finalizers
// (which should not happen), this method's return value might not match the actual number of finalizers with the prefix.
func (c *Cluster) GetTenancyCount() int {
	return c.GetRequestUIDs().Len()
}

// GetRequestUIDs returns the UIDs of all ClusterRequests that have marked this cluster with a corresponding finalizer.
func (c *Cluster) GetRequestUIDs() sets.Set[string] {
	res := sets.New[string]()
	for _, fin := range c.Finalizers {
		if uid, ok := strings.CutPrefix(fin, RequestFinalizerOnClusterPrefix); ok {
			res.Insert(uid)
		}
	}
	return res
}
