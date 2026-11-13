package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	bootstrapv1 "github.com/cbridges1/cluster-api-provider-civo/api/bootstrap/v1alpha1"
)

// CivoConfigReconciler gives each CivoConfig its bootstrap data: an empty
// Secret, as Civo builds and joins the nodes itself.
type CivoConfigReconciler struct {
	Client client.Client
}

// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=civoconfigs;civoconfigtemplates,verbs=get;list;watch
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=civoconfigs/status,verbs=get;update;patch

// Reconcile implements reconcile.Reconciler.
func (r *CivoConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var cfg bootstrapv1.CivoConfig
	if err := r.Client.Get(ctx, req.NamespacedName, &cfg); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !cfg.DeletionTimestamp.IsZero() || (cfg.Status.Initialization.DataSecretCreated != nil && *cfg.Status.Initialization.DataSecretCreated) {
		return ctrl.Result{}, nil
	}
	s := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: cfg.Name, Namespace: cfg.Namespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, s, func() error {
		if s.Labels == nil {
			s.Labels = map[string]string{}
		}
		if n := cfg.Labels[ClusterNameLabel]; n != "" {
			s.Labels[ClusterNameLabel] = n
		}
		s.Type = "cluster.x-k8s.io/secret"
		s.Data = map[string][]byte{"value": {}, "format": []byte("cloud-config")}
		return controllerutil.SetControllerReference(&cfg, s, r.Client.Scheme())
	}); err != nil {
		return ctrl.Result{}, err
	}
	base := cfg.DeepCopy()
	cfg.Status.DataSecretName = ptr(s.Name)
	cfg.Status.Initialization.DataSecretCreated = ptr(true)
	return ctrl.Result{}, r.Client.Status().Patch(ctx, &cfg, client.MergeFrom(base))
}

// SetupWithManager registers the reconciler.
func (r *CivoConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).For(&bootstrapv1.CivoConfig{}).Owns(&corev1.Secret{}).Complete(r)
}
