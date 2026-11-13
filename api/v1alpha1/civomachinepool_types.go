package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CivoMachinePoolSpec is a node pool of a Civo managed cluster: Cluster
// API's InfraMachinePool, as AWSManagedMachinePool is for EKS and
// AzureManagedMachinePool for AKS. Its MachinePool's replicas are the
// pool's node count.
type CivoMachinePoolSpec struct {
	// size is the nodes' instance size (e.g. g4s.kube.medium). Civo can't
	// change a pool's size: a different size takes a new pool.
	// +optional
	// +kubebuilder:default=g4s.kube.medium
	Size string `json:"size,omitempty"`

	// poolID adopts this existing pool of the cluster. Unset, the pool
	// is the one the provider made for it, or (on a cluster that was
	// adopted or made before machine pools) one of the cluster's pools
	// no other CivoMachinePool has.
	// +optional
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="poolID can't be changed"
	PoolID string `json:"poolID,omitempty"`

	// labels are put on the pool's nodes when it's made.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// taints are put on the pool's nodes when it's made.
	// +optional
	// +listType=atomic
	Taints []corev1.Taint `json:"taints,omitempty"`

	// publicIPNodePool gives the pool's nodes public IPs.
	// +optional
	PublicIPNodePool bool `json:"publicIPNodePool,omitempty"`

	// providerIDList is the pool's nodes (civo://INSTANCE-ID). Set by the
	// provider.
	// +optional
	// +listType=atomic
	ProviderIDList []string `json:"providerIDList,omitempty"`
}

// CivoMachinePoolInitializationStatus reports Cluster API initialization.
// +kubebuilder:validation:MinProperties=1
type CivoMachinePoolInitializationStatus struct {
	// provisioned is true once the pool exists.
	// +optional
	Provisioned *bool `json:"provisioned,omitempty"`
}

// CivoMachinePoolStatus is the observed state.
type CivoMachinePoolStatus struct {
	// +optional
	Initialization CivoMachinePoolInitializationStatus `json:"initialization,omitempty,omitzero"`
	// poolID is the Civo pool's ID.
	// +optional
	PoolID string `json:"poolID,omitempty"`
	// replicas is the pool's node count, as Civo last reported it.
	// +optional
	Replicas int32 `json:"replicas"`
	// size is the pool's instance size, as Civo reports it.
	// +optional
	Size string `json:"size,omitempty"`
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=civomachinepools,scope=Namespaced,categories=cluster-api
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".metadata.labels.cluster\\.x-k8s\\.io/cluster-name"
// +kubebuilder:printcolumn:name="Pool",type="string",JSONPath=".status.poolID"
// +kubebuilder:printcolumn:name="Size",type="string",JSONPath=".status.size"
// +kubebuilder:printcolumn:name="Replicas",type="integer",JSONPath=".status.replicas"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// CivoMachinePool is a node pool of a Civo managed cluster.
type CivoMachinePool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CivoMachinePoolSpec   `json:"spec,omitempty"`
	Status CivoMachinePoolStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// CivoMachinePoolList is a list of CivoMachinePools.
type CivoMachinePoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CivoMachinePool `json:"items"`
}

// CivoMachinePoolTemplateResource is a template's CivoMachinePool.
type CivoMachinePoolTemplateResource struct {
	// +optional
	ObjectMeta ObjectMeta          `json:"metadata,omitempty,omitzero"`
	Spec       CivoMachinePoolSpec `json:"spec"`
}

// CivoMachinePoolTemplateSpec is a CivoMachinePoolTemplate's spec.
type CivoMachinePoolTemplateSpec struct {
	Template CivoMachinePoolTemplateResource `json:"template"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=civomachinepooltemplates,scope=Namespaced,categories=cluster-api

// CivoMachinePoolTemplate is the ClusterClass template for
// CivoMachinePools.
type CivoMachinePoolTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec CivoMachinePoolTemplateSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// CivoMachinePoolTemplateList is a list of CivoMachinePoolTemplates.
type CivoMachinePoolTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CivoMachinePoolTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CivoMachinePool{}, &CivoMachinePoolList{}, &CivoMachinePoolTemplate{}, &CivoMachinePoolTemplateList{})
}
