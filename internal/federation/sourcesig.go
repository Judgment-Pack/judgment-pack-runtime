package federation

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"regexp"
	"slices"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/edkey"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// A lock writer given an assembly verifies each source's signature as §11
// does before it names it (§8): it reads the document (§11, Reading a
// signature document), and checks it against the key the expected signer set
// names for the source and the digest of the statement rebuilt from the
// index's item (§11, Checking a document). This is that part of §11 and no
// more: the lock signature, the serial record and verification are the lock
// signature verifier's (§15, verify.go).

// MaxSignatureBytes is a signature document's bound (§2).
const MaxSignatureBytes = 4096

// SourceSignatureVersion is a source signature document's selector (§10.2).
const SourceSignatureVersion = "1.1.0"

// sourceSignaturePrefix begins every source signature's message (§10.3).
const sourceSignaturePrefix = "judgment-pack-registry/source-signature/1:"

var (
	hex64  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	hex128 = regexp.MustCompile(`^[0-9a-f]{128}$`)
)

// sourceSignatureMembers are a source signature document's five members.
var sourceSignatureMembers = []string{"sourceSignatureVersion", "algorithm", "publicKey", "sourceDigest", "signature"}

// trustedKey reports whether text is a key of §11's form, 64 lowercase
// hexadecimal characters, that the trust rule admits (edkey.Check: canonical,
// not of small order, on the curve).
func trustedKey(text string) bool {
	if !hex64.MatchString(text) {
		return false
	}
	key, err := hex.DecodeString(text)
	return err == nil && edkey.Check(key) == nil
}

// signerProblems is the signer set's part of §11's Verifying step 1, for a
// writer given an assembly, which requires an index and so a repository key
// map: an expected key missing or refused by the trust rule is
// LOCK-SIG-EXPECTED-KEY; no map, a map that gives a repository the standard
// owner's key, or one key to two repository identities, is LOCK-EXPECTED.
func signerProblems(signers *Signers) []string {
	var out []string
	key := ""
	var repositories map[string]string
	if signers != nil {
		key, repositories = signers.Key, signers.Repositories
	}
	if !trustedKey(key) {
		out = append(out, "LOCK-SIG-EXPECTED-KEY")
	}
	if repositories == nil {
		out = append(out, "LOCK-EXPECTED")
	}
	seen := map[string]bool{}
	for _, identity := range sortedKeys(repositories) {
		repositoryKey := repositories[identity]
		if !trustedKey(repositoryKey) {
			out = append(out, "LOCK-SIG-EXPECTED-KEY")
			continue
		}
		if repositoryKey == key || seen[repositoryKey] {
			out = append(out, "LOCK-EXPECTED")
		}
		seen[repositoryKey] = true
	}
	return out
}

// sourceSignature is a source signature document read as §11 reads one.
type sourceSignature struct {
	root map[string]any
	// identity is the document's digest (registry §2.3), which the lock
	// names as the signature it accepted.
	identity string
}

// readSourceSignature reads a source signature document, with one code per
// failure (§11): over its bound, not one JSON object in UTF-8, a member given
// twice or a backslash (LOCK-SIG-DOCUMENT); a version other than "1.1.0",
// checked first (LOCK-SIG-VERSION); a member unknown or missing
// (LOCK-SIG-MEMBER); an algorithm other than ed25519 (LOCK-SIG-ALGORITHM); a
// public key not of its form or refused by the trust rule
// (LOCK-SIG-PUBLIC-KEY); a digest or signature not of its form
// (LOCK-SIG-FORM).
func readSourceSignature(document registry.Document) (*sourceSignature, []string) {
	if document.Oversize || len(document.Data) > MaxSignatureBytes || bytes.IndexByte(document.Data, '\\') >= 0 {
		return nil, []string{"LOCK-SIG-DOCUMENT"}
	}
	value, code, _ := registry.AdmitValue(document.Data, MaxSignatureBytes)
	root, isObject := value.(map[string]any)
	if code != "" || !isObject {
		return nil, []string{"LOCK-SIG-DOCUMENT"}
	}
	if version, present := root["sourceSignatureVersion"]; present && version != SourceSignatureVersion {
		return nil, []string{"LOCK-SIG-VERSION"}
	}
	var codes []string
	for _, name := range sortedKeys(root) {
		if !slices.Contains(sourceSignatureMembers, name) {
			codes = append(codes, "LOCK-SIG-MEMBER")
		}
	}
	for _, name := range sourceSignatureMembers {
		if _, present := root[name]; !present {
			codes = append(codes, "LOCK-SIG-MEMBER")
		}
	}
	if algorithm, present := root["algorithm"]; present && algorithm != "ed25519" {
		codes = append(codes, "LOCK-SIG-ALGORITHM")
	}
	if key, present := root["publicKey"]; present {
		if text, isString := key.(string); !isString || !trustedKey(text) {
			codes = append(codes, "LOCK-SIG-PUBLIC-KEY")
		}
	}
	if digest, present := root["sourceDigest"]; present && !isDigest(digest) {
		codes = append(codes, "LOCK-SIG-FORM")
	}
	if signature, present := root["signature"]; present {
		if text, isString := signature.(string); !isString || !hex128.MatchString(text) {
			codes = append(codes, "LOCK-SIG-FORM")
		}
	}
	if len(codes) > 0 {
		return nil, codes
	}
	identity, err := registry.Digest(root)
	if err != nil {
		return nil, []string{"LOCK-SIG-DOCUMENT"}
	}
	return &sourceSignature{root: root, identity: identity}, nil
}

