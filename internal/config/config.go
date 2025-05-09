package config

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"dario.cat/mergo"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/yaml"
)

type Config struct {
	// add new configs here
	// During initialization, the config will be loaded from one or more YAML files and merged together.
	// If a field's value has a 'Default() error' method, it will be called to set default values.
	// If a field's value has a 'Validate() error' method, it will be called to validate the config.
	// If a field's value has a 'Complete() error' method, it will be called to complete the config.
	// These methods will be called in the order Default -> Validate -> Complete.
	// The config printed during startup, therefore its fields should contain json markers.
}

func (c *Config) Dump(out io.Writer) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("error marshalling config: %w", err)
	}
	if _, err := out.Write(data); err != nil {
		return fmt.Errorf("error writing config: %w", err)
	}
	return nil
}

// LoadFromBytes builds the config from multiple raw YAML byte slices.
// It merges the configs together, with later configs overriding earlier ones.
func LoadFromBytes(rawConfigs ...[]byte) (*Config, error) {
	var res *Config
	for i, data := range rawConfigs {
		tmp := &Config{}
		if err := yaml.Unmarshal(data, &tmp); err != nil {
			return nil, fmt.Errorf("error unmarshalling raw config at index %d: %w", i, err)
		}
		if res == nil {
			// shortcut to avoid merging if only one config is specified
			res = tmp
			continue
		}
		if err := mergo.Merge(res, tmp, mergo.WithOverride); err != nil {
			return nil, fmt.Errorf("error merging raw config at index %d: %w", i, err)
		}
	}
	return res, nil
}

// LoadFromFiles builds the config from multiple YAML files.
func LoadFromFiles(paths ...string) (*Config, error) {
	rawConfigs := [][]byte{}
	for i, p := range paths {
		fi, err := os.Stat(p)
		if err != nil {
			return nil, fmt.Errorf("error getting file info for path '%s': %w", p, err)
		}
		if fi.IsDir() {
			if err := filepath.WalkDir(p, func(sp string, d fs.DirEntry, err error) error {
				if err != nil {
					return fmt.Errorf("error for path '%s': %w", sp, err)
				}
				if d.IsDir() {
					// skip nested directories
					return nil
				}
				if !strings.HasSuffix(sp, ".yaml") && !strings.HasSuffix(sp, ".yml") && !strings.HasSuffix(sp, ".json") {
					// skip non-YAML files
					return nil
				}
				data, err := os.ReadFile(sp)
				if err != nil {
					return fmt.Errorf("error reading file '%s': %w", sp, err)
				}
				rawConfigs = append(rawConfigs, data)
				return nil
			}); err != nil {
				return nil, fmt.Errorf("error walking directory '%s' at index %d: %w", p, i, err)
			}
		} else {
			data, err := os.ReadFile(p)
			if err != nil {
				return nil, fmt.Errorf("error reading file '%s' at index %d: %w", p, i, err)
			}
			rawConfigs = append(rawConfigs, data)
		}
	}
	return LoadFromBytes(rawConfigs...)
}

type Defaultable interface {
	// Default sets default values for the configuration.
	// It is expected to modify the configuration in place.
	// The fieldPath parameter can be used to create error messages.
	Default(fldPath *field.Path) error
}

type Validatable interface {
	// Validate validates the configuration.
	// It should return an error if the configuration is invalid.
	// It is not supposed to modify the configuration in any way.
	// The fieldPath parameter can be used to create error messages.
	Validate(fldPath *field.Path) error
}

type Completable interface {
	// Complete performs any required transformations on the configuration,
	// e.g. filling a map[string]X field from an []X field with keys.
	// It is expected to modify the configuration in place.
	// The fieldPath parameter can be used to create error messages.
	Complete(fldPath *field.Path) error
}

// Default defaults the config by calling the Default() method on each field that implements the Defaultable interface.
func (c *Config) Default() error {
	return c.perform(defaulting)
}

// Validate validates the config by calling the Validate() method on each field that implements the Validatable interface.
func (c *Config) Validate() error {
	return c.perform(validation)
}

// Complete completes the config by calling the Complete() method on each field that implements the Completable interface.
func (c *Config) Complete() error {
	return c.perform(completion)
}

const (
	defaulting actionType = "defaulting"
	validation actionType = "validation"
	completion actionType = "completion"
)

type actionType string

// perform is a helper function to avoid code duplication in Default, Validate and Complete.
// It performs the action specified by the actionType parameter on each field of the config struct.
// It uses reflection to find the fields and their types, and calls the appropriate method on each field.
func (c *Config) perform(action actionType) error {
	v := reflect.ValueOf(c)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	var intfType reflect.Type
	var funcName string
	switch action {
	case defaulting:
		intfType = reflect.TypeOf((*Defaultable)(nil)).Elem()
		funcName = "Default"
	case validation:
		intfType = reflect.TypeOf((*Validatable)(nil)).Elem()
		funcName = "Validate"
	case completion:
		intfType = reflect.TypeOf((*Completable)(nil)).Elem()
		funcName = "Complete"
	default:
		return fmt.Errorf("internal error: unknown action '%s'", action)
	}
	var errs error
	for i := range v.NumField() {
		sField := v.Field(i)
		if (sField.Kind() == reflect.Slice || sField.Kind() == reflect.Map || sField.Kind() == reflect.Pointer) && sField.IsNil() {
			// skip fields with nil values
			continue
		}
		if sField.CanInterface() {
			var method reflect.Value
			if sField.Type().Implements(intfType) {
				method = sField.MethodByName(funcName)
			} else if sField.CanAddr() && sField.Addr().Type().Implements(intfType) {
				method = sField.Addr().MethodByName(funcName)
			}
			if method.IsValid() {
				fieldMeta := v.Type().Field(i)
				fieldName := fieldMeta.Name
				jsonFieldName := strings.SplitN(fieldMeta.Tag.Get("json"), ",", 2)[0]
				if jsonFieldName != "" {
					fieldName = jsonFieldName
				}
				var err error
				errVal := (method.Call([]reflect.Value{reflect.ValueOf(field.NewPath(fieldName))})[0])
				if errVal.IsValid() && !errVal.IsNil() {
					err = errVal.Interface().(error)
				}
				if err != nil {
					errs = errors.Join(errs, fmt.Errorf("%s failed for %s: %w", string(action), v.Type().Field(i).Name, err))
				}
			}
		}
	}
	return errs
}
