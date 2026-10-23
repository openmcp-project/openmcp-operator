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

// validate-versions is a one-shot CLI tool that runs the VersionValidatingWebhook
// logic against a local dump of onboarding-cluster resources.
//
// Usage:
//
//	go run ./cmd/validate-versions --dump /tmp/onboarding-dump
//
// Reads from the dump directory:
//   - catalog.json             ManagedService "catalog" (v2 versions + GVKs)
//   - managedcomponents.json   ManagedComponent list (v1 versions)
//   - <resource>.json          one file per service kind and managedcontrolplanes
//
// Reports every version violation found.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/openmcp-project/controller-utils/pkg/logging"
	"github.com/openmcp-project/openmcp-operator/internal/controllers/provider/webhook"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func main() {
	dump := flag.String("dump", "/tmp/onboarding-dump", "Directory with JSON dumps from the onboarding cluster")
	flag.Parse()

	log := logging.Discard()

	// ---- 1. Build catalog cache ----

	cache := webhook.NewCatalogCacheForTesting()

	// v1: ManagedComponent list
	mcList, err := loadList(filepath.Join(*dump, "managedcomponents.json"))
	if err != nil {
		fatalf("load managedcomponents.json: %v", err)
	}
	for _, mc := range mcList {
		versions, _, _ := unstructured.NestedStringSlice(mc.Object, "status", "versions")
		cache.SetVersionsForProvider(mc.GetName(), versions)
	}
	fmt.Printf("Loaded %d ManagedComponent catalog entries\n", len(mcList))

	// v2: ManagedService catalog
	catalogObj, err := loadSingle(filepath.Join(*dump, "catalog.json"))
	if err != nil {
		fatalf("load catalog.json: %v", err)
	}
	entries := extractCatalogEntries(catalogObj)
	cache.SetManagedServiceCatalog(entries)
	fmt.Printf("Loaded %d ManagedService catalog entries\n", len(entries))
	fmt.Printf("Service GVKs discovered: %d\n\n", len(cache.ServiceGVKs()))

	// No platform-cluster provider GVKs in this dump — only service + MCP kinds.
	hook := webhook.NewVersionValidatingWebhook(cache, log)

	// ---- 2. Validate all resources ----

	type violation struct {
		resource string
		errors   []string
	}
	var violations []violation
	var totalResources int

	validate := func(obj *unstructured.Unstructured) {
		totalResources++
		_, verr := hook.ValidateCreate(context.Background(), obj)
		if verr != nil {
			gvk := obj.GroupVersionKind()
			ref := fmt.Sprintf("%s/%s/%s", gvk.Kind, obj.GetNamespace(), obj.GetName())
			violations = append(violations, violation{
				resource: ref,
				errors:   strings.Split(strings.TrimSpace(verr.Error()), "\n"),
			})
		}
	}

	// CO v2 service kinds
	for _, gvk := range cache.ServiceGVKs() {
		items, err := loadList(filepath.Join(*dump, gvkFilename(gvk)))
		if err != nil {
			continue // file not present → no resources of this kind
		}
		for i := range items {
			items[i].SetGroupVersionKind(gvk)
			validate(&items[i])
		}
	}

	// CO v1 ManagedControlPlanes
	mcpGVK := schema.GroupVersionKind{Group: "core.openmcp.cloud", Version: "v1alpha1", Kind: "ManagedControlPlane"}
	mcps, err := loadList(filepath.Join(*dump, "managedcontrolplanes.json"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "warn: %v\n", err)
	}
	for i := range mcps {
		mcps[i].SetGroupVersionKind(mcpGVK)
		validate(&mcps[i])
	}

	// ---- 3. Report ----

	fmt.Printf("Validated %d resources\n\n", totalResources)

	if len(violations) == 0 {
		fmt.Println("✓ All resources pass version validation")
		return
	}

	sort.Slice(violations, func(i, j int) bool { return violations[i].resource < violations[j].resource })

	fmt.Printf("✗ %d violation(s) found:\n\n", len(violations))
	for _, v := range violations {
		fmt.Printf("  %s\n", v.resource)
		for _, line := range v.errors {
			fmt.Printf("    %s\n", line)
		}
		fmt.Println()
	}
	os.Exit(1)
}

// ---- helpers ----

func loadList(path string) ([]unstructured.Unstructured, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var list unstructured.UnstructuredList
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", filepath.Base(path), err)
	}
	return list.Items, nil
}

func loadSingle(path string) (*unstructured.Unstructured, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	obj := &unstructured.Unstructured{}
	if err := json.Unmarshal(data, &obj.Object); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", filepath.Base(path), err)
	}
	return obj, nil
}

// extractCatalogEntries parses the ManagedService "catalog" object.
// Mirrors catalog.go's extractManagedServiceEntries without using unexported functions.
func extractCatalogEntries(obj *unstructured.Unstructured) []webhook.ManagedServiceEntry {
	var entries []webhook.ManagedServiceEntry

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
		if gv, err := schema.ParseGroupVersion(apiVersion); err == nil && kind != "" {
			g := gv.WithKind(kind)
			gvkPtr = &g
		}
		entries = append(entries, webhook.ManagedServiceEntry{
			Name:     name,
			Versions: parseVersionList(item),
			GVK:      gvkPtr,
		})
	}

	for _, field := range []string{"crossplaneProviders", "crossplaneFunctions"} {
		rawList, _, _ := unstructured.NestedSlice(obj.Object, "spec", field)
		for _, raw := range rawList {
			item, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			name, _, _ := unstructured.NestedString(item, "name")
			if name == "" {
				continue
			}
			entries = append(entries, webhook.ManagedServiceEntry{
				Name:     name,
				Versions: parseVersionList(item),
			})
		}
	}
	return entries
}

func parseVersionList(item map[string]interface{}) []string {
	rawVersions, _, _ := unstructured.NestedSlice(item, "versions")
	var out []string
	for _, rv := range rawVersions {
		vm, ok := rv.(map[string]interface{})
		if !ok {
			continue
		}
		if v, _, _ := unstructured.NestedString(vm, "version"); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// gvkFilename maps a GVK to the dump filename (plural resource name + .json).
func gvkFilename(gvk schema.GroupVersionKind) string {
	switch strings.ToLower(gvk.Kind) {
	case "flux":
		return "fluxes.json"
	case "crossplane":
		return "crossplanes.json"
	case "kro":
		return "kroes.json"
	case "kyverno":
		return "kyvernoes.json"
	case "velero":
		return "veleroes.json"
	case "ocm":
		return "ocms.json"
	case "odg":
		return "odgs.json"
	case "landscaper":
		return "landscapers.json"
	case "externalsecretsoperator":
		return "externalsecretsoperators.json"
	case "metricsoperator":
		return "metricsoperators.json"
	case "oteloperator":
		return "oteloperators.json"
	default:
		return strings.ToLower(gvk.Kind) + "s.json"
	}
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(2)
}
