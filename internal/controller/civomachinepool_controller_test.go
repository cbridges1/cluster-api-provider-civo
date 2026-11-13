package controller

import (
	"context"
	"testing"

	"github.com/civo/civogo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	bootstrapv1 "github.com/cbridges1/cluster-api-provider-civo/api/bootstrap/v1alpha1"
	"github.com/cbridges1/cluster-api-provider-civo/api/v1alpha1"
	"github.com/cbridges1/cluster-api-provider-civo/internal/civo"
)

const ns = "capi-clusters"

// pool is a topology machine pool: its name, and replicas (-1: none).
type pool struct {
	name     string
	replicas int64
}

// poolEnv is a Cluster whose topology lists pools, with a MachinePool
// (of replicas mpReplicas, as defaulted or set) and a CivoMachinePool for
// each.
type poolEnv struct {
	c   client.Client
	f   *fakeCivo
	cp  *CivoControlPlaneReconciler
	mp  *CivoMachinePoolReconciler
	ctx context.Context
}

func setupPools(t *testing.T, pools []pool, mpReplicas []int64, existing ...*civogo.KubernetesCluster) *poolEnv {
	t.Helper()
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = v1alpha1.AddToScheme(s)
	_ = bootstrapv1.AddToScheme(s)
	var topo []any
	for _, p := range pools {
		m := map[string]any{"class": "default", "name": p.name}
		if p.replicas >= 0 {
			m["replicas"] = p.replicas
		}
		topo = append(topo, m)
	}
	capi := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "cluster.x-k8s.io/v1beta2", "kind": "Cluster",
		"metadata": map[string]any{"name": "web", "namespace": ns},
		"spec": map[string]any{
			"infrastructureRef": map[string]any{"name": "web-cc"},
			"controlPlaneRef":   map[string]any{"name": "web-cp"},
			"topology":          map[string]any{"workers": map[string]any{"machinePools": topo}},
		},
	}}
	lbl := map[string]string{ClusterNameLabel: "web"}
	objs := []client.Object{capi,
		&v1alpha1.CivoCluster{ObjectMeta: metav1.ObjectMeta{Name: "web-cc", Namespace: ns, Labels: lbl},
			Spec:   v1alpha1.CivoClusterSpec{Region: "LON1", CredentialsRef: v1alpha1.SecretRef{Name: "civo"}},
			Status: v1alpha1.CivoClusterStatus{Initialization: v1alpha1.CivoClusterInitializationStatus{Provisioned: ptr(true)}}},
		&v1alpha1.CivoControlPlane{ObjectMeta: metav1.ObjectMeta{Name: "web-cp", Namespace: ns, Labels: lbl},
			Spec: v1alpha1.CivoControlPlaneSpec{Version: "v1.31.2", NodeCount: 3}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "civo", Namespace: ns}, Data: map[string][]byte{"token": []byte("t")}},
	}
	for i, p := range pools {
		name := "web-" + p.name
		mp := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "cluster.x-k8s.io/v1beta2", "kind": "MachinePool",
			"metadata": map[string]any{"name": name, "namespace": ns,
				"labels": map[string]any{ClusterNameLabel: "web", PoolNameLabel: p.name}},
			"spec": map[string]any{"replicas": mpReplicas[i], "template": map[string]any{"spec": map[string]any{
				"infrastructureRef": map[string]any{"kind": "CivoMachinePool", "name": name}}}},
		}}
		objs = append(objs, mp, &v1alpha1.CivoMachinePool{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: lbl, UID: types.UID("uid-" + p.name),
				OwnerReferences: []metav1.OwnerReference{{APIVersion: "cluster.x-k8s.io/v1beta2", Kind: "MachinePool", Name: name, UID: "mp-" + types.UID(p.name)}}},
			Spec: v1alpha1.CivoMachinePoolSpec{Size: "g4s.kube.small"},
		})
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).
		WithStatusSubresource(&v1alpha1.CivoCluster{}, &v1alpha1.CivoControlPlane{}, &v1alpha1.CivoMachinePool{}, &bootstrapv1.CivoConfig{}).Build()
	f := &fakeCivo{clusters: map[string]*civogo.KubernetesCluster{}}
	for _, e := range existing {
		f.clusters[e.ID] = e
	}
	api := func(string, string) (civo.API, error) { return f, nil }
	return &poolEnv{c: c, f: f, ctx: context.Background(),
		cp: &CivoControlPlaneReconciler{Client: c, NewAPI: api},
		mp: &CivoMachinePoolReconciler{Client: c, NewAPI: api}}
}

