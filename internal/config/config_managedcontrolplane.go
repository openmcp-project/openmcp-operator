package config

import (
	"k8s.io/apimachinery/pkg/util/validation/field"

	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
)

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
	return nil
}

func (c *ManagedControlPlaneConfig) Validate(fldPath *field.Path) error {
	errs := field.ErrorList{}

	if c.MCPClusterPurpose == "" {
		errs = append(errs, field.Required(fldPath.Child("mcpClusterPurpose"), "MCP cluster purpose must be set"))
	}

	return errs.ToAggregate()
}
