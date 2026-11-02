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

// Package webhook implements an admission webhook that validates the versions
// embedded in provider resources and CO service resources against the catalog
// published on the onboarding cluster.
//
// The webhook is optional and disabled by default; enable with --enable-version-webhook.
package webhook

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/openmcp-project/controller-utils/pkg/logging"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// VersionValidatingWebhook validates version fields in provider and CO service resources
// against the catalog on the onboarding cluster.
//
// Three resource families are handled:
//
//  1. Provider resources (ClusterProvider, PlatformService, ServiceProvider):
//     validate spec.image tag → lookup by resource name (e.g. "provider-helm").
//
//  2. CO v2 service resources (Flux, Crossplane, KRO, …):
//     validate spec.version → lookup by catalog service name (e.g. "flux").
//     Crossplane additionally validates spec.providers[].version and
//     spec.functions[].version against "provider-<name>" / function name entries.
//
//  3. CO v1 ManagedControlPlane:
//     validate spec.components.{flux,crossplane,kyverno,…}.version and
//     spec.components.crossplane.providers[].version against ManagedComponent entries.
//
// In all cases:
//   - If the catalog entry for the name is not yet known, the request is allowed
//     (catalog may not have synced yet, or the component is not tracked).
//   - If the version is not in the catalog the request is rejected with a message
//     listing the available versions.
type VersionValidatingWebhook struct {
	cache *CatalogCache
	log   logging.Logger
	// providerGVKs is the exact set of GVKs that validate via spec.image
	// (ClusterProvider, PlatformService, ServiceProvider).
	providerGVKs map[schema.GroupVersionKind]struct{}
}

// NewVersionValidatingWebhook creates a new webhook backed by cache.
// providerGVKs must be the exact GVKs that carry spec.image. Any GVK not in
// this set and not ManagedControlPlane is treated as a CO v2 service kind.
func NewVersionValidatingWebhook(cache *CatalogCache, log logging.Logger, providerGVKs ...schema.GroupVersionKind) *VersionValidatingWebhook {
	set := make(map[schema.GroupVersionKind]struct{}, len(providerGVKs))
	for _, g := range providerGVKs {
		set[g] = struct{}{}
	}
	return &VersionValidatingWebhook{
		cache:        cache,
		log:          log.WithName("version-validating-webhook"),
		providerGVKs: set,
	}
}

// Cache returns the underlying CatalogCache.
func (v *VersionValidatingWebhook) Cache() *CatalogCache { return v.cache }

// --- admission.Validator[*unstructured.Unstructured] ---

var _ admission.Validator[*unstructured.Unstructured] = &VersionValidatingWebhook{}

func (v *VersionValidatingWebhook) ValidateCreate(ctx context.Context, obj *unstructured.Unstructured) (admission.Warnings, error) {
	return v.validate(ctx, obj)
}
func (v *VersionValidatingWebhook) ValidateUpdate(ctx context.Context, _, newObj *unstructured.Unstructured) (admission.Warnings, error) {
	return v.validate(ctx, newObj)
}
func (v *VersionValidatingWebhook) ValidateDelete(_ context.Context, _ *unstructured.Unstructured) (admission.Warnings, error) {
	return nil, nil
}

func (v *VersionValidatingWebhook) validate(_ context.Context, obj *unstructured.Unstructured) (admission.Warnings, error) {
	gvk := obj.GroupVersionKind()
	log := v.log.WithValues("kind", gvk.Kind, "name", obj.GetName(), "namespace", obj.GetNamespace())

	var errs []string
	switch {
	case v.isProviderGVK(gvk):
		errs = v.validateProvider(log, obj)
	case isManagedControlPlane(gvk):
		errs = v.validateMCP(log, obj)
	default:
		errs = v.validateService(log, obj, gvk)
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("version validation failed for %s %q:\n  - %s",
			gvk.Kind, obj.GetName(), strings.Join(errs, "\n  - "))
	}
	return nil, nil
}

