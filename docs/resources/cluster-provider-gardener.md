# Cluster Provider: Gardener [Resource Example]

```yaml
apiVersion: openmcp.cloud/v1alpha1
kind: ClusterProvider
metadata:
    name: cluster-provider-gardener
spec:
  image: "ghcr.io/openmcp-project/images/cluster-provider-gardener:v0.0.1"
  imagePullSecrets: []
```
