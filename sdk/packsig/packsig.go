// Package packsig checks a detached signature over a pack file's bytes before
// the pack is loaded (ADR-0083), so a program that embeds the runtime can
// refuse an unsigned or wrongly signed pack before any rule of it is read.
// "jpack packs sign" writes the signature document and "jpack packs
// verify-signature" checks one; this package applies the same check. Like
// package sdk it reads no file, no environment variable and no network: the
// pack and its signature document are byte slices the caller holds.
// Everything in this package is experimental.
//
// # The convention
//
// The signature of the pack file packs/intake.json is the document
// packs/intake.json.sig.json (FileSuffix). It is one JSON object:
//
//	{"algorithm":"ed25519","packDigest":"sha256:<64 hex>","publicKey":"<64 hex>","signature":"<128 hex>","signatureVersion":"1.0.0"}
//
// packDigest is the SHA-256 of the pack file's exact bytes, the digest a
// reviewed-set lock pins and Pack.Identity reports; no canonical form of a
// pack is introduced, so a pack re-indented or re-encoded is other bytes and
// needs a signature of its own. publicKey is the signing key's public key,
// in the form "jpack audit key public" prints. signature is Ed25519 over a
// fixed context string followed by the canonical form of
// {"packDigest","signatureVersion"}, never over the bare bytes, so it is a
// signature of a pack and of nothing else: an audit trail's record signature
// or key rotation made with the same key is not one, and the reverse.
//
// # What is checked
//
// The caller names the key it expects. A document is never trusted for the
// public key it carries: one that names another key is refused
// (CodeUnexpectedKey) whether or not its signature verifies under that key,
// and Verify and Load refuse to run without an expected key
// (CodeExpectedKey). The document is read strictly: at most
// MaxSignatureBytes, exactly the five members, each once and each of its
// exact form, and no escape. Each refusal is a *Refusal with its own code.
//
// # What a valid signature establishes, and what it does not
//
// It establishes that these exact pack bytes were signed by whoever held the
// private key of the expected public key. It does not establish that the
// pack is correct, tested, reviewed, approved or current; it carries no time,
// so it says nothing about when it was made; and it says nothing after the
// key is copied. Which key to expect, how it reaches the verifier, and when
// to stop trusting it are the verifier's: this package distributes and
// revokes no key.
package packsig

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/edkey"
	internal "github.com/Judgment-Pack/judgment-pack-runtime/internal/packsig"
	"github.com/Judgment-Pack/judgment-pack-runtime/sdk"
)

const (
	// SignatureVersion is the version of the signature document's form and
	// of the message it signs, which the document carries in band as
	// signatureVersion. Its major part moves when a member is removed or
	// renamed, a value is written in another form, or the message changes;
	// its minor part when a member is added; its patch part for any other
	// change of bytes. Verify admits this one version. Experimental.
	SignatureVersion = internal.Version
	// Algorithm is the one signature algorithm of SignatureVersion.
	Algorithm = internal.Algorithm
	// FileSuffix names a pack's signature document by convention: the pack
	// file's own name followed by FileSuffix, beside the pack.
	FileSuffix = internal.Suffix
	// MaxSignatureBytes bounds a signature document; a larger one is
	// refused (CodeTooLarge) before it is parsed.
	MaxSignatureBytes = internal.MaxBytes
)

// The codes a Refusal carries, one per way a check fails. Experimental.
const (
	// CodeExpectedKey: no expected key was given, or it is not a public key
	// a verifier may trust (a point of small order, a non-canonical encoding,
	// or no point of the curve).
	CodeExpectedKey = internal.CodeExpectedKey
	// CodeTooLarge: the document is larger than MaxSignatureBytes.
	CodeTooLarge = internal.CodeTooLarge
	// CodeMalformed: the document is not one JSON object, which includes a
	// truncated one, or it holds a backslash.
	CodeMalformed = internal.CodeMalformed
	// CodeDuplicateMember: a member is given more than once.
	CodeDuplicateMember = internal.CodeDuplicateMember
	// CodeUnknownMember: a member SignatureVersion does not define.
	CodeUnknownMember = internal.CodeUnknownMember
	// CodeMissingMember: one of the five members is missing.
	CodeMissingMember = internal.CodeMissingMember
	// CodeVersion: signatureVersion is not SignatureVersion.
	CodeVersion = internal.CodeVersion
	// CodeAlgorithm: algorithm is not Algorithm.
	CodeAlgorithm = internal.CodeAlgorithm
	// CodePublicKey: publicKey is not 64 lowercase hexadecimal characters
	// encoding a public key a verifier may trust.
	CodePublicKey = internal.CodePublicKey
	// CodePackDigest: packDigest is not "sha256:" and 64 lowercase
	// hexadecimal characters.
	CodePackDigest = internal.CodePackDigest
	// CodeValue: signature is not 128 lowercase hexadecimal characters.
	CodeValue = internal.CodeValue
	// CodeUnexpectedKey: publicKey is not the expected key.
	CodeUnexpectedKey = internal.CodeUnexpectedKey
	// CodeDigestMismatch: packDigest is not the SHA-256 of the pack bytes
	// given. The pack changed, or the document is another pack's.
	CodeDigestMismatch = internal.CodeDigestMismatch
	// CodeInvalid: the signature does not verify under the expected key over
	// the message for the pack bytes given.
	CodeInvalid = internal.CodeInvalid
)

