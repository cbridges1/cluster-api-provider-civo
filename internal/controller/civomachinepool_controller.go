package controller

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/civo/civogo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/cbridges1/cluster-api-provider-civo/api/v1alpha1"
	"github.com/cbridges1/cluster-api-provider-civo/internal/civo"
)

// PoolFinalizer keeps a CivoMachinePool until its Civo pool is gone.
const PoolFinalizer = "civomachinepool.infrastructure.cluster.x-k8s.io/finalizer"

// reasonCreating is the Ready reason while a pool the provider asked for
// hasn't shown up yet.
const reasonCreating = "Creating"

// CivoMachinePoolReconciler keeps a node pool of a Civo cluster at its
// MachinePool's replicas, and reports the pool's nodes to Cluster API.
type CivoMachinePoolReconciler struct {
	Client client.Client
	NewAPI civo.Factory
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=civomachinepools;civomachinepooltemplates,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=civomachinepools/status;civomachinepools/finalizers,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=civocontrolplanes,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinepools,verbs=get;list;watch;patch

// Reconcile implements reconcile.Reconciler.
func (r *CivoMachinePoolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var mp v1alpha1.CivoMachinePool
	if err := r.Client.Get(ctx, req.NamespacedName, &mp); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	cluster, err := ownerCluster(ctx, r.Client, &mp)
	if err != nil {
		return ctrl.Result{}, err
	}
	if paused(cluster, &mp) {
		return ctrl.Result{}, nil
	}
	if !mp.DeletionTimestamp.IsZero() {
		return r.delete(ctx, &mp, cluster)
	}
	if controllerutil.AddFinalizer(&mp, PoolFinalizer) {
		if err := r.Client.Update(ctx, &mp); err != nil {
			return ctrl.Result{}, err
		}
	}
	owner, err := ownerMachinePool(ctx, r.Client, &mp)
	if err != nil {
		return ctrl.Result{}, err
	}
	if cluster == nil || owner == nil {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	base := mp.DeepCopy()
	res, err := r.reconcile(ctx, &mp, owner, cluster)
	if err != nil && !errors.Is(err, errNotReady) {
		v1alpha1.SetCondition(&mp.Status.Conditions, condition(false, "Error", err.Error()))
		_ = r.Client.Status().Patch(ctx, &mp, client.MergeFrom(base))
		log.FromContext(ctx).Error(err, "reconcile failed")
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}
	if perr := r.Client.Status().Patch(ctx, &mp, client.MergeFrom(base)); perr != nil {
		return ctrl.Result{}, perr
	}
	return res, nil
}

// civoCluster is the Civo cluster's ID, once its control plane has made it.
func (r *CivoMachinePoolReconciler) civoCluster(ctx context.Context, cluster *unstructured.Unstructured) (string, error) {
	name, _, _ := unstructured.NestedString(cluster.Object, "spec", "controlPlaneRef", "name")
	if name == "" {
		return "", nil
	}
	var cp v1alpha1.CivoControlPlane
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: cluster.GetNamespace(), Name: name}, &cp); err != nil {
		return "", client.IgnoreNotFound(err)
	}
	return cp.Status.ClusterID, nil
}

