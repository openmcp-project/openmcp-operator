# AccessRequest Controller

The _AccessRequest Controller_ is responsible for labelling `AccessRequest` resources with the name of the ClusterProvider that is responsible for them. It also adds a label for the corresponding `ClusterProfile` and adds the cluster reference to the spec, if only the request reference is specified.

This is needed because the information, which ClusterProvider is responsible for answering the `AccessRequest` is contained in the referenced `ClusterProfile`. Depending on `AccessRequest`'s spec, a `Cluster` and potentially also a `ClusterRequest` must be fetched before the `ClusterProfile` is known, which then has to be fetched too. If multiple ClusterProviders are running in the cluster, all of them would need to fetch these resources, only for all but one of them to notice that they are not responsible and don't have to do anything.

To increase performance and simplify reconciliation logic in the individual ClusterProviders, this central AccessRequest controller takes over the task of figuring out the ClusterProfile and the responsible ClusterProvider and it adds these as labels to the `AccessRequest` resource. It reacts only on resources which do not yet have both of these labels set, so it should reconcile each `AccessRequest` only once (excluding repeated reconciliations due to errors).

The added labels are:
```yaml
provider.clusters.openmcp.cloud: <provider-name>
profile.clusters.openmcp.cloud: <profile-name>
```

ClusterProviders should only reconcile `AccessRequest` resources where both labels are set and the value of the provider label matches their own provider name. Resources where either label is missing or the value of the provider label does not match the own provider name must be ignored.

Note that if a reconciled `AccessRequest` already has one of the labels set, but its value differs from the expected one, the controller will log an error, but not update the resource in any way, to not accidentally move the responsibility from one provider to another. This also means that `AccessRequest` resources that have only one of the labels set, and that one to a wrong value, will not be handled - this controller won't update the resource and the ClusterProvider should not pick it up because one of the labels is missing. It is therefore strongly recommended to not set the labels when creating a new `AccessRequest` resource.

In addition to the labels, the controller also sets `spec.clusterRef`, if only `spec.requestRef` is specified.

After an `AccessRequest` has been prepared this way, the ClusterProviders can easily infer which one is responsible, which `Cluster` resource this request belongs to, and which `ClusterProfile` is used by the `Cluster`, directly from the labels and spec of the `AccessRequest` resource.

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
