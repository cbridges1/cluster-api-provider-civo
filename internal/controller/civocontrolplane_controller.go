package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/civo/civogo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/cbridges1/cluster-api-provider-civo/api/v1alpha1"
	"github.com/cbridges1/cluster-api-provider-civo/internal/civo"
)

// Finalizer keeps a CivoControlPlane until its Civo cluster is gone.
const Finalizer = "civocontrolplane.infrastructure.cluster.x-k8s.io/finalizer"

// CivoControlPlaneReconciler creates, scales and deletes a Civo managed
// cluster and publishes its kubeconfig.
type CivoControlPlaneReconciler struct {
	Client client.Client
	NewAPI civo.Factory
	// PVCCleanupTimeout bounds the wait for volumes to be reclaimed on delete.
	PVCCleanupTimeout time.Duration
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=civocontrolplanes;civocontrolplanetemplates,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=civocontrolplanes/status;civocontrolplanes/finalizers,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile implements reconcile.Reconciler.
func (r *CivoControlPlaneReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var cp v1alpha1.CivoControlPlane
	if err := r.Client.Get(ctx, req.NamespacedName, &cp); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	cluster, err := ownerCluster(ctx, r.Client, &cp)
	if err != nil {
		return ctrl.Result{}, err
	}
	if paused(cluster, &cp) {
		return ctrl.Result{}, nil
	}
	if !cp.DeletionTimestamp.IsZero() {
		return r.delete(ctx, &cp, cluster)
	}
	if controllerutil.AddFinalizer(&cp, Finalizer) {
		if err := r.Client.Update(ctx, &cp); err != nil {
			return ctrl.Result{}, err
		}
	}
	if cluster == nil {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	base := cp.DeepCopy()
	res, err := r.reconcile(ctx, &cp, cluster)
	if err != nil && !errors.Is(err, errNotReady) {
		v1alpha1.SetCondition(&cp.Status.Conditions, condition(false, "Error", err.Error()))
		_ = r.Client.Status().Patch(ctx, &cp, client.MergeFrom(base))
		log.FromContext(ctx).Error(err, "reconcile failed")
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}
	if perr := r.Client.Status().Patch(ctx, &cp, client.MergeFrom(base)); perr != nil {
		return ctrl.Result{}, perr
	}
	return res, nil
}

// api returns the Cluster's CivoCluster and an API client for it.
func (r *CivoControlPlaneReconciler) api(ctx context.Context, cluster *unstructured.Unstructured) (*v1alpha1.CivoCluster, civo.API, error) {
	name, _, _ := unstructured.NestedString(cluster.Object, "spec", "infrastructureRef", "name")
	if name == "" {
		return nil, nil, fmt.Errorf("%w: Cluster has no infrastructureRef yet", errNotReady)
	}
	var cc v1alpha1.CivoCluster
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: cluster.GetNamespace(), Name: name}, &cc); err != nil {
		return nil, nil, err
	}
	tok, err := token(ctx, r.Client, &cc)
	if err != nil {
		return &cc, nil, err
	}
	a, err := r.NewAPI(tok, cc.Spec.Region)
	return &cc, a, err
}

