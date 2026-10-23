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
	"strings"
	"sync"

	"github.com/openmcp-project/controller-utils/pkg/logging"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	// v1 catalog: ManagedComponent CRD on onboarding cluster
	managedComponentGroup   = "core.openmcp.cloud"
	managedComponentVersion = "v1alpha1"
	managedComponentKind    = "ManagedComponent"

	// v2 catalog: ManagedService CR named "catalog" on onboarding cluster
	managedServiceGroup   = "open-control-plane.io"
	managedServiceVersion = "v1"
	managedServiceKind    = "ManagedService"
	managedServiceCatalog = "catalog"
)

var (
	managedComponentGVK = schema.GroupVersionKind{
		Group:   managedComponentGroup,
		Version: managedComponentVersion,
		Kind:    managedComponentKind,
	}
	managedServiceGVK = schema.GroupVersionKind{
		Group:   managedServiceGroup,
		Version: managedServiceVersion,
		Kind:    managedServiceKind,
	}
)

// CatalogCache is a thread-safe in-memory cache populated from the onboarding cluster:
//   - ManagedComponent (v1): name → []version  (providers, crossplane, flux, kyverno, …)
//   - ManagedService "catalog" (v2): same + stores GVK→catalogName mappings for service kinds
//
// All webhook validation decisions read from this cache.
type CatalogCache struct {
	mu sync.RWMutex

	// versions maps catalog entry name → set of valid version strings.
	versions map[string]map[string]struct{}

	// serviceGVKs maps GVK → catalog entry name for CO v2 service kinds
	// (e.g. flux.services.open-control-plane.io/v1alpha1/Flux → "flux").
	serviceGVKs map[schema.GroupVersionKind]string
}

func newCatalogCache() *CatalogCache {
	return &CatalogCache{
		versions:    make(map[string]map[string]struct{}),
		serviceGVKs: make(map[schema.GroupVersionKind]string),
	}
}

// NewCatalogCacheForTesting creates an empty CatalogCache for use in tests.
func NewCatalogCacheForTesting() *CatalogCache { return newCatalogCache() }

// HasVersion reports whether providerName has version in the catalog.
// Normalises leading "v" so "v0.5.3" and "0.5.3" are equivalent.
func (c *CatalogCache) HasVersion(providerName, version string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	versions, ok := c.versions[providerName]
	if !ok {
		return false
	}
	if _, ok := versions[version]; ok {
		return true
	}
	// normalise v-prefix
	if len(version) > 0 && version[0] == 'v' {
		_, ok = versions[version[1:]]
	} else {
		_, ok = versions["v"+version]
	}
	return ok
}

// KnownVersions returns the known versions for providerName, or nil if unknown.
// Versions differing only by a leading "v" are deduplicated; the v-prefixed form is kept.
func (c *CatalogCache) KnownVersions(providerName string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	set, ok := c.versions[providerName]
	if !ok {
		return nil
	}
	seen := make(map[string]struct{}, len(set))
	for v := range set {
		if len(v) > 0 && v[0] != 'v' {
			v = "v" + v
		}
		seen[v] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	return out
}

// IsKnownProvider reports whether providerName has any versions in the cache.
func (c *CatalogCache) IsKnownProvider(providerName string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.versions[providerName]
	return ok
}

// ServiceCatalogName returns the catalog entry name for a CO v2 service GVK,
// or "" if the GVK is not registered in the cache.
func (c *CatalogCache) ServiceCatalogName(gvk schema.GroupVersionKind) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.serviceGVKs[gvk]
}

// ServiceGVKs returns all CO v2 service GVKs known to the cache.
func (c *CatalogCache) ServiceGVKs() []schema.GroupVersionKind {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]schema.GroupVersionKind, 0, len(c.serviceGVKs))
	for gvk := range c.serviceGVKs {
		out = append(out, gvk)
	}
	return out
}

// SetVersionsForProvider replaces the version set for name. Used by the v1 watcher and tests.
func (c *CatalogCache) SetVersionsForProvider(name string, versions []string) {
	set := make(map[string]struct{}, len(versions))
	for _, v := range versions {
		set[v] = struct{}{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.versions[name] = set
}

// DeleteProvider removes a provider entry. Called when a ManagedComponent is deleted.
func (c *CatalogCache) DeleteProvider(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.versions, name)
}

// ManagedServiceEntry is a catalog entry parsed from ManagedService "catalog".
type ManagedServiceEntry struct {
	// Name is the catalog lookup key (e.g. "flux", "crossplane", "provider-helm").
	Name string
	// Versions is the list of valid versions.
	Versions []string
	// GVK is set only for entries in spec.services (non-nil means it is a CO v2 service kind).
	GVK *schema.GroupVersionKind
}

// SetManagedServiceCatalog replaces all v2 catalog data atomically.
// Existing v1 ManagedComponent entries (versions without a GVK) are preserved.
func (c *CatalogCache) SetManagedServiceCatalog(entries []ManagedServiceEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Build new serviceGVKs map from entries that carry a GVK.
	newServiceGVKs := make(map[schema.GroupVersionKind]string)
	for _, e := range entries {
		if e.GVK != nil {
			newServiceGVKs[*e.GVK] = e.Name
		}
		// Merge versions into the existing map.
		existing, ok := c.versions[e.Name]
		if !ok {
			existing = make(map[string]struct{})
			c.versions[e.Name] = existing
		}
		for _, v := range e.Versions {
			existing[v] = struct{}{}
		}
	}
	c.serviceGVKs = newServiceGVKs
}

// ---- v1 reconciler: one reconcile per ManagedComponent ----

type managedComponentReconciler struct {
	client client.Client
	cache  *CatalogCache
	log    logging.Logger
}

func (r *managedComponentReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log := r.log.WithValues("managedcomponent", req.Name)
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(managedComponentGVK)

	if err := r.client.Get(ctx, req.NamespacedName, obj); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return reconcile.Result{}, err
		}
		log.Info("ManagedComponent deleted, removing from cache")
		r.cache.DeleteProvider(req.Name)
		return reconcile.Result{}, nil
	}

	versions, _, _ := unstructured.NestedStringSlice(obj.Object, "status", "versions")
	log.Info("Updating version cache from ManagedComponent", "provider", req.Name, "versions", versions)
	r.cache.SetVersionsForProvider(req.Name, versions)
	return reconcile.Result{}, nil
}

