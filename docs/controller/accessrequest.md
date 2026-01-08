# AccessRequest Controller

The _AccessRequest Controller_ handles some tasks for all AccessRequests so that not every ClusterProvider's AccessRequest controller has to do them. In short, it
- adds labels for the responsible ClusterProvider and the related profile
- sets `spec.clusterRef` if only `spec.requestRef` is set
- sets the AccessRequest's phase to `Pending` when something has changed (and after initial creation)
- deletes AccessRequests whose TTL has expired

The labelling is needed because the information, which ClusterProvider is responsible for answering the `AccessRequest` is contained in the referenced `ClusterProfile`. Depending on `AccessRequest`'s spec, a `Cluster` and potentially also a `ClusterRequest` must be fetched before the `ClusterProfile` is known, which then has to be fetched too. If multiple ClusterProviders are running in the cluster, all of them would need to fetch these resources, only for all but one of them to notice that they are not responsible and don't have to do anything.

To increase performance and simplify reconciliation logic in the individual ClusterProviders, this central AccessRequest controller takes over the task of figuring out the ClusterProfile and the responsible ClusterProvider and it adds these as labels to the `AccessRequest` resource. It reacts only on resources which do not yet have both of these labels set, so it should reconcile each `AccessRequest` only once (excluding repeated reconciliations due to errors).

The added labels are:
```yaml
clusters.openmcp.cloud/provider: <provider-name>
clusters.openmcp.cloud/profile: <profile-name>
```

In addition to the labels, the controller also sets `spec.clusterRef`, if only `spec.requestRef` is specified.

### Determining Responsibility

The library's `utils` package contains a function named `IsClusterProviderResponsibleForAccessRequest` which takes an `AccessRequest` and a ClusterProvider's name and returns whether the ClusterProvider should reconcile the `AccessRequest` or not. A ClusterProvider must only reconcile an `AccessRequest` if this function returns `true`.

See the description of the function for further details on how the responsibility is determined.

## Configuration

The AccessRequest controller is run as long as `accessrequest` is included in the `--controllers` flag. It is included by default.

The entire configuration for the AccessRequest controller is optional.
```yaml
accessRequest:              # optional
  selector:                 # optional
    matchLabels: <...>      # optional
    matchExpressions: <...> # optional
```

The following fields can be specified inside the `accessRequest` node:
- `selector` _(optional)_
  - A standard k8s label selector, as it is also used in Deployments, for example. If specified, only `AccessRequest` resources matching the selector are reconciled by the controller. This can be used to distribute resources between multiple instances of the AccessRequest controller watching the same cluster.
