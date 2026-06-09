// Package naming derives the composite identifiers CLAP manages.
package naming

import "k8s.io/apimachinery/pkg/runtime/schema"

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
