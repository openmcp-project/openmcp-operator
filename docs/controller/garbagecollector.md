# Garbage Collectors

The openmcp-operator comes with multiple garbage collector modules that try to keep the landscape cleaned up. This document explains the different modules and how they can be configured.

## General Configuration

All garbage collector configuration is under a key `garbageCollector` within the openmcp-operator configuration. Apart from some generic configuration, each garbage collector module has its own configuration in a sub-section under this key.

> [!NOTE]
> As garbage collection is destructive by definition, all garbage collector modules must be explicitly enabled in the configuration.

```yaml
garbageCollector:
  interval: 24h # optional, defaults to 24h

  <module-specific configuration>
```

The following fields are part of the generic configuration:
- **`interval`** _(optional, default `24h`)_ - How often the garbage collection should run. This value serves as a global default for all garbage collector modules. It can be overwritten for individual modules by setting the `interval` field in the module-specific configuration.

## Controlling Garbage Collection

On some rare occasions, garbage collection could be undesirable for a specific resource, e.g. during debugging. This can be handled via the garbage collector annotation `open-control-plane.io/garbage-collector`.

If the annotation is set on a resource with value `ignore`, all garbage collector modules will ignore this resource during garbage collection.

It is also possible to set a timestamp in RFC3339 format as the value of the annotation, then the garbage collector will only interact with the resource when that specified point in time has been reached.

## Modules

#### Common Configuration

A few config fields are available in all garbage collector modules:
- **`interval`** _(optional, falls back to global interval)_ - How often this specific garbage collector module should run. Uses the interval from the generic configuration, if not specified.
- **`enabled`** _(optional, default `false`)_ - Whether this garbage collector module is enabled.

### AccessRequests without Clusters

```yaml
garbageCollector:
  ...
  accessRequestNoCluster:
    interval: ...
    enabled: ...
```

This module searches for `AccessRequest` resources which reference `Cluster` or `ClusterRequest` resources that do not exist. The most likely reason for this is that an `AccessRequest` was not properly cleaned up when its target `Cluster` got removed and is now orphaned, but wrongly configured `AccessRequest`s which never became ready qualify too.
If such an `AccessRequest` is found, the garbage collector deletes it and removes all finalizers from it. Removing the finalizers should be fine - if there is no cluster anymore, there is also no way to clean anything up.

`AccessRequest`s which are excluded by the garbage collector annotation will neither be deleted, nor will their finalizers be removed.

The module takes no configuration apart from the common one.

### Orphaned ClusterRequest Finalizers

```yaml
garbageCollector:
  ...
  orphanedClusterRequestFinalizer:
    interval: ...
    enabled: ...
```

When assigning `ClusterRequest`s to `Cluster`s, the scheduler adds a finalizer to the `Cluster` that is `request.clusters.openmcp.cloud/`, followed by the `ClusterRequest`'s UID. This garbage collector module identifies those finalizers on `Cluster` resources and deletes the ones where no `ClusterRequest` with the specified UID exists anymore.

Orphaned finalizers will never be removed otherwise, because the scheduler only removes them when reconciling the corresponding `ClusterRequest`. This situation could lead to increased cost, because `Cluster`s might not be deleted, even if they are not used anymore, or the scheduler might not schedule further workload onto shared `Cluster`s, because they appear full.

> [!WARNING]
> The garbage collector only removes the finalizer. It cannot identify which workload on the `Cluster` was connected to the deleted `ClusterRequest`, so if not only the finalizer, but also some resources on the cluster itself were leaked, those resources will not be cleaned up by the garbage collector.

No finalizers will be removed from `Cluster` resources which are excluded by the garbage collector annotation.

The module takes no configuration apart from the common one.
