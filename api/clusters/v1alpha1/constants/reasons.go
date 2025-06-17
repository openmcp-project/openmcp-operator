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
	// ReasonSchedulingFailed indicates that there was a problem with scheduling a request.
	ReasonSchedulingFailed = "SchedulingFailed"
)
