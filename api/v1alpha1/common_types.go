package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// APIEndpoint is a reachable Kubernetes API endpoint.
// +kubebuilder:validation:MinProperties=1
type APIEndpoint struct {
	// host is the hostname or IP the API server is served on.
	// +optional
	Host string `json:"host,omitempty"`
	// port is the port the API server is served on.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port,omitempty"`
}

// IsZero reports whether the endpoint is unset.
func (e APIEndpoint) IsZero() bool { return e.Host == "" && e.Port == 0 }

// SecretRef names a Secret in the object's namespace.
type SecretRef struct {
	// name of the Secret.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// Condition types and reasons.
const (
	ReadyCondition = "Ready"
)

// SetCondition adds or replaces a condition by type.
func SetCondition(conds *[]metav1.Condition, c metav1.Condition) {
	c.LastTransitionTime = metav1.Now()
	for i := range *conds {
		if (*conds)[i].Type == c.Type {
			if (*conds)[i].Status == c.Status && (*conds)[i].Reason == c.Reason && (*conds)[i].Message == c.Message {
				return
			}
			(*conds)[i] = c
			return
		}
	}
	*conds = append(*conds, c)
}

// ObjectMeta is the metadata a template may carry.
type ObjectMeta struct {
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}
