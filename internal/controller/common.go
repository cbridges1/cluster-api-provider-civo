// Package controller reconciles CivoClusters, CivoControlPlanes,
// CivoMachinePools and CivoConfigs.
package controller

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cbridges1/cluster-api-provider-civo/api/v1alpha1"
	"github.com/cbridges1/cluster-api-provider-civo/internal/civo"
)

// ClusterNameLabel is the label Cluster API puts on a cluster's objects.
const ClusterNameLabel = "cluster.x-k8s.io/cluster-name"

// PoolNameLabel is the label Cluster API puts on a topology's
// MachinePools: the pool's name in the Cluster's topology.
const PoolNameLabel = "topology.cluster.x-k8s.io/pool-name"

var (
	clusterGVK     = schema.GroupVersionKind{Group: "cluster.x-k8s.io", Version: "v1beta2", Kind: "Cluster"}
	machinePoolGVK = schema.GroupVersionKind{Group: "cluster.x-k8s.io", Version: "v1beta2", Kind: "MachinePool"}
)

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

// civoAPI returns a Cluster's CivoCluster and an API client for it.
func civoAPI(ctx context.Context, c client.Client, newAPI civo.Factory, cluster *unstructured.Unstructured) (*v1alpha1.CivoCluster, civo.API, error) {
	name, _, _ := unstructured.NestedString(cluster.Object, "spec", "infrastructureRef", "name")
	if name == "" {
		return nil, nil, fmt.Errorf("%w: Cluster has no infrastructureRef yet", errNotReady)
	}
	var cc v1alpha1.CivoCluster
	if err := c.Get(ctx, types.NamespacedName{Namespace: cluster.GetNamespace(), Name: name}, &cc); err != nil {
		return nil, nil, err
	}
	tok, err := token(ctx, c, &cc)
	if err != nil {
		return &cc, nil, err
	}
	a, err := newAPI(tok, cc.Spec.Region)
	return &cc, a, err
}

// machinePools are a Cluster's CivoMachinePools, by name.
func machinePools(ctx context.Context, c client.Client, cluster *unstructured.Unstructured) ([]v1alpha1.CivoMachinePool, error) {
	var list v1alpha1.CivoMachinePoolList
	if err := c.List(ctx, &list, client.InNamespace(cluster.GetNamespace()), client.MatchingLabels{ClusterNameLabel: cluster.GetName()}); err != nil {
		return nil, err
	}
	sort.Slice(list.Items, func(i, j int) bool { return list.Items[i].Name < list.Items[j].Name })
	return list.Items, nil
}

// ownerMachinePool returns the MachinePool that owns obj, or nil if none
// does (yet).
func ownerMachinePool(ctx context.Context, c client.Client, obj metav1.Object) (*unstructured.Unstructured, error) {
	for _, ref := range obj.GetOwnerReferences() {
		gv, err := schema.ParseGroupVersion(ref.APIVersion)
		if err != nil || gv.Group != machinePoolGVK.Group || ref.Kind != machinePoolGVK.Kind {
			continue
		}
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(machinePoolGVK)
		if err := c.Get(ctx, types.NamespacedName{Namespace: obj.GetNamespace(), Name: ref.Name}, u); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, nil
			}
			return nil, err
		}
		return u, nil
	}
	return nil, nil
}

// topologyPools are the machine pools a Cluster's topology lists.
func topologyPools(cluster *unstructured.Unstructured) []any {
	pools, _, _ := unstructured.NestedSlice(cluster.Object, "spec", "topology", "workers", "machinePools")
	return pools
}

// topologyReplicas is the replicas the Cluster's topology gives its
// machine pool named pool; ok is false when it gives none (the pool
// keeps the count it has).
func topologyReplicas(cluster *unstructured.Unstructured, pool string) (int64, bool) {
	for _, p := range topologyPools(cluster) {
		m, _ := p.(map[string]any)
		if m["name"] != pool {
			continue
		}
		n, ok, _ := unstructured.NestedInt64(m, "replicas")
		return n, ok
	}
	return 0, false
}

func condition(status bool, reason, msg string) metav1.Condition {
	s := metav1.ConditionFalse
	if status {
		s = metav1.ConditionTrue
	}
	return metav1.Condition{Type: v1alpha1.ReadyCondition, Status: s, Reason: reason, Message: msg}
}

func ptr[T any](v T) *T { return &v }
