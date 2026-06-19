package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/appslap/clap/internal/naming"
)

// xrdGVK is the XRD GVK CLAP watches; v2 is Crossplane 2.0 (v1 is 1.x).
var xrdGVK = schema.GroupVersionKind{
	Group:   "apiextensions.crossplane.io",
	Version: "v2",
	Kind:    "CompositeResourceDefinition",
}

// +kubebuilder:rbac:groups=apiextensions.crossplane.io,resources=compositeresourcedefinitions,verbs=get;list;watch
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch;create;update;patch;delete

// XRDReconciler generates a claim CRD per appslap.io XRD. An ownerReference on
// the CRD lets GC delete it with its XRD, so there's no delete path here.
type XRDReconciler struct {
	client.Client
}

func (r *XRDReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	xrd := &unstructured.Unstructured{}
	xrd.SetGroupVersionKind(xrdGVK)
	if err := r.Get(ctx, req.NamespacedName, xrd); err != nil {
		// XRD gone: its owned CRD is being garbage-collected. Nothing to do.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	group, _, _ := unstructured.NestedString(xrd.Object, "spec", "group")
	if group != ClaimGroup {
		return ctrl.Result{}, nil
	}

	// A non-X composite would yield a claim CRD colliding with the composite's.
	compositeKind, _, _ := unstructured.NestedString(xrd.Object, "spec", "names", "kind")
	if !naming.IsCompositeKind(compositeKind) {
		log.FromContext(ctx).Info("XRD composite kind not X-prefixed, skipping", "kind", compositeKind)
		return ctrl.Result{}, nil
	}

	desired, err := buildClaimCRD(xrd, group)
	if err != nil {
		// Malformed XRD: skip without requeue rather than error-loop.
		log.FromContext(ctx).Info("skipping XRD", "xrd", xrd.GetName(), "reason", err.Error())
		return ctrl.Result{}, nil
	}

	crd := &apiextensionsv1.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{Name: desired.Name}}
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, crd, func() error {
		crd.Spec = desired.Spec
		crd.OwnerReferences = desired.OwnerReferences
		return nil
	})
	return ctrl.Result{}, err
}

// buildClaimCRD renders the claim CRD from an XRD: composite names with the
// leading X stripped, versions/schema copied verbatim, Namespaced scope.
func buildClaimCRD(xrd *unstructured.Unstructured, group string) (*apiextensionsv1.CustomResourceDefinition, error) {
	// Read fields individually: spec.names also holds shortNames/categories
	// (arrays), so NestedStringMap would reject the whole map.
	name := func(field string) string {
		s, _, _ := unstructured.NestedString(xrd.Object, "spec", "names", field)
		return naming.StripXPrefix(s)
	}
	names := apiextensionsv1.CustomResourceDefinitionNames{
		Kind:     name("kind"),
		ListKind: name("listKind"),
		Plural:   name("plural"),
		Singular: name("singular"),
	}
	if names.Plural == "" {
		return nil, fmt.Errorf("XRD has no spec.names.plural")
	}

	versions, err := buildVersions(xrd)
	if err != nil {
		return nil, err
	}
	if !slices.ContainsFunc(versions, func(v apiextensionsv1.CustomResourceDefinitionVersion) bool { return v.Storage }) {
		return nil, fmt.Errorf("XRD has no referenceable (storage) version")
	}

	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: names.Plural + "." + group,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: xrdGVK.GroupVersion().String(),
				Kind:       xrdGVK.Kind,
				Name:       xrd.GetName(),
				UID:        xrd.GetUID(),
			}},
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group:    group,
			Scope:    apiextensionsv1.NamespaceScoped,
			Names:    names,
			Versions: versions,
		},
	}, nil
}

// buildVersions maps XRD versions to claim CRD versions: storage = the XRD's
// referenceable flag, status subresource on (ClaimReconciler writes status).
func buildVersions(xrd *unstructured.Unstructured) ([]apiextensionsv1.CustomResourceDefinitionVersion, error) {
	raw, found, err := unstructured.NestedSlice(xrd.Object, "spec", "versions")
	if err != nil || !found || len(raw) == 0 {
		return nil, fmt.Errorf("XRD has no spec.versions")
	}
	out := make([]apiextensionsv1.CustomResourceDefinitionVersion, 0, len(raw))
	for _, v := range raw {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		served, _ := m["served"].(bool)
		referenceable, _ := m["referenceable"].(bool)

		schemaMap, _, _ := unstructured.NestedMap(m, "schema", "openAPIV3Schema")
		if schemaMap == nil {
			return nil, fmt.Errorf("version %q has no schema.openAPIV3Schema", name)
		}
		props, err := toJSONSchemaProps(schemaMap)
		if err != nil {
			return nil, fmt.Errorf("version %q schema: %w", name, err)
		}
		out = append(out, apiextensionsv1.CustomResourceDefinitionVersion{
			Name:    name,
			Served:  served,
			Storage: referenceable,
			Schema:  &apiextensionsv1.CustomResourceValidation{OpenAPIV3Schema: props},
			Subresources: &apiextensionsv1.CustomResourceSubresources{
				Status: &apiextensionsv1.CustomResourceSubresourceStatus{},
			},
		})
	}
	return out, nil
}

// toJSONSchemaProps converts the unstructured schema to typed via a JSON
// round-trip (JSONSchemaProps carries matching json tags).
func toJSONSchemaProps(m map[string]any) (*apiextensionsv1.JSONSchemaProps, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	props := &apiextensionsv1.JSONSchemaProps{}
	if err := json.Unmarshal(data, props); err != nil {
		return nil, err
	}
	return props, nil
}

func (r *XRDReconciler) SetupWithManager(mgr ctrl.Manager) error {
	xrd := &unstructured.Unstructured{}
	xrd.SetGroupVersionKind(xrdGVK)
	return ctrl.NewControllerManagedBy(mgr).
		For(xrd).
		Named("xrd").
		Complete(r)
}
