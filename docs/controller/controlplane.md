# ControlPlane

The *ControlPlane Controller* is a platform service that is responsible for reconciling `ControlPlane` (CP) resources.

Out of an CP resource, it generates a `ClusterRequest` and multiple `AccessReqests`, thereby handling cluster management and authentication/authorization for MCPs.

## Configuration

The CP controller takes the following configuration:
```yaml
managedControlPlane:
  mcpClusterPurpose: mcp # defaults to 'mcp'
  reconcileMCPEveryXDays: 7 # defaults to 0
  defaultOIDCProvider:
    name: openmcp # defaults to 'openmcp' when omitted
    issuer: https://oidc.example.com
    clientID: my-client-id
    extraScopes:
    - foo
  exposedEndpoints: # list of endpoint names from the Cluster status which should be forwarded to the MCP status
  - name: internal
  - name: external
    renameTo: apiserver-external # optional, defaults to name
```

The configuration is optional.

## ControlPlane

This is an example CP resource:
```yaml
apiVersion: core.open-control-plane.io/v2alpha1
kind: ControlPlane
metadata:
  name: mcp-01
  namespace: foo
spec:
  iam:
    oidc:
      defaultProvider:
        roleBindings: # this sets the role bindings for the default OIDC provider (no effect if none is configured)
        - subjects:
          - kind: User
            name: john.doe@example.com
          roleRefs:
          - kind: ClusterRole
            name: cluster-admin
            
        extraProviders: # here, additional OIDC providers can be configured
        - name: my-oidc-provider
          issuer: https://oidc.example.com
          clientID: my-client-id
          extraScopes:
          - foo
          roleBindings:
          - subjects:
            - kind: User
              name: foo
            - kind: Group
              name: bar
            roleRefs:
            - kind: ClusterRole
              name: my-cluster-role
            - kind: Role
              name: my-role
              namespace: default
              
      tokens: # here, static tokens can be configured
      - name: admin # this token will be named 'admin' and must be unique per CP
        # roleRefs and permissions can be either set individually or together
        roleRefs: # this sets the role bindings for the static token named 'admin'
          - kind: ClusterRole
            name: cluster-admin
        permissions: # here, additional permissions can be configured
          - rules:
              - apiGroups: [ '' ]
                resources: [ 'secretcs']
                verbs: [ '*' ]
      - name: viewer
        permissions:
          - rules:
              - apiGroups: [ '' ]
                resources: [ 'pods', 'services' ]
                verbs: [ 'get', 'list', 'watch' ]

```

### Purpose Overriding

Usually, an CP resource results in a `ClusterRequest` with its `spec.purpose` set to whatever is configured in the CP controller configuration (defaults to `mcp` if not specified). The `core.openmcp.cloud/purpose` label allows to override this setting and specify a different purpose for a single CP.

Note that the purpose cannot be changed anymore after creation of the `ClusterRequest`, therefore the label has to be present already during creation of the CP resource, it cannot be added afterwards.

Also, it is not verified whether the chosen purpose actually is known to the scheduler. Specifying a unknown purpose will result in the CP resource never becoming ready.

#### Validation

During setup, the CP controller deploys a `ValidatingAdmissionPolicy` for the aforementioned label. It has the following effects:
- The label cannot be added or removed to/from an existing CP resource.
- The label's value cannot be changed.
- The label's value must contain the substring `mcp`.
  - This is meant to prevent customers (who have access to this label) from hijacking cluster purposes that are not meant for CP clusters.

This validation is currently not configurable in any way.

### Force Deletion

Sometimes it happens that a `ControlPlane` gets stuck in deletion due to not all services being able to successfully delete their corresponding instances. This check is meant to prevent leaking external resources, e.g. DNS records, which are managed by the service instances and not properly cleaned up if the instances are not waited for. However, there are situations in which the end-user that owns the `ControlPlane` knows that there are no external resources and/or just wants the `ControlPlane` to be gone.

The force-delete annotation `open-control-plane.io/force-delete` can be used to enforce finalizer deletion on service instances, leading to their immediate deletion. It can be used in two variants, which are explained below.

> [!CAUTION]
> By removing the finalizers from the service instance resources, proper cleanup of the services is disrupted. This can lead to leaked external resources.

#### Direct

```yaml
open-control-plane.io/force-delete: "true"
```
With value `true`, the force deletion is triggered immedately when the `ControlPlane` is deleted.

#### Timed

```yaml
open-control-plane.io/force-delete: 1h
```
With a duration as value, the force deletion is triggered this amount of time after the `ControlPlane` deletion got triggered. As an example, if the `ControlPlane` has a deletion timestamp showing 3pm and a force-delete annotation with value `1h`, the service instance finalizers will be removed at 4pm.

If the annotation is added to an already deleted `ControlPlane`, with a duration that has already elapsed since the `ControlPlane`'s deletion, the effect is the same as being added with value `true` and the force deletion is triggered immediately.

> [!NOTE]
> The duration must be parsable by [golang's `ParseDuration`](https://pkg.go.dev/time#ParseDuration).
