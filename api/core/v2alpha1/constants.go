package v2alpha1

const (
	// DefaultOIDCProviderName is the identifier for the default OIDC provider.
	DefaultOIDCProviderName = "openmcp"
	// DefaultMCPClusterPurpose is the default purpose for ManagedControlPlane clusters.
	DefaultMCPClusterPurpose = "mcp"
)

const (
	MCPNameLabel            = GroupName + "/mcp-name"
	MCPNamespaceLabel       = GroupName + "/mcp-namespace"
	OIDCProviderLabel       = GroupName + "/oidc-provider"
	TokenProviderLabel      = GroupName + "/token-provider"
	MCPPurposeOverrideLabel = GroupName + "/purpose"

	// ManagedPurposeMCPPurposeOverride is used as value for the managed purpose label. It must not be modified.
	ManagedPurposeMCPPurposeOverride = "mcp-purpose-override"
	// ManagedPurposeOIDCProviderNameUniqueness is used as value for the managed purpose label. It must not be modified.
	ManagedPurposeOIDCProviderNameUniqueness = "oidc-provider-name-uniqueness"

	MCPFinalizer = GroupName + "/mcp"

	// ClusterRequestFinalizerPrefix is the prefix for the finalizers that are added to MCP resources for cluster requests.
	ClusterRequestFinalizerPrefix = "request.clusters.openmcp.cloud/"
)

const (
	ConditionMeta = "Meta"

	ConditionClusterRequestReady       = "ClusterRequestReady"
	ConditionClusterConditionsSynced   = "ClusterConditionsSynced"
	ConditionPrefixClusterCondition    = "Cluster."
	ConditionPrefixAccessReady         = "AccessReady."
	ConditionAllAccessReady            = "AllAccessReady"
	ConditionAllServicesDeleted        = "AllServicesDeleted"
	ConditionAllClusterRequestsDeleted = "AllClusterRequestsDeleted"
)

const (
	OIDCNamePrefix  = "oidc_"
	TokenNamePrefix = "token_"
)