func (r *CivoControlPlaneReconciler) reconcile(ctx context.Context, cp *v1alpha1.CivoControlPlane, cluster *unstructured.Unstructured) (ctrl.Result, error) {
	lg := log.FromContext(ctx)
	wait := func(after time.Duration, reason, msg string) (ctrl.Result, error) {
		v1alpha1.SetCondition(&cp.Status.Conditions, condition(false, reason, msg))
		return ctrl.Result{RequeueAfter: after}, errNotReady
	}
	cc, api, err := r.api(ctx, cluster)
	if err != nil {
		if errors.Is(err, errNotReady) {
			return wait(10*time.Second, "WaitingForInfrastructure", err.Error())
		}
		return ctrl.Result{}, err
	}
	if cc.Status.Initialization.Provisioned == nil || !*cc.Status.Initialization.Provisioned {
		return wait(10*time.Second, "WaitingForInfrastructure", "CivoCluster "+cc.Name+" isn't provisioned yet")
	}
	name := cluster.GetName()

	// Find or create the Civo cluster (adopting one of the same name, so a
	// lost status doesn't make a second cluster).
	var kc *civogo.KubernetesCluster
	if cp.Status.ClusterID != "" {
		kc, err = api.GetKubernetesCluster(cp.Status.ClusterID)
		if err != nil && !civo.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}
	if kc == nil {
		list, err := api.ListKubernetesClusters()
		if err != nil {
			return ctrl.Result{}, err
		}
		for i := range list.Items {
			if list.Items[i].Name == name {
				kc = &list.Items[i]
				lg.Info("adopting existing Civo cluster", "name", name, "id", kc.ID)
			}
		}
	}
	if kc == nil {
		versions, err := api.ListAvailableKubernetesVersions()
		if err != nil {
			return ctrl.Result{}, err
		}
		version, err := civo.MatchVersion(&cp.Spec, versions)
		if err != nil {
			return ctrl.Result{}, err
		}
		cfg := civo.Config(name, cc.Spec.Region, cc.Spec.NetworkID, version, &cp.Spec)
		lg.Info("creating Civo cluster", "name", name, "region", cc.Spec.Region, "version", version, "size", cfg.Pools[0].Size, "nodes", cfg.Pools[0].Count)
		kc, err = api.NewKubernetesClusters(cfg)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("creating Civo cluster: %w", err)
		}
	}
	cp.Status.ClusterID = kc.ID
	cp.Status.CivoStatus = kc.Status
	if kc.FirewallID != "" && cp.Status.FirewallID == "" {
		cp.Status.FirewallID = kc.FirewallID
		cp.Status.FirewallCreated = cp.Spec.FirewallID == ""
	}
	if kc.Status != "ACTIVE" || !kc.Ready || kc.KubeConfig == "" || kc.APIEndPoint == "" {
		return wait(20*time.Second, "Provisioning", "Civo cluster "+kc.ID+" is "+kc.Status)
	}

	ep, err := civo.Endpoint(kc.APIEndPoint)
	if err != nil {
		return ctrl.Result{}, err
	}
	if cp.Spec.ControlPlaneEndpoint != ep {
		patch := client.MergeFrom(cp.DeepCopy())
		cp.Spec.ControlPlaneEndpoint = ep
		if err := r.Client.Patch(ctx, cp, patch); err != nil {
			return ctrl.Result{}, err
		}
	}
	if err := r.writeKubeconfig(ctx, cp, name, []byte(kc.KubeConfig)); err != nil {
		return ctrl.Result{}, err
	}

	// Scale the pool to nodeCount.
	if want := int(cp.Spec.NodeCount); want > 0 && len(kc.Pools) > 0 && kc.Pools[0].Count != want {
		lg.Info("scaling Civo node pool", "pool", kc.Pools[0].ID, "from", kc.Pools[0].Count, "to", want)
		if _, err := api.UpdateKubernetesClusterPool(kc.ID, kc.Pools[0].ID, &civogo.KubernetesClusterPoolUpdateConfig{Count: &want, Region: cc.Spec.Region}); err != nil {
			return ctrl.Result{}, fmt.Errorf("scaling pool: %w", err)
		}
	}

	cp.Status.Initialization.ControlPlaneInitialized = ptr(true)
	cp.Status.Version = cp.Spec.Version
	v1alpha1.SetCondition(&cp.Status.Conditions, condition(true, "Active", fmt.Sprintf("Civo cluster %s active (%s)", kc.ID, kc.Version)))
	return ctrl.Result{RequeueAfter: 2 * time.Minute}, nil
}

func (r *CivoControlPlaneReconciler) writeKubeconfig(ctx context.Context, cp *v1alpha1.CivoControlPlane, clusterName string, kc []byte) error {
	s := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-kubeconfig", Namespace: cp.Namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, s, func() error {
		if s.Labels == nil {
			s.Labels = map[string]string{}
		}
		s.Labels[ClusterNameLabel] = clusterName
		s.Type = "cluster.x-k8s.io/secret"
		s.Data = map[string][]byte{"value": kc}
		return controllerutil.SetOwnerReference(cp, s, r.Client.Scheme())
	})
	return err
}

