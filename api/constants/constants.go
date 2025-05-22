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
)
