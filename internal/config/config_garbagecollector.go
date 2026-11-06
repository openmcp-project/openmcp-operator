package config

import (
	"time"

	"k8s.io/apimachinery/pkg/util/validation/field"
)

const (
	DefaultGarbageCollectionInterval = 24 * time.Hour
)

var _ Defaultable = &GarbageCollectorConfig{}
var _ Validatable = &GarbageCollectorConfig{}
var _ Completable = &GarbageCollectorConfig{}

type GarbageCollectorConfig struct {
	// Interval applies unless overwritten for specific garbage collector modules.
	// Defaults to 24h if not specified.
	// optional
	Interval time.Duration `json:"interval,omitempty"`

	// AccessRequestNoCluster contains the configuration for the AccessRequestNoCluster garbage collector module.
	// optional
	AccessRequestNoCluster AccessRequestNoClusterConfig `json:"accessRequestNoCluster"`
}

type AccessRequestNoClusterConfig struct {
	// Interval can be used to override the default garbage collection interval for this specific module.
	// optional
	Interval time.Duration `json:"interval,omitempty"`
	// Enabled specifies whether this specific garbage collector module is enabled.
	// optional
	Enabled bool `json:"enabled"`
}

func (c *GarbageCollectorConfig) Default(_ *field.Path) error {
	if c.Interval == 0 {
		c.Interval = DefaultGarbageCollectionInterval
	}

	return nil
}

func (c *GarbageCollectorConfig) Validate(fldPath *field.Path) error {
	errs := field.ErrorList{}

	// nothing to do yet

	return errs.ToAggregate()
}

func (c *GarbageCollectorConfig) Complete(fldPath *field.Path) error {
	return nil
}