// Refusal is a signature check that failed. Its Message repeats nothing read
// from the document or the pack. It is never a *sdk.Refusal: a pack Load
// refuses after its signature was accepted is reported with sdk.Load's own
// error. Experimental.
type Refusal struct {
	// Code is one of the Code constants.
	Code string
	// Message says, in one sentence, what failed.
	Message string
}

// Error is the refusal's code and message on one line.
func (r *Refusal) Error() string { return r.Code + ": " + r.Message }

// Signature is an accepted signature document. Experimental.
type Signature struct {
	// SignatureVersion is the document's signatureVersion.
	SignatureVersion string
	// PublicKey is the signing key's public key, 64 lowercase hexadecimal
	// characters; it is the expected key.
	PublicKey string
	// KeyID is that key's keyId: the first 32 lowercase hexadecimal
	// characters of the SHA-256 of its 32 bytes, as the audit tooling names
	// a key.
	KeyID string
	// PackDigest is the SHA-256 of the pack bytes the signature is over, in
	// the "sha256:" form Pack.Identity reports.
	PackDigest string
}

// Verify checks signature, a signature document's bytes, against pack, a
// pack file's exact bytes, and the public key the caller expects, and
// returns the accepted document. It parses nothing of the pack: a signature
// over bytes that are no pack is accepted as a signature, and sdk.Load is
// what then refuses them. A refused check is a *Refusal.
func Verify(pack, signature []byte, expected ed25519.PublicKey) (Signature, error) {
	document, refusal := internal.Verify(pack, signature, expected)
	if refusal != nil {
		return Signature{}, &Refusal{Code: refusal.Code, Message: refusal.Message}
	}
	return Signature{
		SignatureVersion: SignatureVersion,
		PublicKey:        hex.EncodeToString(document.PublicKey),
		KeyID:            edkey.ID(document.PublicKey),
		PackDigest:       internal.DigestText(document.PackDigest),
	}, nil
}

// beforeLoad runs between the signature check and sdk.Load. It does nothing;
// a test changes the caller's slice there, to hold Load to loading the bytes
// it checked.
var beforeLoad = func() {}

// errNilContext is Load's refusal of a nil context, as sdk.Load's.
var errNilContext = errors.New("packsig: nil context")

// Load checks the pack's signature and then loads the pack with sdk.Load, so
// that no rule of a pack is read before its signature is accepted. pack is
// copied once, on entry; the signature is checked over that copy and that
// copy is what sdk.Load admits, so a caller changing its slice meanwhile
// changes neither, and Pack.Identity's digest is the Signature's PackDigest.
//
// A refused signature is a *Refusal and nothing is loaded. When the
// signature is accepted and sdk.Load refuses the pack, the accepted
// Signature is returned with sdk.Load's error, a *sdk.Refusal for a pack the
// evaluator refuses, so the caller can say both. ctx is checked on entry and
// then by sdk.Load.
func Load(ctx context.Context, pack, signature []byte, expected ed25519.PublicKey, options sdk.LoadOptions) (*sdk.Pack, Signature, error) {
	if ctx == nil {
		return nil, Signature{}, errNilContext
	}
	if err := ctx.Err(); err != nil {
		return nil, Signature{}, err
	}
	held := bytes.Clone(pack)
	accepted, err := Verify(held, signature, expected)
	if err != nil {
		return nil, Signature{}, err
	}
	beforeLoad()
	loaded, err := sdk.Load(ctx, held, options)
	if err != nil {
		return nil, accepted, err
	}
	return loaded, accepted, nil
}
