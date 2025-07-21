package common

import (
	corev1 "k8s.io/api/core/v1"
	apimachinery "k8s.io/apimachinery/pkg/types"
)

// ClusterScoped can be passed into a LocalObjectReference's NamespacedName method to indicate that the object is cluster-scoped.
const ClusterScoped = ""

// ObjectReference is a reference to an object in any namespace.
type ObjectReference struct {
	// Name is the name of the object.
	Name string `json:"name"`
	// Namespace is the namespace of the object.
	Namespace string `json:"namespace"`
}

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

// NamespacedName returns the NamespacedName of the ObjectReference.
// This is a convenience method to convert the ObjectReference to a NamespacedName, which can be passed into k8s client methods.
func (r *ObjectReference) NamespacedName() apimachinery.NamespacedName {
	return apimachinery.NamespacedName{
		Name:      r.Name,
		Namespace: r.Namespace,
	}
}

// NamespacedName returns the NamespacedName of the LocalObjectReference.
// This is a convenience method to convert the LocalObjectReference to a NamespacedName, which can be passed into k8s client methods.
// Since LocalObjectReference refers to an object in the same namespace as the resource referencing it,
// which is only known from context, this method requires a namespace parameter.
// An empty string (or the ClusterScoped constant) can be used to indicate that the object is cluster-scoped.
func (r *LocalObjectReference) NamespacedName(namespace string) apimachinery.NamespacedName {
	return apimachinery.NamespacedName{
		Name:      r.Name,
		Namespace: namespace,
	}
}