// check holds a read document against a key and a statement digest, in
// §11's order, and returns the first failure's code, or "": the document's
// key is not that key, compared as 32 bytes (LOCK-SIG-UNEXPECTED-KEY); its
// digest is not that digest (LOCK-SIG-DIGEST-MISMATCH); the signature does
// not verify under that key over §10.3's message (LOCK-SIG-INVALID).
// ed25519.Verify is cofactorless, refuses an S of L or more and compares R
// byte for byte, the equation checkpoint-witness §5 states.
func (s *sourceSignature) check(key, digest string) string {
	expected, _ := hex.DecodeString(key)
	public, _ := hex.DecodeString(s.root["publicKey"].(string))
	if !bytes.Equal(public, expected) {
		return "LOCK-SIG-UNEXPECTED-KEY"
	}
	if s.root["sourceDigest"] != digest {
		return "LOCK-SIG-DIGEST-MISMATCH"
	}
	signed, err := jcs.EncodeDocument(map[string]any{"sourceDigest": digest, "sourceSignatureVersion": SourceSignatureVersion})
	signature, _ := hex.DecodeString(s.root["signature"].(string))
	if err != nil || !ed25519.Verify(expected, append([]byte(sourceSignaturePrefix), signed...), signature) {
		return "LOCK-SIG-INVALID"
	}
	return ""
}

// sources is an assembled lock's sources (§7): for each source of the
// index, in its order, its name, role and repository, the digest of its
// statement, and the digest of the source signature document the writer
// accepted for it, verified against the key the signer set names for it (the
// standard owner's for the standard, the map's for a repository). A source
// with no document is LOCK-SIG-MISSING, and a document for no source is
// LOCK-SIG-EXTRA.
func (w *lockWriter) sources(index map[string]any, documents map[string]registry.Document, signers *Signers) []any {
	problems := signerProblems(signers)
	for _, code := range problems {
		w.refuse(code)
	}
	items, _ := index["sources"].([]any)
	names := map[string]bool{}
	for _, item := range items {
		name, _ := item.(map[string]any)["name"].(string)
		names[name] = true
	}
	for name := range documents {
		if !names[name] {
			w.refuse("LOCK-SIG-EXTRA")
		}
	}
	if slices.Contains(problems, "LOCK-SIG-EXPECTED-KEY") {
		return nil
	}
	var repositories map[string]string
	if signers != nil {
		repositories = signers.Repositories
	}
	out := make([]any, 0, len(items))
	for _, raw := range items {
		item := raw.(map[string]any)
		name, _ := item["name"].(string)
		repository, _ := item["repository"].(string)
		document, given := documents[name]
		if !given {
			w.refuse("LOCK-SIG-MISSING")
			continue
		}
		key, named := repositories[repository]
		if item["role"] == RoleStandard {
			if named {
				// The standard source is signed by the standard owner's
				// key alone (§11, Verifying, step 6).
				w.refuse("LOCK-EXPECTED")
				continue
			}
			key = signers.Key
		} else if !named {
			w.refuse("LOCK-EXPECTED")
			continue
		}
		signature, codes := readSourceSignature(document)
		for _, code := range codes {
			w.refuse(code)
		}
		if signature == nil {
			continue
		}
		digest := statementDigest(item)
		if code := signature.check(key, digest); code != "" {
			w.refuse(code)
			continue
		}
		out = append(out, map[string]any{
			"name": name, "role": item["role"], "repository": repository, "statement": digest, "signature": signature.identity,
		})
	}
	return out
}
