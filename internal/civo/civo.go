// Package civo maps CivoControlPlanes onto Civo's Kubernetes API.
package civo

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/civo/civogo"

	"github.com/cbridges1/cluster-api-provider-civo/api/v1alpha1"
)

// API is the part of civogo's client the provider uses (faked in tests).
type API interface {
	ListKubernetesClusters() (*civogo.PaginatedKubernetesClusters, error)
	NewKubernetesClusters(*civogo.KubernetesClusterConfig) (*civogo.KubernetesCluster, error)
	GetKubernetesCluster(id string) (*civogo.KubernetesCluster, error)
	UpdateKubernetesCluster(id string, c *civogo.KubernetesClusterConfig) (*civogo.KubernetesCluster, error)
	UpdateKubernetesClusterPool(cid, pid string, c *civogo.KubernetesClusterPoolUpdateConfig) (*civogo.KubernetesPool, error)
	DeleteKubernetesCluster(id string) (*civogo.SimpleResponse, error)
	ListAvailableKubernetesVersions() ([]civogo.KubernetesVersion, error)
	DeleteFirewall(id string) (*civogo.SimpleResponse, error)
}

// Factory makes an API client for a token and region.
type Factory func(token, region string) (API, error)

// NewAPI is the real Factory.
func NewAPI(token, region string) (API, error) {
	return civogo.NewClient(token, region)
}

// IsNotFound reports whether a civogo error means "doesn't exist".
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, civogo.DatabaseKubernetesClusterNotFoundError) {
		return true
	}
	s := err.Error()
	return strings.Contains(s, "NotFound") || strings.Contains(s, "404")
}

// Applications renders Civo's applications string: installs, then
// removals prefixed with "-" (as the civo CLI's --remove-applications does).
func Applications(spec *v1alpha1.CivoControlPlaneSpec) string {
	apps := append([]string(nil), spec.Applications...)
	if spec.DisableTraefik {
		apps = append(apps, "-traefik2-nodeport")
	}
	if spec.DisableMetricsServer {
		apps = append(apps, "-metrics-server")
	}
	return strings.Join(apps, ",")
}

// MatchVersion picks Civo's version label for a Kubernetes version and
// cluster type: civoVersion if set, else the newest available label for
// that type whose version is the requested one (v1.31.2 → 1.31.2-k3s1).
// An empty version means Civo's default.
func MatchVersion(spec *v1alpha1.CivoControlPlaneSpec, available []civogo.KubernetesVersion) (string, error) {
	if spec.CivoVersion != "" {
		return spec.CivoVersion, nil
	}
	want := strings.TrimPrefix(spec.Version, "v")
	if want == "" {
		return "", nil
	}
	ctype := spec.ClusterType
	if ctype == "" {
		ctype = "k3s"
	}
	var hits []string
	for _, v := range available {
		if v.ClusterType != "" && v.ClusterType != ctype {
			continue
		}
		label := strings.TrimPrefix(v.Version, "v")
		if label == want || strings.HasPrefix(label, want+"-") || strings.HasPrefix(label, want+"+") {
			hits = append(hits, v.Version)
		}
	}
	if len(hits) == 0 {
		var all []string
		for _, v := range available {
			if v.ClusterType == "" || v.ClusterType == ctype {
				all = append(all, v.Version)
			}
		}
		sort.Strings(all)
		return "", fmt.Errorf("Civo has no %s version for Kubernetes %s (available: %s)", ctype, spec.Version, strings.Join(all, ", "))
	}
	sort.Strings(hits)
	return hits[len(hits)-1], nil
}

// Config builds the create request.
func Config(name, region, networkID, version string, spec *v1alpha1.CivoControlPlaneSpec) *civogo.KubernetesClusterConfig {
	count := int(spec.NodeCount)
	if count == 0 {
		count = 3
	}
	size := spec.NodeSize
	if size == "" {
		size = "g4s.kube.medium"
	}
	return &civogo.KubernetesClusterConfig{
		Name:              name,
		Region:            region,
		ClusterType:       firstNonEmpty(spec.ClusterType, "k3s"),
		CNIPlugin:         firstNonEmpty(spec.CNIPlugin, "flannel"),
		KubernetesVersion: version,
		NetworkID:         networkID,
		FirewallID:        spec.FirewallID,
		Applications:      Applications(spec),
		Tags:              "cluster-api",
		Pools:             []civogo.KubernetesClusterPoolConfig{{Count: count, Size: size}},
	}
}

// Endpoint parses Civo's api_endpoint (https://IP:6443).
func Endpoint(raw string) (v1alpha1.APIEndpoint, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return v1alpha1.APIEndpoint{}, fmt.Errorf("api endpoint %q: not a URL", raw)
	}
	host, portS, err := net.SplitHostPort(u.Host)
	if err != nil {
		return v1alpha1.APIEndpoint{Host: u.Host, Port: 6443}, nil
	}
	port, err := strconv.Atoi(portS)
	if err != nil {
		return v1alpha1.APIEndpoint{}, fmt.Errorf("api endpoint %q: bad port", raw)
	}
	return v1alpha1.APIEndpoint{Host: host, Port: int32(port)}, nil
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}
