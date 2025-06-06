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

// DeploymentSpec defines the desired state of a provider.
type DeploymentSpec struct {
	// Image is the name of the image of a provider.
	// +kubebuilder:validation:MinLength=1
	Image string `json:"image"`

	// ImagePullSecrets are secrets in the same namespace.
	// They can be used to fetch provider images from private registries.
	ImagePullSecrets []ObjectReference `json:"imagePullSecrets,omitempty"`

	// Env is a list of environment variables to set in the containers of the init job and deployment of the provider.
	// +optional
	// +patchMergeKey=name
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=name
	Env []EnvVar `json:"env,omitempty" patchStrategy:"merge" patchMergeKey:"name"`

	// Verbosity is the verbosity level of the provider.
	// +kubebuilder:validation:Enum=DEBUG;INFO;ERROR
	// +kubebuilder:default=INFO
	Verbosity string `json:"verbosity,omitempty"`
}

// DeploymentStatus defines the observed state of a provider.
type DeploymentStatus struct {
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration is the last observed generation.
	ObservedGeneration int64 `json:"observedGeneration"`

	Phase string `json:"phase,omitempty"`
}

type ObjectReference struct {
	// Name is the name of the referenced resource.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// EnvVar represents an environment variable present in a Container.
type EnvVar struct {
	// Name is the name of the environment variable.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// Value is the value of the environment variable.
	// +optional
	Value string `json:"value,omitempty"`
}
