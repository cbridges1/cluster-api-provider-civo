package controller

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

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

	"github.com/cbridges1/cluster-api-provider-civo/api/v1alpha1"
	"github.com/cbridges1/cluster-api-provider-civo/internal/civo"
)

// fakeCivo is an in-memory Civo Kubernetes API.
type fakeCivo struct {
	clusters         map[string]*civogo.KubernetesCluster
	created          []*civogo.KubernetesClusterConfig
	deletedFirewalls []string
	scaled           []int
	rules            []civogo.FirewallRule
	nextRule         int
}

func (f *fakeCivo) ListKubernetesClusters() (*civogo.PaginatedKubernetesClusters, error) {
	l := &civogo.PaginatedKubernetesClusters{}
	for _, c := range f.clusters {
		l.Items = append(l.Items, *c)
	}
	return l, nil
}
func (f *fakeCivo) NewKubernetesClusters(c *civogo.KubernetesClusterConfig) (*civogo.KubernetesCluster, error) {
	f.created = append(f.created, c)
	kc := &civogo.KubernetesCluster{ID: "kc-1", Name: c.Name, Status: "BUILDING", FirewallID: "fw-1",
		Pools: []civogo.KubernetesPool{{ID: "pool-1", Count: c.Pools[0].Count, Size: c.Pools[0].Size}}}
	f.clusters[kc.ID] = kc
	return kc, nil
}
func (f *fakeCivo) GetKubernetesCluster(id string) (*civogo.KubernetesCluster, error) {
	if c, ok := f.clusters[id]; ok {
		return c, nil
	}
	return nil, civogo.DatabaseKubernetesClusterNotFoundError
}
func (f *fakeCivo) UpdateKubernetesCluster(string, *civogo.KubernetesClusterConfig) (*civogo.KubernetesCluster, error) {
	return nil, nil
}
func (f *fakeCivo) UpdateKubernetesClusterPool(cid, pid string, c *civogo.KubernetesClusterPoolUpdateConfig) (*civogo.KubernetesPool, error) {
	f.scaled = append(f.scaled, *c.Count)
	f.clusters[cid].Pools[0].Count = *c.Count
	return &f.clusters[cid].Pools[0], nil
}
func (f *fakeCivo) DeleteKubernetesCluster(id string) (*civogo.SimpleResponse, error) {
	delete(f.clusters, id)
	return &civogo.SimpleResponse{Result: "success"}, nil
}
func (f *fakeCivo) ListAvailableKubernetesVersions() ([]civogo.KubernetesVersion, error) {
	return []civogo.KubernetesVersion{{Version: "1.31.2-k3s1", ClusterType: "k3s", Type: "stable"}}, nil
}
func (f *fakeCivo) DeleteFirewall(id string) (*civogo.SimpleResponse, error) {
	f.deletedFirewalls = append(f.deletedFirewalls, id)
	return &civogo.SimpleResponse{Result: "success"}, nil
}

func (f *fakeCivo) ListFirewallRules(id string) ([]civogo.FirewallRule, error) {
	var out []civogo.FirewallRule
	for _, r := range f.rules {
		if r.FirewallID == id {
			out = append(out, r)
		}
	}
	return out, nil
}
func (f *fakeCivo) NewFirewallRule(c *civogo.FirewallRuleConfig) (*civogo.FirewallRule, error) {
	f.nextRule++
	r := civogo.FirewallRule{ID: fmt.Sprintf("rule-%d", f.nextRule), FirewallID: c.FirewallID, Protocol: c.Protocol, StartPort: c.StartPort,
		EndPort: c.EndPort, Cidr: c.Cidr, Direction: c.Direction, Action: c.Action, Label: c.Label}
	f.rules = append(f.rules, r)
	return &r, nil
}
func (f *fakeCivo) DeleteFirewallRule(id, ruleID string) (*civogo.SimpleResponse, error) {
	for i, r := range f.rules {
		if r.FirewallID == id && r.ID == ruleID {
			f.rules = append(f.rules[:i], f.rules[i+1:]...)
			return &civogo.SimpleResponse{Result: "success"}, nil
		}
	}
	return nil, fmt.Errorf("404 NotFound")
}

