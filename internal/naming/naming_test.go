package naming

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestRandomSuffix(t *testing.T) {
	s, err := RandomSuffix()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(s) != suffixLen {
		t.Fatalf("expected length %d, got %d (%q)", suffixLen, len(s), s)
	}
	for _, c := range s {
		if !strings.ContainsRune(suffixAlphabet, c) {
			t.Fatalf("char %q not in alphabet", c)
		}
	}
}

func TestRandomSuffixUnique(t *testing.T) {
	a, _ := RandomSuffix()
	b, _ := RandomSuffix()
	if a == b {
		t.Fatalf("expected different suffixes, both %q", a)
	}
}

func TestInstanceNamespace(t *testing.T) {
	if got := InstanceNamespace("mydb", "x7f2k"); got != "mydb-x7f2k" {
		t.Fatalf("expected mydb-x7f2k, got %q", got)
	}
}

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
