package v2alpha1

const (
	// DefaultOIDCProviderName is the identifier for the default OIDC provider.
	DefaultOIDCProviderName = "default"
	// DefaultMCPClusterPurpose is the default purpose for ManagedControlPlane clusters.
	DefaultMCPClusterPurpose = "mcp"
)

const (
	MCPNameLabel            = GroupName + "/mcp-name"
	MCPNamespaceLabel       = GroupName + "/mcp-namespace"
	OIDCProviderLabel       = GroupName + "/oidc-provider"
	MCPPurposeOverrideLabel = GroupName + "/purpose-override"

	// ManagedPurposeMCPPurposeOverride is used as value for the managed purpose label. It must not be modified.
	ManagedPurposeMCPPurposeOverride = "mcp-purpose-override"

	MCPFinalizer = GroupName + "/mcp"

	// ServiceDependencyFinalizerPrefix is the prefix for the dependency finalizers that are added to MCP resources by associated services.
	ServiceDependencyFinalizerPrefix = "services.openmcp.cloud/"
	// ClusterRequestFinalizerPrefix is the prefix for the finalizers that are added to MCP resources for cluster requests.
	ClusterRequestFinalizerPrefix = "request.clusters.openmcp.cloud/"
)

const (
	ConditionMeta = "Meta"

	ConditionClusterRequestReady       = "ClusterRequestReady"
	ConditionPrefixOIDCAccessReady     = "OIDCAccessReady."
	ConditionAllAccessReady            = "AllAccessReady"
	ConditionAllServicesDeleted        = "AllServicesDeleted"
	ConditionAllClusterRequestsDeleted = "AllClusterRequestsDeleted"
)
