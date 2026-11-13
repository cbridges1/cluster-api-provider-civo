# cluster-api-provider-civo

A [Cluster API](https://cluster-api.sigs.k8s.io) provider for
[Civo](https://www.civo.com) managed Kubernetes (K3s or Talos), built on
Civo's Go SDK. It creates the cluster with its node pools (Cluster API
MachinePools, as for EKS and AKS), publishes its kubeconfig and endpoint
for Cluster API, scales and adds pools, and on delete
reclaims the volumes (PVCs first — deleting the cluster abandons them),
deletes the cluster, then deletes the firewall Civo created for it. Works
with clusterctl, the Cluster API Operator and
[Rancher Turtles](https://turtles.docs.rancher.com).

## Kinds

`infrastructure.cluster.x-k8s.io/v1alpha1`, but for `CivoConfig`
(`bootstrap.cluster.x-k8s.io/v1alpha1`):

| Kind | What it is |
|---|---|
| `CivoCluster` | `region`, `credentialsRef` (a Secret with `token`), optional `networkID`. Provisioned once the API key works there. |
| `CivoControlPlane` | The managed cluster: `version` (from the Cluster's topology, matched to Civo's labels; or `civoVersion`), `clusterType` (k3s/talos), `cniPlugin` (flannel/cilium), `applications`, `disableTraefik`, `disableMetricsServer`, optional `firewallID`, `clusterID` (adopt an existing cluster), `deletionPolicy` (`Delete` or `Retain`), and `firewallRules` (extra ingress rules — protocol, port, endPort, cidrs — the provider keeps on the cluster's firewall; it adds missing ones and removes ones dropped from the list, leaving Civo's and hand-made rules alone). `nodeSize` and `nodeCount` make the one pool of a cluster with no machine pools (one made before v0.4.0). |
| `CivoMachinePool` | A node pool: Cluster API's InfraMachinePool, so its MachinePool's `replicas` are the pool's node count. `size`, optional `labels`, `taints` and `publicIPNodePool` (when it's made), and `poolID` (adopt a particular pool). Reports the nodes (`providerIDList`, `civo://INSTANCE-ID`). Deleting it deletes the pool, unless the whole cluster is going. Civo can't resize a pool: a new size takes a new pool. |
| `CivoConfig` | A MachinePool's bootstrap config, which Cluster API requires: empty, as Civo builds and joins the nodes (like EKSConfig for EKS node groups). |

Each has a `…Template` kind; see [templates/clusterclass-civo.yaml](templates/clusterclass-civo.yaml).
A Civo cluster with the Cluster's name is adopted rather than duplicated
(unless `clusterID` is set); see [Adopting a cluster](#adopting-a-cluster).

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

## ClusterClass

Each release publishes `clusterclass-civo.yaml` (from
[templates/](templates/clusterclass-civo.yaml)) beside the components, where
clusterctl finds it. It has no namespace; install it in the one your
clusters are built from:

```bash
kubectl -n capi-clusters apply -f https://github.com/cbridges1/cluster-api-provider-civo/releases/download/v0.2.0/clusterclass-civo.yaml
```

Its variables are optional; `firewallRules` opens nothing unless a cluster
sets it. Nodes are machine pools of class `default`, listed in the
Cluster's topology and scaled by their replicas; `nodeSize` sets their
size, and a pool's own variables override it:

```yaml
spec:
  topology:
    classRef: {name: civo}
    version: v1.35.0
    variables:
      - {name: nodeSize, value: g4s.kube.small}
    workers:
      machinePools:
        - {class: default, name: general, replicas: 3}
        - class: default
          name: big
          replicas: 1
          variables:
            overrides:
              - {name: nodeSize, value: g4s.kube.large}
```

The cluster is made with all its pools; one added later is created in it,
and one removed is deleted. The provider reads the API key from a Secret in each *cluster's*
namespace:

```bash
kubectl -n <cluster namespace> create secret generic civo-credentials --from-literal=token=...
```

## Adopting a cluster

To bring a Civo cluster made some other way under Cluster API, set
`clusterID` to its ID (`civo k8s show NAME`). The provider takes it over
instead of creating one; the Cluster's name needn't match. It refuses an ID
another CivoControlPlane manages.

- It keeps its version and pools. Its machine pools take its pools in
  order (by name), or the one a CivoMachinePool's `poolID` names; a pool no
  machine pool takes is left alone. A machine pool whose topology gives no
  `replicas` keeps the pool's node count (its MachinePool takes it); giving
  `replicas` later scales it.
- `deletionPolicy: Retain` makes deleting the Cluster leave the Civo
  cluster, its volumes and its firewall; with `Delete` (the default) they go
  with it.
- `clusterID` can't be changed once set.

With the ClusterClass, these are the `clusterID` and `deletionPolicy`
variables:

```yaml
spec:
  topology:
    classRef: {name: civo}
    version: v1.35.0
    variables:
      - {name: region, value: LON1}
      - {name: clusterID, value: 7f3c0d6e-...}
      - {name: deletionPolicy, value: Retain}
    workers:
      machinePools:
        - {class: default, name: nodes}
```

## Moving to machine pools (v0.4.0)

A cluster made before v0.4.0 has one pool, sized by `nodeCount`. With
v0.4.0's ClusterClass, add a machine pool to its topology without
`replicas`: it takes the existing pool and its node count (no nodes are
replaced), and from then on scales by its replicas. `nodeCount` is ignored
once a cluster has machine pools.

```bash
kubectl -n <namespace> patch cluster <name> --type merge \
  -p '{"spec":{"topology":{"workers":{"machinePools":[{"class":"default","name":"nodes"}]}}}}'
```

## Develop

`go test ./...` (the reconciler runs against an in-memory Civo API);
`hack/components.py IMAGE` writes the release artifacts to `out/`.
