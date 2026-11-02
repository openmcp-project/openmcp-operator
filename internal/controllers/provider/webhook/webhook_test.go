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

package webhook_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openmcp-project/controller-utils/pkg/logging"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/openmcp-project/openmcp-operator/internal/controllers/provider/webhook"
)

var discardLog = logging.Discard()

// newHook is a test helper that returns a fresh cache+hook pair.
func newHook() (*webhook.CatalogCache, *webhook.VersionValidatingWebhook) {
	cache := webhook.NewCatalogCacheForTesting()
	return cache, webhook.NewVersionValidatingWebhook(cache, discardLog, providerGVK)
}

// makeObj creates a minimal Unstructured for the given GVK/name, optionally setting fields.
func makeObj(gvk schema.GroupVersionKind, name string, fields map[string]interface{}) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	obj.SetName(name)
	for path, val := range fields {
		parts := splitPath(path)
		_ = unstructured.SetNestedField(obj.Object, val, parts...)
	}
	return obj
}

func splitPath(path string) []string {
	var out []string
	cur := ""
	for _, c := range path {
		if c == '.' {
			out = append(out, cur)
			cur = ""
		} else {
			cur += string(c)
		}
	}
	return append(out, cur)
}

var providerGVK = schema.GroupVersionKind{Group: "openmcp.cloud", Version: "v1alpha1", Kind: "ClusterProvider"}
var mcpGVK = schema.GroupVersionKind{Group: "core.openmcp.cloud", Version: "v1alpha1", Kind: "ManagedControlPlane"}
var fluxGVK = schema.GroupVersionKind{Group: "flux.services.open-control-plane.io", Version: "v1alpha1", Kind: "Flux"}
var crossplaneGVK = schema.GroupVersionKind{Group: "crossplane.services.open-control-plane.io", Version: "v1alpha1", Kind: "Crossplane"}

var _ = Describe("CatalogCache", func() {
	var cache *webhook.CatalogCache
	BeforeEach(func() { cache = webhook.NewCatalogCacheForTesting() })

	It("returns false for unknown providers", func() {
		Expect(cache.IsKnownProvider("provider-btp")).To(BeFalse())
		Expect(cache.HasVersion("provider-btp", "v0.5.3")).To(BeFalse())
		Expect(cache.KnownVersions("provider-btp")).To(BeNil())
	})

	It("stores and retrieves versions", func() {
		cache.SetVersionsForProvider("provider-btp", []string{"v0.5.3", "v0.5.2"})
		Expect(cache.IsKnownProvider("provider-btp")).To(BeTrue())
		Expect(cache.HasVersion("provider-btp", "v0.5.3")).To(BeTrue())
		Expect(cache.HasVersion("provider-btp", "v0.5.2")).To(BeTrue())
		Expect(cache.HasVersion("provider-btp", "v9.9.9")).To(BeFalse())
	})

	It("normalises v-prefix both ways", func() {
		cache.SetVersionsForProvider("provider-helm", []string{"v1.0.1", "0.21.1"})
		Expect(cache.HasVersion("provider-helm", "1.0.1")).To(BeTrue())   // stored with v, queried without
		Expect(cache.HasVersion("provider-helm", "v0.21.1")).To(BeTrue()) // stored without v, queried with
	})

	It("deletes a provider", func() {
		cache.SetVersionsForProvider("provider-x", []string{"v1"})
		cache.DeleteProvider("provider-x")
		Expect(cache.IsKnownProvider("provider-x")).To(BeFalse())
	})

	It("stores service GVKs from SetManagedServiceCatalog", func() {
		gvk := fluxGVK
		cache.SetManagedServiceCatalog([]webhook.ManagedServiceEntry{
			{Name: "flux", Versions: []string{"v2.18.2"}, GVK: &gvk},
		})
		Expect(cache.ServiceCatalogName(fluxGVK)).To(Equal("flux"))
		Expect(cache.HasVersion("flux", "v2.18.2")).To(BeTrue())
	})

	It("merges v1 and v2 entries without losing v1 data", func() {
		cache.SetVersionsForProvider("provider-btp", []string{"v1.0.0"}) // from v1
		cache.SetManagedServiceCatalog([]webhook.ManagedServiceEntry{
			{Name: "provider-btp", Versions: []string{"v1.1.0"}},
			{Name: "flux", Versions: []string{"v2.18.2"}},
		})
		Expect(cache.HasVersion("provider-btp", "v1.0.0")).To(BeTrue())
		Expect(cache.HasVersion("provider-btp", "v1.1.0")).To(BeTrue())
		Expect(cache.HasVersion("flux", "v2.18.2")).To(BeTrue())
	})
})

