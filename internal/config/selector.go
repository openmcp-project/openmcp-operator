package config

import (
	"maps"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

type Selector struct {
	*metav1.LabelSelector `json:",inline"`
	completed             labels.Selector `json:"-"`
}

func (s *Selector) Validate(fldPath *field.Path) error {
	if s == nil || s.LabelSelector == nil {
		return nil
	}
	_, err := metav1.LabelSelectorAsSelector(s.LabelSelector)
	if err != nil {
		return field.Invalid(fldPath, s, err.Error())
	}
	return nil
}

func (s *Selector) Complete(fldPath *field.Path) error {
	if s == nil {
		return nil
	}
	if s.LabelSelector == nil {
		s.completed = labels.Everything()
		return nil
	}
	var err error
	s.completed, err = metav1.LabelSelectorAsSelector(s.LabelSelector)
	if err != nil {
		return field.Invalid(fldPath, s, err.Error())
	}
	return nil
}

// Completed returns the labels.Selector version of the selector.
// Returns a selector that matches everything if the selector is nil, empty, or has not been completed yet.
func (s *Selector) Completed() labels.Selector {
	if s == nil || s.completed == nil {
		return labels.Everything()
	}
	return s.completed
}

// Combine returns a new selector that is a combination of the two selectors.
// Neither the original nor the other selector is modified.
// For the MatchLabels field, entries from other overwrite entries from the receiver object in case of key collisions.
// Note that the requirements of both selectors are ANDed. This can lead to selectors that cannot be satisfied.
// The returned selector is completed, even if neither the receiver nor the other one was.
func (s *Selector) Combine(other *Selector) (*Selector, error) {
	if s == nil || s.LabelSelector == nil {
		if other == nil || other.LabelSelector == nil {
			return nil, nil
		}
		res := other.DeepCopy()
		var err error
		if res.completed == nil {
			err = res.Complete(nil)
		}
		return res, err
	}
	res := s.DeepCopy()
	if other == nil || other.LabelSelector == nil {
		var err error
		if res.completed == nil {
			err = res.Complete(nil)
		}
		return res, err
	}

	if len(res.MatchLabels) == 0 {
		if len(other.MatchLabels) > 0 {
			res.MatchLabels = make(map[string]string, len(other.MatchLabels))
			maps.Copy(res.MatchLabels, other.MatchLabels)
		}
	} else if len(other.MatchLabels) > 0 {
		maps.Insert(res.MatchLabels, maps.All(other.MatchLabels))
	}

	if len(res.MatchExpressions) == 0 {
		if len(other.MatchExpressions) > 0 {
			res.MatchExpressions = make([]metav1.LabelSelectorRequirement, len(other.MatchExpressions))
			copy(res.MatchExpressions, other.MatchExpressions)
		}
	} else if len(other.MatchExpressions) > 0 {
		for _, ome := range other.MatchExpressions {
			res.MatchExpressions = append(res.MatchExpressions, *ome.DeepCopy())
		}
	}

	err := res.Complete(nil)
	return res, err
}

func (s *Selector) DeepCopy() *Selector {
	if s == nil {
		return nil
	}
	return &Selector{
		LabelSelector: s.LabelSelector.DeepCopy(),
		completed:     s.completed,
	}
}
