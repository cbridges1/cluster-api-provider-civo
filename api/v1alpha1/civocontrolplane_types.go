package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// CivoControlPlaneSpec is a Civo managed Kubernetes cluster (K3s or Talos)
// with one node pool.
type CivoControlPlaneSpec struct {
	// version is the Kubernetes version (set from the Cluster's topology),
	// e.g. v1.31.2. It's matched against Civo's available versions for the
	// cluster type; civoVersion overrides the match.
	// +kubebuilder:validation:MinLength=1
	Version string `json:"version"`

	// civoVersion is an exact Civo version label (e.g. 1.31.2-k3s1).
	// +optional
	CivoVersion string `json:"civoVersion,omitempty"`

	// clusterType is k3s or talos.
	// +optional
	// +kubebuilder:validation:Enum=k3s;talos
	// +kubebuilder:default=k3s
	ClusterType string `json:"clusterType,omitempty"`

	// cniPlugin is flannel or cilium.
	// +optional
	// +kubebuilder:validation:Enum=flannel;cilium
	// +kubebuilder:default=flannel
	CNIPlugin string `json:"cniPlugin,omitempty"`

	// nodeSize is the node instance size (e.g. g4s.kube.small).
	// +optional
	// +kubebuilder:default=g4s.kube.medium
	NodeSize string `json:"nodeSize,omitempty"`

	// nodeCount is the pool's node count; changing it scales the pool.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=3
	NodeCount int32 `json:"nodeCount,omitempty"`

	// applications are Civo marketplace applications to install.
	// +optional
	Applications []string `json:"applications,omitempty"`

	// disableTraefik removes the default Traefik ingress (k3s).
	// +optional
	DisableTraefik bool `json:"disableTraefik,omitempty"`

	// disableMetricsServer removes the default metrics-server.
	// +optional
	DisableMetricsServer bool `json:"disableMetricsServer,omitempty"`

	// firewallID uses an existing firewall; empty lets Civo create one,
	// which the provider deletes with the cluster.
	// +optional
	FirewallID string `json:"firewallID,omitempty"`

	// firewallRules are ingress rules the provider keeps on the cluster's
	// firewall, beside Civo's own (80, 443 and 6443 on the one it
	// creates): added when missing, and removed when dropped from this
	// list. Rules made some other way are left alone.
	// +optional
	// +listType=atomic
	FirewallRules []FirewallRule `json:"firewallRules,omitempty"`

	// controlPlaneEndpoint is the cluster's API endpoint. Set by the
	// provider.
	// +optional
	ControlPlaneEndpoint APIEndpoint `json:"controlPlaneEndpoint,omitempty,omitzero"`
}

// CivoControlPlaneInitializationStatus reports Cluster API initialization.
// +kubebuilder:validation:MinProperties=1
type CivoControlPlaneInitializationStatus struct {
	// +optional
	ControlPlaneInitialized *bool `json:"controlPlaneInitialized,omitempty"`
}

// CivoControlPlaneStatus is the observed state.
type CivoControlPlaneStatus struct {
	// +optional
	Initialization CivoControlPlaneInitializationStatus `json:"initialization,omitempty,omitzero"`
	// clusterID is the Civo cluster's ID.
	// +optional
	ClusterID string `json:"clusterID,omitempty"`
	// firewallID is the cluster's firewall.
	// +optional
	FirewallID string `json:"firewallID,omitempty"`
	// firewallCreated is true when Civo created the firewall for this
	// cluster (and so the provider deletes it).
	// +optional
	FirewallCreated bool `json:"firewallCreated,omitempty"`
	// civoStatus is Civo's own status (BUILDING, ACTIVE, ...).
	// +optional
	CivoStatus string `json:"civoStatus,omitempty"`
	// version is the Kubernetes version running.
	// +optional
	Version string `json:"version,omitempty"`
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=civocontrolplanes,scope=Namespaced,categories=cluster-api
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".metadata.labels.cluster\\.x-k8s\\.io/cluster-name"
// +kubebuilder:printcolumn:name="Civo",type="string",JSONPath=".status.civoStatus"
// +kubebuilder:printcolumn:name="Version",type="string",JSONPath=".status.version"
// +kubebuilder:printcolumn:name="Initialized",type="boolean",JSONPath=".status.initialization.controlPlaneInitialized"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// CivoControlPlane is a Civo managed Kubernetes cluster.
type CivoControlPlane struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CivoControlPlaneSpec   `json:"spec,omitempty"`
	Status CivoControlPlaneStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// CivoControlPlaneList is a list of CivoControlPlanes.
type CivoControlPlaneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CivoControlPlane `json:"items"`
}

// CivoControlPlaneTemplateResourceSpec is a template's control plane
// spec; version comes from the Cluster's topology.
type CivoControlPlaneTemplateResourceSpec struct {
	// +optional
	CivoVersion string `json:"civoVersion,omitempty"`
	// +optional
	// +kubebuilder:validation:Enum=k3s;talos
	ClusterType string `json:"clusterType,omitempty"`
	// +optional
	// +kubebuilder:validation:Enum=flannel;cilium
	CNIPlugin string `json:"cniPlugin,omitempty"`
	// +optional
	NodeSize string `json:"nodeSize,omitempty"`
	// +optional
	NodeCount int32 `json:"nodeCount,omitempty"`
	// +optional
	Applications []string `json:"applications,omitempty"`
	// +optional
	DisableTraefik bool `json:"disableTraefik,omitempty"`
	// +optional
	DisableMetricsServer bool `json:"disableMetricsServer,omitempty"`
	// +optional
	FirewallID string `json:"firewallID,omitempty"`
	// +optional
	// +listType=atomic
	FirewallRules []FirewallRule `json:"firewallRules,omitempty"`
}

// FirewallRule is one ingress rule: a port (or range) for a protocol,
// from some CIDRs.
type FirewallRule struct {
	// protocol is tcp or udp.
	// +kubebuilder:validation:Enum=tcp;udp
	Protocol string `json:"protocol"`
	// port is the port, or the first of a range.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port"`
	// endPort is the last port of a range; empty is just port.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	EndPort int32 `json:"endPort,omitempty"`
	// cidrs it's open to; empty is everywhere (0.0.0.0/0).
	// +optional
	// +listType=atomic
	CIDRs []string `json:"cidrs,omitempty"`
}

// CivoControlPlaneTemplateResource is a template's CivoControlPlane.
type CivoControlPlaneTemplateResource struct {
	// +optional
	ObjectMeta ObjectMeta                           `json:"metadata,omitempty,omitzero"`
	Spec       CivoControlPlaneTemplateResourceSpec `json:"spec"`
}

// CivoControlPlaneTemplateSpec is a CivoControlPlaneTemplate's spec.
type CivoControlPlaneTemplateSpec struct {
	Template CivoControlPlaneTemplateResource `json:"template"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=civocontrolplanetemplates,scope=Namespaced,categories=cluster-api

// CivoControlPlaneTemplate is the ClusterClass template for
// CivoControlPlanes.
type CivoControlPlaneTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec CivoControlPlaneTemplateSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// CivoControlPlaneTemplateList is a list of CivoControlPlaneTemplates.
type CivoControlPlaneTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CivoControlPlaneTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CivoControlPlane{}, &CivoControlPlaneList{}, &CivoControlPlaneTemplate{}, &CivoControlPlaneTemplateList{})
}
