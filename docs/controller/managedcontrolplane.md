# ManagedControlPlane v2

The *ManagedControlPlane v2 Controller* is a platform service that is responsible for reconciling `ManagedControlPlaneV2` (MCP) resources.

Out of an MCP resource, it generates a `ClusterRequest` and multiple `AccessReqests`, thereby handling cluster management and authentication/authorization for MCPs.

## Configuration

The MCP controller takes the following configuration:
```yaml
managedControlPlane:
  mcpClusterPurpose: mcp # defaults to 'mcp'
  reconcileMCPEveryXDays: 7 # defaults to 0
  defaultOIDCProvider:
    name: default # must be 'default' or omitted for the default oidc provider
    issuer: https://oidc.example.com
    clientID: my-client-id
    usernamePrefix: "my-user:"
    groupsPrefix: "my-group:"
    extraScopes:
    - foo
```

The configuration is optional.

## ManagedControlPlaneV2

This is an example MCP resource:
```yaml
apiVersion: core.openmcp.cloud/v2alpha1
kind: ManagedControlPlaneV2
metadata:
  name: mcp-01
  namespace: foo
spec:
  iam:
    roleBindings: # this sets the role bindings for the default OIDC provider (no effect if none is configured)
    - subjects:
      - kind: User
        name: john.doe@example.com
      roleRefs:
      - kind: ClusterRole
        name: cluster-admin
    oidcProviders: # here, additional OIDC providers can be configured
    - name: my-oidc-provider
      issuer: https://oidc.example.com
      clientID: my-client-id
      usernamePrefix: "my-user:"
      groupsPrefix: "my-group:"
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
```

### Purpose Overriding

Usually, an MCP resource results in a `ClusterRequest` with its `spec.purpose` set to whatever is configured in the MCP controller configuration (defaults to `mcp` if not specified). The `core.openmcp.cloud/purpose` label allows to override this setting and specify a different purpose for a single MCP.

Note that the purpose cannot be changed anymore after creation of the `ClusterRequest`, therefore the label has to be present already during creation of the MCP resource, it cannot be added afterwards.

Also, it is not verified whether the chosen purpose actually is known to the scheduler. Specifying a unknown purpose will result in the MCP resource never becoming ready.

#### Validation

During setup, the MCP controller deploys a `ValidatingAdmissionPolicy` for the aforementioned label. It has the following effects:
- The label cannot be added or removed to/from an existing MCP resource.
- The label's value cannot be changed.
- The label's value must contain the substring `mcp`.
  - This is meant to prevent customers (who have access to this label) from hijacking cluster purposes that are not meant for MCP clusters.

This validation is currently not configurable in any way.
