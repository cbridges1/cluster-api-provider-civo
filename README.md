# cluster-api-provider-civo

A [Cluster API](https://cluster-api.sigs.k8s.io) provider for
[Civo](https://www.civo.com) managed Kubernetes (K3s or Talos), built on
Civo's Go SDK. It does what [hyve-civo-module](https://github.com/cbridges1/hyve-civo-module)
did: create the cluster with one node pool, publish its kubeconfig and
endpoint for Cluster API, scale the pool, and on delete reclaim the volumes
(PVCs first — deleting the cluster abandons them), delete the cluster, then
delete the firewall Civo created for it. Works with
[Rancher Turtles](https://turtles.docs.rancher.com) and [herd](https://github.com/cbridges1/herd).

## Kinds (`infrastructure.cluster.x-k8s.io/v1alpha1`)

| Kind | What it is |
|---|---|
| `CivoCluster` | `region`, `credentialsRef` (a Secret with `token`), optional `networkID`. Provisioned once the API key works there. |
| `CivoControlPlane` | The managed cluster: `version` (from the Cluster's topology, matched to Civo's labels; or `civoVersion`), `clusterType` (k3s/talos), `cniPlugin` (flannel/cilium), `nodeSize`, `nodeCount`, `applications`, `disableTraefik`, `disableMetricsServer`, optional `firewallID`. |

Both have `…Template` kinds; see [templates/clusterclass-civo.yaml](templates/clusterclass-civo.yaml).
A Civo cluster with the Cluster's name is adopted rather than duplicated.

## Install

```yaml
apiVersion: turtles-capi.cattle.io/v1alpha1
kind: CAPIProvider
metadata: {name: civo, namespace: capcivo-system}
spec:
  name: civo
  type: infrastructure
  version: v0.1.0
  fetchConfig:
    url: https://github.com/cbridges1/cluster-api-provider-civo/releases/v0.1.0/infrastructure-components.yaml
```

```bash
kubectl -n capi-clusters create secret generic civo-credentials --from-literal=token=...
kubectl -n capi-clusters apply -f templates/clusterclass-civo.yaml
herd cluster create web --class civo --set region=NYC1 --set nodeSize=g4s.kube.small --set nodeCount=1
```

## Develop

`go test ./...` (the reconciler runs against an in-memory Civo API);
`hack/components.py IMAGE` writes the release artifacts to `out/`.
