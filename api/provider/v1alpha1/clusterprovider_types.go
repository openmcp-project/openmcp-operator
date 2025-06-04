/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ClusterProviderSpec defines the desired state of ClusterProvider.
type ClusterProviderSpec struct {
	DeploymentSpec `json:",inline"`
}

// ClusterProviderStatus defines the observed state of ClusterProvider.
type ClusterProviderStatus struct {
	DeploymentStatus `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:metadata:labels="openmcp.cloud/cluster=platform"
// +kubebuilder:selectablefield:JSONPath=".status.phase"
// +kubebuilder:printcolumn:JSONPath=".status.phase",name="Phase",type=string

// ClusterProvider is the Schema for the clusterproviders API.
type ClusterProvider struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ClusterProviderSpec   `json:"spec,omitempty"`
	Status            ClusterProviderStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterProviderList contains a list of ClusterProvider.
type ClusterProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterProvider `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterProvider{}, &ClusterProviderList{})
}

func ClusterProviderGKV() schema.GroupVersionKind {
	return GroupVersion.WithKind("ClusterProvider")
}
