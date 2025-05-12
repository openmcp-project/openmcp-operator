package provider

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/openmcp-project/controller-utils/pkg/controller"
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
		deleted := false
		deleted, res, err = r.handleDeleteOperation(ctx, provider)
		if deleted {
			return ctrl.Result{}, nil
		}
	}

	// patch the status
	updateErr := r.PlatformClient.Status().Patch(ctx, provider, client.MergeFrom(providerOrig))
	err = errors.Join(err, updateErr)
	return res, err
}

// HandleJob - the ProviderReconciler reconciles the provider resources (ClusterProviders, ServiceProviders, PlatformServices).
// During a reconcile, the ProviderReconciler creates the init job of the provider and must wait for it to complete.
// Therefore, the ProviderReconciler watches also jobs. The present method handles the job events and creates a reconcile request.
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

	if err := r.checkReconcileAnnotation(ctx, provider, deploymentStatus); err != nil {
		return reconcile.Result{}, err
	}
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
	}

	if !isInitialized(deploymentStatus) {
		log.Debug("installing init job")
		completed, err := installer.InstallInitJob(ctx)
		if err != nil {
			log.Error(err, "failed to install init job")
			apimeta.SetStatusCondition(&deploymentStatus.Conditions, conditionInitJobCreationFailed(provider, err))
			return reconcile.Result{}, err
		}
		if !completed {
			log.Debug("init job has not yet completed")
			apimeta.SetStatusCondition(&deploymentStatus.Conditions, conditionInitRunning(provider, "init job has not yet completed"))
			return reconcile.Result{}, nil
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

func (r *ProviderReconciler) handleDeleteOperation(ctx context.Context, provider *unstructured.Unstructured) (deleted bool, res ctrl.Result, err error) {

	deploymentStatus, err := r.deploymentStatusFromUnstructured(provider)
	if err != nil {
		return false, reconcile.Result{}, err
	}

	installer := install.Installer{
		PlatformClient: r.PlatformClient,
		Provider:       provider,
		DeploymentSpec: nil,
	}

	deploymentStatus.Phase = phaseTerminating

	deleted, err = installer.UninstallProvider(ctx)
	if err != nil {
		conversionErr := r.deploymentStatusIntoUnstructured(deploymentStatus, provider)
		err = errors.Join(err, conversionErr)
		return false, reconcile.Result{}, err

	}

	if !deleted {
		return false, reconcile.Result{Requeue: true}, nil
	}

	if err = r.removeFinalizer(ctx, provider); err != nil {
		return false, reconcile.Result{}, err
	}

	return true, reconcile.Result{}, nil
}

func (r *ProviderReconciler) checkReconcileAnnotation(
	ctx context.Context,
	provider *unstructured.Unstructured,
	deploymentStatus *v1alpha1.DeploymentStatus,
) error {
	if controller.HasAnnotationWithValue(provider, openmcpOperationAnnotation, operationReconcile) && deploymentStatus.Phase == phaseReady {
		log := logging.FromContextOrPanic(ctx)
		deploymentStatus.Phase = phaseProgressing

		if err := r.deploymentStatusIntoUnstructured(deploymentStatus, provider); err != nil {
			log.Error(err, "failed to handle reconcile annotation")
			return fmt.Errorf("failed to handle reconcile annotation: %w", err)
		}

		if err := r.PlatformClient.Status().Update(ctx, provider); err != nil {
			log.Error(err, "failed to handle reconcile annotation: unable to change phase of provider to progressing")
			return fmt.Errorf("failed to handle reconcile annotation: unable to change phase of provider %s/%s to progressing: %w", provider.GetNamespace(), provider.GetName(), err)
		}

		if err := controller.EnsureAnnotation(ctx, r.PlatformClient, provider, openmcpOperationAnnotation, operationReconcile, true, controller.DELETE); err != nil {
			log.Error(err, "failed to remove reconcile annotation from provider")
			return fmt.Errorf("failed to remove reconcile annotation from provider %s/%s: %w", provider.GetNamespace(), provider.GetName(), err)
		}
	}
	return nil
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

func (r *ProviderReconciler) removeFinalizer(ctx context.Context, provider *unstructured.Unstructured) error {
	if controllerutil.ContainsFinalizer(provider, openmcpFinalizer) {
		controllerutil.RemoveFinalizer(provider, openmcpFinalizer)
		if err := r.PlatformClient.Update(ctx, provider); err != nil {
			log := logging.FromContextOrPanic(ctx)
			log.Error(err, "failed to remove finalizer from provider")
			return fmt.Errorf("failed to remove finalizer from provider %s: %w", provider.GetName(), err)
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
