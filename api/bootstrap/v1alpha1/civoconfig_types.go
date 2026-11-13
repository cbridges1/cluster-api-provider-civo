package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// CivoConfigSpec is empty: Civo builds and joins a managed cluster's nodes
// itself. A MachinePool needs a bootstrap config all the same (and a
// ClusterClass's machine pool class a bootstrap template), as EKSConfig
// is for EKS managed node groups.
type CivoConfigSpec struct{}

// CivoConfigInitializationStatus reports Cluster API initialization.
// +kubebuilder:validation:MinProperties=1
type CivoConfigInitializationStatus struct {
	// dataSecretCreated is true once the (empty) bootstrap data Secret
	// exists.
	// +optional
	DataSecretCreated *bool `json:"dataSecretCreated,omitempty"`
}

// CivoConfigStatus is the observed state.
type CivoConfigStatus struct {
	// +optional
	Initialization CivoConfigInitializationStatus `json:"initialization,omitempty,omitzero"`
	// dataSecretName is the bootstrap data Secret: empty, as Civo needs none.
	// +optional
	DataSecretName *string `json:"dataSecretName,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=civoconfigs,scope=Namespaced,categories=cluster-api
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".metadata.labels.cluster\\.x-k8s\\.io/cluster-name"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// CivoConfig is the bootstrap config of a Civo machine pool's nodes.
type CivoConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CivoConfigSpec   `json:"spec,omitempty"`
	Status CivoConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// CivoConfigList is a list of CivoConfigs.
type CivoConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CivoConfig `json:"items"`
}

// ObjectMeta is the metadata a template may carry.
type ObjectMeta struct {
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// CivoConfigTemplateResource is a template's CivoConfig.
type CivoConfigTemplateResource struct {
	// +optional
	ObjectMeta ObjectMeta     `json:"metadata,omitempty,omitzero"`
	Spec       CivoConfigSpec `json:"spec,omitempty"`
}

// CivoConfigTemplateSpec is a CivoConfigTemplate's spec.
type CivoConfigTemplateSpec struct {
	Template CivoConfigTemplateResource `json:"template"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=civoconfigtemplates,scope=Namespaced,categories=cluster-api

// CivoConfigTemplate is the ClusterClass template for CivoConfigs.
type CivoConfigTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec CivoConfigTemplateSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// CivoConfigTemplateList is a list of CivoConfigTemplates.
type CivoConfigTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CivoConfigTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CivoConfig{}, &CivoConfigList{}, &CivoConfigTemplate{}, &CivoConfigTemplateList{})
}
