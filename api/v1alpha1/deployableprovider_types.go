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
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// DeployableProviderSpec defines the desired state of DeployableProvider.
type DeployableProviderSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	DeploymentSpec DeployableSpec `json:"deploymentSpec,omitempty"`

	// Foo is an example field of DeployableProvider. Edit deployableprovider_types.go to remove/update
	Foo string `json:"foo,omitempty"`
}

// DeployableProviderStatus defines the observed state of DeployableProvider.
type DeployableProviderStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	DeploymentStatus DeployableStatus `json:"deploymentStatus,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// DeployableProvider is the Schema for the deployableproviders API.
type DeployableProvider struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DeployableProviderSpec   `json:"spec,omitempty"`
	Status DeployableProviderStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DeployableProviderList contains a list of DeployableProvider.
type DeployableProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DeployableProvider `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DeployableProvider{}, &DeployableProviderList{})
}
