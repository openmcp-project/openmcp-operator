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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openmcp-project/openmcp-operator/api/common"
)

// DeploymentSpec defines the desired state of a provider.
type DeploymentSpec struct {
	// Image is the name of the image of a provider.
	// +kubebuilder:validation:MinLength=1
	Image string `json:"image"`

	// ImagePullSecrets are secrets in the same namespace.
	// They can be used to fetch provider images from private registries.
	ImagePullSecrets []common.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// InitCommand is the command that is executed to run the init job of the provider.
	// Defaults to ["init"], if not specified.
	// The '--environment', '--verbosity', and '--provider-name' flags will be appended to the command automatically.
	// +optional
	InitCommand []string `json:"initCommand,omitempty"`

	// RunCommand is the command that is executed to run the provider controllers.
	// Defaults to ["run"], if not specified.
	// The '--environment', '--verbosity', and '--provider-name' flags will be appended to the command automatically.
	// +optional
	RunCommand []string `json:"runCommand,omitempty"`

	// Env is a list of environment variables to set in the containers of the init job and deployment of the provider.
	// +optional
	// +patchMergeKey=name
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=name
	Env []EnvVar `json:"env,omitempty" patchStrategy:"merge" patchMergeKey:"name"`

	// Verbosity is the verbosity level of the provider.
	// +kubebuilder:validation:Enum=DEBUG;INFO;ERROR;debug;info;error
	// +kubebuilder:default=INFO
	Verbosity string `json:"verbosity,omitempty"`

	// List of additional volumes that are mounted in the main container.
	// More info: https://kubernetes.io/docs/concepts/storage/volumes
	// +optional
	// +patchMergeKey=name
	// +patchStrategy=merge,retainKeys
	// +listType=map
	// +listMapKey=name
	ExtraVolumes []corev1.Volume `json:"extraVolumes,omitempty" patchStrategy:"merge,retainKeys" patchMergeKey:"name"`

	// List of additional volume mounts that are mounted in the main container.
	// Cannot be updated.
	// +optional
	// +patchMergeKey=mountPath
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=mountPath
	ExtraVolumeMounts []corev1.VolumeMount `json:"extraVolumeMounts,omitempty" patchStrategy:"merge" patchMergeKey:"mountPath"`

	// RunReplicas is the number of replicas for the provider controller.
	// Defaults to 1.
	// If greater thant 1, automatically sets the `--leader-elect=true` flag in the RunCommand.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=1
	// +optional
	RunReplicas int32 `json:"runReplicas,omitempty"`

	// TopologySpreadConstraints describes how to spread the provider pods
	// across your cluster among failure-domains such as zones, nodes, regions, etc.
	// More info: https://kubernetes.io/docs/concepts/workloads/pods/pod-topology-spread-constraints/
	// The label selector for the topology spread constraints is automatically set to match the provider deployment pods.
	// +optional
	// +patchMergeKey=topologyKey
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=topologyKey
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty" patchStrategy:"merge" patchMergeKey:"topologyKey"`
}

type WebhookConfiguration struct {
	// Enabled indicates whether the webhook is enabled.
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`
}

// DeploymentStatus defines the observed state of a provider.
type DeploymentStatus struct {
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration is the last observed generation.
	ObservedGeneration int64 `json:"observedGeneration"`

	Phase string `json:"phase,omitempty"`
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