func (r *CivoMachinePoolReconciler) reconcile(ctx context.Context, mp *v1alpha1.CivoMachinePool, owner, cluster *unstructured.Unstructured) (ctrl.Result, error) {
	lg := log.FromContext(ctx)
	wait := func(after time.Duration, reason, msg string) (ctrl.Result, error) {
		v1alpha1.SetCondition(&mp.Status.Conditions, condition(false, reason, msg))
		return ctrl.Result{RequeueAfter: after}, errNotReady
	}
	id, err := r.civoCluster(ctx, cluster)
	if err != nil {
		return ctrl.Result{}, err
	}
	if id == "" {
		return wait(15*time.Second, "WaitingForControlPlane", "the Civo cluster isn't made yet")
	}
	cc, api, err := civoAPI(ctx, r.Client, r.NewAPI, cluster)
	if err != nil {
		if errors.Is(err, errNotReady) {
			return wait(10*time.Second, "WaitingForInfrastructure", err.Error())
		}
		return ctrl.Result{}, err
	}
	kc, err := api.GetKubernetesCluster(id)
	if err != nil {
		return ctrl.Result{}, err
	}
	replicas, _, _ := unstructured.NestedInt64(owner.Object, "spec", "replicas")

	pool, err := r.findPool(ctx, mp, cluster, kc)
	if err != nil {
		return ctrl.Result{}, err
	}
	if pool == nil {
		if mp.Spec.PoolID != "" {
			return wait(time.Minute, "PoolNotFound", "Civo cluster "+kc.ID+" has no pool "+mp.Spec.PoolID)
		}
		if c := readyCondition(mp); c != nil && c.Reason == reasonCreating && time.Since(c.LastTransitionTime.Time) < 5*time.Minute {
			return wait(10*time.Second, reasonCreating, c.Message)
		}
		cfg := civo.PoolConfig(string(mp.UID), cc.Spec.Region, int32(replicas), &mp.Spec)
		lg.Info("creating Civo node pool", "cluster", kc.ID, "size", cfg.Size, "nodes", cfg.Count)
		if _, err := api.CreateKubernetesClusterPool(kc.ID, &cfg); err != nil {
			return ctrl.Result{}, fmt.Errorf("creating pool: %w", err)
		}
		return wait(10*time.Second, reasonCreating, fmt.Sprintf("creating a pool of %d %s nodes", cfg.Count, cfg.Size))
	}

	first := mp.Status.PoolID == ""
	if first && pool.ID != string(mp.UID) {
		lg.Info("adopting Civo node pool", "cluster", kc.ID, "pool", pool.ID, "nodes", pool.Count)
	}
	mp.Status.PoolID = pool.ID
	mp.Status.Replicas = int32(pool.Count)
	mp.Status.Size = pool.Size

	// A pool whose topology gives no replicas keeps the count it has:
	// the MachinePool takes it, once, as it would an autoscaler's.
	_, set := topologyReplicas(cluster, owner.GetLabels()[PoolNameLabel])
	if first && !set && owner.GetLabels()[PoolNameLabel] != "" && replicas != int64(pool.Count) {
		patch := client.MergeFrom(owner.DeepCopy())
		_ = unstructured.SetNestedField(owner.Object, int64(pool.Count), "spec", "replicas")
		if err := r.Client.Patch(ctx, owner, patch); err != nil {
			return ctrl.Result{}, err
		}
		replicas = int64(pool.Count)
	}

	ids := civo.ProviderIDs(pool)
	if !slices.Equal(mp.Spec.ProviderIDList, ids) {
		patch := client.MergeFrom(mp.DeepCopy())
		mp.Spec.ProviderIDList = ids
		status := mp.Status.DeepCopy()
		if err := r.Client.Patch(ctx, mp, patch); err != nil {
			return ctrl.Result{}, err
		}
		mp.Status = *status
	}
	mp.Status.Initialization.Provisioned = ptr(true)

	if replicas < 1 {
		return wait(time.Minute, "InvalidReplicas", "a Civo pool has at least one node")
	}
	if int(replicas) != pool.Count {
		want := int(replicas)
		lg.Info("scaling Civo node pool", "cluster", kc.ID, "pool", pool.ID, "from", pool.Count, "to", want)
		if _, err := api.UpdateKubernetesClusterPool(kc.ID, pool.ID, &civogo.KubernetesClusterPoolUpdateConfig{Count: &want, Region: cc.Spec.Region}); err != nil {
			return ctrl.Result{}, fmt.Errorf("scaling pool: %w", err)
		}
		return wait(20*time.Second, "Scaling", fmt.Sprintf("scaling from %d to %d nodes", pool.Count, want))
	}
	msg := fmt.Sprintf("Civo pool %s: %d %s nodes", pool.ID, pool.Count, pool.Size)
	if mp.Spec.Size != "" && pool.Size != "" && mp.Spec.Size != pool.Size {
		msg += fmt.Sprintf(" (size %s wanted; Civo can't resize a pool, so a new size takes a new pool)", mp.Spec.Size)
	}
	if len(ids) < pool.Count {
		return wait(20*time.Second, "WaitingForNodes", fmt.Sprintf("%s, %d up", msg, len(ids)))
	}
	v1alpha1.SetCondition(&mp.Status.Conditions, condition(true, "Active", msg))
	return ctrl.Result{RequeueAfter: 2 * time.Minute}, nil
}

