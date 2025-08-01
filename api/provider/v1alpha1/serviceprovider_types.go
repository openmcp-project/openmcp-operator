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

// ServiceProviderSpec defines the desired state of ServiceProvider.
type ServiceProviderSpec struct {
	DeploymentSpec `json:",inline"`
}

// ServiceProviderStatus defines the observed state of ServiceProvider.
type ServiceProviderStatus struct {
	DeploymentStatus `json:",inline"`

	// Resources is a list of Group/Version/Kind that specifies which service resources this provider exposes.
	Resources []metav1.GroupVersionKind `json:"resources,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:metadata:labels="openmcp.cloud/cluster=platform"
// +kubebuilder:selectablefield:JSONPath=".status.phase"
// +kubebuilder:printcolumn:JSONPath=".status.phase",name="Phase",type=string

// ServiceProvider is the Schema for the serviceproviders API.
type ServiceProvider struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ServiceProviderSpec   `json:"spec,omitempty"`
	Status            ServiceProviderStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ServiceProviderList contains a list of ServiceProvider.
type ServiceProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ServiceProvider `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ServiceProvider{}, &ServiceProviderList{})
}

func ServiceProviderGKV() schema.GroupVersionKind {
	return GroupVersion.WithKind("ServiceProvider")
}