func (e *poolEnv) reconcileCP(t *testing.T) {
	t.Helper()
	if _, err := e.cp.Reconcile(e.ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: "web-cp"}}); err != nil {
		t.Fatal(err)
	}
}

func (e *poolEnv) reconcilePool(t *testing.T, name string) *v1alpha1.CivoMachinePool {
	t.Helper()
	k := types.NamespacedName{Namespace: ns, Name: "web-" + name}
	if _, err := e.mp.Reconcile(e.ctx, ctrl.Request{NamespacedName: k}); err != nil {
		t.Fatal(err)
	}
	var mp v1alpha1.CivoMachinePool
	_ = e.c.Get(e.ctx, k, &mp)
	return &mp
}

func (e *poolEnv) replicas(t *testing.T, name string) int64 {
	t.Helper()
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(machinePoolGVK)
	if err := e.c.Get(e.ctx, types.NamespacedName{Namespace: ns, Name: "web-" + name}, u); err != nil {
		t.Fatal(err)
	}
	n, _, _ := unstructured.NestedInt64(u.Object, "spec", "replicas")
	return n
}

func (e *poolEnv) setReplicas(t *testing.T, name string, n int64) {
	t.Helper()
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(machinePoolGVK)
	_ = e.c.Get(e.ctx, types.NamespacedName{Namespace: ns, Name: "web-" + name}, u)
	_ = unstructured.SetNestedField(u.Object, n, "spec", "replicas")
	if err := e.c.Update(e.ctx, u); err != nil {
		t.Fatal(err)
	}
}

// activate makes the fake cluster ready, each pool with its nodes.
func (e *poolEnv) activate(id string) {
	kc := e.f.clusters[id]
	kc.Status, kc.Ready = "ACTIVE", true
	kc.KubeConfig, kc.APIEndPoint = "apiVersion: v1\nkind: Config\n", "https://74.220.21.10:6443"
	for i := range kc.Pools {
		p := &kc.Pools[i]
		p.Instances = nil
		for n := range p.Count {
			p.Instances = append(p.Instances, civogo.KubernetesInstance{ID: p.ID + "-node-" + string(rune('a'+n))})
		}
	}
}

func TestNewClusterWithMachinePools(t *testing.T) {
	e := setupPools(t, []pool{{"a", 2}, {"b", 1}}, []int64{2, 1})
	e.reconcileCP(t)
	if len(e.f.created) != 1 {
		t.Fatalf("created = %d", len(e.f.created))
	}
	got := e.f.created[0].Pools
	if len(got) != 2 || got[0].ID != "uid-a" || got[0].Count != 2 || got[0].Size != "g4s.kube.small" || got[1].ID != "uid-b" || got[1].Count != 1 {
		t.Fatalf("pools = %+v", got)
	}

	e.activate("kc-1")
	e.reconcileCP(t)
	if len(e.f.scaled) != 0 {
		t.Fatalf("the control plane scaled by nodeCount: %v", e.f.scaled)
	}
	a := e.reconcilePool(t, "a")
	if a.Status.PoolID != "uid-a" || a.Status.Replicas != 2 || len(a.Spec.ProviderIDList) != 2 || a.Spec.ProviderIDList[0] != "civo://uid-a-node-a" ||
		a.Status.Initialization.Provisioned == nil || readyCondition(a).Status != metav1.ConditionTrue {
		t.Fatalf("pool a = %+v / %+v", a.Spec, a.Status)
	}
	if b := e.reconcilePool(t, "b"); b.Status.PoolID != "uid-b" {
		t.Fatalf("pool b = %+v", b.Status)
	}
	if e.f.poolsCreated != 0 {
		t.Fatal("made a pool the cluster was made with")
	}

	// Scaling the MachinePool scales the pool.
	e.setReplicas(t, "a", 4)
	e.reconcilePool(t, "a")
	if len(e.f.scaled) != 1 || e.f.scaled[0] != 4 || e.f.clusters["kc-1"].Pools[0].Count != 4 {
		t.Fatalf("scaled = %v", e.f.scaled)
	}
}

