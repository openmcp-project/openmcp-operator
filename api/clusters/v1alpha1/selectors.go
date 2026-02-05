package v1alpha1

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openmcp-project/openmcp-operator/api/common"
)

// This file contains selector types and implementations for selecting Cluster and ClusterRequest objects based on their identity, purposes and labels.
// The selectors are meant to be used in controllers and other components that need to filter these objects.

// INTERFACES //

// +kubebuilder:object:generate=false
type ObjectWithPurposes interface {
	client.Object
	GetPurposes() []string
}

// +kubebuilder:object:generate=false
type Selector interface {
	// Empty checks whether the selector is empty (i.e. matches all objects).
	Empty() bool

	// Matches checks whether the given object matches the selector.
	// A nil object always matches.
	Matches(obj ObjectWithPurposes) bool

	// Validate checks whether the selector is valid.
	// Note that an invalid selector's behavior is undefined.
	Validate() error
}

// AGGREGATE TYPES //

// IdentityLabelPurposeSelector combines an identity selector, a purpose selector and a label selector.
// If the identity selector is not nil, the other selectors are ignored and only the identities are matched.
// Otherwise, an object must match both the label selector and the purpose selector to be selected.
type IdentityLabelPurposeSelector struct {
	IdentitySelector `json:",inline"`
	PurposeSelector  `json:",inline"`
	LabelSelector    `json:",inline"`
}

// IdentityLabelSelector combines an identity selector and a label selector.
// Note that the label selector is ignored if the identity selector is not nil.
type IdentityLabelSelector struct {
	IdentitySelector `json:",inline"`
	LabelSelector    `json:",inline"`
}

// IdentityPurposeSelector combines an identity selector and a purpose selector.
// Note that the purpose selector is ignored if the identity selector is not nil.
type IdentityPurposeSelector struct {
	IdentitySelector `json:",inline"`
	PurposeSelector  `json:",inline"`
}

// LabelPurposeSelector combines a label selector and a purpose selector.
// An object must match both selectors to be selected.
type LabelPurposeSelector struct {
	PurposeSelector `json:",inline"`
	LabelSelector   `json:",inline"`
}

// SIMPLE TYPES //

type IdentitySelector struct {
	// MatchIdentities contains a list of object references and matches only the objects with the given identities.
	// If this selector is nil, all objects match.
	// If this selector is not nil, but the list is empty, no objects match.
	// If not nil, all other selector fields are ignored and only the identities are matched.
	// +optional
	// +listType=atomic
	MatchIdentities []common.ObjectReference `json:"matchIdentities,omitempty"`
}

type LabelSelector metav1.LabelSelector

type PurposeSelector struct {
	// MatchPurposes contains a list of purpose selector requirements.
	// An empty or nil list matches all objects.
	// Duplicate purposes within a single requirement are ignored.
	// The requirements are ANDed.
	// +optional
	// +listType=atomic
	MatchPurposes []PurposeSelectorRequirement `json:"matchPurposes,omitempty"`
}

// A label selector requirement is a selector that contains values, a key, and an operator that
// relates the key and values.
type PurposeSelectorRequirement struct {
	// Operator represents how the list of purposes is matched against the values.
	// Valid operators are: 'ContainsAll', 'ContainsAny', 'ContainsNone', 'Equals'.
	Operator PurposeSelectorOperator `json:"operator"`
	// Values is an array of string values.
	// An empty or nil array will match no objects for 'ContainsAll' and 'ContainsAny',
	// all objects for 'ContainsNone', and only objects with no purposes for 'Equals'.
	// This array is replaced during a strategic merge patch.
	// +optional
	// +listType=atomic
	Values []string `json:"values,omitempty" protobuf:"bytes,3,rep,name=values"`
}

// PurposeSelectorOperator is the set of operators that can be used in a selector requirement.
type PurposeSelectorOperator string

const (
	// PurposeSelectorOpContainsAll matches only if the actual purpose list contains all of the values from the selector.
	PurposeSelectorOpContainsAll PurposeSelectorOperator = "ContainsAll"
	// PurposeSelectorOpContainsAny matches if the actual purpose list contains any of the values from the selector.
	PurposeSelectorOpContainsAny PurposeSelectorOperator = "ContainsAny"
	// PurposeSelectorOpContainsNone matches only if the actual purpose list contains none of the values from the selector.
	PurposeSelectorOpContainsNone PurposeSelectorOperator = "ContainsNone"
	// PurposeSelectorOpEquals matches only if the actual purpose list is exactly equal to the values from the selector (ignoring order).
	PurposeSelectorOpEquals PurposeSelectorOperator = "Equals"
)

// AGGREGATE IMPLEMENTATIONS //

var _ Selector = &IdentityLabelPurposeSelector{}

func (s *IdentityLabelPurposeSelector) Empty() bool {
	return s == nil || (s.IdentitySelector.Empty() && s.PurposeSelector.Empty() && s.LabelSelector.Empty())
}

func (s *IdentityLabelPurposeSelector) Matches(obj ObjectWithPurposes) bool {
	if s.Empty() || obj == nil {
		return true
	}
	if !s.IdentitySelector.Empty() {
		return s.IdentitySelector.Matches(obj)
	}
	return s.PurposeSelector.Matches(obj) && s.LabelSelector.Matches(obj)
}

func (s *IdentityLabelPurposeSelector) Validate() error {
	if s == nil {
		return nil
	}
	if err := s.IdentitySelector.Validate(); err != nil {
		return fmt.Errorf("identity selector invalid: %w", err)
	}
	if err := s.LabelSelector.Validate(); err != nil {
		return fmt.Errorf("label selector invalid: %w", err)
	}
	if err := s.PurposeSelector.Validate(); err != nil {
		return fmt.Errorf("purpose selector invalid: %w", err)
	}
	return nil
}

