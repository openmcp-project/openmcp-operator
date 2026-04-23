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
