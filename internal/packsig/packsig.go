// Package packsig is the detached signature over a pack file's bytes
// (ADR-0083): the signature document's form, the message an Ed25519 key signs
// for it, and the check of a document against a pack's bytes and the key a
// verifier expects. It reaches no file, environment or network, so the jpack
// CLI and the public Go API (sdk/packsig) apply the one check. The private
// half of a key never comes here: internal/audit's Signer signs the message.
//
// # The document
//
// A signature document is one JSON object with exactly five members, each a
// string, each once:
//
//	{"algorithm":"ed25519","packDigest":"sha256:<64 hex>","publicKey":"<64 hex>","signature":"<128 hex>","signatureVersion":"1.0.0"}
//
// Hexadecimal is lowercase, the form the audit tooling writes a public key
// and a signature in, so each value has one spelling. Encode writes the
// members in code-point order with no white space and a final newline. Parse
// admits white space between tokens and refuses everything else that is not
// that object: more than MaxBytes, a text that is not one JSON object, a
// text that is not UTF-8 (so no two member names decode to one), a
// backslash anywhere (no member name or value needs an escape), a member
// given twice, a member it does not define, a member missing, and a value not
// of its exact form. Each refusal has its own code.
//
// # The message
//
// The signature is Ed25519 over Message(digest): messagePrefix followed by the
// canonical form of {"packDigest","signatureVersion"}, where packDigest is the
// SHA-256 of the pack file's exact bytes. The prefix is not a prefix of, and
// does not begin with, the prefix of any other message this runtime or its
// gateway signs or checks (a trail's record signature and key rotation, a
// checkpoint witness's statement), and every one of those messages begins
// with its own prefix, so no signature of one kind is a signature of another
// under the same key.
package packsig

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"unicode/utf8"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/edkey"
)

const (
	// Version is the signature document's form and the message it signs. Its
	// major part moves when a member is removed or renamed, a value is
	// written in another form, or the message changes; its minor part when a
	// member is added; its patch part for any other change of bytes. A reader
	// admits the one version it knows.
	Version = "1.0.0"
	// Algorithm is the one signature algorithm of Version.
	Algorithm = "ed25519"
	// Suffix names a pack's signature document by convention: the pack
	// file's own name followed by Suffix, in the pack's directory.
	Suffix = ".sig.json"
	// MaxBytes bounds a signature document read whole. A document Encode
	// writes is 360 bytes.
	MaxBytes = 4096

	messagePrefix = "judgment-pack-runtime/pack-signature/1:"
)

// The refusal codes, one per way a document or a check fails.
const (
	// CodeExpectedKey is an expected key that is missing, or that is not a
	// public key a verifier may trust (edkey.Check).
	CodeExpectedKey = "JPS-PACK-SIGNATURE-EXPECTED-KEY"
	// CodeTooLarge is a document of more than MaxBytes.
	CodeTooLarge = "JPS-PACK-SIGNATURE-TOO-LARGE"
	// CodeMalformed is a document that is not one JSON object, that is not
	// UTF-8, or that holds a backslash.
	CodeMalformed = "JPS-PACK-SIGNATURE-MALFORMED"
	// CodeDuplicateMember is a member given more than once.
	CodeDuplicateMember = "JPS-PACK-SIGNATURE-DUPLICATE-MEMBER"
	// CodeUnknownMember is a member Version does not define.
	CodeUnknownMember = "JPS-PACK-SIGNATURE-UNKNOWN-MEMBER"
	// CodeMissingMember is a member Version requires and the document lacks.
	CodeMissingMember = "JPS-PACK-SIGNATURE-MISSING-MEMBER"
	// CodeVersion is a signatureVersion that is not Version.
	CodeVersion = "JPS-PACK-SIGNATURE-VERSION"
	// CodeAlgorithm is an algorithm that is not Algorithm.
	CodeAlgorithm = "JPS-PACK-SIGNATURE-ALGORITHM"
	// CodePublicKey is a publicKey that is not 64 lowercase hexadecimal
	// characters encoding a public key a verifier may trust.
	CodePublicKey = "JPS-PACK-SIGNATURE-PUBLIC-KEY"
	// CodePackDigest is a packDigest that is not "sha256:" and 64 lowercase
	// hexadecimal characters.
	CodePackDigest = "JPS-PACK-SIGNATURE-PACK-DIGEST"
	// CodeValue is a signature that is not 128 lowercase hexadecimal
	// characters.
	CodeValue = "JPS-PACK-SIGNATURE-VALUE"
	// CodeUnexpectedKey is a document whose publicKey is not the expected
	// key, whatever it signed.
	CodeUnexpectedKey = "JPS-PACK-SIGNATURE-UNEXPECTED-KEY"
	// CodeDigestMismatch is a document whose packDigest is not the SHA-256
	// of the pack bytes given: the bytes changed, or the document is
	// another pack's.
	CodeDigestMismatch = "JPS-PACK-SIGNATURE-DIGEST-MISMATCH"
	// CodeInvalid is a signature that does not verify under the expected key
	// over the message for the pack bytes given.
	CodeInvalid = "JPS-PACK-SIGNATURE-INVALID"
)

