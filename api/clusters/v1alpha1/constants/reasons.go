package constants

const (
	// ReasonOnboardingClusterInteractionProblem is used when the onboarding cluster cannot be reached.
	ReasonOnboardingClusterInteractionProblem = "OnboardingClusterInteractionProblem"
	// ReasonPlatformClusterInteractionProblem is used when the platform cluster cannot be reached.
	ReasonPlatformClusterInteractionProblem = "PlatformClusterInteractionProblem"
	// ReasonInvalidReference means that a reference points to a non-existing or otherwise invalid object.
	ReasonInvalidReference = "InvalidReference"
	// ReasonConfigurationProblem indicates that something is configured incorrectly.
	ReasonConfigurationProblem = "ConfigurationProblem"
	// ReasonInternalError indicates that something went wrong internally.
	ReasonInternalError = "InternalError"
	// ReasonWaitingForClusterRequest indicates that something is waiting for a ClusterRequest to become ready.
	ReasonWaitingForClusterRequest = "WaitingForClusterRequest"
	// ReasonWaitingForClusterRequestDeletion indicates that something is waiting for a ClusterRequest to be deleted.
	ReasonWaitingForClusterRequestDeletion = "WaitingForClusterRequestDeletion"
	// ReasonWaitingForAccessRequest indicates that something is waiting for an AccessRequest to become ready.
	ReasonWaitingForAccessRequest = "WaitingForAccessRequest"
	// ReasonWaitingForAccessRequestDeletion indicates that something is waiting for an AccessRequest to be deleted.
	ReasonWaitingForAccessRequestDeletion = "WaitingForAccessRequestDeletion"
	// ReasonWaitingForServices indicates that something is waiting for one or more service providers to do something.
	ReasonWaitingForServices = "WaitingForServices"
	// ReasonWaitingForServiceDeletion indicates that something is waiting for a service to be deleted.
	ReasonWaitingForServiceDeletion = "WaitingForServiceDeletion"
)
