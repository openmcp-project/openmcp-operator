package provider

import (
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/openmcp-project/openmcp-operator/api/provider/v1alpha1"
)

// init conditions

const (
	// condition types
	typeInitialized = "Initialized"
	typeReady       = "Ready"

	// reasons of initialized condition
	reasonInitPending        = "InitPending"
	reasonInitCreationFailed = "InitJobCreationFailed"
	reasonInitRunning        = "InitRunning"
	reasonInitCompleted      = "InitCompleted"
	reasonInitFailed         = "InitFailed"

	// reasons of provider ready condition
	reasonProviderInstallationPending = "ProviderInstallationPending"
	reasonProviderInstallationFailed  = "ProviderInstallationFailed"
	reasonProviderNotReady            = "ProviderNotReady"
	reasonProviderReady               = "ProviderReady"
)

func resetConditions(provider *unstructured.Unstructured, deploymentStatus *v1alpha1.DeploymentStatus) {
	apimeta.SetStatusCondition(&deploymentStatus.Conditions, conditionInitUnknown(provider))
	apimeta.SetStatusCondition(&deploymentStatus.Conditions, conditionReadyUnknown(provider))
}

func isInitialized(deploymentStatus *v1alpha1.DeploymentStatus) bool {
	return apimeta.IsStatusConditionTrue(deploymentStatus.Conditions, typeInitialized)
}

//  func initConditionReason(deploymentStatus *v1alpha1.DeploymentStatus) string {
//	condition := apimeta.FindStatusCondition(deploymentStatus.Conditions, typeInitialized)
//	if condition != nil {
//		return condition.Reason
//	}
//	return ""
//  }

func isProviderInstalledAndReady(deploymentStatus *v1alpha1.DeploymentStatus) bool {
	return apimeta.IsStatusConditionTrue(deploymentStatus.Conditions, typeReady)
}

//  func readyConditionReason(deploymentStatus *v1alpha1.DeploymentStatus) string {
//	condition := apimeta.FindStatusCondition(deploymentStatus.Conditions, typeReady)
//	if condition != nil {
//		return condition.Reason
//	}
//	return ""
//  }

func conditionInitUnknown(provider *unstructured.Unstructured) meta.Condition {
	return meta.Condition{
		Type:               typeInitialized,
		Status:             meta.ConditionUnknown,
		ObservedGeneration: provider.GetGeneration(),
		Reason:             reasonInitPending,
	}
}

func conditionInitJobCreationFailed(provider *unstructured.Unstructured, err error) meta.Condition {
	return meta.Condition{
		Type:               typeInitialized,
		Status:             meta.ConditionFalse,
		ObservedGeneration: provider.GetGeneration(),
		Reason:             reasonInitCreationFailed,
		Message:            err.Error(),
	}
}

func conditionInitRunning(provider *unstructured.Unstructured, message string) meta.Condition {
	return meta.Condition{
		Type:               typeInitialized,
		Status:             meta.ConditionFalse,
		ObservedGeneration: provider.GetGeneration(),
		Reason:             reasonInitRunning,
		Message:            "Initialization is running: " + message,
	}
}

//  func conditionInitFailed(provider *unstructured.Unstructured) meta.Condition {
//	return meta.Condition{
//		Type:               typeInitialized,
//		Status:             meta.ConditionFalse,
//		ObservedGeneration: provider.GetGeneration(),
//		Reason:             reasonInitFailed,
//		Message:            "Initialization has failed",
//	}
//  }

func conditionInitCompleted(provider *unstructured.Unstructured) meta.Condition {
	return meta.Condition{
		Type:               typeInitialized,
		Status:             meta.ConditionTrue,
		ObservedGeneration: provider.GetGeneration(),
		Reason:             reasonInitCompleted,
		Message:            "Initialization has been completed",
	}
}

// provider ready conditions

func conditionReadyUnknown(provider *unstructured.Unstructured) meta.Condition {
	return meta.Condition{
		Type:               typeReady,
		Status:             meta.ConditionUnknown,
		ObservedGeneration: provider.GetGeneration(),
		Reason:             reasonProviderInstallationPending,
	}
}

func conditionProviderInstallationFailed(provider *unstructured.Unstructured, err error) meta.Condition {
	return meta.Condition{
		Type:               typeReady,
		Status:             meta.ConditionFalse,
		ObservedGeneration: provider.GetGeneration(),
		Reason:             reasonProviderInstallationFailed,
		Message:            err.Error(),
	}
}

func conditionProviderNotReady(provider *unstructured.Unstructured, message string) meta.Condition {
	return meta.Condition{
		Type:               typeReady,
		Status:             meta.ConditionFalse,
		ObservedGeneration: provider.GetGeneration(),
		Reason:             reasonProviderNotReady,
		Message:            "Waiting for provider to become ready: " + message,
	}
}

func conditionProviderReady(provider *unstructured.Unstructured) meta.Condition {
	return meta.Condition{
		Type:               typeReady,
		Status:             meta.ConditionTrue,
		ObservedGeneration: provider.GetGeneration(),
		Reason:             reasonProviderReady,
		Message:            "Provider is ready",
	}
}
