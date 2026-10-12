package controller

import (
	"context"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cbridges1/cluster-api-provider-civo/api/v1alpha1"
	"github.com/cbridges1/cluster-api-provider-civo/internal/civo"
)

// CivoClusterReconciler checks the API key works in the region; there's
// nothing to create for the cluster itself.
type CivoClusterReconciler struct {
	Client client.Client
	NewAPI civo.Factory
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=civoclusters;civoclustertemplates,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=civoclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile implements reconcile.Reconciler.
func (r *CivoClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var cc v1alpha1.CivoCluster
	if err := r.Client.Get(ctx, req.NamespacedName, &cc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !cc.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}
	cluster, err := ownerCluster(ctx, r.Client, &cc)
	if err != nil {
		return ctrl.Result{}, err
	}
	if paused(cluster, &cc) {
		return ctrl.Result{}, nil
	}
	base := cc.DeepCopy()
	fail := func(reason string, err error) (ctrl.Result, error) {
		v1alpha1.SetCondition(&cc.Status.Conditions, condition(false, reason, err.Error()))
		_ = r.Client.Status().Patch(ctx, &cc, client.MergeFrom(base))
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}
	tok, err := token(ctx, r.Client, &cc)
	if err != nil {
		return fail("CredentialsMissing", err)
	}
	api, err := r.NewAPI(tok, cc.Spec.Region)
	if err != nil {
		return fail("APIUnavailable", err)
	}
	if _, err := api.ListKubernetesClusters(); err != nil {
		return fail("APIUnavailable", err)
	}
	cc.Status.Initialization.Provisioned = ptr(true)
	v1alpha1.SetCondition(&cc.Status.Conditions, condition(true, "Reachable", "Civo API reachable in "+cc.Spec.Region))
	if err := r.Client.Status().Patch(ctx, &cc, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 30 * time.Minute}, nil
}

// SetupWithManager registers the reconciler.
func (r *CivoClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).For(&v1alpha1.CivoCluster{}).Complete(r)
}