func TestControlPlaneWaitsForMachinePools(t *testing.T) {
	e := setupPools(t, []pool{{"a", 2}}, []int64{2})
	// The topology lists a second pool whose objects don't exist yet.
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(clusterGVK)
	_ = e.c.Get(e.ctx, types.NamespacedName{Namespace: ns, Name: "web"}, u)
	_ = unstructured.SetNestedSlice(u.Object, []any{map[string]any{"name": "a"}, map[string]any{"name": "b"}}, "spec", "topology", "workers", "machinePools")
	_ = e.c.Update(e.ctx, u)
	e.reconcileCP(t)
	if len(e.f.created) != 0 {
		t.Fatal("made the cluster before its machine pools")
	}
}

func TestAdoptedClusterKeepsItsNodeCount(t *testing.T) {
	old := &civogo.KubernetesCluster{ID: "kc-old", Name: "web", Status: "ACTIVE",
		Pools: []civogo.KubernetesPool{{ID: "p-old", Count: 5, Size: "g4s.kube.medium"}}}
	// No replicas in the topology: the MachinePool was defaulted to 1.
	e := setupPools(t, []pool{{"a", -1}}, []int64{1}, old)
	e.activate("kc-old")
	e.reconcileCP(t)
	if len(e.f.created) != 0 || len(e.f.scaled) != 0 {
		t.Fatalf("created %d, scaled %v", len(e.f.created), e.f.scaled)
	}
	a := e.reconcilePool(t, "a")
	if a.Status.PoolID != "p-old" || a.Status.Replicas != 5 {
		t.Fatalf("pool a = %+v", a.Status)
	}
	if n := e.replicas(t, "a"); n != 5 {
		t.Fatalf("MachinePool replicas = %d, want the pool's 5", n)
	}
	e.reconcilePool(t, "a")
	if len(e.f.scaled) != 0 || e.f.poolsCreated != 0 {
		t.Fatalf("scaled %v, created %d pools", e.f.scaled, e.f.poolsCreated)
	}

	// Scaling it from then on works as on any cluster.
	e.setReplicas(t, "a", 2)
	e.reconcilePool(t, "a")
	if len(e.f.scaled) != 1 || e.f.scaled[0] != 2 {
		t.Fatalf("scaled = %v", e.f.scaled)
	}
}

func TestTopologyReplicasWinOnAdoption(t *testing.T) {
	old := &civogo.KubernetesCluster{ID: "kc-old", Name: "web", Status: "ACTIVE",
		Pools: []civogo.KubernetesPool{{ID: "p-old", Count: 5}}}
	e := setupPools(t, []pool{{"a", 3}}, []int64{3}, old)
	e.activate("kc-old")
	e.reconcileCP(t)
	e.reconcilePool(t, "a")
	if n := e.replicas(t, "a"); n != 3 {
		t.Fatalf("MachinePool replicas = %d", n)
	}
	if len(e.f.scaled) != 1 || e.f.scaled[0] != 3 {
		t.Fatalf("scaled = %v", e.f.scaled)
	}
}

