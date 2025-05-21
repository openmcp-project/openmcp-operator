package v1alpha1

const (
	// PURPOSE_PLATFORM means platform controllers will run on the cluster.
	PURPOSE_PLATFORM = "platform"
	// PURPOSE_WORKLOAD means workload controllers will run on the cluster.
	PURPOSE_WORKLOAD = "workload"
	// PURPOSE_ONBOARDING means the cluster is used for onboarding resources.
	// Onboarding clusters can be workerless.
	PURPOSE_ONBOARDING = "onboarding"
	// PURPOSE_MCP means the cluster is used as an MCP cluster.
	// MCP clusters can be workerless.
	PURPOSE_MCP = "mcp"
)

const (
	// CONDITION_UNKNOWN represents an unknown status for the condition.
	CONDITION_UNKNOWN ConditionStatus = "Unknown"
	// CONDITION_TRUE marks the condition as true.
	CONDITION_TRUE ConditionStatus = "True"
	// CONDITION_FALSE marks the condition as false.
	CONDITION_FALSE ConditionStatus = "False"
)

const (
	// PHASE_UNKNOWN represents an unknown phase for the cluster.
	PHASE_UNKNOWN ClusterPhase = "Unknown"
	// PHASE_PROGRESSING indicates that the cluster is being created or updated.
	PHASE_PROGRESSING ClusterPhase = "Progressing"
	// PHASE_SUCCEEDED indicates that the cluster is ready.
	PHASE_SUCCEEDED ClusterPhase = "Succeeded"
	// PHASE_FAILED indicates that an error occurred while creating or updating the cluster.
	PHASE_FAILED ClusterPhase = "Failed"
	// PHASE_DELETING indicates that the cluster is being deleted.
	PHASE_DELETING ClusterPhase = "Deleting"
	// PHASE_DELETION_FAILED indicates that an error occurred while deleting the cluster.
	PHASE_DELETION_FAILED ClusterPhase = "DeletionFailed"
)

const (
	// REQUEST_PENDING indicates that the request has neither been granted nor denied yet.
	REQUEST_PENDING RequestPhase = "Pending"
	// REQUEST_GRANTED indicates that the request has been granted.
	REQUEST_GRANTED RequestPhase = "Granted"
	// REQUEST_DENIED indicates that the request has been denied.
	REQUEST_DENIED RequestPhase = "Denied"
)

type Tenancy string

const (
	// TENANCY_SHARED means the cluster is shared among multiple tenants.
	TENANCY_SHARED Tenancy = "Shared"
	// TENANCY_EXCLUSIVE means the cluster is dedicated to a single tenant.
	TENANCY_EXCLUSIVE Tenancy = "Exclusive"
)

const (
	// ClusterLabel can be used on CRDs to indicate onto which cluster they should be deployed.
	ClusterLabel = ParentGroupName + "/cluster"
	// OperationAnnotation is used to trigger specific operations on resources.
	OperationAnnotation = ParentGroupName + "/operation"
	// OperationAnnotationValueIgnore is used to ignore the resource.
	OperationAnnotationValueIgnore = "ignore"
	// OperationAnnotationValueReconcile is used to trigger a reconcile on the resource.
	OperationAnnotationValueReconcile = "reconcile"

	// K8sVersionAnnotation can be used to display the k8s version of the cluster.
	K8sVersionAnnotation = GroupName + "/k8sversion"
	// ProviderInfoAnnotation can be used to display provider-specific information about the cluster.
	ProviderInfoAnnotation = GroupName + "/providerinfo"
	// ProfileNameAnnotation can be used to display the actual name (not the hash) of the cluster profile.
	ProfileNameAnnotation = GroupName + "/profile"
	// EnvironmentAnnotation can be used to display the environment of the cluster.
	EnvironmentAnnotation = GroupName + "/environment"
	// ProviderAnnotation can be used to display the provider of the cluster.
	ProviderAnnotation = GroupName + "/provider"

	// DeleteWithoutRequestsLabel marks that the corresponding cluster can be deleted if the scheduler removes the last request pointing to it.
	// Its value must be "true" for the label to take effect.
	DeleteWithoutRequestsLabel = GroupName + "/delete-without-requests"
	// ProviderLabel is used to indicate the provider that is responsible for an AccessRequest.
	ProviderLabel = "provider." + GroupName
	// ProfileLabel is used to make the profile information easily accessible for the ClusterProviders.
	ProfileLabel = "profile." + GroupName
)

const (
	// ClusterRequestFinalizer is the finalizer used on ClusterRequest resources
	ClusterRequestFinalizer = GroupName + "/request"
	// RequestFinalizerOnClusterPrefix is the prefix for the finalizers that mark a Cluster as being referenced by a ClusterRequest.
	RequestFinalizerOnClusterPrefix = "request." + GroupName + "/"
)
