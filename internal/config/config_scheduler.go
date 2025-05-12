package config

import (
	"fmt"
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/validation/field"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
)

type SchedulerConfig struct {
	// Scope determines whether the scheduler considers all clusters or only the ones in the same namespace as the ClusterRequest.
	// Defaults to "Namespaced".
	Scope SchedulerScope `json:"scope"`

	// Strategy determines how the scheduler chooses between multiple fitting clusters:
	// - Random: chooses a random cluster
	// - Simple: chooses the first cluster in the list
	// - Balanced: chooses the cluster with the least number of requests (first one in case of a tie)
	// Defaults to "Balanced".
	Strategy Strategy `json:"strategy"`

	// +optional
	// TODO: validate that all templates' labels actually match the selector
	Selector          *metav1.LabelSelector `json:"selector,omitempty"`
	ConvertedSelector labels.Selector       `json:"-"`

	PurposeMappings map[string]ClusterDefinition `json:"purposeMappings"`
}

type SchedulerScope string

const (
	SCOPE_CLUSTER    SchedulerScope = "Cluster"
	SCOPE_NAMESPACED SchedulerScope = "Namespaced"
)

type Strategy string

const (
	STRATEGY_BALANCED Strategy = "Balanced"
	STRATEGY_RANDOM   Strategy = "Random"
	STRATEGY_SIMPLE   Strategy = "Simple"
)

type ClusterDefinition struct {
	// TenancyCount determines how many ClusterRequests may point to the same Cluster.
	// Has no effect if the tenancy in the Cluster template is set to "Exclusive".
	// Must be equal to or greater than 0 otherwise, with 0 meaning "unlimited".
	TenancyCount int `json:"tenancyCount,omitempty"`

	Template ClusterTemplate `json:"template"`
}

type ClusterTemplate struct {
	metav1.ObjectMeta `json:"metadata"`
	Spec              clustersv1alpha1.ClusterSpec `json:"spec"`
}

func (c *SchedulerConfig) Default(_ *field.Path) error {
	if c.Scope == "" {
		c.Scope = SCOPE_NAMESPACED
	}
	if c.Strategy == "" {
		c.Strategy = STRATEGY_BALANCED
	}
	if c.PurposeMappings == nil {
		c.PurposeMappings = map[string]ClusterDefinition{}
	}
	return nil
}

func (c *SchedulerConfig) Validate(fldPath *field.Path) error {
	errs := field.ErrorList{}

	// validate scope and strategy
	validScopes := []string{string(SCOPE_CLUSTER), string(SCOPE_NAMESPACED)}
	if !slices.Contains(validScopes, string(c.Scope)) {
		errs = append(errs, field.NotSupported(fldPath.Child("scope"), string(c.Scope), validScopes))
	}
	validStrategies := []string{string(STRATEGY_BALANCED), string(STRATEGY_RANDOM), string(STRATEGY_SIMPLE)}
	if !slices.Contains(validStrategies, string(c.Strategy)) {
		errs = append(errs, field.NotSupported(fldPath.Child("strategy"), string(c.Strategy), validStrategies))
	}

	// validate label selector
	var ls labels.Selector
	if c.Selector != nil {
		var err error
		ls, err = metav1.LabelSelectorAsSelector(c.Selector)
		if err != nil {
			errs = append(errs, field.Invalid(fldPath.Child("selector"), c.Selector, err.Error()))
		}
	}

	// validate purpose mappings
	validTenancies := []string{string(clustersv1alpha1.TENANCY_EXCLUSIVE), string(clustersv1alpha1.TENANCY_SHARED)}
	fldPath = fldPath.Child("purposeMappings")
	for purpose, definition := range c.PurposeMappings {
		pPath := fldPath.Key(purpose)
		if purpose == "" {
			errs = append(errs, field.Invalid(pPath, purpose, "purpose must not be empty"))
		}
		if definition.TenancyCount < 0 {
			errs = append(errs, field.Invalid(pPath.Child("tenancyCount"), definition.TenancyCount, "tenancyCount must be greater than or equal to 0"))
		}
		if definition.Template.Spec.Profile == "" {
			errs = append(errs, field.Required(pPath.Child("template").Child("spec").Child("profile"), definition.Template.Spec.Profile))
		}
		if definition.Template.Spec.Tenancy == "" {
			errs = append(errs, field.Required(pPath.Child("template").Child("spec").Child("tenancy"), string(definition.Template.Spec.Tenancy)))
			continue
		} else if !slices.Contains(validTenancies, string(definition.Template.Spec.Tenancy)) {
			errs = append(errs, field.NotSupported(pPath.Child("template").Child("spec").Child("tenancy"), string(definition.Template.Spec.Tenancy), validTenancies))
			continue
		}
		if definition.Template.Spec.Tenancy == clustersv1alpha1.TENANCY_EXCLUSIVE && definition.TenancyCount != 0 {
			errs = append(errs, field.Invalid(pPath.Child("tenancyCount"), definition.TenancyCount, fmt.Sprintf("tenancyCount must be 0 if the template specifies '%s' tenancy", string(clustersv1alpha1.TENANCY_EXCLUSIVE))))
		}
		if ls != nil && !ls.Matches(labels.Set(definition.Template.Labels)) {
			errs = append(errs, field.Invalid(pPath.Child("template").Child("metadata").Child("labels"), definition.Template.Labels, "labels do not match specified selector"))
		}
	}
	return errs.ToAggregate()
}

func (c *SchedulerConfig) Complete(fldPath *field.Path) error {
	if c.Selector != nil {
		var err error
		c.ConvertedSelector, err = metav1.LabelSelectorAsSelector(c.Selector)
		if err != nil {
			return field.Invalid(fldPath.Child("selector"), c.Selector, err.Error())
		}
	}
	return nil
}