func TestPoolsFoundWhenCivoNamesThem(t *testing.T) {
	e := setupPools(t, []pool{{"a", 2}, {"b", 1}}, []int64{2, 1})
	e.f.ignorePoolIDs = true
	e.reconcileCP(t)
	e.activate("kc-1")
	// b first: the pools still go in order of their machine pools' names.
	if b := e.reconcilePool(t, "b"); b.Status.PoolID != "pool-2" {
		t.Fatalf("pool b = %+v", b.Status)
	}
	if a := e.reconcilePool(t, "a"); a.Status.PoolID != "pool-1" {
		t.Fatalf("pool a = %+v", a.Status)
	}
	if e.f.poolsCreated != 0 || len(e.f.scaled) != 0 {
		t.Fatalf("created %d, scaled %v", e.f.poolsCreated, e.f.scaled)
	}
}

func TestAddedPoolIsCreatedAndDeleted(t *testing.T) {
	old := &civogo.KubernetesCluster{ID: "kc-old", Name: "web", Status: "ACTIVE",
		Pools: []civogo.KubernetesPool{{ID: "p-old", Count: 1}}}
	e := setupPools(t, []pool{{"a", 1}, {"b", 2}}, []int64{1, 2}, old)
	e.activate("kc-old")
	e.reconcileCP(t)
	e.reconcilePool(t, "a")
	b := e.reconcilePool(t, "b")
	if e.f.poolsCreated != 1 || readyCondition(b).Reason != reasonCreating {
		t.Fatalf("created %d, b = %+v", e.f.poolsCreated, b.Status)
	}
	e.reconcilePool(t, "b")
	if e.f.poolsCreated != 1 {
		t.Fatal("created the pool twice")
	}
	e.activate("kc-old")
	if b = e.reconcilePool(t, "b"); b.Status.PoolID != "uid-b" || b.Status.Replicas != 2 {
		t.Fatalf("pool b = %+v", b.Status)
	}

	_ = e.c.Delete(e.ctx, b)
	e.reconcilePool(t, "b")
	if len(e.f.poolsDeleted) != 1 || e.f.poolsDeleted[0] != "uid-b" {
		t.Fatalf("deleted = %v", e.f.poolsDeleted)
	}
	if err := e.c.Get(e.ctx, types.NamespacedName{Namespace: ns, Name: "web-b"}, b); err == nil {
		t.Fatal("finalizer not removed")
	}
}

func TestDeletingClusterLeavesPools(t *testing.T) {
	e := setupPools(t, []pool{{"a", 1}}, []int64{1})
	e.reconcileCP(t)
	e.activate("kc-1")
	a := e.reconcilePool(t, "a")

	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(clusterGVK)
	_ = e.c.Get(e.ctx, types.NamespacedName{Namespace: ns, Name: "web"}, u)
	u.SetFinalizers([]string{"cluster.cluster.x-k8s.io"})
	_ = e.c.Update(e.ctx, u)
	_ = e.c.Delete(e.ctx, u)
	_ = e.c.Delete(e.ctx, a)
	e.reconcilePool(t, "a")
	if len(e.f.poolsDeleted) != 0 {
		t.Fatalf("deleted pools %v with the cluster going", e.f.poolsDeleted)
	}
}

func TestCivoConfig(t *testing.T) {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = bootstrapv1.AddToScheme(s)
	cfg := &bootstrapv1.CivoConfig{ObjectMeta: metav1.ObjectMeta{Name: "web-a", Namespace: ns, Labels: map[string]string{ClusterNameLabel: "web"}}}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(cfg).WithStatusSubresource(&bootstrapv1.CivoConfig{}).Build()
	r := &CivoConfigReconciler{Client: c}
	k := types.NamespacedName{Namespace: ns, Name: "web-a"}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: k}); err != nil {
		t.Fatal(err)
	}
	_ = c.Get(context.Background(), k, cfg)
	if cfg.Status.DataSecretName == nil || *cfg.Status.DataSecretName != "web-a" || cfg.Status.Initialization.DataSecretCreated == nil {
		t.Fatalf("status = %+v", cfg.Status)
	}
	var sec corev1.Secret
	if err := c.Get(context.Background(), k, &sec); err != nil || sec.Labels[ClusterNameLabel] != "web" {
		t.Fatalf("Secret = %+v, %v", sec, err)
	}
}
