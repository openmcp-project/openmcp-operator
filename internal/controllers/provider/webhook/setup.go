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

package webhook

import (
	"context"
	"fmt"

	"github.com/openmcp-project/controller-utils/pkg/clusters"
	"github.com/openmcp-project/controller-utils/pkg/logging"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// Setup wires up the version-validating webhook on platformMgr.
//
// It does an initial synchronous fetch of the catalog from the onboarding cluster so that
// CO v2 service GVKs and all catalog versions are known before webhook paths are registered.
// It then starts catalog watches (ManagedComponent + ManagedService) on a lightweight secondary
// manager and registers the VersionValidatingWebhook on the platform manager for:
//   - providerGVKs (ClusterProvider, PlatformService, ServiceProvider)
//   - ManagedControlPlane (CO v1)
//   - all CO v2 service GVKs discovered from the catalog
//
// The ValidatingWebhookConfiguration itself is managed externally (Helm/kustomize).
// The operator only serves the TLS handler; it does not create or delete the VWC.
func Setup(
	platformMgr ctrl.Manager,
	onboardingCluster *clusters.Cluster,
	onboardingScheme *runtime.Scheme,
	providerGVKs []schema.GroupVersionKind,
	log logging.Logger,
) error {
	log = log.WithName("version-webhook-setup")

	// Initial synchronous catalog fetch so GVKs are known before webhook registration.
	if err := onboardingCluster.InitializeClient(onboardingScheme); err != nil {
		return fmt.Errorf("failed to initialize onboarding cluster client: %w", err)
	}

	cache := newCatalogCache()
	if err := initialCatalogSync(context.Background(), onboardingCluster.Client(), cache, log); err != nil {
		log.Error(err, "Initial catalog sync failed; webhook will allow all requests until cache is populated")
	}

	// Secondary manager: watches onboarding cluster, no metrics/health/leader-election.
	onboardingMgr, err := ctrl.NewManager(onboardingCluster.RESTConfig(), ctrl.Options{
		Scheme:                 onboardingScheme,
		HealthProbeBindAddress: "0",
		Metrics:                metricsserver.Options{BindAddress: "0"},
		LeaderElection:         false,
	})
	if err != nil {
		return fmt.Errorf("failed to create onboarding manager for catalog watches: %w", err)
	}

	if err := SetupManagedComponentWatcher(onboardingMgr, cache, log); err != nil {
		return fmt.Errorf("failed to setup ManagedComponent watcher: %w", err)
	}
	if err := SetupManagedServiceWatcher(onboardingMgr, cache, log); err != nil {
		return fmt.Errorf("failed to setup ManagedService catalog watcher: %w", err)
	}
	if err := platformMgr.Add(onboardingMgr); err != nil {
		return fmt.Errorf("failed to add onboarding manager to platform manager: %w", err)
	}

	hook := NewVersionValidatingWebhook(cache, log, providerGVKs...)
	allGVKs := buildWebhookGVKs(providerGVKs, cache.ServiceGVKs())
	if err := hook.SetupWithManager(platformMgr, allGVKs); err != nil {
		return fmt.Errorf("failed to setup version-validating webhook: %w", err)
	}

	log.Info("Version-validating webhook setup complete",
		"providerGVKs", gvkStrings(providerGVKs),
		"serviceGVKs", gvkStrings(cache.ServiceGVKs()),
	)
	return nil
}

func initialCatalogSync(ctx context.Context, c client.Client, cache *CatalogCache, log logging.Logger) error {
	catalogObj := &unstructured.Unstructured{}
	catalogObj.SetGroupVersionKind(managedServiceGVK)
	if err := c.Get(ctx, types.NamespacedName{Name: managedServiceCatalog}, catalogObj); err != nil {
		if client.IgnoreNotFound(err) == nil {
			log.Info("ManagedService catalog not found during initial sync")
		} else {
			return fmt.Errorf("failed to fetch ManagedService catalog: %w", err)
		}
	} else {
		entries := extractManagedServiceEntries(catalogObj)
		cache.SetManagedServiceCatalog(entries)
		log.Info("Initial ManagedService catalog sync complete", "entries", len(entries))
	}

	mcList := &unstructured.UnstructuredList{}
	mcList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   managedComponentGroup,
		Version: managedComponentVersion,
		Kind:    managedComponentKind + "List",
	})
	if err := c.List(ctx, mcList); err != nil {
		return fmt.Errorf("failed to list ManagedComponents: %w", err)
	}
	for _, mc := range mcList.Items {
		versions, _, _ := unstructured.NestedStringSlice(mc.Object, "status", "versions")
		cache.SetVersionsForProvider(mc.GetName(), versions)
	}
	log.Info("Initial ManagedComponent sync complete", "count", len(mcList.Items))
	return nil
}

func buildWebhookGVKs(providerGVKs, serviceGVKs []schema.GroupVersionKind) []schema.GroupVersionKind {
	seen := make(map[schema.GroupVersionKind]struct{})
	var all []schema.GroupVersionKind
	add := func(g schema.GroupVersionKind) {
		if _, ok := seen[g]; !ok {
			seen[g] = struct{}{}
			all = append(all, g)
		}
	}
	for _, g := range providerGVKs {
		add(g)
	}
	add(schema.GroupVersionKind{Group: mcpGroup, Version: "v1alpha1", Kind: mcpKind})
	for _, g := range serviceGVKs {
		add(g)
	}
	return all
}