// Refusal is one failed check: its code and a message that repeats nothing
// read from the document or the pack.
type Refusal struct {
	Code    string
	Message string
}

func (r *Refusal) Error() string { return r.Message }

func refuse(code, message string) *Refusal { return &Refusal{Code: code, Message: message} }

var (
	publicKeyForm  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	digestForm     = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	signatureForm  = regexp.MustCompile(`^[0-9a-f]{128}$`)
	definedMembers = map[string]bool{"algorithm": true, "packDigest": true, "publicKey": true, "signature": true, "signatureVersion": true}
	// requiredOrder is the order a missing member is looked for in, so the
	// refusal is the same whatever else the document lacks.
	requiredOrder = []string{"signatureVersion", "algorithm", "publicKey", "packDigest", "signature"}
)

// Document is one signature document as read: the public key it names, the
// digest of the pack it is for, and the signature.
type Document struct {
	PublicKey  ed25519.PublicKey
	PackDigest [sha256.Size]byte
	Signature  []byte
}

// DigestText is the pack digest in the form a lock and the pack view write
// it: "sha256:" and 64 lowercase hexadecimal characters.
func DigestText(digest [sha256.Size]byte) string {
	return "sha256:" + hex.EncodeToString(digest[:])
}

// Message is what a pack signature signs for the pack whose file bytes have
// the SHA-256 digest: messagePrefix, then the canonical form of
// {"packDigest","signatureVersion"}. Both values need no escape, so these
// bytes are that canonical form exactly.
func Message(digest [sha256.Size]byte) []byte {
	return []byte(messagePrefix + `{"packDigest":"` + DigestText(digest) + `","signatureVersion":"` + Version + `"}`)
}

// Encode is the signature document, newline included, naming public, digest
// and signature: its members in code-point order, no white space.
func Encode(public ed25519.PublicKey, digest [sha256.Size]byte, signature []byte) []byte {
	return []byte(`{"algorithm":"` + Algorithm + `","packDigest":"` + DigestText(digest) + `","publicKey":"` + hex.EncodeToString(public) +
		`","signature":"` + hex.EncodeToString(signature) + `","signatureVersion":"` + Version + `"}` + "\n")
}

