// Package v1alpha1 contains the Civo provider's bootstrap kinds.
// +kubebuilder:object:generate=true
// +groupName=bootstrap.cluster.x-k8s.io
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion is the API group and version of the bootstrap kinds.
	GroupVersion = schema.GroupVersion{Group: "bootstrap.cluster.x-k8s.io", Version: "v1alpha1"}

	// SchemeBuilder adds the bootstrap kinds to a scheme.
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme adds the bootstrap kinds to a scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)
