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
	ListFirewallRules(id string) ([]civogo.FirewallRule, error)
	NewFirewallRule(r *civogo.FirewallRuleConfig) (*civogo.FirewallRule, error)
	DeleteFirewallRule(id, ruleID string) (*civogo.SimpleResponse, error)
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

// MatchVersion picks Civo's version label for a cluster: civoVersion if
// set; for k3s, the newest non-deprecated label for the Kubernetes version
// (v1.35.0 → 1.35.0-k3s1) — Civo refuses to create deprecated versions;
// for Talos (whose labels are Talos versions, not Kubernetes ones), Civo's
// default. An empty version means Civo's default.
func MatchVersion(spec *v1alpha1.CivoControlPlaneSpec, available []civogo.KubernetesVersion) (string, error) {
	if spec.CivoVersion != "" {
		return spec.CivoVersion, nil
	}
	ctype := firstNonEmpty(spec.ClusterType, "k3s")
	var usable []civogo.KubernetesVersion
	for _, v := range available {
		if (v.ClusterType == "" || v.ClusterType == ctype) && v.Type != "deprecated" {
			usable = append(usable, v)
		}
	}
	want := strings.TrimPrefix(spec.Version, "v")
	if ctype == "talos" || want == "" {
		for _, v := range usable {
			if v.Default {
				return v.Version, nil
			}
		}
		return "", nil // Civo's own default
	}
	var hits []string
	for _, v := range usable {
		label := strings.TrimPrefix(v.Version, "v")
		if label == want || strings.HasPrefix(label, want+"-") || strings.HasPrefix(label, want+"+") {
			hits = append(hits, v.Version)
		}
	}
	if len(hits) == 0 {
		var all []string
		for _, v := range usable {
			all = append(all, v.Version+" ("+v.Type+")")
		}
		sort.Strings(all)
		return "", fmt.Errorf("Civo has no usable %s version for Kubernetes %s (usable: %s; deprecated versions can't be created)", ctype, spec.Version, strings.Join(all, ", "))
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

// RuleLabel marks the firewall rules the provider made from
// firewallRules, so it changes only those.
const RuleLabel = "cluster-api"

func ruleKey(protocol, start, end string, cidrs []string) string {
	c := append([]string(nil), cidrs...)
	sort.Strings(c)
	return strings.ToLower(protocol) + "/" + start + "-" + end + "/" + strings.Join(c, ",")
}

// SyncFirewallRules makes the firewall's cluster-api rules match
// firewallRules: adds the missing ones and deletes those no longer listed.
// Civo's own rules, and any made by hand, are left alone. Reports whether
// it changed anything.
func SyncFirewallRules(api API, firewallID string, rules []v1alpha1.FirewallRule) (bool, error) {
	have, err := api.ListFirewallRules(firewallID)
	if err != nil {
		return false, fmt.Errorf("listing firewall %s's rules: %w", firewallID, err)
	}
	want := map[string]*civogo.FirewallRuleConfig{}
	var order []string
	for _, r := range rules {
		end := r.EndPort
		if end == 0 {
			end = r.Port
		}
		cidrs := r.CIDRs
		if len(cidrs) == 0 {
			cidrs = []string{"0.0.0.0/0"}
		}
		start, stop := strconv.Itoa(int(r.Port)), strconv.Itoa(int(end))
		k := ruleKey(r.Protocol, start, stop, cidrs)
		if _, dup := want[k]; dup {
			continue
		}
		order = append(order, k)
		want[k] = &civogo.FirewallRuleConfig{FirewallID: firewallID, Protocol: strings.ToLower(r.Protocol), StartPort: start, EndPort: stop,
			Cidr: cidrs, Direction: "ingress", Action: "allow", Label: RuleLabel}
	}
	changed := false
	for _, h := range have {
		if h.Label != RuleLabel || h.Direction != "ingress" {
			continue
		}
		end := h.EndPort
		if end == "" {
			end = h.StartPort
		}
		k := ruleKey(h.Protocol, h.StartPort, end, h.Cidr)
		if _, ok := want[k]; ok {
			delete(want, k)
			continue
		}
		if _, err := api.DeleteFirewallRule(firewallID, h.ID); err != nil && !IsNotFound(err) {
			return changed, fmt.Errorf("deleting firewall rule %s: %w", h.ID, err)
		}
		changed = true
	}
	for _, k := range order {
		if cfg, ok := want[k]; ok {
			if _, err := api.NewFirewallRule(cfg); err != nil {
				return changed, fmt.Errorf("adding firewall rule %s %s-%s: %w", cfg.Protocol, cfg.StartPort, cfg.EndPort, err)
			}
			changed = true
		}
	}
	return changed, nil
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}