// Parse reads one signature document by the rules the package documentation
// states, and refuses it with the code of the first rule it breaks: its size,
// then its JSON, then a member given twice in document order, then its
// version, then a member it does not define, then a member it lacks, then
// each value, in requiredOrder.
func Parse(data []byte) (Document, *Refusal) {
	if len(data) > MaxBytes {
		return Document{}, refuse(CodeTooLarge, "The signature document is larger than the 4096 bytes a signature document may be.")
	}
	if !json.Valid(data) {
		return Document{}, refuse(CodeMalformed, "The signature document is not one JSON text; it may be truncated.")
	}
	if !utf8.Valid(data) {
		return Document{}, refuse(CodeMalformed, "The signature document is not UTF-8.")
	}
	if bytes.IndexByte(data, '\\') >= 0 {
		return Document{}, refuse(CodeMalformed, "The signature document holds a backslash; no member name or value of a signature document is written with an escape.")
	}
	members, refusal := objectMembers(data)
	if refusal != nil {
		return Document{}, refusal
	}
	if raw, ok := members["signatureVersion"]; ok {
		if version, ok := stringValue(raw); !ok || version != Version {
			return Document{}, refuse(CodeVersion, "The signature document's signatureVersion is not "+Version+", the one version this runtime reads.")
		}
	}
	for name := range members {
		if !definedMembers[name] {
			return Document{}, refuse(CodeUnknownMember, "The signature document has a member signature version "+Version+" does not define.")
		}
	}
	for _, name := range requiredOrder {
		if _, ok := members[name]; !ok {
			return Document{}, refuse(CodeMissingMember, "The signature document has no "+name+" member.")
		}
	}
	if algorithm, ok := stringValue(members["algorithm"]); !ok || algorithm != Algorithm {
		return Document{}, refuse(CodeAlgorithm, "The signature document's algorithm is not "+Algorithm+".")
	}
	var document Document
	publicText, ok := stringValue(members["publicKey"])
	if !ok || !publicKeyForm.MatchString(publicText) {
		return Document{}, refuse(CodePublicKey, "The signature document's publicKey is not 64 lowercase hexadecimal characters.")
	}
	document.PublicKey, _ = hex.DecodeString(publicText)
	if err := edkey.Check(document.PublicKey); err != nil {
		return Document{}, refuse(CodePublicKey, "The signature document's publicKey is not a public key a verifier may trust: "+err.Error()+".")
	}
	digestText, ok := stringValue(members["packDigest"])
	if !ok || !digestForm.MatchString(digestText) {
		return Document{}, refuse(CodePackDigest, `The signature document's packDigest is not "sha256:" and 64 lowercase hexadecimal characters.`)
	}
	digest, _ := hex.DecodeString(digestText[len("sha256:"):])
	copy(document.PackDigest[:], digest)
	signatureText, ok := stringValue(members["signature"])
	if !ok || !signatureForm.MatchString(signatureText) {
		return Document{}, refuse(CodeValue, "The signature document's signature is not 128 lowercase hexadecimal characters.")
	}
	document.Signature, _ = hex.DecodeString(signatureText)
	return document, nil
}

// objectMembers reads a valid JSON text as one object, each member's value
// kept raw, and refuses a text that is not an object or that names a member
// twice.
func objectMembers(data []byte) (map[string]json.RawMessage, *Refusal) {
	notObject := refuse(CodeMalformed, "The signature document is not a JSON object.")
	decoder := json.NewDecoder(bytes.NewReader(data))
	open, err := decoder.Token()
	if delim, ok := open.(json.Delim); err != nil || !ok || delim != '{' {
		return nil, notObject
	}
	members := map[string]json.RawMessage{}
	for decoder.More() {
		token, err := decoder.Token()
		name, ok := token.(string)
		if err != nil || !ok {
			return nil, notObject
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, notObject
		}
		if _, seen := members[name]; seen {
			return nil, refuse(CodeDuplicateMember, "The signature document names a member more than once.")
		}
		members[name] = raw
	}
	return members, nil
}

// stringValue is a raw value as a string, and false when it is not one.
func stringValue(raw json.RawMessage) (string, bool) {
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return "", false
	}
	return text, true
}

// Verify checks the signature document data against the pack file bytes pack
// and the public key the verifier expects, and returns the document read.
// expected must be a public key a verifier may trust: none is refused before
// anything is read, because a document is never checked against the key it
// carries alone. The document must then parse; name expected as its
// publicKey (CodeUnexpectedKey); name the SHA-256 of pack as its packDigest
// (CodeDigestMismatch); and carry an Ed25519 signature under expected over
// Message of that digest, computed here from pack (CodeInvalid).
func Verify(pack, data []byte, expected ed25519.PublicKey) (Document, *Refusal) {
	if len(expected) == 0 {
		return Document{}, refuse(CodeExpectedKey, "No expected public key was given; a pack signature is checked only against a key the verifier names.")
	}
	if err := edkey.Check(expected); err != nil {
		return Document{}, refuse(CodeExpectedKey, "The expected public key is not one a verifier may trust: "+err.Error()+".")
	}
	document, refusal := Parse(data)
	if refusal != nil {
		return Document{}, refusal
	}
	if !bytes.Equal(document.PublicKey, expected) {
		return Document{}, refuse(CodeUnexpectedKey, "The signature names a public key that is not the expected key.")
	}
	digest := sha256.Sum256(pack)
	if document.PackDigest != digest {
		return Document{}, refuse(CodeDigestMismatch, "The signature is for other bytes than these: its packDigest is not the SHA-256 of the pack file.")
	}
	if !ed25519.Verify(expected, Message(digest), document.Signature) {
		return Document{}, refuse(CodeInvalid, "The signature does not verify under the expected key over these pack bytes.")
	}
	return document, nil
}
