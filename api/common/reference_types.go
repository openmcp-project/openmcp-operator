package common

import (
	corev1 "k8s.io/api/core/v1"
	apimachinery "k8s.io/apimachinery/pkg/types"
)

// ObjectReference is a reference to an object in any namespace.
type ObjectReference apimachinery.NamespacedName

// LocalObjectReference is a reference to an object in the same namespace as the resource referencing it.
type LocalObjectReference corev1.LocalObjectReference

// SecretReference is a reference to a secret in any namespace with a key.
type SecretReference struct {
	ObjectReference `json:",inline"`
	// Key is the key in the secret to use.
	Key string `json:"key"`
}

// LocalSecretReference is a reference to a secret in the same namespace as the resource referencing it with a key.
type LocalSecretReference struct {
	LocalObjectReference `json:",inline"`
	// Key is the key in the secret to use.
	Key string `json:"key"`
}
