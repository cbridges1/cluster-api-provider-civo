package civo

import (
	"strings"
	"testing"

	"github.com/civo/civogo"

	"github.com/cbridges1/cluster-api-provider-civo/api/v1alpha1"
)

func TestApplications(t *testing.T) {
	got := Applications(&v1alpha1.CivoControlPlaneSpec{Applications: []string{"cert-manager"}, DisableTraefik: true, DisableMetricsServer: true})
	if got != "cert-manager,-traefik2-nodeport,-metrics-server" {
		t.Errorf("applications = %q", got)
	}
}

func TestMatchVersion(t *testing.T) {
	// What Civo's API returned on 2026-10-12 (trimmed).
	avail := []civogo.KubernetesVersion{
		{Version: "1.31.6-k3s1", ClusterType: "k3s", Type: "deprecated"},
		{Version: "1.34.2-k3s1", ClusterType: "k3s", Type: "deprecated"},
		{Version: "1.35.0-k3s1", ClusterType: "k3s", Type: "stable", Default: true},
		{Version: "1.36.0-k3s1", ClusterType: "k3s", Type: "development"},
		{Version: "1.27.0", ClusterType: "talos", Type: "stable", Default: true},
	}
	if v, err := MatchVersion(&v1alpha1.CivoControlPlaneSpec{Version: "v1.35.0"}, avail); err != nil || v != "1.35.0-k3s1" {
		t.Errorf("got %q, %v", v, err)
	}
	_, err := MatchVersion(&v1alpha1.CivoControlPlaneSpec{Version: "v1.34.2"}, avail)
	if err == nil || !strings.Contains(err.Error(), "1.35.0-k3s1 (stable)") || strings.Contains(err.Error(), "1.34.2") && !strings.Contains(err.Error(), "Kubernetes v1.34.2") {
		t.Errorf("deprecated version must be refused, listing usable ones: %v", err)
	}
	if v, _ := MatchVersion(&v1alpha1.CivoControlPlaneSpec{Version: "v1.34.2", CivoVersion: "1.34.2-k3s1"}, avail); v != "1.34.2-k3s1" {
		t.Errorf("civoVersion override ignored: %q", v)
	}
	if v, _ := MatchVersion(&v1alpha1.CivoControlPlaneSpec{Version: "v1.35.0", ClusterType: "talos"}, avail); v != "1.27.0" {
		t.Errorf("talos: got %q, want Civo's default Talos version", v)
	}
}

func TestConfigDefaults(t *testing.T) {
	c := Config("web", "LON1", "net-1", "1.31.2-k3s1", &v1alpha1.CivoControlPlaneSpec{})
	if c.ClusterType != "k3s" || c.CNIPlugin != "flannel" || c.Pools[0].Count != 3 || c.Pools[0].Size != "g4s.kube.medium" || c.NetworkID != "net-1" {
		t.Errorf("config = %+v", c)
	}
}

func TestEndpoint(t *testing.T) {
	ep, err := Endpoint("https://74.220.21.10:6443")
	if err != nil || ep.Host != "74.220.21.10" || ep.Port != 6443 {
		t.Errorf("endpoint = %+v, %v", ep, err)
	}
}
