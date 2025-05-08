package provider

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/openmcp-project/controller-utils/pkg/logging"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/openmcp-project/openmcp-operator/api/provider/v1alpha1"
	"github.com/openmcp-project/openmcp-operator/internal/controllers/provider/install"
)

const (
	openmcpNamespace           = "openmcp-system"
	openmcpDomain              = "openmcp.cloud"
	openmcpFinalizer           = openmcpDomain + "/finalizer"
	openmcpOperationAnnotation = openmcpDomain + "/operation"
	operationReconcile         = "reconcile"

	phaseProgressing = "Progressing"
	phaseTerminating = "Terminating"
	phaseReady       = "Ready"
)

type ProviderReconciler struct {
	schema.GroupVersionKind
	PlatformClient client.Client
}

func (r *ProviderReconciler) ControllerName() string {
	return r.GroupVersionKind.String()
}

// HandleJob creates a reconcile request for the provider associated with the job.
func (r *ProviderReconciler) HandleJob(_ context.Context, job client.Object) []reconcile.Request {
	providerKind, found := job.GetLabels()[install.ProviderKindLabel]
	if !found || providerKind != r.GroupVersionKind.Kind {
		return nil
	}

	providerName, found := job.GetLabels()[install.ProviderNameLabel]
	if !found {
		return nil
	}

	return []reconcile.Request{
		{
			NamespacedName: client.ObjectKey{
				Name: providerName,
			},
		},
	}
}

func (r *ProviderReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	log := logging.FromContextOrPanic(ctx).WithName(r.ControllerName())
	ctx = logging.NewContext(ctx, log)
	log.Debug("Starting reconcile")

	provider := &unstructured.Unstructured{}
	provider.SetGroupVersionKind(r.GroupVersionKind)

	if err := r.PlatformClient.Get(ctx, req.NamespacedName, provider); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get provider resource: %w", err)
	}

	providerOrig := provider.DeepCopy()

	if provider.GetDeletionTimestamp().IsZero() {
		res, err = r.handleCreateUpdateOperation(ctx, provider)
	} else {
		res, err = r.handleDeleteOperation(ctx, provider)
	}

	// patch the status
	updateErr := r.PlatformClient.Status().Patch(ctx, provider, client.MergeFrom(providerOrig))
	err = errors.Join(err, updateErr)
	return res, err
}

func (r *ProviderReconciler) handleCreateUpdateOperation(ctx context.Context, provider *unstructured.Unstructured) (ctrl.Result, error) {
	log := logging.FromContextOrPanic(ctx)

	if err := r.ensureFinalizer(ctx, provider); err != nil {
		return reconcile.Result{}, err
	}

	deploymentSpec, err := r.deploymentSpecFromUnstructured(provider)
	if err != nil {
		return reconcile.Result{}, err
	}

	deploymentStatus, err := r.deploymentStatusFromUnstructured(provider)
	if err != nil {
		return reconcile.Result{}, err
	}

	// If the provider is not yet in phase ready, we continue with the installation.
	// If the provider is already in phase ready, we install it again if the generation has changed or if it has a reconcile annotation.
	// TODO: checkReconcileAnnotation

	r.observeGeneration(provider, deploymentStatus)

	if deploymentStatus.Phase == phaseReady {
		log.Debug("provider is already in phase ready")
		return reconcile.Result{}, nil
	}

	res, err := r.install(ctx, provider, deploymentSpec, deploymentStatus)

	conversionErr := r.deploymentStatusIntoUnstructured(deploymentStatus, provider)
	err = errors.Join(err, conversionErr)

	return res, err
}

