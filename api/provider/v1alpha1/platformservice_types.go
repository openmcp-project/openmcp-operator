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

// PlatformServiceSpec defines the desired state of PlatformService.
type PlatformServiceSpec struct {
	DeploymentSpec `json:",inline"`
}

// PlatformServiceStatus defines the observed state of PlatformService.
type PlatformServiceStatus struct {
	DeploymentStatus `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:metadata:labels="openmcp.cloud/cluster=platform"
// +kubebuilder:selectablefield:JSONPath=".status.phase"
// +kubebuilder:printcolumn:JSONPath=".status.phase",name="Phase",type=string

// PlatformService is the Schema for the platformservices API.
type PlatformService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              PlatformServiceSpec   `json:"spec,omitempty"`
	Status            PlatformServiceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PlatformServiceList contains a list of PlatformService.
type PlatformServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PlatformService `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PlatformService{}, &PlatformServiceList{})
}

func PlatformServiceGKV() schema.GroupVersionKind {
	return GroupVersion.WithKind("PlatformService")
}
