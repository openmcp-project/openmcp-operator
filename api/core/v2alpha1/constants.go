package v2alpha1

const (
	// DefaultOIDCProviderName is the identifier for the default OIDC provider.
	DefaultOIDCProviderName = "default"
)

const (
	MCPLabel          = GroupName + "/mcp"
	OIDCProviderLabel = GroupName + "/oidc-provider"
)

const (
	ConditionClusterRequestReady   = "ClusterRequestReady"
	ConditionPrefixOIDCAccessReady = "OIDCAccessReady_"
	ConditionAllAccessReady        = "AllAccessReady"
)