func (r *ProviderReconciler) install(
	ctx context.Context,
	provider *unstructured.Unstructured,
	deploymentSpec *v1alpha1.DeploymentSpec,
	deploymentStatus *v1alpha1.DeploymentStatus,
) (reconcile.Result, error) {

	log := logging.FromContextOrPanic(ctx)
	installer := install.Installer{
		PlatformClient: r.PlatformClient,
		Provider:       provider,
		DeploymentSpec: deploymentSpec,
		Namespace:      openmcpNamespace,
	}

	if !isInitialized(deploymentStatus) {
		log.Debug("installing init job")
		_, err := installer.InstallInitJob(ctx)
		if err != nil {
			log.Error(err, "failed to install init job")
			apimeta.SetStatusCondition(&deploymentStatus.Conditions, conditionInitJobCreationFailed(provider, err))
			return reconcile.Result{}, err
		}

		log.Debug("checking init job readiness")
		if readinessCheckResult := installer.CheckInitJobReadiness(ctx); !readinessCheckResult.IsReady() {
			log.Debug("init job has not yet finished")
			apimeta.SetStatusCondition(&deploymentStatus.Conditions, conditionInitRunning(provider, readinessCheckResult.Message()))
			// TODO: RequeueAfter is unnecessary, if we watch jobs
			return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
		}

		log.Debug("init job completed")
		apimeta.SetStatusCondition(&deploymentStatus.Conditions, conditionInitCompleted(provider))
	}

	if !isProviderInstalledAndReady(deploymentStatus) {
		log.Debug("installing provider")
		if err := installer.InstallProvider(ctx); err != nil {
			log.Error(err, "failed to install provider")
			apimeta.SetStatusCondition(&deploymentStatus.Conditions, conditionProviderInstallationFailed(provider, err))
			return reconcile.Result{}, err
		}

		readinessCheckResult := installer.CheckProviderReadiness(ctx)
		if !readinessCheckResult.IsReady() {
			log.Debug("provider is not yet ready")
			apimeta.SetStatusCondition(&deploymentStatus.Conditions, conditionProviderNotReady(provider, readinessCheckResult.Message()))
			return reconcile.Result{RequeueAfter: 40 * time.Second}, nil
		}

		log.Debug("provider has become ready")
		apimeta.SetStatusCondition(&deploymentStatus.Conditions, conditionProviderReady(provider))
	}

	deploymentStatus.Phase = phaseReady
	return reconcile.Result{}, nil
}

func (r *ProviderReconciler) handleDeleteOperation(ctx context.Context, provider *unstructured.Unstructured) (ctrl.Result, error) {

	deploymentStatus, err := r.deploymentStatusFromUnstructured(provider)
	if err != nil {
		return reconcile.Result{}, err
	}

	installer := install.Installer{
		PlatformClient: r.PlatformClient,
		Provider:       provider,
		DeploymentSpec: nil,
	}

	deploymentStatus.Phase = phaseTerminating

	err = installer.UninstallProvider(ctx)

	conversionErr := r.deploymentStatusIntoUnstructured(deploymentStatus, provider)
	err = errors.Join(err, conversionErr)

	return ctrl.Result{}, err
}

func (r *ProviderReconciler) ensureFinalizer(ctx context.Context, provider *unstructured.Unstructured) error {
	if !controllerutil.ContainsFinalizer(provider, openmcpFinalizer) && provider.GetDeletionTimestamp().IsZero() {
		controllerutil.AddFinalizer(provider, openmcpFinalizer)
		if err := r.PlatformClient.Update(ctx, provider); err != nil {
			log := logging.FromContextOrPanic(ctx)
			log.Error(err, "failed to add finalizer to provider")
			return fmt.Errorf("failed to add finalizer to provider %s: %w", provider.GetName(), err)
		}
	}
	return nil
}

func (r *ProviderReconciler) observeGeneration(provider *unstructured.Unstructured, status *v1alpha1.DeploymentStatus) {
	gen := provider.GetGeneration()
	if status.ObservedGeneration != gen {
		status.ObservedGeneration = gen
		status.Phase = phaseProgressing
		resetConditions(provider, status)
	}
}

func (r *ProviderReconciler) deploymentSpecFromUnstructured(provider *unstructured.Unstructured) (*v1alpha1.DeploymentSpec, error) {
	deploymentSpec := v1alpha1.DeploymentSpec{}
	deploymentSpecRaw, found, err := unstructured.NestedFieldNoCopy(provider.Object, "spec")
	if !found {
		return nil, fmt.Errorf("provider spec not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get provider spec: %w", err)
	}

	if err = runtime.DefaultUnstructuredConverter.FromUnstructured(deploymentSpecRaw.(map[string]interface{}), &deploymentSpec); err != nil {
		return nil, fmt.Errorf("failed to convert provider spec: %w", err)
	}
	return &deploymentSpec, nil
}

func (r *ProviderReconciler) deploymentStatusFromUnstructured(provider *unstructured.Unstructured) (*v1alpha1.DeploymentStatus, error) {
	status := v1alpha1.DeploymentStatus{}
	statusRaw, found, _ := unstructured.NestedFieldNoCopy(provider.Object, "status")
	if found {
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(statusRaw.(map[string]interface{}), &status); err != nil {
			return nil, fmt.Errorf("failed to convert provider status from unstructured: %w", err)
		}
	}
	return &status, nil
}

func (r *ProviderReconciler) deploymentStatusIntoUnstructured(status *v1alpha1.DeploymentStatus, provider *unstructured.Unstructured) error {
	statusRaw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(status)
	if err != nil {
		return fmt.Errorf("failed to convert provider status to unstructured: %w", err)
	}

	if err = unstructured.SetNestedField(provider.Object, statusRaw, "status"); err != nil {
		return fmt.Errorf("failed to set provider status: %w", err)
	}
	return nil
}
