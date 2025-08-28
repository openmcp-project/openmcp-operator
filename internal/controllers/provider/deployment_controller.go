/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package provider

import (
	"fmt"
	"os"

	"github.com/openmcp-project/controller-utils/pkg/logging"
	apps "k8s.io/api/apps/v1"
	batch "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/openmcp-project/openmcp-operator/api/constants"
	"github.com/openmcp-project/openmcp-operator/api/provider/v1alpha1"
)

const ControllerName = "DeploymentController"

// DeploymentController is a collection of controllers reconciling ClusterProviders, ServiceProviders, and PlatformServices.
type DeploymentController struct {
	Reconcilers []*ProviderReconciler
}

func NewDeploymentController() *DeploymentController {
	return &DeploymentController{}
}

// SetupWithManager sets up the controllers with the Manager.
func (r *DeploymentController) SetupWithManager(setupLog *logging.Logger, mgr ctrl.Manager, providerGKVList []schema.GroupVersionKind, environment string) error {
	log := setupLog.WithName(ControllerName)
	allErrs := field.ErrorList{}

	systemNamespace := os.Getenv(constants.EnvVariablePodNamespace)
	if systemNamespace == "" {
		return fmt.Errorf("environment variable %s not set", constants.EnvVariablePodNamespace)
	}

	r.Reconcilers = make([]*ProviderReconciler, len(providerGKVList))

	for i, gvk := range providerGKVList {
		log.Info("Registering deployment controller", "groupVersionKind", gvk.String())

		r.Reconcilers[i] = NewProviderReconciler(gvk, mgr.GetClient(), environment, systemNamespace)

		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk)

		err := ctrl.NewControllerManagedBy(mgr).
			Named(r.Reconcilers[i].ControllerName()).
			For(obj).
			Owns(&batch.Job{}).
			Owns(&apps.Deployment{}).
			Complete(r.Reconcilers[i])
		if err != nil {
			log.Error(err, "Failed to register controller", "groupVersionKind", gvk.String())
			allErrs = append(allErrs, field.InternalError(field.NewPath(gvk.String()), err))
		}
	}

	return allErrs.ToAggregate()
}

func deploymentSpecFromUnstructured(provider *unstructured.Unstructured) (*v1alpha1.DeploymentSpec, error) {
	deploymentSpec := v1alpha1.DeploymentSpec{}
	deploymentSpecRaw, found, err := unstructured.NestedFieldNoCopy(provider.Object, "spec")
	if !found {
		return nil, fmt.Errorf("provider spec not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get provider spec: %w", err)
	}

	specObj, ok := deploymentSpecRaw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("failed to cast provider spec")
	}
	if err = runtime.DefaultUnstructuredConverter.FromUnstructured(specObj, &deploymentSpec); err != nil {
		return nil, fmt.Errorf("failed to convert provider spec: %w", err)
	}
	return &deploymentSpec, nil
}

func deploymentSpecIntoUnstructured(spec *v1alpha1.DeploymentSpec, provider *unstructured.Unstructured) error {
	specRaw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(spec)
	if err != nil {
		return fmt.Errorf("failed to convert provider spec to unstructured: %w", err)
	}

	if err = unstructured.SetNestedField(provider.Object, specRaw, "spec"); err != nil {
		return fmt.Errorf("failed to set provider spec: %w", err)
	}
	return nil
}

func deploymentStatusFromUnstructured(provider *unstructured.Unstructured) (*v1alpha1.DeploymentStatus, error) {
	status := v1alpha1.DeploymentStatus{}
	statusRaw, found, _ := unstructured.NestedFieldNoCopy(provider.Object, "status")
	if found {
		statusObj, ok := statusRaw.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("failed to cast provider status")
		}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(statusObj, &status); err != nil {
			return nil, fmt.Errorf("failed to convert provider status from unstructured: %w", err)
		}
	}
	return &status, nil
}

func deploymentStatusIntoUnstructured(status *v1alpha1.DeploymentStatus, provider *unstructured.Unstructured) error {
	statusRaw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(status)
	if err != nil {
		return fmt.Errorf("failed to convert provider status to unstructured: %w", err)
	}

	if err = unstructured.SetNestedField(provider.Object, statusRaw, "status"); err != nil {
		return fmt.Errorf("failed to set provider status: %w", err)
	}
	return nil
}
