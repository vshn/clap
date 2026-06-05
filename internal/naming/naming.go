// Package naming derives the namespace and composite identifiers CLAP manages.
package naming

import (
	"crypto/rand"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	suffixLen      = 5
	suffixAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
)

// RandomSuffix returns a random lowercase-alphanumeric string of fixed length.
func RandomSuffix() (string, error) {
	b := make([]byte, suffixLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, suffixLen)
	for i, c := range b {
		out[i] = suffixAlphabet[int(c)%len(suffixAlphabet)]
	}
	return string(out), nil
}

// InstanceNamespace builds the instance namespace name for a claim.
func InstanceNamespace(claimName, suffix string) string {
	return fmt.Sprintf("%s-%s", claimName, suffix)
}

// CompositeGVK maps a claim GVK to its composite GVK: same group/version,
// kind prefixed with "X".
func CompositeGVK(claim schema.GroupVersionKind) schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   claim.Group,
		Version: claim.Version,
		Kind:    "X" + claim.Kind,
	}
}

// IsCompositeKind reports whether a kind is a composite, i.e. "X" followed by
// an uppercase letter (XVSHNPostgreSQL). Claim kinds that merely start with a
// lowercase letter after X (Xpostgres) are not treated as composites.
func IsCompositeKind(kind string) bool {
	if len(kind) < 2 || kind[0] != 'X' {
		return false
	}
	return kind[1] >= 'A' && kind[1] <= 'Z'
}
