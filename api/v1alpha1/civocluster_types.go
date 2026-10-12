package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// CivoClusterSpec is where a cluster lives on Civo and how to reach the API.
type CivoClusterSpec struct {
	// region is the Civo region (e.g. NYC1, LON1, FRA1, PHX1).
	// +kubebuilder:validation:MinLength=1
	Region string `json:"region"`

	// credentialsRef names a Secret in this namespace holding `token`, a
	// Civo API key.
	CredentialsRef SecretRef `json:"credentialsRef"`

	// networkID is the Civo network; empty uses the region's default.
	// +optional
	NetworkID string `json:"networkID,omitempty"`

	// controlPlaneEndpoint is set by the CivoControlPlane.
	// +optional
	ControlPlaneEndpoint APIEndpoint `json:"controlPlaneEndpoint,omitempty,omitzero"`
}

// CivoClusterInitializationStatus reports Cluster API initialization.
// +kubebuilder:validation:MinProperties=1
type CivoClusterInitializationStatus struct {
	// provisioned is true once the API key works in the region.
	// +optional
	Provisioned *bool `json:"provisioned,omitempty"`
}

// CivoClusterStatus is the observed state.
type CivoClusterStatus struct {
	// +optional
	Initialization CivoClusterInitializationStatus `json:"initialization,omitempty,omitzero"`
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=civoclusters,scope=Namespaced,categories=cluster-api
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".metadata.labels.cluster\\.x-k8s\\.io/cluster-name"
// +kubebuilder:printcolumn:name="Region",type="string",JSONPath=".spec.region"
// +kubebuilder:printcolumn:name="Provisioned",type="boolean",JSONPath=".status.initialization.provisioned"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// CivoCluster is the infrastructure for a Civo Kubernetes cluster.
type CivoCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CivoClusterSpec   `json:"spec,omitempty"`
	Status CivoClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// CivoClusterList is a list of CivoClusters.
type CivoClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CivoCluster `json:"items"`
}

// CivoClusterTemplateResource is a template's CivoCluster.
type CivoClusterTemplateResource struct {
	// +optional
	ObjectMeta ObjectMeta      `json:"metadata,omitempty,omitzero"`
	Spec       CivoClusterSpec `json:"spec"`
}

// CivoClusterTemplateSpec is a CivoClusterTemplate's spec.
type CivoClusterTemplateSpec struct {
	Template CivoClusterTemplateResource `json:"template"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=civoclustertemplates,scope=Namespaced,categories=cluster-api

// CivoClusterTemplate is the ClusterClass template for CivoClusters.
type CivoClusterTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec CivoClusterTemplateSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// CivoClusterTemplateList is a list of CivoClusterTemplates.
type CivoClusterTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CivoClusterTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CivoCluster{}, &CivoClusterList{}, &CivoClusterTemplate{}, &CivoClusterTemplateList{})
}
