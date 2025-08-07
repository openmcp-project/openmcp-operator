package v2alpha1

const (
	// DefaultOIDCProviderName is the identifier for the default OIDC provider.
	DefaultOIDCProviderName = "default"
)

const (
	MCPLabel          = GroupName + "/mcp"
	OIDCProviderLabel = GroupName + "/oidc-provider"

	MCPFinalizer = MCPLabel

	// ServiceDependencyFinalizerPrefix is the prefix for the dependency finalizers that are added to MCP resources by associated services.
	ServiceDependencyFinalizerPrefix = "services.openmcp.cloud/"
	// ClusterRequestFinalizerPrefix is the prefix for the finalizers that are added to MCP resources for cluster requests.
	ClusterRequestFinalizerPrefix = "request.clusters.openmcp.cloud/"
)

const (
	ConditionMeta = "Meta"

	ConditionClusterRequestReady       = "ClusterRequestReady"
	ConditionPrefixOIDCAccessReady     = "OIDCAccessReady:"
	ConditionAllAccessReady            = "AllAccessReady"
	ConditionAllServicesDeleted        = "AllServicesDeleted"
	ConditionAllClusterRequestsDeleted = "AllClusterRequestsDeleted"
)
