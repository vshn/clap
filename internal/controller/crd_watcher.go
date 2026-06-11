package controller

import (
	"context"
	"fmt"
	"strings"
	"sync"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/appslap/clap/internal/naming"
)

// ClaimGroup is the API group whose CRDs CLAP treats as claims.
const ClaimGroup = "appslap.io"

//+kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;delete
//+kubebuilder:rbac:groups=appslap.io,resources=*,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=appslap.io,resources=*/status,verbs=get;update;patch

// CRDWatcher discovers claim CRDs and starts a dynamic controller per claim GVK.
// It is both a reconciler (for CRDs) and a manager.Runnable (to capture the
// manager's base context used to start child controllers).
type CRDWatcher struct {
	client.Client
	Manager manager.Manager

	mu      sync.Mutex
	started map[schema.GroupVersionKind]bool
	baseCtx context.Context
}

// Start implements manager.Runnable; it captures the long-lived context the
// dynamic controllers run under and blocks until shutdown.
func (w *CRDWatcher) Start(ctx context.Context) error {
	w.mu.Lock()
	w.baseCtx = ctx
	if w.started == nil {
		w.started = map[schema.GroupVersionKind]bool{}
	}
	w.mu.Unlock()
	<-ctx.Done()
	return nil
}

func (w *CRDWatcher) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err := w.Get(ctx, req.NamespacedName, crd); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if crd.Spec.Group != ClaimGroup {
		return ctrl.Result{}, nil
	}
	if naming.IsCompositeKind(crd.Spec.Names.Kind) {
		return ctrl.Result{}, nil // composites are managed indirectly
	}
	version := storageVersion(crd)
	if version == "" {
		return ctrl.Result{}, nil
	}
	gvk := schema.GroupVersionKind{Group: crd.Spec.Group, Version: version, Kind: crd.Spec.Names.Kind}
	return ctrl.Result{}, w.ensureController(gvk)
}

func storageVersion(crd *apiextensionsv1.CustomResourceDefinition) string {
	for _, v := range crd.Spec.Versions {
		if v.Storage {
			return v.Name
		}
	}
	return ""
}

func (w *CRDWatcher) ensureController(gvk schema.GroupVersionKind) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.baseCtx == nil {
		// Manager not started yet; returning an error requeues this CRD.
		return fmt.Errorf("manager base context not ready")
	}
	if w.started[gvk] {
		return nil
	}

	c, err := controller.NewUnmanaged("claim-"+strings.ToLower(gvk.Kind), controller.Options{
		Reconciler: &ClaimReconciler{Client: w.Client, ClaimGVK: gvk},
	})
	if err != nil {
		return err
	}

	claim := &unstructured.Unstructured{}
	claim.SetGroupVersionKind(gvk)
	if err := c.Watch(source.Kind(
		w.Manager.GetCache(),
		client.Object(claim),
		&handler.TypedEnqueueRequestForObject[client.Object]{},
	)); err != nil {
		return err
	}

	// Also watch the composite so its status changes re-reconcile the claim.
	// The claim lives in a different namespace, read from the annotation CLAP
	// sets on the composite.
	comp := &unstructured.Unstructured{}
	comp.SetGroupVersionKind(naming.CompositeGVK(gvk))
	if err := c.Watch(source.Kind(
		w.Manager.GetCache(),
		client.Object(comp),
		handler.EnqueueRequestsFromMapFunc(func(_ context.Context, o client.Object) []reconcile.Request {
			ns := o.GetAnnotations()[claimNamespaceAnnotation]
			if ns == "" {
				return nil
			}
			return []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: ns, Name: o.GetName()}}}
		}),
	)); err != nil {
		return err
	}

	go func() {
		if err := c.Start(w.baseCtx); err != nil {
			log.FromContext(w.baseCtx).Error(err, "dynamic claim controller stopped", "gvk", gvk.String())
		}
	}()
	w.started[gvk] = true
	log.FromContext(w.baseCtx).Info("started dynamic claim controller", "gvk", gvk.String())
	return nil
}

// SetupWithManager registers the CRD watch controller and the Runnable.
func (w *CRDWatcher) SetupWithManager(mgr ctrl.Manager) error {
	w.Manager = mgr
	w.started = map[schema.GroupVersionKind]bool{}
	if err := mgr.Add(w); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&apiextensionsv1.CustomResourceDefinition{}).
		Complete(w)
}
