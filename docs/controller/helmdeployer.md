# HelmDeployer

The *HelmDeployer* is a platform service that watches `HelmDeployment` resources.
It depends on a [flux](https://fluxcd.io/) instance running on the platform cluster and can deploy arbitrary helm charts onto selected clusters (represented by `Cluster` resources) by creating fitting `HelmRelease` resources on the platform cluster, which are then taken care of by flux.

## Configuration

The HelmDeployer platform service searches for a `HelmDeployerConfig` resource with the same name as the `PlatformService`. The resource is cluster-scoped and looks like this:
```yaml
apiVersion: helm.open-control-plane.io/v1alpha1
kind: HelmDeployerConfig
metadata:
  name: helmdeploy
spec:
  helmReleaseReconciliationIntervals: # optional
    default: 1h # optional, defaults to 1h
    helm: 1h # optional, defaults to default above
    git: 1h # optional, defaults to default above
    oci: 1h # optional, defaults to default above
  selectorDefinitions: # optional
    mcp-and-workload: # arbitrary identifier
      # cluster selector
      matchPurposes:
      - operator: ContainsAny
        values:
        - mcp
        - workload
      secretsToCopy: # optional
        toPlatformCluster: # optional
        - source:
            name: secret-cfg-1
        - source:
            name: secret-cfg-2
          target: # optional, defaults to source
            name: copied-secret-cfg-2
        toTargetCluster: # optional
        - source:
            name: secret-cfg-3
        - source:
            name: secret-cfg-4
          target: # optional, defaults to source
            name: copied-secret-cfg-4
```

> [!IMPORTANT]
> Opposed to most other platform services, the config for the HelmDeployer is **optional**.

`spec.helmReleaseReconciliationIntervals` specifies the value that will end up in `spec.interval` of the created `HelmRelease` resources, determining in which interval flux reconciles the `HelmRelease`. The struct itself is optional, as are all of its fields. The `helm`, `git`, and `oci` fields can be used to specify default reconciliation intervals depending on the helm chart source kind, with all of them defaulting to the value from `default`, if not specified. 

Via `spec.selectorDefinitions`, pre-defined cluster selectors can be created. While cluster selectors can also be specified in the `HelmDeployment` resources directly, the advantage of a shared definition is that it reduces code duplication and makes it easy to use commonly used selectors, as the 'select all clusters with purposes `mcp` and/or `workload` from the example above.
See [here](../libraries/selectors.md) for a detailed explanation of the selector API.

Next to selector definitions, secret copying instructions can be placed. They will be explained in a separate paragraph [below](#the-problem-with-secrets).

## HelmDeployments

The primary resource of the HelmDeployer platform service is a `HelmDeployment`.

```yaml
apiVersion: helm.open-control-plane.io/v1alpha1
kind: HelmDeployment
metadata:
  name: mydeploy
  namespace: default
spec:
  chartSource:
    chartName: <...> # required for git and helm
    oci:
      url: "oci://example.org/repo/charts"
      interval: 10m
  namespace: helm
  selector: # optional, cluster selector
    ref: mcp-and-workload
  secretsToCopy: # optional
    toPlatformCluster: # optional
    - source:
        name: secret-1
    - source:
        name: secret-2
      target: # optional, defaults to source
        name: copied-secret-2
    toTargetCluster: # optional
    - source:
        name: secret-3
    - source:
        name: secret-4
      target: # optional, defaults to source
        name: copied-secret-4
```
#### Helm Chart Source

`spec.chartSource` specifies the source of the helm chart.

The `chartName` field is ignored for `oci` sources, but required for `git`, where it contains the path to the helm chart within the git repository, and `helm`, where it specifies the name of the chart. In both cases, the desired version can be specified by appending it to the name with an `@` as separater, e.g. `chartName: charts/mychart@v1.2.3`. For `oci` sources, there is an explicit `ref.tag` field to specify the version.

Next to `chartName`, it has to contain exactly one of either `git`, `helm` or `oci`. The content of this field will then be used as `spec` for a Flux [`GitRepository`](https://fluxcd.io/flux/components/source/gitrepositories/), [`HelmRepository`](https://fluxcd.io/flux/components/source/helmrepositories/), or [`OCIRepository`](https://fluxcd.io/flux/components/source/ocirepositories/), respectively. Please refer to the flux documentation for further information about the available fields.

#### Namespace

`spec.namespace` is required and specifies the namespace into which the helm chart should be deployed on the target cluster. The namespace will be created (by flux) if it does not exist, but it will not be removed during deletion.

#### Cluster Selector

The `spec.selector` field determines which clusters the referenced helm chart will be deployed onto. In addition to the standard [cluster selector](../libraries/selectors.md) fields, it alternatively takes `ref` field, which specifies a selector definition from the provider configuration. If any of the other selector fields is specified in addition to `ref`, the reference is ignored and only the directly specified selector is used.

When referencing a selector definition, reconciliation will result in an error if there either is no provider configuration or it does not contain a selector reference with the specified name.

> [!CAUTION]
> The `spec.selector` field is optional and an empty selector will match every `Cluster`.

#### Secret Copying

See [below](#the-problem-with-secrets).

## The Problem with Secrets

The HelmDeployer dynamically creates (and deletes) flux' `HelmRelease` resources next to `Cluster` resources, as specified in the `HelmDeployment`s. This creates two scenarios in which additional logic for copying secrets is required:
1. Flux itself might require secrets next to the `HelmRelease` resource. This is the case if any authentication is required to access the helm chart, for example.
2. The helm chart might reference secrets which are expected to exist on the target cluster. A common example for this are image pull secrets.

In both cases, the secrets need to be created dynamically, since a new namespace with a new `Cluster` that fits a `HelmDeployment`'s selector can be created anytime, potentially requiring secrets to be copied into the `Cluster` resource's namespace on the platform cluster (scenario 1) and/or into the designated namespace on the cluster itself (scenario 2).

To solve this problem, the HelmDeployer platform service comes with a built-in secret copying mechanism, which is explained in this section.

The secret copying instructions in the `HelmDeployerConfig` as well as those in the `HelmDeployment` both use the same API, which looks like this:
```yaml
secretsToCopy:
  toPlatformCluster:
  - source:
      name: secret-1
  - source:
      name: secret-2
    target:
      name: copied-secret-2
  toTargetCluster:
  - source:
      name: secret-3
  - source:
      name: secret-4
    target:
      name: copied-secret-4
```

- Each entry in the `toPlatformCluster` list results in one secret being copied into the `Cluster` resource's namespace on the platform cluster.
- Each entry in the `toTargetCluster` list results in one secret being copied onto the target cluster, with its namespace being determined by `spec.namespace` of the `HelmDeployment` that causes the copying.
- `source.name` must always be set to the source secret's name. The namespace depends on where the copying instructions come from:
  - For instructions in the `HelmDeployerConfig`, the source secret has to be in the namespace where the platform service's pod is running.
  - For instructions in the `HelmDeployment`, the source secret has to be in the same namespace as the `HelmDeployment`.
- `target` is optional and can be used to rename a secret (via `target.name`). Otherwise, the source secret's name will be used for the target secret as well.

#### Conflict Resolution

A conflict occurs if multiple secret copying instances refer to the same target secret.

- If multiple copying instructions within the same list refer to the same target, latter ones overwrite earlier ones.
- If an instruction from a `HelmDeployment` refers to the same target as an instruction from an `HelmDeployerConfig`'s selector definition, which the `HelmDeployment` references, the instruction from the `HelmDeployment` overwrites the one from the `HelmDeployerConfig`.
- If multiple `HelmDeployment`s create the same target secret, all but the first one will return an error.
  - Exception: If a `HelmDeployment` tries to create a secret which already exists and is managed by another `HelmDeployment`, but both secrets use the same source secret, then the conflict is simply ignored without raising an error.

#### Caveats

> [!WARNING]
> Since we are [planning](https://github.com/openmcp-project/backlog/issues/577) to implement secret copying as its own platform service, this implementation is rudimentary and to be seen as temporary solution. It therefore ignores some of the more complex problems on the topic of secret copying.
> Most noteworthy: It only creates secrets, but does not delete them again.
