package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/openmcp-project/controller-utils/pkg/controller"
	"github.com/openmcp-project/controller-utils/pkg/logging"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/openmcp-project/openmcp-operator/api/constants"
	"github.com/openmcp-project/openmcp-operator/api/provider/v1alpha1"
	"github.com/openmcp-project/openmcp-operator/internal/controllers/provider/install"
)

const (
	openmcpFinalizer = constants.OpenMCPGroupName + "/finalizer"
)

func NewProviderReconciler(gvk schema.GroupVersionKind, client client.Client, environment string) *ProviderReconciler {
	return &ProviderReconciler{
		GroupVersionKind: gvk,
		PlatformClient:   client,
		Environment:      environment,
	}
}

type ProviderReconciler struct {
	schema.GroupVersionKind
	PlatformClient client.Client
	Environment    string
}

func (r *ProviderReconciler) ControllerName() string {
	return strings.ToLower(r.Kind)
}

func (r *ProviderReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	log := logging.FromContextOrPanic(ctx).WithName(r.ControllerName())
	ctx = logging.NewContext(ctx, log)
	log.Debug("Starting reconcile")

	provider := &unstructured.Unstructured{}
	provider.SetGroupVersionKind(r.GroupVersionKind)

	if err := r.PlatformClient.Get(ctx, req.NamespacedName, provider); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Resource not found, ignoring reconcile request")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get provider resource: %w", err)
	}

	if controller.HasAnnotationWithValue(provider, constants.OperationAnnotation, constants.OperationAnnotationValueIgnore) {
		log.Info("Ignoring resource due to ignore operation annotation")
		return ctrl.Result{}, nil
	}

	providerOrig := provider.DeepCopy()

	if provider.GetDeletionTimestamp().IsZero() {
		err = r.handleCreateUpdateOperation(ctx, provider)
		res = reconcile.Result{}
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

func (r *ProviderReconciler) handleCreateUpdateOperation(ctx context.Context, provider *unstructured.Unstructured) error {
	log := logging.FromContextOrPanic(ctx)
	log.Debug("Handling create or update operation")

	if err := r.ensureFinalizer(ctx, provider); err != nil {
		return err
	}

	deploymentSpec, err := deploymentSpecFromUnstructured(provider)
	if err != nil {
		return err
	}

	if err := r.removeReconcileAnnotation(ctx, provider); err != nil {
		return err
	}

	status, err := r.install(ctx, provider, deploymentSpec)

	// transfer status into provider
	conversionErr := status.intoProviderStatus(provider)
	err = errors.Join(err, conversionErr)

	return err
}

func (r *ProviderReconciler) install(
	ctx context.Context,
	provider *unstructured.Unstructured,
	deploymentSpec *v1alpha1.DeploymentSpec,
) (*ReconcileStatus, error) {

	status := newInstallStatus(provider.GetGeneration())

	log := logging.FromContextOrPanic(ctx)
	installer := install.Installer{
		PlatformClient: r.PlatformClient,
		Provider:       provider,
		DeploymentSpec: deploymentSpec,
		Environment:    r.Environment,
	}

	// init job
	log.Debug("installing init job")
	completed, err := installer.InstallInitJob(ctx)
	if err != nil {
		log.Error(err, "failed to install init job")
		status.setInitConditionJobCreationFailed(err)
		return status, err
	}
	if !completed {
		log.Debug("init job has not yet completed")
		status.setInitConditionJobRunning()
		return status, nil
	}

	log.Debug("init job completed")
	status.setInitConditionJobCompleted()

	// deployment
	log.Debug("installing provider")
	if err := installer.InstallProvider(ctx); err != nil {
		log.Error(err, "failed to install provider")
		status.setReadyConditionProviderInstallationFailed(err)
		return status, err
	}

	readinessCheckResult := installer.CheckProviderReadiness(ctx)
	if !readinessCheckResult.IsReady() {
		log.Debug("provider is not yet ready")
		status.setReadyConditionProviderNotReady(readinessCheckResult.Message())
		return status, nil
	}

	log.Debug("provider has become ready")
	status.setReadyConditionProviderReady()

	status.Phase = phaseReady
	return status, nil
}

func (r *ProviderReconciler) handleDeleteOperation(ctx context.Context, provider *unstructured.Unstructured) (deleted bool, res ctrl.Result, err error) {
	log := logging.FromContextOrPanic(ctx)
	log.Debug("Handling delete operation")

	status := newUninstallStatus(provider.GetGeneration())

	installer := install.Installer{
		PlatformClient: r.PlatformClient,
		Provider:       provider,
		DeploymentSpec: nil,
		Environment:    r.Environment,
	}

	deleted, err = installer.UninstallProvider(ctx)
	if err != nil {
		status.setUninstalledConditionFailed(err)
		conversionErr := status.intoProviderStatus(provider)
		err = errors.Join(err, conversionErr)
		return false, reconcile.Result{}, err
	} else if !deleted {
		conversionErr := status.intoProviderStatus(provider)
		return false, reconcile.Result{Requeue: true}, conversionErr
	}

	if err = r.removeFinalizer(ctx, provider); err != nil {
		return false, reconcile.Result{}, err
	}

	return true, reconcile.Result{}, nil
}

func (r *ProviderReconciler) removeReconcileAnnotation(ctx context.Context, provider *unstructured.Unstructured) error {
	if controller.HasAnnotationWithValue(provider, constants.OperationAnnotation, constants.OperationAnnotationValueReconcile) {
		log := logging.FromContextOrPanic(ctx)
		log.Debug("found reconcile annotation")
		if err := controller.EnsureAnnotation(ctx, r.PlatformClient, provider, constants.OperationAnnotation, constants.OperationAnnotationValueReconcile, true, controller.DELETE); err != nil {
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
