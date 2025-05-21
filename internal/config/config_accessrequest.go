package config

import (
	"fmt"

	"k8s.io/apimachinery/pkg/util/validation/field"
)

type AccessRequestConfig struct {
	// If set, only AccessRequests that match the selector will be reconciled.
	Selector *Selector `json:"selector,omitempty"`
}

func (c *AccessRequestConfig) Validate(fldPath *field.Path) error {
	return c.Selector.Validate(fldPath.Child("selector"))
}

func (c *AccessRequestConfig) Complete(fldPath *field.Path) error {
	if err := c.Selector.Complete(fldPath.Child("selector")); err != nil {
		return fmt.Errorf("error completing selector: %w", err)
	}

	return nil
}
