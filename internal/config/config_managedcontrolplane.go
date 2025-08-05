package config

import (
	"fmt"

	"k8s.io/apimachinery/pkg/util/validation/field"

	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
	corev2alpha1 "github.com/openmcp-project/openmcp-operator/api/core/v2alpha1"
)

var _ Defaultable = &ManagedControlPlaneConfig{}
var _ Validatable = &ManagedControlPlaneConfig{}

type ManagedControlPlaneConfig struct {
	// MCPClusterPurpose is the purpose that is used for ClusterRequests created for ManagedControlPlane resources.
	MCPClusterPurpose string `json:"mcpClusterPurpose"`

	// StandardOIDCProvider is the standard OIDC provider that is enabled for all ManagedControlPlane resources, unless explicitly disabled.
	// If nil, no standard OIDC provider will be used.
	StandardOIDCProvider *commonapi.OIDCProviderConfig `json:"standardOIDCProvider,omitempty"`

	// ReconcileMCPEveryXDays specifies after how many days an MCP should be reconciled.
	// This is useful if the AccessRequests created by the MCP use an expiring authentication method and the MCP needs to refresh the access regularly.
	// A value of 0 disables the periodic reconciliation.
	// +optional
	ReconcileMCPEveryXDays int `json:"reconcileMCPEveryXDays,omitempty"`
}

func (c *ManagedControlPlaneConfig) Default(_ *field.Path) error {
	c.StandardOIDCProvider.Default()
	if c.StandardOIDCProvider.Name == "" {
		c.StandardOIDCProvider.Name = corev2alpha1.DefaultOIDCProviderName
	}
	return nil
}

func (c *ManagedControlPlaneConfig) Validate(fldPath *field.Path) error {
	errs := field.ErrorList{}

	if c.MCPClusterPurpose == "" {
		errs = append(errs, field.Required(fldPath.Child("mcpClusterPurpose"), "MCP cluster purpose must be set"))
	}
	if c.ReconcileMCPEveryXDays < 0 {
		errs = append(errs, field.Invalid(fldPath.Child("reconcileMCPEveryXDays"), c.ReconcileMCPEveryXDays, "reconcile interval must be 0 or greater"))
	}
	if c.StandardOIDCProvider == nil {
		oidcFldPath := fldPath.Child("standardOIDCProvider")
		if len(c.StandardOIDCProvider.RoleBindings) > 0 {
			errs = append(errs, field.Forbidden(oidcFldPath.Child("roleBindings"), "role bindings are specified in the MCP spec and may not be set in the config"))
		}
		if c.StandardOIDCProvider.Name != "" && c.StandardOIDCProvider.Name != corev2alpha1.DefaultOIDCProviderName {
			errs = append(errs, field.Invalid(oidcFldPath.Child("name"), c.StandardOIDCProvider.Name, fmt.Sprintf("standard OIDC provider name must be '%s' or left empty (in which case it will be defaulted)", corev2alpha1.DefaultOIDCProviderName)))
		}
	}

	return errs.ToAggregate()
}