func setup(t *testing.T, existing ...*civogo.KubernetesCluster) (client.Client, *fakeCivo, *CivoControlPlaneReconciler) {
	t.Helper()
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = v1alpha1.AddToScheme(s)
	capi := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "cluster.x-k8s.io/v1beta2", "kind": "Cluster",
		"metadata": map[string]any{"name": "web", "namespace": "capi-clusters"},
		"spec":     map[string]any{"infrastructureRef": map[string]any{"name": "web-cc"}},
	}}
	lbl := map[string]string{ClusterNameLabel: "web"}
	cc := &v1alpha1.CivoCluster{ObjectMeta: metav1.ObjectMeta{Name: "web-cc", Namespace: "capi-clusters", Labels: lbl},
		Spec:   v1alpha1.CivoClusterSpec{Region: "LON1", CredentialsRef: v1alpha1.SecretRef{Name: "civo"}},
		Status: v1alpha1.CivoClusterStatus{Initialization: v1alpha1.CivoClusterInitializationStatus{Provisioned: ptr(true)}}}
	cp := &v1alpha1.CivoControlPlane{ObjectMeta: metav1.ObjectMeta{Name: "web-cp", Namespace: "capi-clusters", Labels: lbl},
		Spec: v1alpha1.CivoControlPlaneSpec{Version: "v1.31.2", NodeSize: "g4s.kube.small", NodeCount: 1}}
	sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "civo", Namespace: "capi-clusters"}, Data: map[string][]byte{"token": []byte("t")}}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(capi, cc, cp, sec).
		WithStatusSubresource(&v1alpha1.CivoCluster{}, &v1alpha1.CivoControlPlane{}).Build()
	f := &fakeCivo{clusters: map[string]*civogo.KubernetesCluster{}}
	for _, e := range existing {
		f.clusters[e.ID] = e
	}
	r := &CivoControlPlaneReconciler{Client: c, NewAPI: func(string, string) (civo.API, error) { return f, nil }}
	return c, f, r
}

var key = types.NamespacedName{Namespace: "capi-clusters", Name: "web-cp"}

