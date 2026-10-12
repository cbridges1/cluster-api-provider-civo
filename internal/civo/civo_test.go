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
	avail := []civogo.KubernetesVersion{
		{Version: "1.30.5-k3s1", ClusterType: "k3s"}, {Version: "1.31.2-k3s1", ClusterType: "k3s"},
		{Version: "1.31.2-k3s2", ClusterType: "k3s"}, {Version: "talos-v1.5.0", ClusterType: "talos"},
	}
	if v, err := MatchVersion(&v1alpha1.CivoControlPlaneSpec{Version: "v1.31.2"}, avail); err != nil || v != "1.31.2-k3s2" {
		t.Errorf("got %q, %v; want the newest 1.31.2 label", v, err)
	}
	if v, _ := MatchVersion(&v1alpha1.CivoControlPlaneSpec{Version: "v1.31.2", CivoVersion: "1.30.5-k3s1"}, avail); v != "1.30.5-k3s1" {
		t.Errorf("civoVersion override ignored: %q", v)
	}
	_, err := MatchVersion(&v1alpha1.CivoControlPlaneSpec{Version: "v1.99.0"}, avail)
	if err == nil || !strings.Contains(err.Error(), "1.31.2-k3s1") {
		t.Errorf("want an error listing what's available, got %v", err)
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
