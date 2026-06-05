package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/appslap/clap/internal/naming"
)

// ClaimReconciler reconciles a single claim GVK. One instance is created per
// dynamically discovered claim kind by the CRDWatcher.
type ClaimReconciler struct {
	client.Client
	ClaimGVK schema.GroupVersionKind
}

func (r *ClaimReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	claim := &unstructured.Unstructured{}
	claim.SetGroupVersionKind(r.ClaimGVK)
	if err := r.Get(ctx, req.NamespacedName, claim); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// 1. Ensure the instance namespace. Reuse from status; never regenerate.
	ns, _, err := unstructured.NestedString(claim.Object, "status", "instanceNamespace")
	if err != nil {
		return ctrl.Result{}, err
	}
	if ns == "" {
		suffix, err := naming.RandomSuffix()
		if err != nil {
			return ctrl.Result{}, err
		}
		ns = naming.InstanceNamespace(claim.GetName(), suffix)
		if err := r.ensureNamespace(ctx, ns); err != nil {
			return ctrl.Result{}, err
		}
		if err := unstructured.SetNestedField(claim.Object, ns, "status", "instanceNamespace"); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Status().Update(ctx, claim); err != nil {
			return ctrl.Result{}, err
		}
	} else if err := r.ensureNamespace(ctx, ns); err != nil {
		return ctrl.Result{}, err
	}

	// 2. Ensure the composite, copying the claim spec.
	if err := r.ensureComposite(ctx, claim, ns); err != nil {
		return ctrl.Result{}, err
	}

	// 3. Sync the composite status back to the claim.
	if err := r.syncStatus(ctx, claim, ns); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *ClaimReconciler) ensureNamespace(ctx context.Context, name string) error {
	ns := &corev1.Namespace{}
	err := r.Get(ctx, types.NamespacedName{Name: name}, ns)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	if err := r.Create(ctx, ns); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

// buildComposite returns the desired composite for a claim. Only the spec is
// copied; labels and annotations are intentionally NOT propagated (avoids
// ArgoCD ownership conflicts).
func buildComposite(claim *unstructured.Unstructured, namespace string) *unstructured.Unstructured {
	comp := &unstructured.Unstructured{}
	comp.SetGroupVersionKind(naming.CompositeGVK(claim.GroupVersionKind()))
	comp.SetNamespace(namespace)
	comp.SetName(claim.GetName())
	if spec, found, _ := unstructured.NestedMap(claim.Object, "spec"); found {
		// NestedMap returns a deep copy, safe to set directly.
		_ = unstructured.SetNestedMap(comp.Object, spec, "spec")
	}
	return comp
}

func (r *ClaimReconciler) ensureComposite(ctx context.Context, claim *unstructured.Unstructured, namespace string) error {
	desired := buildComposite(claim, namespace)
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(desired.GroupVersionKind())
	err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: desired.GetName()}, existing)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}
	// Reconcile spec onto the existing composite.
	spec, _, _ := unstructured.NestedMap(desired.Object, "spec")
	if err := unstructured.SetNestedMap(existing.Object, spec, "spec"); err != nil {
		return err
	}
	return r.Update(ctx, existing)
}

func (r *ClaimReconciler) syncStatus(ctx context.Context, claim *unstructured.Unstructured, namespace string) error {
	comp := &unstructured.Unstructured{}
	comp.SetGroupVersionKind(naming.CompositeGVK(claim.GroupVersionKind()))
	if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: claim.GetName()}, comp); err != nil {
		return client.IgnoreNotFound(err)
	}
	compStatus, found, err := unstructured.NestedMap(comp.Object, "status")
	if err != nil {
		return err
	}
	if !found {
		return nil // composite has no status yet
	}
	// Verbatim copy, preserving the CLAP-managed instanceNamespace field.
	compStatus["instanceNamespace"] = namespace
	if err := unstructured.SetNestedMap(claim.Object, compStatus, "status"); err != nil {
		return err
	}
	return r.Status().Update(ctx, claim)
}