func reconcile(t *testing.T, r *CivoControlPlaneReconciler) ctrl.Result {
	t.Helper()
	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestLifecycle(t *testing.T) {
	c, f, r := setup(t)
	ctx := context.Background()

	reconcile(t, r) // creates
	if len(f.created) != 1 || f.created[0].Name != "web" || f.created[0].KubernetesVersion != "1.31.2-k3s1" || f.created[0].Pools[0].Size != "g4s.kube.small" {
		t.Fatalf("create request = %+v", f.created)
	}
	var cp v1alpha1.CivoControlPlane
	_ = c.Get(ctx, key, &cp)
	if cp.Status.ClusterID != "kc-1" || !cp.Status.FirewallCreated || cp.Status.Initialization.ControlPlaneInitialized != nil {
		t.Fatalf("status after create = %+v", cp.Status)
	}

	reconcile(t, r) // still building: no second create
	if len(f.created) != 1 {
		t.Fatal("created twice")
	}

	f.clusters["kc-1"].Status, f.clusters["kc-1"].Ready = "ACTIVE", true
	f.clusters["kc-1"].KubeConfig, f.clusters["kc-1"].APIEndPoint = "apiVersion: v1\nkind: Config\n", "https://74.220.21.10:6443"
	reconcile(t, r)
	_ = c.Get(ctx, key, &cp)
	if cp.Status.Initialization.ControlPlaneInitialized == nil || !*cp.Status.Initialization.ControlPlaneInitialized ||
		cp.Spec.ControlPlaneEndpoint.Host != "74.220.21.10" || cp.Status.Version != "v1.31.2" {
		t.Fatalf("not initialized: %+v / %+v", cp.Status, cp.Spec.ControlPlaneEndpoint)
	}
	var s corev1.Secret
	if err := c.Get(ctx, types.NamespacedName{Namespace: "capi-clusters", Name: "web-kubeconfig"}, &s); err != nil || string(s.Type) != "cluster.x-k8s.io/secret" || s.Labels[ClusterNameLabel] != "web" {
		t.Fatalf("kubeconfig Secret = %+v, %v", s, err)
	}

	cp.Spec.NodeCount = 3
	_ = c.Update(ctx, &cp)
	reconcile(t, r)
	if len(f.scaled) != 1 || f.scaled[0] != 3 {
		t.Errorf("scaled = %v", f.scaled)
	}

	// delete: cluster, then its firewall, then the finalizer
	_ = c.Delete(ctx, &cp)
	reconcile(t, r)
	if _, ok := f.clusters["kc-1"]; ok {
		t.Fatal("Civo cluster not deleted")
	}
	reconcile(t, r)
	if len(f.deletedFirewalls) != 1 || f.deletedFirewalls[0] != "fw-1" {
		t.Errorf("firewalls deleted = %v", f.deletedFirewalls)
	}
	if err := c.Get(ctx, key, &cp); err == nil {
		t.Error("finalizer not removed")
	}
}

func TestAdoptsExistingClusterByName(t *testing.T) {
	_, f, r := setup(t, &civogo.KubernetesCluster{ID: "kc-old", Name: "web", Status: "BUILDING"})
	reconcile(t, r)
	if len(f.created) != 0 {
		t.Error("created a duplicate instead of adopting")
	}
}

var _ = time.Second

func TestFirewallRules(t *testing.T) {
	c, f, r := setup(t)
	ctx := context.Background()
	var cp v1alpha1.CivoControlPlane
	_ = c.Get(ctx, key, &cp)
	cp.Spec.FirewallRules = []v1alpha1.FirewallRule{{Protocol: "udp", Port: 31820}, {Protocol: "tcp", Port: 8000, EndPort: 8010, CIDRs: []string{"10.0.0.0/8"}}}
	if err := c.Update(ctx, &cp); err != nil {
		t.Fatal(err)
	}
	// Civo's own rule, and one made by hand: never touched.
	f.rules = []civogo.FirewallRule{
		{ID: "civo-6443", FirewallID: "fw-1", Protocol: "tcp", StartPort: "6443", EndPort: "6443", Cidr: []string{"0.0.0.0/0"}, Direction: "ingress", Label: "kubernetes api server"},
		{ID: "manual", FirewallID: "fw-1", Protocol: "udp", StartPort: "9999", Cidr: []string{"0.0.0.0/0"}, Direction: "ingress"},
	}

	reconcile(t, r) // creates the cluster (firewall fw-1) and adds the rules while it builds
	labelled := func() []string {
		var out []string
		for _, x := range f.rules {
			if x.Label == civo.RuleLabel {
				out = append(out, x.Protocol+"/"+x.StartPort+"-"+x.EndPort+"/"+strings.Join(x.Cidr, ","))
			}
		}
		return out
	}
	if got := strings.Join(labelled(), " "); got != "udp/31820-31820/0.0.0.0/0 tcp/8000-8010/10.0.0.0/8" {
		t.Fatalf("rules after create = %q", got)
	}
	reconcile(t, r) // nothing new
	if len(f.rules) != 4 {
		t.Fatalf("rules duplicated: %+v", f.rules)
	}

	_ = c.Get(ctx, key, &cp)
	cp.Spec.FirewallRules = cp.Spec.FirewallRules[:1]
	if err := c.Update(ctx, &cp); err != nil {
		t.Fatal(err)
	}
	reconcile(t, r) // the dropped one goes; the others stay
	if got := strings.Join(labelled(), " "); got != "udp/31820-31820/0.0.0.0/0" || len(f.rules) != 3 {
		t.Fatalf("rules after dropping one = %q (%d in all)", got, len(f.rules))
	}
}