var _ = Describe("VersionValidatingWebhook — provider (spec.image)", func() {
	var cache *webhook.CatalogCache
	var hook *webhook.VersionValidatingWebhook
	ctx := context.Background()
	BeforeEach(func() { cache, hook = newHook() })

	It("allows image with no tag", func() {
		cache.SetVersionsForProvider("provider-btp", []string{"v0.5.3"})
		_, err := hook.ValidateCreate(ctx, makeObj(providerGVK, "provider-btp", map[string]interface{}{
			"spec.image": "ghcr.io/org/provider-btp",
		}))
		Expect(err).NotTo(HaveOccurred())
	})

	It("allows empty image", func() {
		_, err := hook.ValidateCreate(ctx, makeObj(providerGVK, "provider-btp", nil))
		Expect(err).NotTo(HaveOccurred())
	})

	It("allows provider not in catalog", func() {
		_, err := hook.ValidateCreate(ctx, makeObj(providerGVK, "provider-unknown", map[string]interface{}{
			"spec.image": "ghcr.io/org/provider-unknown:v9.9.9",
		}))
		Expect(err).NotTo(HaveOccurred())
	})

	It("allows a valid version", func() {
		cache.SetVersionsForProvider("provider-btp", []string{"v0.5.3", "v0.5.2"})
		_, err := hook.ValidateCreate(ctx, makeObj(providerGVK, "provider-btp", map[string]interface{}{
			"spec.image": "ghcr.io/org/provider-btp:v0.5.3",
		}))
		Expect(err).NotTo(HaveOccurred())
	})

	It("rejects an unavailable version with helpful message", func() {
		cache.SetVersionsForProvider("provider-btp", []string{"v0.5.3", "v0.5.2"})
		_, err := hook.ValidateCreate(ctx, makeObj(providerGVK, "provider-btp", map[string]interface{}{
			"spec.image": "ghcr.io/org/provider-btp:v0.5.1",
		}))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("v0.5.1"))
		Expect(err.Error()).To(ContainSubstring("provider-btp"))
		Expect(err.Error()).To(And(ContainSubstring("v0.5.2"), ContainSubstring("v0.5.3")))
	})

	It("allows digest references", func() {
		cache.SetVersionsForProvider("provider-btp", []string{"v0.5.3"})
		_, err := hook.ValidateCreate(ctx, makeObj(providerGVK, "provider-btp", map[string]interface{}{
			"spec.image": "ghcr.io/org/provider-btp@sha256:abc123",
		}))
		Expect(err).NotTo(HaveOccurred())
	})

	It("handles registry:port/image:tag", func() {
		cache.SetVersionsForProvider("provider-helm", []string{"v1.0.1"})
		_, err := hook.ValidateCreate(ctx, makeObj(providerGVK, "provider-helm", map[string]interface{}{
			"spec.image": "registry.example.com:5000/crossplane/provider-helm:v1.0.1",
		}))
		Expect(err).NotTo(HaveOccurred())
	})

	It("normalises v-prefix mismatch (catalog stores without v)", func() {
		cache.SetVersionsForProvider("provider-ai-core", []string{"0.1.0", "0.1.1"})
		_, err := hook.ValidateCreate(ctx, makeObj(providerGVK, "provider-ai-core", map[string]interface{}{
			"spec.image": "ghcr.io/org/provider-ai-core:v0.1.0",
		}))
		Expect(err).NotTo(HaveOccurred())
	})

	It("allows on update to valid version, rejects on update to invalid", func() {
		cache.SetVersionsForProvider("provider-btp", []string{"v0.5.3"})
		old := makeObj(providerGVK, "provider-btp", map[string]interface{}{"spec.image": "img:v0.5.2"})
		newOk := makeObj(providerGVK, "provider-btp", map[string]interface{}{"spec.image": "img:v0.5.3"})
		newBad := makeObj(providerGVK, "provider-btp", map[string]interface{}{"spec.image": "img:v9.9.9"})
		_, err := hook.ValidateUpdate(ctx, old, newOk)
		Expect(err).NotTo(HaveOccurred())
		_, err = hook.ValidateUpdate(ctx, old, newBad)
		Expect(err).To(HaveOccurred())
	})

	It("always allows delete", func() {
		cache.SetVersionsForProvider("provider-btp", []string{"v0.5.3"})
		obj := makeObj(providerGVK, "provider-btp", map[string]interface{}{"spec.image": "img:v0.5.3"})
		_, err := hook.ValidateDelete(ctx, obj)
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("VersionValidatingWebhook — CO v2 service (spec.version)", func() {
	var cache *webhook.CatalogCache
	var hook *webhook.VersionValidatingWebhook
	ctx := context.Background()
	BeforeEach(func() { cache, hook = newHook() })

	It("allows when service GVK not in catalog", func() {
		_, err := hook.ValidateCreate(ctx, makeObj(fluxGVK, "my-flux", map[string]interface{}{
			"spec.version": "v9.9.9",
		}))
		Expect(err).NotTo(HaveOccurred())
	})

	It("allows a valid version", func() {
		gvk := fluxGVK
		cache.SetManagedServiceCatalog([]webhook.ManagedServiceEntry{
			{Name: "flux", Versions: []string{"v2.18.2"}, GVK: &gvk},
		})
		_, err := hook.ValidateCreate(ctx, makeObj(fluxGVK, "my-flux", map[string]interface{}{
			"spec.version": "v2.18.2",
		}))
		Expect(err).NotTo(HaveOccurred())
	})

	It("rejects an unavailable version", func() {
		gvk := fluxGVK
		cache.SetManagedServiceCatalog([]webhook.ManagedServiceEntry{
			{Name: "flux", Versions: []string{"v2.18.2"}, GVK: &gvk},
		})
		_, err := hook.ValidateCreate(ctx, makeObj(fluxGVK, "my-flux", map[string]interface{}{
			"spec.version": "v1.0.0",
		}))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("v1.0.0"))
		Expect(err.Error()).To(ContainSubstring("flux"))
	})

	It("allows when spec.version is absent", func() {
		gvk := fluxGVK
		cache.SetManagedServiceCatalog([]webhook.ManagedServiceEntry{
			{Name: "flux", Versions: []string{"v2.18.2"}, GVK: &gvk},
		})
		_, err := hook.ValidateCreate(ctx, makeObj(fluxGVK, "my-flux", nil))
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("VersionValidatingWebhook — CO v2 Crossplane (spec.providers/functions)", func() {
	var cache *webhook.CatalogCache
	var hook *webhook.VersionValidatingWebhook
	ctx := context.Background()
	BeforeEach(func() { cache, hook = newHook() })

	buildCrossplane := func(version string, providers, functions []interface{}) *unstructured.Unstructured {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(crossplaneGVK)
		obj.SetName("my-crossplane")
		gvk := crossplaneGVK
		cache.SetManagedServiceCatalog([]webhook.ManagedServiceEntry{
			{Name: "crossplane", Versions: []string{"v2.3.3"}, GVK: &gvk},
		})
		spec := map[string]interface{}{}
		if version != "" {
			spec["version"] = version
		}
		if providers != nil {
			spec["providers"] = providers
		}
		if functions != nil {
			spec["functions"] = functions
		}
		_ = unstructured.SetNestedMap(obj.Object, spec, "spec")
		return obj
	}

	It("allows valid crossplane version + providers", func() {
		cache.SetVersionsForProvider("provider-btp", []string{"v2.2.2"})
		cache.SetVersionsForProvider("provider-helm", []string{"v1.0.1"})
		obj := buildCrossplane("v2.3.3", []interface{}{
			map[string]interface{}{"name": "provider-btp", "version": "v2.2.2"},
			map[string]interface{}{"name": "provider-helm", "version": "v1.0.1"},
		}, nil)
		_, err := hook.ValidateCreate(ctx, obj)
		Expect(err).NotTo(HaveOccurred())
	})

	It("rejects invalid crossplane top-level version", func() {
		obj := buildCrossplane("v9.9.9", nil, nil)
		_, err := hook.ValidateCreate(ctx, obj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("crossplane"))
	})

	It("rejects invalid provider version inside crossplane spec", func() {
		cache.SetVersionsForProvider("provider-btp", []string{"v2.2.2"})
		obj := buildCrossplane("v2.3.3", []interface{}{
			map[string]interface{}{"name": "provider-btp", "version": "v9.9.9"},
		}, nil)
		_, err := hook.ValidateCreate(ctx, obj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("provider-btp"))
	})

	It("adds provider- prefix when looking up short name", func() {
		// provider stored in catalog as "provider-helm"; MCP stores short name "helm"
		cache.SetVersionsForProvider("provider-helm", []string{"v1.0.1"})
		obj := buildCrossplane("v2.3.3", []interface{}{
			map[string]interface{}{"name": "helm", "version": "v1.0.1"},
		}, nil)
		_, err := hook.ValidateCreate(ctx, obj)
		Expect(err).NotTo(HaveOccurred())
	})

	It("allows unknown provider (not in catalog)", func() {
		obj := buildCrossplane("v2.3.3", []interface{}{
			map[string]interface{}{"name": "provider-exotic", "version": "v0.0.1"},
		}, nil)
		_, err := hook.ValidateCreate(ctx, obj)
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("VersionValidatingWebhook — CO v1 ManagedControlPlane", func() {
	var cache *webhook.CatalogCache
	var hook *webhook.VersionValidatingWebhook
	ctx := context.Background()
	BeforeEach(func() { cache, hook = newHook() })

	buildMCP := func(components map[string]interface{}) *unstructured.Unstructured {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(mcpGVK)
		obj.SetName("my-mcp")
		_ = unstructured.SetNestedMap(obj.Object, components, "spec", "components")
		return obj
	}

	It("allows all valid component versions", func() {
		cache.SetVersionsForProvider("crossplane", []string{"1.20.5"})
		cache.SetVersionsForProvider("flux", []string{"2.18.2"})
		cache.SetVersionsForProvider("kyverno", []string{"3.7.1"})
		obj := buildMCP(map[string]interface{}{
			"crossplane": map[string]interface{}{"version": "1.20.5"},
			"flux":       map[string]interface{}{"version": "2.18.2"},
			"kyverno":    map[string]interface{}{"version": "3.7.1"},
		})
		_, err := hook.ValidateCreate(ctx, obj)
		Expect(err).NotTo(HaveOccurred())
	})

	It("rejects invalid flux version", func() {
		cache.SetVersionsForProvider("flux", []string{"2.18.2"})
		obj := buildMCP(map[string]interface{}{
			"flux": map[string]interface{}{"version": "1.0.0"},
		})
		_, err := hook.ValidateCreate(ctx, obj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("flux"))
		Expect(err.Error()).To(ContainSubstring("1.0.0"))
	})

	It("validates crossplane top-level version and providers", func() {
		cache.SetVersionsForProvider("crossplane", []string{"1.20.5"})
		cache.SetVersionsForProvider("provider-btp", []string{"1.9.0"})
		obj := buildMCP(map[string]interface{}{
			"crossplane": map[string]interface{}{
				"version": "1.20.5",
				"providers": []interface{}{
					map[string]interface{}{"name": "btp", "version": "1.9.0"},
				},
			},
		})
		_, err := hook.ValidateCreate(ctx, obj)
		Expect(err).NotTo(HaveOccurred())
	})

	It("rejects invalid provider version in MCP crossplane block", func() {
		cache.SetVersionsForProvider("crossplane", []string{"1.20.5"})
		cache.SetVersionsForProvider("provider-btp", []string{"1.9.0"})
		obj := buildMCP(map[string]interface{}{
			"crossplane": map[string]interface{}{
				"version": "1.20.5",
				"providers": []interface{}{
					map[string]interface{}{"name": "btp", "version": "9.9.9"},
				},
			},
		})
		_, err := hook.ValidateCreate(ctx, obj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("provider-btp"))
	})

	It("allows component versions not in catalog (unknown component)", func() {
		// externalSecretsOperator not populated in cache
		obj := buildMCP(map[string]interface{}{
			"externalSecretsOperator": map[string]interface{}{"version": "2.5.0"},
		})
		_, err := hook.ValidateCreate(ctx, obj)
		Expect(err).NotTo(HaveOccurred())
	})

	It("maps btpServiceOperator to sap-btp-service-operator in catalog", func() {
		cache.SetVersionsForProvider("sap-btp-service-operator", []string{"0.6.8"})
		obj := buildMCP(map[string]interface{}{
			"btpServiceOperator": map[string]interface{}{"version": "0.6.8"},
		})
		_, err := hook.ValidateCreate(ctx, obj)
		Expect(err).NotTo(HaveOccurred())
	})

	It("reports multiple errors in one rejection", func() {
		cache.SetVersionsForProvider("flux", []string{"2.18.2"})
		cache.SetVersionsForProvider("kyverno", []string{"3.7.1"})
		obj := buildMCP(map[string]interface{}{
			"flux":    map[string]interface{}{"version": "1.0.0"},
			"kyverno": map[string]interface{}{"version": "1.0.0"},
		})
		_, err := hook.ValidateCreate(ctx, obj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("flux"))
		Expect(err.Error()).To(ContainSubstring("kyverno"))
	})
})
