package provider

import (
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/openmcp-project/openmcp-operator/api/provider/v1alpha1"
)

const (
	// Phases
	phaseProgressing  = "Progressing"
	phaseReady        = "Ready"
	phaseUninstalling = "Uninstalling"

	// Condition types
	typeInitialized = "Initialized"
	typeReady       = "Ready"
	typeUninstalled = "Uninstalled"

	// Reasons of conditions with type "Initialized"
	reasonInitPending        = "InitPending"
	reasonInitCreationFailed = "InitJobCreationFailed"
	reasonInitRunning        = "InitRunning"
	reasonInitCompleted      = "InitCompleted"

	// Reasons of conditions with type "Ready"
	reasonProviderInstallationPending = "ProviderInstallationPending"
	reasonProviderInstallationFailed  = "ProviderInstallationFailed"
	reasonProviderNotReady            = "ProviderNotReady"
	reasonProviderReady               = "ProviderReady"

	// Reasons of conditions with type "Uninstalled"
	reasonUninstalling    = "Uninstalling"
	reasonUninstallFailed = "UninstallFailed"
)

func isInitialized(deploymentStatus *v1alpha1.DeploymentStatus) bool {
	return apimeta.IsStatusConditionTrue(deploymentStatus.Conditions, typeInitialized)
}

func isProviderInstalledAndReady(deploymentStatus *v1alpha1.DeploymentStatus) bool {
	return apimeta.IsStatusConditionTrue(deploymentStatus.Conditions, typeReady)
}

func newInstallStatus(generation int64) *ReconcileStatus {
	s := &ReconcileStatus{
		ObservedGeneration: generation,
		Phase:              phaseProgressing,
	}
	s.setInitConditionJobCreationPending()
	s.setReadyConditionProviderInstallationPending()
	return s
}

func newUninstallStatus(generation int64) *ReconcileStatus {
	s := &ReconcileStatus{
		ObservedGeneration: generation,
		Phase:              phaseUninstalling,
	}
	s.setUninstalledConditionUninstalling()
	return s
}

type ReconcileStatus struct {
	InitCondition        *meta.Condition
	ReadyCondition       *meta.Condition
	UninstalledCondition *meta.Condition
	ObservedGeneration   int64
	Phase                string
}

func (s *ReconcileStatus) setInitConditionJobCreationPending() {
	s.InitCondition = &meta.Condition{
		Type:               typeInitialized,
		Status:             meta.ConditionUnknown,
		ObservedGeneration: s.ObservedGeneration,
		Reason:             reasonInitPending,
	}
}

func (s *ReconcileStatus) setInitConditionJobCreationFailed(err error) {
	s.InitCondition = &meta.Condition{
		Type:               typeInitialized,
		Status:             meta.ConditionFalse,
		ObservedGeneration: s.ObservedGeneration,
		Reason:             reasonInitCreationFailed,
		Message:            err.Error(),
	}
}

func (s *ReconcileStatus) setInitConditionJobRunning() {
	s.InitCondition = &meta.Condition{
		Type:               typeInitialized,
		Status:             meta.ConditionFalse,
		ObservedGeneration: s.ObservedGeneration,
		Reason:             reasonInitRunning,
		Message:            "Initialization is running",
	}
}

func (s *ReconcileStatus) setInitConditionJobCompleted() {
	s.InitCondition = &meta.Condition{
		Type:               typeInitialized,
		Status:             meta.ConditionTrue,
		ObservedGeneration: s.ObservedGeneration,
		Reason:             reasonInitCompleted,
		Message:            "Initialization has been completed",
	}
}

func (s *ReconcileStatus) setReadyConditionProviderInstallationPending() {
	s.ReadyCondition = &meta.Condition{
		Type:               typeReady,
		Status:             meta.ConditionUnknown,
		ObservedGeneration: s.ObservedGeneration,
		Reason:             reasonProviderInstallationPending,
	}
}

func (s *ReconcileStatus) setReadyConditionProviderInstallationFailed(err error) {
	s.ReadyCondition = &meta.Condition{
		Type:               typeReady,
		Status:             meta.ConditionFalse,
		ObservedGeneration: s.ObservedGeneration,
		Reason:             reasonProviderInstallationFailed,
		Message:            err.Error(),
	}
}

func (s *ReconcileStatus) setReadyConditionProviderNotReady(message string) {
	s.ReadyCondition = &meta.Condition{
		Type:               typeReady,
		Status:             meta.ConditionFalse,
		ObservedGeneration: s.ObservedGeneration,
		Reason:             reasonProviderNotReady,
		Message:            "Waiting for provider to become ready: " + message,
	}
}

func (s *ReconcileStatus) setReadyConditionProviderReady() {
	s.ReadyCondition = &meta.Condition{
		Type:               typeReady,
		Status:             meta.ConditionTrue,
		ObservedGeneration: s.ObservedGeneration,
		Reason:             reasonProviderReady,
		Message:            "Provider is ready",
	}
}

func (s *ReconcileStatus) setUninstalledConditionUninstalling() {
	s.UninstalledCondition = &meta.Condition{
		Type:               typeUninstalled,
		Status:             meta.ConditionFalse,
		ObservedGeneration: s.ObservedGeneration,
		Reason:             reasonUninstalling,
		Message:            "Provider is being uninstalled",
	}
}

func (s *ReconcileStatus) setUninstalledConditionFailed(err error) {
	s.UninstalledCondition = &meta.Condition{
		Type:               typeUninstalled,
		Status:             meta.ConditionFalse,
		ObservedGeneration: s.ObservedGeneration,
		Reason:             reasonUninstallFailed,
		Message:            err.Error(),
	}
}

// intoProviderStatus updates the status of the provider resource based on the ReconcileStatus.
// As the provider resource is an Unstructured, it requires a conversion to and from a DeploymentStatus.
func (s *ReconcileStatus) intoProviderStatus(provider *unstructured.Unstructured) error {
	deploymentStatus, err := deploymentStatusFromUnstructured(provider)
	if err != nil {
		return err
	}
	if err := s.intoDeploymentStatus(deploymentStatus); err != nil {
		return err
	}
	if err = deploymentStatusIntoUnstructured(deploymentStatus, provider); err != nil {
		return err
	}
	return nil
}

func (s *ReconcileStatus) intoDeploymentStatus(deploymentStatus *v1alpha1.DeploymentStatus) error {
	deploymentStatus.ObservedGeneration = s.ObservedGeneration
	deploymentStatus.Phase = s.Phase

	// Note that SetStatusCondition sets the LastTransitionTime when the condition status changes.
	if s.InitCondition != nil {
		apimeta.SetStatusCondition(&deploymentStatus.Conditions, *s.InitCondition)
	} else {
		apimeta.RemoveStatusCondition(&deploymentStatus.Conditions, typeInitialized)
	}

	if s.ReadyCondition != nil {
		apimeta.SetStatusCondition(&deploymentStatus.Conditions, *s.ReadyCondition)
	} else {
		apimeta.RemoveStatusCondition(&deploymentStatus.Conditions, typeReady)
	}

	if s.UninstalledCondition != nil {
		apimeta.SetStatusCondition(&deploymentStatus.Conditions, *s.UninstalledCondition)
	} else {
		apimeta.RemoveStatusCondition(&deploymentStatus.Conditions, typeUninstalled)
	}

	return nil
}