// SetupManagedComponentWatcher registers the ManagedComponent list/watch on mgr.
func SetupManagedComponentWatcher(mgr manager.Manager, cache *CatalogCache, log logging.Logger) error {
	r := &managedComponentReconciler{
		client: mgr.GetClient(),
		cache:  cache,
		log:    log.WithName("managed-component-watcher"),
	}
	mc := &unstructured.Unstructured{}
	mc.SetGroupVersionKind(managedComponentGVK)
	return ctrl.NewControllerManagedBy(mgr).
		For(mc).
		Named("catalog-managed-component").
		Complete(r)
}

// ---- v2 reconciler: watches the single "catalog" ManagedService ----

type managedServiceReconciler struct {
	client client.Client
	cache  *CatalogCache
	log    logging.Logger
}

func (r *managedServiceReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	if req.Name != managedServiceCatalog {
		return reconcile.Result{}, nil
	}
	log := r.log.WithValues("managedservice", req.Name)

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(managedServiceGVK)

	if err := r.client.Get(ctx, types.NamespacedName{Name: managedServiceCatalog}, obj); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return reconcile.Result{}, err
		}
		log.Info("ManagedService catalog not found, skipping")
		return reconcile.Result{}, nil
	}

	entries := extractManagedServiceEntries(obj)
	log.Info("Updating version cache from ManagedService catalog", "entries", len(entries))
	r.cache.SetManagedServiceCatalog(entries)
	return reconcile.Result{}, nil
}

// extractManagedServiceEntries parses all three sections of the ManagedService "catalog":
//   - spec.services          — CO v2 service kinds (Flux, Crossplane, KRO, …); carry a GVK
//   - spec.crossplaneProviders — Crossplane providers (provider-helm, …); no GVK
//   - spec.crossplaneFunctions — Crossplane functions; no GVK
func extractManagedServiceEntries(obj *unstructured.Unstructured) []ManagedServiceEntry {
	var entries []ManagedServiceEntry

	// spec.services — each entry has apiVersion, kind, name, versions[]
	rawServices, _, _ := unstructured.NestedSlice(obj.Object, "spec", "services")
	for _, raw := range rawServices {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		name, _, _ := unstructured.NestedString(item, "name")
		apiVersion, _, _ := unstructured.NestedString(item, "apiVersion")
		kind, _, _ := unstructured.NestedString(item, "kind")
		if name == "" {
			continue
		}
		var gvkPtr *schema.GroupVersionKind
		if apiVersion != "" && kind != "" {
			gv, err := schema.ParseGroupVersion(apiVersion)
			if err == nil {
				gvk := gv.WithKind(kind)
				gvkPtr = &gvk
			}
		}
		versions := parseVersionList(item)
		entries = append(entries, ManagedServiceEntry{Name: name, Versions: versions, GVK: gvkPtr})
	}

	// spec.crossplaneProviders and spec.crossplaneFunctions — name + versions[], no GVK
	for _, fieldPath := range []string{"crossplaneProviders", "crossplaneFunctions"} {
		rawList, _, _ := unstructured.NestedSlice(obj.Object, "spec", fieldPath)
		for _, raw := range rawList {
			item, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			name, _, _ := unstructured.NestedString(item, "name")
			if name == "" {
				continue
			}
			versions := parseVersionList(item)
			entries = append(entries, ManagedServiceEntry{Name: name, Versions: versions})
		}
	}

	return entries
}

// parseVersionList reads a versions:[{version:"…"}] slice from a catalog item map.
func parseVersionList(item map[string]interface{}) []string {
	rawVersions, _, _ := unstructured.NestedSlice(item, "versions")
	var versions []string
	for _, rv := range rawVersions {
		vm, ok := rv.(map[string]interface{})
		if !ok {
			continue
		}
		v, _, _ := unstructured.NestedString(vm, "version")
		if v != "" {
			versions = append(versions, v)
		}
	}
	return versions
}

// SetupManagedServiceWatcher registers the ManagedService "catalog" list/watch on mgr.
func SetupManagedServiceWatcher(mgr manager.Manager, cache *CatalogCache, log logging.Logger) error {
	r := &managedServiceReconciler{
		client: mgr.GetClient(),
		cache:  cache,
		log:    log.WithName("managed-service-watcher"),
	}
	ms := &unstructured.Unstructured{}
	ms.SetGroupVersionKind(managedServiceGVK)
	return ctrl.NewControllerManagedBy(mgr).
		For(ms).
		Named("catalog-managed-service").
		Complete(r)
}

// gvkStrings formats GVKs for logging.
func gvkStrings(gvks []schema.GroupVersionKind) []string {
	s := make([]string, len(gvks))
	for i, g := range gvks {
		s[i] = g.String()
	}
	return s
}

// normaliseName strips a "provider-" prefix only when the catalog has no exact match
// but does have the prefixed form. Used for Crossplane sub-provider lookups where
// the MCP may store "helm" and the catalog stores "provider-helm".
// This is a pure utility — not called at runtime (normalization happens inline in the webhook).
func normaliseName(name string) string {
	if !strings.HasPrefix(name, "provider-") {
		return "provider-" + name
	}
	return name
}
