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

package controller

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ProviderReconcilerList struct {
	PlatformClient client.Client
	Scheme         *runtime.Scheme
	Reconcilers    []*ProviderReconciler
}

// SetupWithManager sets up the controllers with the Manager.
func (r *ProviderReconcilerList) SetupWithManager(mgr ctrl.Manager, providerGKVList []schema.GroupVersionKind) error {
	allErrs := field.ErrorList{}

	r.Reconcilers = make([]*ProviderReconciler, len(providerGKVList))

	for i, gvk := range providerGKVList {
		r.Reconcilers[i] = &ProviderReconciler{
			Name:             gvk.String(),
			GroupVersionKind: gvk,
			Client:           mgr.GetClient(),
		}

		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk)

		err := ctrl.NewControllerManagedBy(mgr).
			For(obj).
			Named(r.Reconcilers[i].Name).
			Complete(r.Reconcilers[i])
		if err != nil {
			allErrs = append(allErrs, field.InternalError(field.NewPath(gvk.String()), err))
		}
	}

	return allErrs.ToAggregate()
}