// findPool finds the machine pool's Civo pool: the one it has, the one
// poolID adopts, or the one made for it (whose ID is its UID). Failing
// those, on a cluster with pools no CivoMachinePool has (an adopted
// cluster, or one made before machine pools, or a Civo that renamed the
// pools it was asked for), the machine pools without one take those in
// order, by name. nil if it has none.
func (r *CivoMachinePoolReconciler) findPool(ctx context.Context, mp *v1alpha1.CivoMachinePool, cluster *unstructured.Unstructured, kc *civogo.KubernetesCluster) (*civogo.KubernetesPool, error) {
	byID := func(id string) *civogo.KubernetesPool {
		for i := range kc.Pools {
			if id != "" && kc.Pools[i].ID == id {
				return &kc.Pools[i]
			}
		}
		return nil
	}
	for _, id := range []string{mp.Status.PoolID, mp.Spec.PoolID, string(mp.UID)} {
		if p := byID(id); p != nil {
			return p, nil
		}
	}
	if mp.Spec.PoolID != "" {
		return nil, nil
	}
	all, err := machinePools(ctx, r.Client, cluster)
	if err != nil {
		return nil, err
	}
	taken := map[string]bool{}
	var waiting []string
	for _, o := range all {
		has := false
		for _, id := range []string{o.Status.PoolID, o.Spec.PoolID, string(o.UID)} {
			if byID(id) != nil {
				taken[id], has = true, true
			}
		}
		if !has && o.Spec.PoolID == "" && o.DeletionTimestamp.IsZero() {
			waiting = append(waiting, o.Name)
		}
	}
	var free []*civogo.KubernetesPool
	for i := range kc.Pools {
		if !taken[kc.Pools[i].ID] {
			free = append(free, &kc.Pools[i])
		}
	}
	if i := slices.Index(waiting, mp.Name); i >= 0 && i < len(free) {
		return free[i], nil
	}
	return nil, nil
}

// delete removes the machine pool's Civo pool, unless the whole cluster
// is going (its control plane deletes or keeps the Civo cluster, pools
// and all).
func (r *CivoMachinePoolReconciler) delete(ctx context.Context, mp *v1alpha1.CivoMachinePool, cluster *unstructured.Unstructured) (ctrl.Result, error) {
	lg := log.FromContext(ctx)
	if !controllerutil.ContainsFinalizer(mp, PoolFinalizer) {
		return ctrl.Result{}, nil
	}
	if cluster != nil && cluster.GetDeletionTimestamp() == nil && mp.Status.PoolID != "" {
		id, err := r.civoCluster(ctx, cluster)
		if err != nil {
			return ctrl.Result{}, err
		}
		if id != "" {
			_, api, err := civoAPI(ctx, r.Client, r.NewAPI, cluster)
			if err != nil {
				lg.Info("can't reach Civo to delete the pool; retrying", "error", err.Error())
				return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
			}
			lg.Info("deleting Civo node pool", "cluster", id, "pool", mp.Status.PoolID)
			if _, err := api.DeleteKubernetesClusterPool(id, mp.Status.PoolID); err != nil && !civo.IsNotFound(err) {
				// Civo keeps a cluster's last pool.
				base := mp.DeepCopy()
				v1alpha1.SetCondition(&mp.Status.Conditions, condition(false, "DeleteFailed", err.Error()))
				_ = r.Client.Status().Patch(ctx, mp, client.MergeFrom(base))
				lg.Info("can't delete the pool yet; retrying", "error", err.Error())
				return ctrl.Result{RequeueAfter: time.Minute}, nil
			}
		}
	}
	controllerutil.RemoveFinalizer(mp, PoolFinalizer)
	return ctrl.Result{}, r.Client.Update(ctx, mp)
}

func readyCondition(mp *v1alpha1.CivoMachinePool) *metav1.Condition {
	for i := range mp.Status.Conditions {
		if mp.Status.Conditions[i].Type == v1alpha1.ReadyCondition {
			return &mp.Status.Conditions[i]
		}
	}
	return nil
}

// SetupWithManager registers the reconciler. A MachinePool's change
// (its replicas) reaches its CivoMachinePool.
func (r *CivoMachinePoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	owner := &unstructured.Unstructured{}
	owner.SetGroupVersionKind(machinePoolGVK)
	return ctrl.NewControllerManagedBy(mgr).For(&v1alpha1.CivoMachinePool{}).
		Watches(owner, handler.EnqueueRequestsFromMapFunc(func(_ context.Context, o client.Object) []ctrl.Request {
			u, ok := o.(*unstructured.Unstructured)
			if !ok {
				return nil
			}
			ref, _, _ := unstructured.NestedMap(u.Object, "spec", "template", "spec", "infrastructureRef")
			if ref["kind"] != "CivoMachinePool" {
				return nil
			}
			name, _ := ref["name"].(string)
			return []ctrl.Request{{NamespacedName: types.NamespacedName{Namespace: u.GetNamespace(), Name: name}}}
		})).
		Complete(r)
}
