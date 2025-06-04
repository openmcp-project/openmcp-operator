package utils

import (
	"fmt"

	"github.com/openmcp-project/controller-utils/pkg/controller"
)

const (
	prefixOnboarding = "ob-"
	prefixMCP        = "mcp-"
	prefixWorkload   = "wl-"
)

// StableControllerNamespace returns a stable namespace for controllers that can be used on the platform cluster.
func StableControllerNamespace(controllerName string) string {
	return fmt.Sprint(controller.K8sNameHash(controllerName), "-system")
}

// StableRequestNamespace returns a stable namespace for ClusterRequests and AccessRequests,
// that can be created/used on the platform cluster.
// onboardingNamespace is the namespace of the reconciled resource on the onboarding cluster.
func StableRequestNamespace(onboardingNamespace string) string {
	return fmt.Sprint(prefixOnboarding, onboardingNamespace)
}

// StableRequestNameMCP returns a stable name for MCP requests that can be used in a request namespace on the platform cluster.
// onboardingName is the name of the reconciled resource on the onboarding cluster.
// controllerName is the name of the controller that is reconciling the resource.
func StableRequestNameMCP(onboardingName, controllerName string) string {
	return fmt.Sprint(prefixMCP, controller.K8sNameHash(onboardingName, controllerName))
}

// StableRequestNameWorkload returns a stable name for Workload requests that can be used in a request namespace on the platform cluster.
// onboardingName is the name of the reconciled resource on the onboarding cluster.
// controllerName is the name of the controller that is reconciling the resource.
func StableRequestNameWorkload(onboardingName, controllerName string) string {
	return fmt.Sprint(prefixWorkload, controller.K8sNameHash(onboardingName, controllerName))
}

// StableRequestNameOnboarding returns a stable name for Onboarding requests that can be used in a request namespace on the platform cluster.
// onboardingName is the name of the reconciled resource on the onboarding cluster.
// controllerName is the name of the controller that is reconciling the resource.
func StableRequestNameOnboarding(onboardingName, controllerName string) string {
	return fmt.Sprint(prefixOnboarding, controller.K8sNameHash(onboardingName, controllerName))
}