// delete removes the cluster the way hyve-civo-module does: PVCs first (so
// Civo reclaims their volumes — deleting the cluster abandons them), then
// the cluster, then the firewall Civo created for it.
func (r *CivoControlPlaneReconciler) delete(ctx context.Context, cp *v1alpha1.CivoControlPlane, cluster *unstructured.Unstructured) (ctrl.Result, error) {
	lg := log.FromContext(ctx)
	if !controllerutil.ContainsFinalizer(cp, Finalizer) {
		return ctrl.Result{}, nil
	}
	if cp.Status.ClusterID != "" && cluster != nil {
		_, api, err := r.api(ctx, cluster)
		if err != nil {
			lg.Info("can't reach Civo to delete the cluster; retrying", "error", err.Error())
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		kc, err := api.GetKubernetesCluster(cp.Status.ClusterID)
		switch {
		case civo.IsNotFound(err):
			// gone: fall through to the firewall
		case err != nil:
			return ctrl.Result{}, err
		default:
			if kc.Status != "DELETING" {
				if done := r.deleteVolumes(ctx, cp, kc); !done {
					return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
				}
				lg.Info("deleting Civo cluster", "id", kc.ID)
				if _, err := api.DeleteKubernetesCluster(kc.ID); err != nil && !civo.IsNotFound(err) {
					return ctrl.Result{}, err
				}
			}
			return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
		}
		if cp.Status.FirewallCreated && cp.Status.FirewallID != "" {
			if _, err := api.DeleteFirewall(cp.Status.FirewallID); err != nil && !civo.IsNotFound(err) {
				// Civo can take a moment to release it from the cluster.
				lg.Info("firewall not deletable yet; retrying", "id", cp.Status.FirewallID, "error", err.Error())
				return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
			}
			lg.Info("deleted the cluster's firewall", "id", cp.Status.FirewallID)
		}
	}
	controllerutil.RemoveFinalizer(cp, Finalizer)
	return ctrl.Result{}, r.Client.Update(ctx, cp)
}

// deleteVolumes deletes the workload cluster's PVCs and reports whether
// their PVs are gone (or the wait has timed out, or the cluster can't be
// reached — it's best effort, as in hyve-civo-module).
func (r *CivoControlPlaneReconciler) deleteVolumes(ctx context.Context, cp *v1alpha1.CivoControlPlane, kc *civogo.KubernetesCluster) bool {
	lg := log.FromContext(ctx)
	if kc.KubeConfig == "" {
		return true
	}
	timeout := r.PVCCleanupTimeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	if since := time.Since(cp.DeletionTimestamp.Time); since > timeout {
		lg.Info("giving up waiting for volumes to be reclaimed", "after", since.Round(time.Second))
		return true
	}
	cfg, err := clientcmd.RESTConfigFromKubeConfig([]byte(kc.KubeConfig))
	if err != nil {
		return true
	}
	cfg.Timeout = 15 * time.Second
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return true
	}
	pvcs, err := cs.CoreV1().PersistentVolumeClaims("").List(ctx, metav1.ListOptions{})
	if err != nil {
		lg.Info("can't list the cluster's PVCs; deleting the cluster anyway", "error", err.Error())
		return true
	}
	for _, p := range pvcs.Items {
		_ = cs.CoreV1().PersistentVolumeClaims(p.Namespace).Delete(ctx, p.Name, metav1.DeleteOptions{})
	}
	pvs, err := cs.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return true
	}
	left := 0
	for _, pv := range pvs.Items {
		if strings.Contains(pv.Spec.StorageClassName, "civo") || pv.Spec.CSI != nil {
			left++
		}
	}
	if left > 0 {
		lg.Info("waiting for volumes to be reclaimed before deleting the cluster", "pvs", left)
		return false
	}
	return true
}

// SetupWithManager registers the reconciler.
func (r *CivoControlPlaneReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).For(&v1alpha1.CivoControlPlane{}).Owns(&corev1.Secret{}).Complete(r)
}
