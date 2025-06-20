package v1alpha1

// These are standard condition types that can be used across resources
const (
	// ReadyConditionType indicates whether the resource is ready
	ReadyConditionType = "Ready"
	// AvailableConditionType indicates whether the resource is available
	AvailableConditionType = "Available"
	// ReconcileSuccessConditionType indicates whether the last reconciliation was successful
	ReconcileSuccessConditionType = "ReconcileSuccess"
)

// These are standard condition reasons that can be used across resources
const (
	// ReconcileSuccessReason indicates that the reconciliation was successful
	ReconcileSuccessReason = "ReconcileSuccess"
	// ReconcileErrorReason indicates that there was an error during reconciliation
	ReconcileErrorReason = "ReconcileError"
	// ResourceAvailableReason indicates that the resource is available
	ResourceAvailableReason = "ResourceAvailable"
	// ResourceUnavailableReason indicates that the resource is not available
	ResourceUnavailableReason = "ResourceUnavailable"
)

type ObjectReference struct {
	// Name is the name of the referenced resource.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

type NamespacedObjectReference struct {
	ObjectReference `json:",inline"`

	// Namespace is the namespace of the referenced resource.
	Namespace string `json:"namespace"`
}
