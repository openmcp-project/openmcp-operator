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
	"github.com/openmcp-project/controller-utils/pkg/controller"
	v1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	"github.com/openmcp-project/openmcp-operator/internal/controllers/provider/install"
)

// DeploymentController is not a controller, but a collection of controllers reconciling
// ClusterProviders, ServiceProviders, and PlatformServices.
type DeploymentController struct {
	Reconcilers []*ProviderReconciler
}

func NewDeploymentController() *DeploymentController {
	return &DeploymentController{}
}

// SetupWithManager sets up the controllers with the Manager.
func (r *DeploymentController) SetupWithManager(mgr ctrl.Manager, providerGKVList []schema.GroupVersionKind) error {
	allErrs := field.ErrorList{}

	r.Reconcilers = make([]*ProviderReconciler, len(providerGKVList))

	for i, gvk := range providerGKVList {
		r.Reconcilers[i] = &ProviderReconciler{
			GroupVersionKind: gvk,
			PlatformClient:   mgr.GetClient(),
		}

		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk)

		err := ctrl.NewControllerManagedBy(mgr).
			For(obj).
			Named(r.Reconcilers[i].ControllerName()).
			Watches(&v1.Job{},
				handler.EnqueueRequestsFromMapFunc(r.Reconcilers[i].HandleJob),
				builder.WithPredicates(controller.HasAnnotationPredicate(install.ProviderKindLabel, "")),
				builder.WithPredicates(controller.HasAnnotationPredicate(install.ProviderNameLabel, ""))).
			Complete(r.Reconcilers[i])
		if err != nil {
			allErrs = append(allErrs, field.InternalError(field.NewPath(gvk.String()), err))
		}
	}

	return allErrs.ToAggregate()
}
