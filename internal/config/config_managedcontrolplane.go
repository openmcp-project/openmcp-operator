package config

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/ptr"

	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
	corev2alpha1 "github.com/openmcp-project/openmcp-operator/api/core/v2alpha1"
)

var _ Defaultable = &ManagedControlPlaneConfig{}
var _ Validatable = &ManagedControlPlaneConfig{}

type ManagedControlPlaneConfig struct {
	// MCPClusterPurpose is the purpose that is used for ClusterRequests created for ManagedControlPlane resources.
	MCPClusterPurpose string `json:"mcpClusterPurpose"`

	// DefaultOIDCProvider is the standard OIDC provider that is enabled for all ManagedControlPlane resources, unless explicitly disabled.
	// If nil, no standard OIDC provider will be used.
	DefaultOIDCProvider *commonapi.OIDCProviderConfig `json:"defaultOIDCProvider,omitempty"`

	// ReconcileMCPEveryXDays specifies after how many days an MCP should be reconciled.
	// This is useful if the AccessRequests created by the MCP use an expiring authentication method and the MCP needs to refresh the access regularly.
	// A value of 0 disables the periodic reconciliation.
	// +optional
	ReconcileMCPEveryXDays int `json:"reconcileMCPEveryXDays,omitempty"`

	// PlatformService specifies the configuration for the ManagedControlPlane platform service.
	PlatformService ManagedControlPlaneConfigPlatformService `json:"platformService,omitempty"`
}
type ManagedControlPlaneConfigPlatformService struct {
	// Replicas specifies the default number of replicas for ManagedControlPlane platform service.
	// Default is 1.
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// TopologySpreadConstraints specifies the default topology spread constraints for ManagedControlPlane platform service.
	// More info: https://kubernetes.io/docs/concepts/workloads/pods/pod-topology-spread-constraints/
	// The label selector for the topology spread constraints is set to match the labels of the ManagedControlPlane platform service.
	// +optional
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`
}

func (c *ManagedControlPlaneConfig) Default(_ *field.Path) error {
	if c.DefaultOIDCProvider != nil {
		c.DefaultOIDCProvider.Default()
		if c.DefaultOIDCProvider.Name == "" {
			c.DefaultOIDCProvider.Name = corev2alpha1.DefaultOIDCProviderName
		}
	}
	if c.MCPClusterPurpose == "" {
		c.MCPClusterPurpose = corev2alpha1.DefaultMCPClusterPurpose
	}
	if c.PlatformService.Replicas == nil {
		c.PlatformService.Replicas = ptr.To[int32](1)
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
	if c.DefaultOIDCProvider != nil {
		oidcFldPath := fldPath.Child("defaultOIDCProvider")
		if len(c.DefaultOIDCProvider.RoleBindings) > 0 {
			errs = append(errs, field.Forbidden(oidcFldPath.Child("roleBindings"), "role bindings are specified in the MCP spec and may not be set in the config"))
		}
		if c.DefaultOIDCProvider.Name == "system" {
			errs = append(errs, field.Invalid(oidcFldPath.Child("name"), c.DefaultOIDCProvider.Name, "'system' is a reserved string and may not be used as name for the default OIDC provider"))
		}
	}
	if c.PlatformService.Replicas != nil && *c.PlatformService.Replicas < 1 {
		errs = append(errs, field.Invalid(fldPath.Child("replicas"), c.PlatformService.Replicas, "replicas must be at least 1"))
	}

	return errs.ToAggregate()
}