// --- provider validation (spec.image tag) ---

func (v *VersionValidatingWebhook) validateProvider(log logging.Logger, obj *unstructured.Unstructured) []string {
	image, _, _ := unstructured.NestedString(obj.Object, "spec", "image")
	if image == "" {
		return nil
	}
	tag := imageTag(image)
	if tag == "" {
		return nil // no tag or digest reference — not our concern
	}
	name := obj.GetName()
	return v.checkVersion(log, name, tag)
}

// --- CO v2 service validation (spec.version + Crossplane sub-resources) ---

func (v *VersionValidatingWebhook) validateService(log logging.Logger, obj *unstructured.Unstructured, gvk schema.GroupVersionKind) []string {
	// Catalog lookup name for this GVK.
	catalogName := v.cache.ServiceCatalogName(gvk)
	if catalogName == "" {
		// GVK not in catalog — allow (unknown service type).
		log.Info("Service kind not found in catalog GVK map — allowing")
		return nil
	}

	var errs []string

	// spec.version — the top-level version for this service kind.
	if topVersion, found, _ := unstructured.NestedString(obj.Object, "spec", "version"); found && topVersion != "" {
		errs = append(errs, v.checkVersion(log.WithValues("field", "spec.version"), catalogName, topVersion)...)
	}

	// Crossplane-specific: spec.providers[] and spec.functions[] sub-versions.
	if strings.EqualFold(gvk.Kind, crossplaneKind) {
		errs = append(errs, v.validateCrossplaneSubResources(log, obj)...)
	}

	return errs
}

func (v *VersionValidatingWebhook) validateCrossplaneSubResources(log logging.Logger, obj *unstructured.Unstructured) []string {
	var errs []string
	for _, field := range []string{"providers", "functions"} {
		items, _, _ := unstructured.NestedSlice(obj.Object, "spec", field)
		for _, raw := range items {
			item, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			name, _, _ := unstructured.NestedString(item, "name")
			ver, _, _ := unstructured.NestedString(item, "version")
			if name == "" || ver == "" {
				continue
			}
			// providers use "provider-<name>" if not already prefixed, functions use name as-is.
			catalogName := name
			if field == "providers" && !strings.HasPrefix(name, "provider-") {
				catalogName = "provider-" + name
			}
			errs = append(errs, v.checkVersion(
				log.WithValues("field", "spec."+field, "item", name, "catalogName", catalogName),
				catalogName, ver)...)
		}
	}
	return errs
}

// --- CO v1 ManagedControlPlane validation ---

const crossplaneKind = "Crossplane"      // CO v2 GVK Kind
const crossplaneComponent = "crossplane" // spec.components key and ManagedComponent catalog name

// v1ComponentCatalogName maps spec.components field names to ManagedComponent catalog names.
var v1ComponentCatalogName = map[string]string{
	crossplaneComponent:       crossplaneComponent,
	"flux":                    "flux",
	"kyverno":                 "kyverno",
	"externalSecretsOperator": "external-secrets",
	"btpServiceOperator":      "sap-btp-service-operator",
	// landscaper: no version field in practice — omitted.
}

func (v *VersionValidatingWebhook) validateMCP(log logging.Logger, obj *unstructured.Unstructured) []string {
	components, _, _ := unstructured.NestedMap(obj.Object, "spec", "components")
	if len(components) == 0 {
		return nil
	}

	var errs []string

	for field, catalogName := range v1ComponentCatalogName {
		comp, ok := components[field]
		if !ok {
			continue
		}
		compMap, ok := comp.(map[string]interface{})
		if !ok {
			continue
		}

		// Top-level version for the component.
		if ver, ok := compMap["version"].(string); ok && ver != "" {
			errs = append(errs, v.checkVersion(
				log.WithValues("field", "spec.components."+field+".version"),
				catalogName, ver)...)
		}

		// Crossplane: also validate spec.components.crossplane.providers[].version.
		if field == crossplaneComponent {
			errs = append(errs, v.validateV1CrossplaneProviders(log, compMap)...)
		}
	}

	return errs
}

