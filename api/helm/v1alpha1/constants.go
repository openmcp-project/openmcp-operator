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

	// ClusterNameLabel is the label key that identifies the name of the cluster associated with a HelmRelease.
	// The cluster namespace does not need an label, as it is always the same one as the resource with the ClusterNameLabel.
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
