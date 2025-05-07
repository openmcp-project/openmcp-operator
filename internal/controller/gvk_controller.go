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

type GVKReconciler struct {
	Name string
	schema.GroupVersionKind
	client.Client
}

func (r *GVKReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logging.FromContextOrPanic(ctx).WithName(r.Name)

	deployable := &unstructured.Unstructured{}
	deployable.SetGroupVersionKind(r.GroupVersionKind)

	if err := r.Get(ctx, req.NamespacedName, deployable); err != nil {
		return ctrl.Result{}, err
	}

	deployableOrig := deployable.DeepCopy()

	log.Info("Reconciling deployable")

	deployableSpec := v1alpha1.DeployableSpec{}
	deploymentSpecRaw, found, err := unstructured.NestedFieldNoCopy(deployable.Object, "spec", "deploymentSpec")
	if !found {
		return ctrl.Result{}, fmt.Errorf("deploymentSpec not found")
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	if err = runtime.DefaultUnstructuredConverter.FromUnstructured(deploymentSpecRaw.(map[string]interface{}), &deployableSpec); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("DeployableSpec", "deployableSpec", deployableSpec)

	deployableStatus := v1alpha1.DeployableStatus{}
	deploymentStatusRaw, found, _ := unstructured.NestedFieldNoCopy(deployable.Object, "status", "deploymentStatus")
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

	statusRaw, found, _ := unstructured.NestedFieldNoCopy(deployable.Object, "status")
	if !found {
		statusRaw = map[string]interface{}{}
		if err = unstructured.SetNestedField(deployable.Object, statusRaw.(map[string]interface{}), "status"); err != nil {
			return ctrl.Result{}, err
		}
	}

	if err = unstructured.SetNestedField(deployable.Object, deploymentSpecRaw, "status", "deploymentStatus"); err != nil {
		return ctrl.Result{}, err
	}

	// patch the status
	if err = r.Status().Patch(ctx, deployable, client.MergeFrom(deployableOrig)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}
