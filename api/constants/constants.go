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
	// ManagedPurposeLabel is used to indicate the purpose of the resource.
	ManagedPurposeLabel = OpenMCPGroupName + "/managed-purpose"

	// OnboardingNameLabel is used to store the name on the onboarding cluster of a resource.
	OnboardingNameLabel = OpenMCPGroupName + "/onboarding-name"
	// OnboardingNamespaceLabel is used to store the namespace on the onboarding cluster of a resource.
	OnboardingNamespaceLabel = OpenMCPGroupName + "/onboarding-namespace"

	// EnvVariablePodName is the name of an environment variable passed to providers.
	// Its value is the name of the pod in which the provider is running.
	EnvVariablePodName = "POD_NAME"
	// EnvVariablePodNamespace is the name of an environment variable passed to providers.
	// Its value is the namespace of the pod in which the pod is running.
	EnvVariablePodNamespace = "POD_NAMESPACE"
	// EnvVariablePodIP is the name of an environment variable passed to providers.
	// Its value is the IP address of the pod in which the provider is running.
	EnvVariablePodIP = "POD_IP"
	// EnvVariablePodServiceAccountName is the name of an environment variable passed to providers.
	// Its value is the name of the service account under which the pod is running.
	EnvVariablePodServiceAccountName = "POD_SERVICE_ACCOUNT_NAME"
)
