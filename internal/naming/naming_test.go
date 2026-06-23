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

func TestStripXPrefix(t *testing.T) {
	cases := map[string]string{
		"XVSHNPostgreSQL":     "VSHNPostgreSQL",
		"XVSHNPostgreSQLList": "VSHNPostgreSQLList",
		"xvshnpostgresqls":    "vshnpostgresqls",
		"xvshnpostgresql":     "vshnpostgresql",
		"Foo":                 "Foo", // no leading X/x, unchanged
		"":                    "",
	}
	for in, want := range cases {
		if got := StripXPrefix(in); got != want {
			t.Errorf("StripXPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}
