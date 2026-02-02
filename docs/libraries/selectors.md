# Cluster Selectors

The `clusters` API package features multiple selector API types which can be used in other API types (e.g. config resource definitions) to specify rules to filter `Cluster` (and `ClusterRequest`) resources by. This page quickly describes the available selector types and some implementation details that developers should be aware of.

## What can be selected for?

The main purpose of this library is to allow filtering `Cluster` resources for their purposes, as this is a feature required by multiple controllers. To not unnecessarily restrict the possibilities, the library uses an `ObjectWithPurposes` interface, which basically combines the `client.Object` interface with an additional `GetPurposes` method. This interface is currently implemented by the `Cluster` and `ClusterRequest` API types.

Furthermore, there are scenarios in which a selector should be configured to only match specific clusters or ones that have certain labels. To support these use cases, the package not only contains individual selector implementations for all three types, but also combinations for all of them.

## The 'Selector' Interface

The `Selector` interface serves as an abstraction layer in front of the actual implementations. It defines the following methods:
- `Empty() bool` returns true if the selector does not restrict the objects in any way, meaning it matches every possible object.
- `Matches(obj ObjectWithPurposes) bool` returns true if the given object is matched by the selector and false otherwise.
- `Validate() error` validates the selector's integrity. `Matches` does not return an error, but will return an undefined result (mostly `false` for the given implementations) in case of an invalid selector.

## Selector Implementations

All selector types are implemented in a way that allows them to be used as nested inline structs in the parent API types.

### Simple Implementations

#### IdentitySelector

The `IdentitySelector` allows to specify a list of object references - each with `name` and `namespace` - and it matches exactly the referenced objects and nothing else.

⚠️ **Attention:** Opposed to the other implementations, the `IdentitySelector` differentiates between a `nil` list of identities, which matches every object, and an empty one, which does not match any object. The `Empty` method will return true only if the list is nil, not if it is empty.

##### YAML Examples

Matches the specified identities:
```yaml
matchIdentities:
- name: foo
  namespace: bar
- name: asdf
  namespace: default
```

Matches everything:
```yaml
matchIdentities: null
```

Matches nothing:
```yaml
matchIdentities: []
```

#### LabelSelector

The `LabelSelector` is just a wrapper around the API type from the `k8s.io/apimachinery/pkg/apis/meta/v1` package with the same name. Its implementation also just calls the corresponding methods from the `k8s.io/apimachinery/pkg/labels` package's `Selector` implementation.

Valid `operation` values are:
- `In`
- `NotIn`
- `Exists`
- `DoesNotExist`

##### YAML Examples

Matches resources with the specified labels
```yaml
matchLabels:
  foo: bar
  baz: bar
matchExpressions:
- key: mylabel
  operator: NotIn
  values:
  - foo
  - bar
  - baz
```

Matches everything:
```yaml
matchLabels: {}
matchExpressions: []
```

#### PurposeSelector

The `PurposeSelector` API type is designed to look similar to the `matchExpressions` part of the k8s-native label selector. It uses the `GetPurposes` method defined in the `ObjectWithPurposes` interface and matches the returned list of purposes against the specified requirements.

Valid `operation` values are:
- `ContainsAll`
  - Matches if the object's purpose list contains all purposes from the requirement.
- `ContainsAny`
  - Matches if the object's purpose list contains any purpose from the requirement.
- `ContainsNone`
  - Matches if the object's purpose list does not contain any purpose from the requirement.
- `Equals`
  - Matches if the object's purpose list is identical to the purpose list from the requirement. Order of elements does not matter and duplicate elements are ignored.

##### YAML Examples

Matches all objects with the purposes `platform` and `onboarding`:
```yaml
matchPurposes:
- operator: ContainsAll
  values:
  - platform
  - onboarding
```

Matches all objects that have purpose `platform` or `workload`:
```yaml
matchPurposes:
- operator: ContainsAny
  values:
  - platform
  - workload
```

Matches all objects that have `mcp` as their only purpose:
```yaml
matchPurposes:
- operator: Equals
  values:
  - mcp
```

### Aggregate Implementations

To simplify aggregation of multiple of the aforementioned selectors, the package also contains the following aggregate implementations, which combine the respective selectors:
- `IdentityLabelSelector`
- `IdentityPurposeSelector`
- `LabelPurposeSelector`
- `IdentityLabelPurposeSelector`

The selector types contain the fields for all combined selectors, e.g. the `IdentityPurposeSelector` type contains the `matchIdentities` and the `matchPurposes` fields.

An aggregate selector's `Empty` method simply ANDs all of its combined selectors' `Empty` methods.
Similarly, `Validate` returns an error if any of the combined selectors' `Validate` returns an error.

For `Matches`, the logic is slightly more complicated: If the aggregation contains an `IdentitySelector` which is not empty - in its `Empty` method sense of 'empty' - this selector overwrites any other selectors that are part of the aggregation and is the only one evaluated. Otherwise, the return values of all other combined selectors' `Matches` methods are ANDed.

##### YAML Examples

The following example of an `IdentityLabelSelector` has a non-empty identity selector and therefore ignores its label selector. It will always exactly match the object with name `myobject` in the `default` namespace, independent of its labels and the existence of any other objects.
```yaml
matchIdentities:
- name: myobject
  namespace: default
matchLabels:
  foo: bar
```

In the following example of an `IdentityLabelPurposeSelector`, the `matchIdentities` field is not specified, defaulting it to `nil`. The results for the other two selectors are ANDed, meaning this will match all objects that have the purpose `mcp` and the `foo: bar` label. Objects which have more purposes and/or labels in addition to the mentioned ones will also be matched, while objects which only have either the purpose or the label will not, both are required to be present.
```yaml
matchLabels:
  foo: bar
matchPurposes:
- operator: ContainsAny
  values:
  - mcp
```
