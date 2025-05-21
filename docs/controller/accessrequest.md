# AccessRequest Controller

The _AccessRequest Controller_ is responsible for labelling `AccessRequest` resources with the name of the ClusterProvider that is responsible for them.

This is needed because the information, which ClusterProvider is responsible for answering the `AccessRequest` is contained in the referenced `ClusterProfile`. Depending on `AccessRequest`'s spec, a `Cluster` and potentially also a `ClusterRequest` must be fetched before the `ClusterProfile` is known, which then has to be fetched too. If multiple ClusterProviders are running in the cluster, all of them would need to fetch these resources, only for all but one of them to notice that they are not responsible and don't have to do anything.

To increase performance and simplify reconciliation logic in the individual ClusterProviders, this central AccessRequest controller takes over the task of figuring out the responsible ClusterProvider and adds a `provider.clusters.openmcp.cloud` label with its name to the `AccessRequest` resource. It reacts only on resources which do not yet have this label, so it should reconcile each `AccessRequest` only once (excluding repeated reconciliations due to errors).

ClusterProviders should only reconcile `AccessRequest` resources where the value of the `provider.clusters.openmcp.cloud` label matches their own provider name and ignore resources with other values or if the label is missing completely.

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