var _ Selector = &IdentityLabelSelector{}

func (s *IdentityLabelSelector) Empty() bool {
	return s == nil || (s.IdentitySelector.Empty() && s.LabelSelector.Empty())
}

func (s *IdentityLabelSelector) Matches(obj ObjectWithPurposes) bool {
	if s.Empty() || obj == nil {
		return true
	}
	if !s.IdentitySelector.Empty() {
		return s.IdentitySelector.Matches(obj)
	}
	return s.LabelSelector.Matches(obj)
}

func (s *IdentityLabelSelector) Validate() error {
	if s == nil {
		return nil
	}
	if err := s.IdentitySelector.Validate(); err != nil {
		return fmt.Errorf("identity selector invalid: %w", err)
	}
	if err := s.LabelSelector.Validate(); err != nil {
		return fmt.Errorf("label selector invalid: %w", err)
	}
	return nil
}

var _ Selector = &IdentityPurposeSelector{}

func (s *IdentityPurposeSelector) Empty() bool {
	return s == nil || (s.IdentitySelector.Empty() && s.PurposeSelector.Empty())
}

func (s *IdentityPurposeSelector) Matches(obj ObjectWithPurposes) bool {
	if s.Empty() || obj == nil {
		return true
	}
	if !s.IdentitySelector.Empty() {
		return s.IdentitySelector.Matches(obj)
	}
	return s.PurposeSelector.Matches(obj)
}

func (s *IdentityPurposeSelector) Validate() error {
	if s == nil {
		return nil
	}
	if err := s.IdentitySelector.Validate(); err != nil {
		return fmt.Errorf("identity selector invalid: %w", err)
	}
	if err := s.PurposeSelector.Validate(); err != nil {
		return fmt.Errorf("purpose selector invalid: %w", err)
	}
	return nil
}

var _ Selector = &LabelPurposeSelector{}

func (s *LabelPurposeSelector) Empty() bool {
	return s == nil || (s.LabelSelector.Empty() && s.PurposeSelector.Empty())
}

func (s *LabelPurposeSelector) Matches(obj ObjectWithPurposes) bool {
	if s.Empty() || obj == nil {
		return true
	}
	return s.LabelSelector.Matches(obj) && s.PurposeSelector.Matches(obj)
}

func (s *LabelPurposeSelector) Validate() error {
	if s == nil {
		return nil
	}
	if err := s.LabelSelector.Validate(); err != nil {
		return fmt.Errorf("label selector invalid: %w", err)
	}
	if err := s.PurposeSelector.Validate(); err != nil {
		return fmt.Errorf("purpose selector invalid: %w", err)
	}
	return nil
}

// SIMPLE IMPLEMENTATIONS //

var _ Selector = &IdentitySelector{}

func (s *IdentitySelector) Empty() bool {
	return s == nil || s.MatchIdentities == nil
}

func (s *IdentitySelector) Matches(obj ObjectWithPurposes) bool {
	if s.Empty() || obj == nil {
		return true
	}
	for _, id := range s.MatchIdentities {
		if obj.GetName() == id.Name && obj.GetNamespace() == id.Namespace {
			return true
		}
	}
	return false
}

func (s *IdentitySelector) Validate() error {
	// Nothing to validate
	return nil
}

var _ Selector = &LabelSelector{}

func (s *LabelSelector) Empty() bool {
	if s == nil {
		return true
	}
	ls, err := metav1.LabelSelectorAsSelector((*metav1.LabelSelector)(s))
	if err != nil {
		// Invalid selector is considered non-empty
		return false
	}
	return ls.Empty()
}

func (s *LabelSelector) Matches(obj ObjectWithPurposes) bool {
	if s.Empty() || obj == nil {
		return true
	}
	ls, err := metav1.LabelSelectorAsSelector((*metav1.LabelSelector)(s))
	if err != nil {
		// Invalid selector does not match anything
		return false
	}
	return ls.Matches(labels.Set(obj.GetLabels()))
}

func (s *LabelSelector) Validate() error {
	if s == nil {
		return nil
	}
	_, err := metav1.LabelSelectorAsSelector((*metav1.LabelSelector)(s))
	return err
}

var _ Selector = &PurposeSelector{}

func (s *PurposeSelector) Empty() bool {
	return s == nil || len(s.MatchPurposes) == 0
}

func (s *PurposeSelector) Matches(obj ObjectWithPurposes) bool {
	if s.Empty() || obj == nil {
		return true
	}
	purposes := sets.New(obj.GetPurposes()...)
	for _, req := range s.MatchPurposes {
		switch req.Operator {
		case PurposeSelectorOpContainsAll:
			if !purposes.HasAll(req.Values...) {
				return false
			}
		case PurposeSelectorOpContainsAny:
			if !purposes.HasAny(req.Values...) {
				return false
			}
		case PurposeSelectorOpContainsNone:
			if purposes.Intersection(sets.New(req.Values...)).Len() > 0 {
				return false
			}
		case PurposeSelectorOpEquals:
			if purposes.Len() != len(req.Values) || !purposes.HasAll(req.Values...) {
				return false
			}
		default:
			// Invalid operator does not match anything
			return false
		}
	}
	return true
}

func (s *PurposeSelector) Validate() error {
	if s == nil {
		return nil
	}
	for _, req := range s.MatchPurposes {
		switch req.Operator {
		case PurposeSelectorOpContainsAll, PurposeSelectorOpContainsAny, PurposeSelectorOpContainsNone, PurposeSelectorOpEquals:
			// valid operator
		default:
			return fmt.Errorf("invalid purpose selector operator: %s", req.Operator)
		}
	}
	return nil
}
