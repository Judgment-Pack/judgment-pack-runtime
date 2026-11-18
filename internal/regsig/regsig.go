// Package regsig is the two signature documents of registry format 1.1.0,
// proposed (§10): the lock signature and the source signature, the message an
// Ed25519 key signs for each (§10.3), and the document a signer writes. It
// reaches no file, environment or network. The private half of a key never
// comes here: internal/audit's Signer signs the message, and
// internal/federation reads and checks a document.
//
// # The messages
//
// Each signature is pure Ed25519, with no context string, over a prefix and
// the JCS of two members, the document's digest member and its version:
//
//	judgment-pack-registry/lock-signature/1:{"lockDigest":"sha256:<64 hex>","lockSignatureVersion":"1.1.0"}
//	judgment-pack-registry/source-signature/1:{"sourceDigest":"sha256:<64 hex>","sourceSignatureVersion":"1.1.0"}
//
// Neither prefix is a prefix of the other, nor of a message of another kind
// this runtime signs or checks (a pack signature, a trail's record signature
// and key rotation, a checkpoint witness's statement), and every message
// begins with its own: no signature of one kind is one of another, even under
// one key.
//
// # The documents
//
// Each has exactly five string members, written in code-point order with no
// white space and one final line feed: for a lock, algorithm, lockDigest,
// lockSignatureVersion, publicKey, signature; for a source, algorithm,
// publicKey, signature, sourceDigest, sourceSignatureVersion. Hexadecimal is
// lowercase.
//
// The format is proposed: what this package writes may change before the
// text is released.
package regsig

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"regexp"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
)

// Version is both documents' selector (§10).
const Version = "1.1.0"

// Algorithm is both documents' algorithm.
const Algorithm = "ed25519"

// MaxBytes is a signature document's bound (§2).
const MaxBytes = 4096

// Kind is one of the two documents: the name of its version member, the name
// of its digest member, and its message's prefix.
type Kind struct {
	VersionMember, DigestMember, Prefix string
}

var (
	// Lock is the lock signature (§10.1).
	Lock = Kind{VersionMember: "lockSignatureVersion", DigestMember: "lockDigest", Prefix: "judgment-pack-registry/lock-signature/1:"}
	// Source is the source signature (§10.2).
	Source = Kind{VersionMember: "sourceSignatureVersion", DigestMember: "sourceDigest", Prefix: "judgment-pack-registry/source-signature/1:"}
)

// ErrDigest refuses a digest not of the form sha256: and 64 lowercase
// hexadecimal characters, which no message and no document is made of.
var ErrDigest = errors.New("regsig: a digest is sha256: and 64 lowercase hexadecimal characters")

var digestForm = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// ValidDigest reports whether text is a digest of §10's form.
func ValidDigest(text string) bool { return digestForm.MatchString(text) }

// Members are the kind's five members, in code-point order.
func (k Kind) Members() []string {
	members := []string{"algorithm", k.DigestMember, k.VersionMember, "publicKey", "signature"}
	if k == Source {
		members = []string{"algorithm", "publicKey", "signature", k.DigestMember, k.VersionMember}
	}
	return members
}

// Message is what a key signs for a digest: the kind's prefix and the JCS of
// its digest member and its version. A digest not of its form is ErrDigest.
func (k Kind) Message(digest string) ([]byte, error) {
	if !ValidDigest(digest) {
		return nil, ErrDigest
	}
	signed, err := jcs.EncodeDocument(map[string]any{k.DigestMember: digest, k.VersionMember: Version})
	if err != nil {
		return nil, err
	}
	return append([]byte(k.Prefix), signed...), nil
}

// Encode is the document a signer writes: the five members, in code-point
// order with no white space, and one final line feed. A digest not of its
// form, a public key of another length or a signature of another length is
// refused.
func (k Kind) Encode(public ed25519.PublicKey, digest string, signature []byte) ([]byte, error) {
	if !ValidDigest(digest) {
		return nil, ErrDigest
	}
	if len(public) != ed25519.PublicKeySize || len(signature) != ed25519.SignatureSize {
		return nil, errors.New("regsig: a key is 32 bytes and a signature 64")
	}
	document, err := jcs.EncodeDocument(map[string]any{
		"algorithm":     Algorithm,
		k.DigestMember:  digest,
		k.VersionMember: Version,
		"publicKey":     hex.EncodeToString(public),
		"signature":     hex.EncodeToString(signature),
	})
	if err != nil {
		return nil, err
	}
	return append(document, '\n'), nil
}