func (v *VersionValidatingWebhook) validateV1CrossplaneProviders(log logging.Logger, crossplaneMap map[string]interface{}) []string {
	rawProviders, ok := crossplaneMap["providers"].([]interface{})
	if !ok {
		return nil
	}
	var errs []string
	for _, raw := range rawProviders {
		p, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := p["name"].(string)
		ver, _ := p["version"].(string)
		if name == "" || ver == "" {
			continue
		}
		catalogName := name
		if !strings.HasPrefix(name, "provider-") {
			catalogName = "provider-" + name
		}
		errs = append(errs, v.checkVersion(
			log.WithValues("field", "spec.components.crossplane.providers", "item", name, "catalogName", catalogName),
			catalogName, ver)...)
	}
	return errs
}

// checkVersion returns a non-empty slice if catalogName is known but version is absent.
// Returns nil if the entry is not in the catalog (allow unknown) or the version matches.
func (v *VersionValidatingWebhook) checkVersion(log logging.Logger, catalogName, version string) []string {
	if !v.cache.IsKnownProvider(catalogName) {
		log.Info("Catalog entry not found — allowing", "name", catalogName, "version", version)
		return nil
	}
	if v.cache.HasVersion(catalogName, version) {
		return nil
	}
	known := v.cache.KnownVersions(catalogName)
	sort.Strings(known)
	log.Info("Version not in catalog — rejecting", "name", catalogName, "version", version, "available", known)
	return []string{fmt.Sprintf(
		"version %q for %q is not available on this landscape; available versions: [%s]",
		version, catalogName, strings.Join(known, ", "),
	)}
}

// --- GVK helpers ---

var mcpGroup = "core.openmcp.cloud"
var mcpKind = "ManagedControlPlane"

// isProviderGVK reports whether gvk is one of the platform-cluster provider kinds
// that carry spec.image (registered at construction time).
func (v *VersionValidatingWebhook) isProviderGVK(gvk schema.GroupVersionKind) bool {
	_, ok := v.providerGVKs[gvk]
	return ok
}

func isManagedControlPlane(gvk schema.GroupVersionKind) bool {
	return gvk.Group == mcpGroup && gvk.Kind == mcpKind
}

// imageTag extracts the version tag from a container image reference.
// Returns "" for digest refs (image@sha256:…) and tag-less refs.
func imageTag(image string) string {
	if strings.Contains(image, "@") {
		return ""
	}
	lastSlash := strings.LastIndex(image, "/")
	segment := image[lastSlash+1:]
	idx := strings.LastIndex(segment, ":")
	if idx < 0 {
		return ""
	}
	return segment[idx+1:]
}

// WebhookPath returns the standard admission webhook path for a GVK.
// Format: /validate-<group-dots-to-dashes>-<version>-<kind-lower>
func WebhookPath(gvk schema.GroupVersionKind) string {
	group := strings.ReplaceAll(gvk.Group, ".", "-")
	return fmt.Sprintf("/validate-%s-%s-%s", group, gvk.Version, strings.ToLower(gvk.Kind))
}

// SetupWithManager registers the webhook for each GVK on mgr's webhook server.
func (v *VersionValidatingWebhook) SetupWithManager(mgr ctrl.Manager, gvks []schema.GroupVersionKind) error {
	server := mgr.GetWebhookServer()
	for _, gvk := range gvks {
		hook := admission.WithValidator[*unstructured.Unstructured](mgr.GetScheme(), v)
		path := WebhookPath(gvk)
		v.log.Info("Registering version-validating webhook", "gvk", gvk.String(), "path", path)
		server.Register(path, hook)
	}
	return nil
}
