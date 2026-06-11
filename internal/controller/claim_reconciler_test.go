package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/appslap/clap/internal/naming"
)

var claimGVK = schema.GroupVersionKind{Group: "appslap.io", Version: "v1", Kind: "VSHNPostgreSQL"}

// newScheme registers core types plus the claim/composite GVKs as unstructured
// so the fake client can track them.
func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	s.AddKnownTypeWithName(claimGVK, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(claimGVK.GroupVersion().WithKind(claimGVK.Kind+"List"), &unstructured.UnstructuredList{})
	compGVK := naming.CompositeGVK(claimGVK)
	s.AddKnownTypeWithName(compGVK, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(compGVK.GroupVersion().WithKind(compGVK.Kind+"List"), &unstructured.UnstructuredList{})
	return s
}

// claimStatusObj is an empty claim used to register the status subresource on
// the fake client, so Status().Update behaves like the real API server.
func claimStatusObj() *unstructured.Unstructured {
	o := &unstructured.Unstructured{}
	o.SetGroupVersionKind(claimGVK)
	return o
}

func newClaim(name, ns string) *unstructured.Unstructured {
	c := &unstructured.Unstructured{}
	c.SetGroupVersionKind(claimGVK)
	c.SetName(name)
	c.SetNamespace(ns)
	c.SetUID("uid-123")
	c.Object["status"] = map[string]any{}
	return c
}

func ctrlRequest() ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Name: "db", Namespace: "customer-a"}}
}

func getClaim(t *testing.T, cl client.Client) (*unstructured.Unstructured, error) {
	t.Helper()
	c := &unstructured.Unstructured{}
	c.SetGroupVersionKind(claimGVK)
	err := cl.Get(context.Background(), types.NamespacedName{Name: "db", Namespace: "customer-a"}, c)
	return c, err
}

// deletingClaim returns a claim already marked for deletion (finalizer present,
// DeletionTimestamp set by the fake client) with its instance namespace recorded.
func deletingClaim(t *testing.T, instanceNS string, extra ...client.Object) (client.Client, *ClaimReconciler) {
	t.Helper()
	claim := newClaim("db", "customer-a")
	claim.SetFinalizers([]string{teardownFinalizer})
	_ = unstructured.SetNestedField(claim.Object, instanceNS, "status", "instanceNamespace")
	objs := append([]client.Object{claim}, extra...)
	cl := fake.NewClientBuilder().WithScheme(newScheme()).
		WithObjects(objs...).WithStatusSubresource(claimStatusObj()).Build()
	if err := cl.Delete(context.Background(), claim); err != nil {
		t.Fatal(err)
	}
	return cl, &ClaimReconciler{Client: cl, ClaimGVK: claimGVK}
}

func composite(ns, name string) *unstructured.Unstructured {
	c := &unstructured.Unstructured{}
	c.SetGroupVersionKind(naming.CompositeGVK(claimGVK))
	c.SetNamespace(ns)
	c.SetName(name)
	return c
}

func TestTeardownDeletesCompositeFirstAndWaits(t *testing.T) {
	comp := composite("db-xyz", "db")
	instNS := &corev1.Namespace{}
	instNS.SetName("db-xyz")
	cl, r := deletingClaim(t, "db-xyz", comp, instNS)

	res, err := r.Reconcile(context.Background(), ctrlRequest())
	if err != nil {
		t.Fatal(err)
	}
	if res.RequeueAfter == 0 {
		t.Errorf("expected RequeueAfter while waiting on composite, got %v", res)
	}
	gotComp := composite("db-xyz", "db")
	if err := cl.Get(context.Background(), types.NamespacedName{Namespace: "db-xyz", Name: "db"}, gotComp); err == nil {
		t.Errorf("composite should have been deleted")
	}
	gotNS := &corev1.Namespace{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: "db-xyz"}, gotNS); err != nil {
		t.Errorf("namespace deleted too early: %v", err)
	}
	if c, err := getClaim(t, cl); err != nil || len(c.GetFinalizers()) == 0 {
		t.Errorf("finalizer should be retained while tearing down; err=%v", err)
	}
}

func TestTeardownDeletesNamespaceAfterCompositeGone(t *testing.T) {
	instNS := &corev1.Namespace{}
	instNS.SetName("db-xyz")
	cl, r := deletingClaim(t, "db-xyz", instNS) // no composite => already gone

	res, err := r.Reconcile(context.Background(), ctrlRequest())
	if err != nil {
		t.Fatal(err)
	}
	if res.RequeueAfter == 0 {
		t.Errorf("expected RequeueAfter while waiting on namespace, got %v", res)
	}
	gotNS := &corev1.Namespace{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: "db-xyz"}, gotNS); err == nil {
		t.Errorf("namespace should have been deleted")
	}
	if c, err := getClaim(t, cl); err != nil || len(c.GetFinalizers()) == 0 {
		t.Errorf("finalizer should be retained until namespace gone; err=%v", err)
	}
}

func TestTeardownRemovesFinalizerWhenAllGone(t *testing.T) {
	cl, r := deletingClaim(t, "db-xyz") // neither composite nor namespace exist

	if _, err := r.Reconcile(context.Background(), ctrlRequest()); err != nil {
		t.Fatal(err)
	}
	if _, err := getClaim(t, cl); err == nil {
		t.Errorf("claim should be gone after finalizer removed")
	}
}

func TestTeardownEmptyNamespaceRemovesFinalizer(t *testing.T) {
	cl, r := deletingClaim(t, "") // never provisioned a namespace

	if _, err := r.Reconcile(context.Background(), ctrlRequest()); err != nil {
		t.Fatal(err)
	}
	if _, err := getClaim(t, cl); err == nil {
		t.Errorf("claim should be gone when no instance namespace was recorded")
	}
}

func TestReconcileAddsFinalizer(t *testing.T) {
	claim := newClaim("db", "customer-a")
	cl := fake.NewClientBuilder().WithScheme(newScheme()).
		WithObjects(claim).WithStatusSubresource(claimStatusObj()).Build()
	r := &ClaimReconciler{Client: cl, ClaimGVK: claimGVK}

	if _, err := r.Reconcile(context.Background(), ctrlRequest()); err != nil {
		t.Fatal(err)
	}
	got, err := getClaim(t, cl)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range got.GetFinalizers() {
		if f == teardownFinalizer {
			found = true
		}
	}
	if !found {
		t.Errorf("finalizer %q not added; finalizers=%v", teardownFinalizer, got.GetFinalizers())
	}
}
