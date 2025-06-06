package constants

const (
	// OpenMCPGroupName is the base API group name for OpenMCP.
	OpenMCPGroupName = "openmcp.cloud"

	// ClusterLabel can be used on CRDs to indicate onto which cluster they should be deployed.
	ClusterLabel = OpenMCPGroupName + "/cluster"

	// OperationAnnotation is used to trigger specific operations on resources.
	OperationAnnotation = OpenMCPGroupName + "/operation"
	// OperationAnnotationValueIgnore is used to ignore the resource.
	OperationAnnotationValueIgnore = "ignore"
	// OperationAnnotationValueReconcile is used to trigger a reconcile on the resource.
	OperationAnnotationValueReconcile = "reconcile"

	// ManagedByLabel is used to indicate which controller manages the resource.
	ManagedByLabel = OpenMCPGroupName + "/managed-by"

	// OnboardingNameLabel is used to store the name on the onboarding cluster of a resource.
	OnboardingNameLabel = OpenMCPGroupName + "/onboarding-name"
	// OnboardingNamespaceLabel is used to store the namespace on the onboarding cluster of a resource.
	OnboardingNamespaceLabel = OpenMCPGroupName + "/onboarding-namespace"

	// EnvVariableProviderName is the name of an environment variable passed to providers.
	// It contains the name of the provider resource.
	EnvVariableProviderName = "OPENMCP_PROVIDER_NAME"
	// EnvVariablePlatformClusterNamespace is the name of an environment variable passed to providers.
	// It contains the namespace on the platform cluster in which the provider is running.
	EnvVariablePlatformClusterNamespace = "OPENMCP_PLATFORM_CLUSTER_NAMESPACE"
)
