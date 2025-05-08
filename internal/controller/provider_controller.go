package controller

import (
	"context"
	"fmt"

	"github.com/openmcp-project/controller-utils/pkg/logging"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openmcp-project/openmcp-operator/api/v1alpha1"
)

type ProviderReconciler struct {
	schema.GroupVersionKind
	PlatformClient client.Client
}

func (r *ProviderReconciler) ControllerName() string {
	return r.GroupVersionKind.String()
}

func (r *ProviderReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logging.FromContextOrPanic(ctx).WithName(r.ControllerName())
	ctx = logging.NewContext(ctx, log)
	log.Debug("Starting reconcile")

	provider := &unstructured.Unstructured{}
	provider.SetGroupVersionKind(r.GroupVersionKind)

	if err := r.PlatformClient.Get(ctx, req.NamespacedName, provider); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get provider resource: %w", err)
	}

	providerOrig := provider.DeepCopy()

	deploymentSpec := v1alpha1.DeploymentSpec{}
	deploymentSpecRaw, found, err := unstructured.NestedFieldNoCopy(provider.Object, "spec")
	if !found {
		return ctrl.Result{}, fmt.Errorf("provider spec not found")
	}
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get provider spec: %w", err)
	}

	if err = runtime.DefaultUnstructuredConverter.FromUnstructured(deploymentSpecRaw.(map[string]interface{}), &deploymentSpec); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to convert provider spec: %w", err)
	}

	log.Info("DeployableSpec", "deploymentSpec", deploymentSpec)

	//if provider.GetDeletionTimestamp().IsZero() {
	//	res, err = r.handleCreateUpdateOperation(ctx, provider)
	//} else {
	//	res, err = r.handleDeleteOperation(ctx, provider)
	//}

	deployableStatus := v1alpha1.DeploymentStatus{}
	deploymentStatusRaw, found, _ := unstructured.NestedFieldNoCopy(provider.Object, "status", "deploymentStatus")
	if found {
		if err = runtime.DefaultUnstructuredConverter.FromUnstructured(deploymentStatusRaw.(map[string]interface{}), &deployableStatus); err != nil {
			return ctrl.Result{}, err
		}
	}

	// set status
	// deployableStatus.Status = true

	deploymentSpecRaw, err = runtime.DefaultUnstructuredConverter.ToUnstructured(&deployableStatus)
	if err != nil {
		return ctrl.Result{}, err
	}

	statusRaw, found, _ := unstructured.NestedFieldNoCopy(provider.Object, "status")
	if !found {
		statusRaw = map[string]interface{}{}
		if err = unstructured.SetNestedField(provider.Object, statusRaw.(map[string]interface{}), "status"); err != nil {
			return ctrl.Result{}, err
		}
	}

	if err = unstructured.SetNestedField(provider.Object, deploymentSpecRaw, "status", "deploymentStatus"); err != nil {
		return ctrl.Result{}, err
	}

	// patch the status
	if err = r.PlatformClient.Status().Patch(ctx, provider, client.MergeFrom(providerOrig)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *ProviderReconciler) handleCreateUpdateOperation(ctx context.Context, provider *unstructured.Unstructured) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

func (r *ProviderReconciler) handleDeleteOperation(ctx context.Context, provider *unstructured.Unstructured) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}
