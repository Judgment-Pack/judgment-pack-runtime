package audit

import "github.com/Judgment-Pack/judgment-pack-runtime/internal/edkey"

// A public key a verifier is given is held to more than its length (#216):
// package edkey says why. Every public key this runtime reads is held to
// CheckPublicKey: each --public-key, each revocation's, and a rotation's
// next, and the expected key of a pack signature (internal/packsig). The rule
// lives in edkey, which reaches no file, so the pack-signature check of the
// public Go API applies the same one; these names are that rule's, unchanged.

// The refusals of a public key. Each reads after "is not one: ". They are
// edkey's own values, so errors.Is matches either name.
var (
	// ErrPublicKeyForm is a public key that is not 64 hexadecimal characters,
	// 32 bytes.
	ErrPublicKeyForm = edkey.ErrForm
	// ErrPublicKeyNotCanonical is an encoding a verifier reads as another:
	// its y is p or more, or its x is 0 and its sign bit is set.
	ErrPublicKeyNotCanonical = edkey.ErrNotCanonical
	// ErrPublicKeySmallOrder is a point whose order divides 8.
	ErrPublicKeySmallOrder = edkey.ErrSmallOrder
	// ErrPublicKeyNotOnCurve is 32 bytes that encode no point of the curve,
	// under which nothing verifies.
	ErrPublicKeyNotOnCurve = edkey.ErrNotOnCurve
)

// CheckPublicKey says why 32 bytes are no public key a verifier may trust, or
// nil: they must be the canonical encoding (RFC 8032 §5.1.2) of a point of the
// curve whose order does not divide 8 (edkey.Check).
func CheckPublicKey(public []byte) error {
	return edkey.Check(public)
}
