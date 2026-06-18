package controller

import (
	"context"
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func xrdScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = apiextensionsv1.AddToScheme(s)
	s.AddKnownTypeWithName(xrdGVK, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(xrdGVK.GroupVersion().WithKind(xrdGVK.Kind+"List"), &unstructured.UnstructuredList{})
	return s
}

// newXRD builds a minimal but valid appslap-shaped XRD as unstructured.
func newXRD(group, compositeKind, plural string) *unstructured.Unstructured {
	x := &unstructured.Unstructured{}
	x.SetGroupVersionKind(xrdGVK)
	x.SetName(plural + "." + group)
	x.SetUID("xrd-uid")
	_ = unstructured.SetNestedField(x.Object, group, "spec", "group")
	_ = unstructured.SetNestedField(x.Object, map[string]any{
		"kind":     compositeKind,
		"listKind": compositeKind + "List",
		"plural":   plural,
		"singular": strings.ToLower(compositeKind),
	}, "spec", "names")
	_ = unstructured.SetNestedSlice(x.Object, []any{
		map[string]any{
			"name":          "v1",
			"served":        true,
			"referenceable": true,
			"schema": map[string]any{
				"openAPIV3Schema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"spec": map[string]any{
							"type":                                 "object",
							"x-kubernetes-preserve-unknown-fields": true,
						},
					},
				},
			},
		},
	}, "spec", "versions")
	return x
}

func TestXRDReconcileCreatesClaimCRD(t *testing.T) {
	xrd := newXRD("appslap.io", "XVSHNPostgreSQL", "xvshnpostgresqls")
	cl := fake.NewClientBuilder().WithScheme(xrdScheme()).WithObjects(xrd).Build()
	r := &XRDReconciler{Client: cl}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: xrd.GetName()}}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatal(err)
	}

	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: "vshnpostgresqls.appslap.io"}, crd); err != nil {
		t.Fatalf("claim CRD not created: %v", err)
	}
	if crd.Spec.Names.Kind != "VSHNPostgreSQL" {
		t.Errorf("kind = %q, want VSHNPostgreSQL", crd.Spec.Names.Kind)
	}
	if crd.Spec.Names.Plural != "vshnpostgresqls" {
		t.Errorf("plural = %q, want vshnpostgresqls", crd.Spec.Names.Plural)
	}
	if crd.Spec.Scope != apiextensionsv1.NamespaceScoped {
		t.Errorf("scope = %q, want Namespaced", crd.Spec.Scope)
	}
	if len(crd.Spec.Versions) != 1 || !crd.Spec.Versions[0].Storage {
		t.Fatalf("expected one storage version, got %+v", crd.Spec.Versions)
	}
	if !crd.Spec.Versions[0].Served {
		t.Errorf("expected version to be served (XRD versions default to served)")
	}
	if crd.Spec.Versions[0].Subresources == nil || crd.Spec.Versions[0].Subresources.Status == nil {
		t.Errorf("expected status subresource enabled")
	}
	if crd.Spec.Versions[0].Schema == nil || crd.Spec.Versions[0].Schema.OpenAPIV3Schema == nil {
		t.Errorf("expected schema copied from XRD")
	}
	if len(crd.OwnerReferences) != 1 || crd.OwnerReferences[0].Name != xrd.GetName() || crd.OwnerReferences[0].Kind != "CompositeResourceDefinition" {
		t.Errorf("expected ownerReference to XRD, got %+v", crd.OwnerReferences)
	}
}

func TestXRDReconcileIgnoresForeignGroup(t *testing.T) {
	xrd := newXRD("example.org", "XThing", "xthings")
	cl := fake.NewClientBuilder().WithScheme(xrdScheme()).WithObjects(xrd).Build()
	r := &XRDReconciler{Client: cl}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: xrd.GetName()}}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	list := &apiextensionsv1.CustomResourceDefinitionList{}
	if err := cl.List(context.Background(), list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 0 {
		t.Errorf("expected no CRD for foreign group, got %d", len(list.Items))
	}
}

func TestXRDReconcileUpdatesExistingCRD(t *testing.T) {
	xrd := newXRD("appslap.io", "XVSHNPostgreSQL", "xvshnpostgresqls")
	cl := fake.NewClientBuilder().WithScheme(xrdScheme()).WithObjects(xrd).Build()
	r := &XRDReconciler{Client: cl}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: xrd.GetName()}}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatal(err)
	}

	// Add a second, served-but-not-storage version.
	versions, _, _ := unstructured.NestedSlice(xrd.Object, "spec", "versions")
	versions = append(versions, map[string]any{
		"name":          "v2",
		"served":        true,
		"referenceable": false,
		"schema":        map[string]any{"openAPIV3Schema": map[string]any{"type": "object"}},
	})
	_ = unstructured.SetNestedSlice(xrd.Object, versions, "spec", "versions")
	if err := cl.Update(context.Background(), xrd); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatal(err)
	}

	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: "vshnpostgresqls.appslap.io"}, crd); err != nil {
		t.Fatal(err)
	}
	if len(crd.Spec.Versions) != 2 {
		t.Errorf("expected CRD updated to 2 versions, got %d", len(crd.Spec.Versions))
	}
}
