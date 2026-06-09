package naming

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestCompositeGVK(t *testing.T) {
	claim := schema.GroupVersionKind{Group: "appslap.io", Version: "v1", Kind: "VSHNPostgreSQL"}
	want := schema.GroupVersionKind{Group: "appslap.io", Version: "v1", Kind: "XVSHNPostgreSQL"}
	if got := CompositeGVK(claim); got != want {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestIsCompositeKind(t *testing.T) {
	cases := map[string]bool{
		"XVSHNPostgreSQL": true,
		"VSHNPostgreSQL":  false,
		"Xpostgres":       false, // lowercase after X -> treated as claim
		"X":               false,
		"":                false,
	}
	for kind, want := range cases {
		if got := IsCompositeKind(kind); got != want {
			t.Fatalf("IsCompositeKind(%q)=%v, want %v", kind, got, want)
		}
	}
}
