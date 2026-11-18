package federation

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"slices"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/edkey"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/regsig"
)

// The verifier reads a signature document of either kind (§10) as §11 reads
// one, with each failure's pointer and the document's key identifier, which a
// report carries (§2.1). readSourceSignature, which a lock writer calls, is
// the same reading without them.

// signatureDocument is a signature document read as §11 reads one. root is
// nil for a document that is not one JSON object (LOCK-SIG-DOCUMENT); keyID
// is the identifier of its publicKey when that member is of its form, trusted
// or not, and "" otherwise.
type signatureDocument struct {
	kind  regsig.Kind
	root  map[string]any
	keyID string
	// identity is the document's digest (registry §2.3), which a lock names
	// as the source signature it accepted; set only for a document read
	// with no failure.
	identity string
}

// signatureProblem is one failure of a read or a check: its code and the
// pointer of the member it is at ("" for the document).
type signatureProblem struct{ code, pointer string }

// readSignature reads a signature document of a kind, with one code per
// failure (§11): over its bound, not one JSON object in UTF-8, a member
// given twice, a backslash or any other document registry §2.2 refuses, such
// as an integer above 2^53 - 1 (LOCK-SIG-DOCUMENT, and nothing else is read);
// a version other than "1.1.0", checked first (LOCK-SIG-VERSION, and nothing
// else is checked); a member not defined or missing (LOCK-SIG-MEMBER); an
// algorithm other than ed25519 (LOCK-SIG-ALGORITHM); a public key not of its
// form or refused by the trust rule (LOCK-SIG-PUBLIC-KEY); a digest or a
// signature not of its form (LOCK-SIG-FORM). The document is admitted as
// registry §2.2 admits one before any member is read, so a number it refuses
// is LOCK-SIG-DOCUMENT wherever it is, and an admitted number in a member
// defined is that member's code.
func readSignature(document registry.Document, kind regsig.Kind) (*signatureDocument, []signatureProblem) {
	whole := []signatureProblem{{"LOCK-SIG-DOCUMENT", ""}}
	if document.Oversize || len(document.Data) > regsig.MaxBytes || bytes.IndexByte(document.Data, '\\') >= 0 {
		return nil, whole
	}
	value, code, _ := registry.AdmitValue(document.Data, regsig.MaxBytes)
	root, isObject := value.(map[string]any)
	if code != "" || !isObject {
		return nil, whole
	}
	read := &signatureDocument{kind: kind, root: root}
	key, keyIsString := root["publicKey"].(string)
	keyOfForm := keyIsString && hex64.MatchString(key)
	if keyOfForm {
		public, _ := hex.DecodeString(key)
		read.keyID = edkey.ID(public)
	}
	if version, present := root[kind.VersionMember]; present && version != regsig.Version {
		return read, []signatureProblem{{"LOCK-SIG-VERSION", memberPointer(kind.VersionMember)}}
	}
	var problems []signatureProblem
	members := kind.Members()
	for _, name := range sortedKeys(root) {
		if !slices.Contains(members, name) {
			problems = append(problems, signatureProblem{"LOCK-SIG-MEMBER", memberPointer(name)})
		}
	}
	for _, name := range members {
		if _, present := root[name]; !present {
			problems = append(problems, signatureProblem{"LOCK-SIG-MEMBER", memberPointer(name)})
		}
	}
	if algorithm, present := root["algorithm"]; present && algorithm != regsig.Algorithm {
		problems = append(problems, signatureProblem{"LOCK-SIG-ALGORITHM", "/algorithm"})
	}
	if _, present := root["publicKey"]; present && !(keyOfForm && trustedKey(key)) {
		problems = append(problems, signatureProblem{"LOCK-SIG-PUBLIC-KEY", "/publicKey"})
	}
	if digest, present := root[kind.DigestMember]; present && !isDigest(digest) {
		problems = append(problems, signatureProblem{"LOCK-SIG-FORM", memberPointer(kind.DigestMember)})
	}
	if signature, present := root["signature"]; present {
		if text, isString := signature.(string); !isString || !hex128.MatchString(text) {
			problems = append(problems, signatureProblem{"LOCK-SIG-FORM", "/signature"})
		}
	}
	if len(problems) > 0 {
		return read, problems
	}
	identity, err := registry.Digest(root)
	if err != nil {
		return nil, whole
	}
	read.identity = identity
	return read, nil
}

// memberPointer is the JSON Pointer of a member of the document's root.
func memberPointer(name string) string { return carrier.Pointer([]string{name}) }

// check holds a document read with no failure against a key and a digest,
// in §11's order, and returns the first failure, or nil: the document's key
// is not that key, compared as 32 bytes (LOCK-SIG-UNEXPECTED-KEY); its
// digest member is not that digest (LOCK-SIG-DIGEST-MISMATCH); the signature
// does not verify under that key over §10.3's message (LOCK-SIG-INVALID).
// Both keys have passed the trust rule (edkey.Check) before this is called:
// the expected one in step 1, the document's when it was read.
// ed25519.Verify is cofactorless, refuses an S of L or more and compares R
// byte for byte, the equation checkpoint-witness §5 states.
func (d *signatureDocument) check(key, digest string) *signatureProblem {
	expected, err := hex.DecodeString(key)
	public, _ := hex.DecodeString(d.root["publicKey"].(string))
	if err != nil || len(expected) != ed25519.PublicKeySize || !bytes.Equal(public, expected) {
		return &signatureProblem{"LOCK-SIG-UNEXPECTED-KEY", "/publicKey"}
	}
	if d.root[d.kind.DigestMember] != digest {
		return &signatureProblem{"LOCK-SIG-DIGEST-MISMATCH", memberPointer(d.kind.DigestMember)}
	}
	message, err := d.kind.Message(digest)
	signature, _ := hex.DecodeString(d.root["signature"].(string))
	if err != nil || !ed25519.Verify(expected, message, signature) {
		return &signatureProblem{"LOCK-SIG-INVALID", "/signature"}
	}
	return nil
}
