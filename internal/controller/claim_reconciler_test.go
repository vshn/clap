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
	c.Object["status"] = map[string]interface{}{}
	return c
}

func ctrlRequest(name, ns string) ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: ns}}
}

func getClaim(t *testing.T, cl client.Client) (*unstructured.Unstructured, error) {
	t.Helper()
	c := &unstructured.Unstructured{}
	c.SetGroupVersionKind(claimGVK)
	err := cl.Get(context.Background(), types.NamespacedName{Name: "db", Namespace: "customer-a"}, c)
	return c, err
}

func TestReconcileAddsFinalizer(t *testing.T) {
	claim := newClaim("db", "customer-a")
	cl := fake.NewClientBuilder().WithScheme(newScheme()).
		WithObjects(claim).WithStatusSubresource(claimStatusObj()).Build()
	r := &ClaimReconciler{Client: cl, ClaimGVK: claimGVK}

	if _, err := r.Reconcile(context.Background(), ctrlRequest("db", "customer-a")); err != nil {
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
