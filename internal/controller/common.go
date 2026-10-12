// Package controller reconciles CivoClusters and CivoControlPlanes.
package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cbridges1/cluster-api-provider-civo/api/v1alpha1"
)

// ClusterNameLabel is the label Cluster API puts on a cluster's objects.
const ClusterNameLabel = "cluster.x-k8s.io/cluster-name"

var clusterGVK = schema.GroupVersionKind{Group: "cluster.x-k8s.io", Version: "v1beta2", Kind: "Cluster"}

// errNotReady means "come back later", not a failure.
var errNotReady = errors.New("not ready")

// ownerCluster returns the Cluster API Cluster an object belongs to, by its
// cluster-name label, or nil if it isn't labeled (yet).
func ownerCluster(ctx context.Context, c client.Client, obj metav1.Object) (*unstructured.Unstructured, error) {
	name := obj.GetLabels()[ClusterNameLabel]
	if name == "" {
		return nil, nil
	}
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(clusterGVK)
	if err := c.Get(ctx, types.NamespacedName{Namespace: obj.GetNamespace(), Name: name}, u); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return u, nil
}

// paused reports whether reconciliation is paused for the object or its
// Cluster.
func paused(cluster *unstructured.Unstructured, obj metav1.Object) bool {
	if _, ok := obj.GetAnnotations()["cluster.x-k8s.io/paused"]; ok {
		return true
	}
	if cluster == nil {
		return false
	}
	p, _, _ := unstructured.NestedBool(cluster.Object, "spec", "paused")
	return p
}

// token reads a CivoCluster's API key Secret.
func token(ctx context.Context, c client.Client, cc *v1alpha1.CivoCluster) (string, error) {
	var s corev1.Secret
	if err := c.Get(ctx, types.NamespacedName{Namespace: cc.Namespace, Name: cc.Spec.CredentialsRef.Name}, &s); err != nil {
		return "", fmt.Errorf("credentials Secret %s: %w", cc.Spec.CredentialsRef.Name, err)
	}
	t := strings.TrimSpace(string(s.Data["token"]))
	if t == "" {
		return "", fmt.Errorf("credentials Secret %s has no token", s.Name)
	}
	return t, nil
}

func condition(status bool, reason, msg string) metav1.Condition {
	s := metav1.ConditionFalse
	if status {
		s = metav1.ConditionTrue
	}
	return metav1.Condition{Type: v1alpha1.ReadyCondition, Status: s, Reason: reason, Message: msg}
}

func ptr[T any](v T) *T { return &v }
