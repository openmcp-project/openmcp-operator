package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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

// CommonStatus is a status shared by multiple resources.
// It uses standard Kubernetes conditions for status representation.
type CommonStatus struct {
	// ObservedGeneration is the generation of this resource that was last reconciled by the controller.
	ObservedGeneration int64 `json:"observedGeneration"`

	// LastReconcileTime is the time when the resource was last reconciled by the controller.
	LastReconcileTime metav1.Time `json:"lastReconcileTime"`

	// Reason is expected to contain a CamelCased string that provides further information in a machine-readable format.
	// +optional
	Reason string `json:"reason,omitempty"`

	// Message contains further details in a human-readable format.
	// +optional
	Message string `json:"message,omitempty"`

	// Conditions contains the conditions of this resource using the standard Kubernetes condition format.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// GetCondition returns the condition with the provided type.
// Returns nil if the condition is not found.
func (s *CommonStatus) GetCondition(conditionType string) *metav1.Condition {
	return meta.FindStatusCondition(s.Conditions, conditionType)
}

// SetCondition sets the status condition. It either overwrites the existing condition or
// creates a new one if the condition wasn't present.
func (s *CommonStatus) SetCondition(condition metav1.Condition) {
	meta.SetStatusCondition(&s.Conditions, condition)
}

// RemoveCondition removes the condition with the specified type.
func (s *CommonStatus) RemoveCondition(conditionType string) {
	meta.RemoveStatusCondition(&s.Conditions, conditionType)
}

// IsReady returns true if the Ready condition is present and set to True.
func (s *CommonStatus) IsReady() bool {
	condition := s.GetCondition(ReadyConditionType)
	return condition != nil && condition.Status == metav1.ConditionTrue
}

// IsAvailable returns true if the Available condition is present and set to True.
func (s *CommonStatus) IsAvailable() bool {
	condition := s.GetCondition(AvailableConditionType)
	return condition != nil && condition.Status == metav1.ConditionTrue
}

// HasReconcileSucceeded returns true if the ReconcileSuccess condition is present and set to True.
func (s *CommonStatus) HasReconcileSucceeded() bool {
	condition := s.GetCondition(ReconcileSuccessConditionType)
	return condition != nil && condition.Status == metav1.ConditionTrue
}
