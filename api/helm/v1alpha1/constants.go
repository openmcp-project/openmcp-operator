package v1alpha1

const (
	SourceKindHelmRepository = "HelmRepository"
	SourceKindGitRepository  = "GitRepository"
	SourceKindOCIRepository  = "OCIRepository"

	// Finalizer is the finalizer used by the HelmDeployer controller.
	Finalizer = GroupName + "/finalizer"

	// AccessFinalizer is the finalizer used by the HelmDeployer's cluster access controller on Cluster resources.
	AccessFinalizer = GroupName + "/access"

	// ConditionPrefixCluster is the prefix for the cluster-related conditions on HelmDeployments.
	ConditionPrefixCluster = "Cluster."

	// ClusterNamespaceLabel is the label key that identifies the namespace of the cluster associated with a HelmRelease.
	ClusterNamespaceLabel = GroupName + "/cluster-namespace"
	// ClusterNameLabel is the label key that identifies the name of the cluster associated with a HelmRelease.
	ClusterNameLabel = GroupName + "/cluster-name"

	ReasonClusterAccessNotAvailable         = "ClusterAccessNotAvailable"
	ReasonFluxResourcesDeployedAndHealthy   = "FluxResourcesDeployedAndHealthy"
	ReasonWaitingForHelmReleaseHealthy      = "WaitingForHelmReleaseHealthy"
	ReasonWaitingForHelmChartSourceHealthy  = "WaitingForHelmChartSourceHealthy"
	ReasonHelmReleaseDeploymentFailed       = "HelmReleaseDeploymentFailed"
	ReasonHelmChartSourceDeploymentFailed   = "HelmChartSourceDeploymentFailed"
	ReasonFluxResourcesDeleted              = "FluxResourcesDeleted"
	ReasonWaitingForHelmReleaseDeletion     = "WaitingForHelmReleaseDeletion"
	ReasonHelmReleaseDeletionFailed         = "HelmReleaseDeletionFailed"
	ReasonWaitingForHelmChartSourceDeletion = "WaitingForHelmChartSourceDeletion"
	ReasonHelmChartSourceDeletionFailed     = "HelmChartSourceDeletionFailed"
)
