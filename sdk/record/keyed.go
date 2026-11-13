package record

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/Judgment-Pack/judgment-pack-runtime/sdk/internal/recordhook"
)

// HMACSHA256 is the label of HMAC (RFC 2104) over SHA-256, whose output is
// 32 bytes: the one keyed-digest algorithm a record names (ADR-0087). The
// list of labels is closed; a label outside it is refused when a record is
// built and when one is read. Experimental.
const HMACSHA256 = "hmac-sha256"

// MaxKeyIDLength is the bound on the length of KeyedDigests.KeyID.
// Experimental.
const MaxKeyIDLength = 128

// KeyedDigests are the keyed digests a caller computed over the canonical
// texts of the facts and evidence documents, with a key this runtime never
// receives, for sdk.Pack.EvaluateWithKeyedRecord to carry in place of the
// plain digests (ADR-0087). Experimental.
//
// The record carries the algorithm's label, the key identifier and the two
// digests, never the key. This runtime cannot compute or check a keyed
// digest: it holds them to their shape, refuses a digest or key identifier
// that is or holds the plain SHA-256 of a document the record names or the
// digest of the plain record of the same evaluation, and otherwise carries
// them as given. A reader holding the key checks them with
// DecisionRecord.CheckDocuments.
type KeyedDigests struct {
	// Algorithm is the label of the keyed-digest function, HMACSHA256.
	Algorithm string
	// KeyID is the caller's identifier for the key: 1 to MaxKeyIDLength
	// characters from A-Z, a-z, 0-9, dot, underscore, hyphen, colon and
	// slash. It is data: this runtime never reads meaning into it and never
	// repeats it in an error, and compares it only with the plain digests a
	// keyed record must not carry, refusing one that holds such a digest in
	// hexadecimal or base64. The record carries it in clear, so it should be
	// an opaque identifier, never derived from the facts, their subject or
	// the key; no other derivation is detected.
	KeyID string
	// Facts is the keyed digest of the bytes CanonicalFacts returns for
	// the facts document evaluated: 32 bytes for HMACSHA256.
	Facts []byte
	// Evidence is the keyed digest of the bytes CanonicalEvidence returns
	// for the evidence document evaluated, the text {} when it was
	// omitted, under the same key: 32 bytes for HMACSHA256.
	Evidence []byte
}

// ErrKeyedDigests is wrapped by the error for keyed digests not of the shape
// KeyedDigests states, or ones that are a plain digest of a document the
// record names. The error says which rule they break in a fixed sentence
// and repeats no label, key identifier or digest. Experimental.
var ErrKeyedDigests = errors.New("record: malformed keyed digests")

var (
	errKeyedAlgorithm  = fmt.Errorf("%w: the algorithm is not %s, the one keyed-digest algorithm a record names", ErrKeyedDigests, HMACSHA256)
	errKeyedKeyID      = fmt.Errorf("%w: the key identifier is not 1 to %d characters from A-Z, a-z, 0-9, dot, underscore, hyphen, colon and slash", ErrKeyedDigests, MaxKeyIDLength)
	errKeyedFacts      = fmt.Errorf("%w: the facts digest is not the size of the algorithm's output", ErrKeyedDigests)
	errKeyedEvidence   = fmt.Errorf("%w: the evidence digest is not the size of the algorithm's output", ErrKeyedDigests)
	errKeyedPlain      = fmt.Errorf("%w: a keyed digest is the plain SHA-256 of the facts or evidence document, or the digest of the plain record of the same evaluation", ErrKeyedDigests)
	errKeyedKeyIDPlain = fmt.Errorf("%w: the key identifier holds the plain SHA-256 of the facts or evidence document, or the digest of the plain record of the same evaluation", ErrKeyedDigests)
)

// digestSize is the output size in bytes of a keyed-digest algorithm, and 0
// for a label outside the closed list.
func digestSize(algorithm string) int {
	switch algorithm {
	case HMACSHA256:
		return sha256.Size
	}
	return 0
}

func checkKeyed(keyed recordhook.Keyed) error {
	size := digestSize(keyed.Algorithm)
	if size == 0 {
		return errKeyedAlgorithm
	}
	if !keyID(keyed.KeyID) {
		return errKeyedKeyID
	}
	if len(keyed.Facts) != size {
		return errKeyedFacts
	}
	if len(keyed.Evidence) != size {
		return errKeyedEvidence
	}
	return nil
}

func keyID(id string) bool {
	if len(id) == 0 || len(id) > MaxKeyIDLength {
		return false
	}
	for index := 0; index < len(id); index++ {
		switch character := id[index]; {
		case 'a' <= character && character <= 'z', 'A' <= character && character <= 'Z', '0' <= character && character <= '9':
		case character == '.', character == '_', character == '-', character == ':', character == '/':
		default:
			return false
		}
	}
	return true
}

func copyKeyed(keyed recordhook.Keyed) KeyedDigests {
	return KeyedDigests{Algorithm: keyed.Algorithm, KeyID: keyed.KeyID, Facts: bytes.Clone(keyed.Facts), Evidence: bytes.Clone(keyed.Evidence)}
}

// KeyedDigestFunc returns the keyed digest of canonical, a document's
// canonical text, under algorithm and the key the caller holds for keyID,
// both as the record names them. It is the caller's: the key stays with it,
// and this package passes it nothing but those three values. It returns an
// error for an algorithm or key identifier it does not hold a key for.
// Experimental.
type KeyedDigestFunc func(algorithm, keyID string, canonical []byte) ([]byte, error)

// Outcome is what DecisionRecord.CheckDocuments found for one document.
// Only Match says the document is the one the record names. Experimental.
type Outcome string

// The outcomes of a check. Experimental.
const (
	// Match: the digest of the document's canonical text is the one the
	// record carries.
	Match Outcome = "match"
	// Mismatch: it is not.
	Mismatch Outcome = "mismatch"
	// NotCheckableWithoutKey: the record carries a keyed digest and the
	// caller gave no keyed-digest function, so whether the document is the
	// one the record names is not known. It is neither a match nor a
	// mismatch.
	NotCheckableWithoutKey Outcome = "not-checkable-without-key"
)

// DocumentCheck is what DecisionRecord.CheckDocuments found. Experimental.
type DocumentCheck struct {
	// Form is the form of the record checked.
	Form Form
	// Facts and Evidence are the outcomes for the two documents.
	Facts, Evidence Outcome
}

// ErrKeyedDigestFunc is wrapped by the error for a keyed-digest function
// that returned an error, or a digest not of the algorithm's output size.
// The error's text repeats nothing the function returned; the function's own
// error is reachable through errors.Is and errors.As. Experimental.
var ErrKeyedDigestFunc = errors.New("record: the keyed-digest function failed")

// keyedDigestFuncError carries the caller's error without writing its text,
// which can hold anything, the key included.
type keyedDigestFuncError struct{ cause error }

func (e keyedDigestFuncError) Error() string {
	return ErrKeyedDigestFunc.Error() + ": it returned an error, whose text is not repeated here"
}

func (e keyedDigestFuncError) Unwrap() []error { return []error{ErrKeyedDigestFunc, e.cause} }

var (
	errKeyedDigestFuncSize = fmt.Errorf("%w: it returned a digest not the size of the algorithm's output", ErrKeyedDigestFunc)
	errNoRecord            = errors.New("record: CheckDocuments on a record that package sdk did not build and Read did not read")
)

// CheckDocuments says whether facts and evidence are the documents the
// record names: it reads the record's bytes, not its exported fields, and
// compares the digests of the documents' canonical texts with the ones the
// record carries. evidence nil is an omitted document, as in
// sdk.Pack.Evaluate. Experimental.
//
// On a record of the plain form it computes the SHA-256 of each text, and
// keyed is not called. On a record of the keyed form it calls keyed with the
// record's algorithm and key identifier and each canonical text, and
// compares in constant time; with keyed nil it reports
// NotCheckableWithoutKey for both documents, never Match and never
// Mismatch. It does not evaluate, and says nothing of the pack, the
// disposition, the binding or who wrote the record.
//
// It returns an error wrapping ErrDocument for a document CanonicalFacts or
// CanonicalEvidence refuses, one wrapping ErrKeyedDigestFunc when keyed
// fails or returns a digest of another size, and a plain error for a record
// neither package sdk nor Read returned.
func (r DecisionRecord) CheckDocuments(facts, evidence []byte, keyed KeyedDigestFunc) (DocumentCheck, error) {
	if r.json == nil {
		return DocumentCheck{}, errNoRecord
	}
	held, err := Read(r.json)
	if err != nil {
		return DocumentCheck{}, err
	}
	canonicalFacts, err := canonicalText("facts", facts)
	if err != nil {
		return DocumentCheck{}, err
	}
	canonicalEvidence, err := canonicalEvidenceText(evidence)
	if err != nil {
		return DocumentCheck{}, err
	}
	if held.form == FormPlain {
		return DocumentCheck{
			Form:     FormPlain,
			Facts:    outcome(digestOf([]byte(canonicalFacts)) == held.FactsDigest),
			Evidence: outcome(digestOf([]byte(canonicalEvidence)) == held.EvidenceDigest),
		}, nil
	}
	if keyed == nil {
		return DocumentCheck{Form: FormKeyed, Facts: NotCheckableWithoutKey, Evidence: NotCheckableWithoutKey}, nil
	}
	found := DocumentCheck{Form: FormKeyed}
	for _, document := range []struct {
		text   string
		want   []byte
		result *Outcome
	}{
		{canonicalFacts, held.Keyed.Facts, &found.Facts},
		{canonicalEvidence, held.Keyed.Evidence, &found.Evidence},
	} {
		got, err := keyed(held.Keyed.Algorithm, held.Keyed.KeyID, []byte(document.text))
		if err != nil {
			return DocumentCheck{}, keyedDigestFuncError{cause: err}
		}
		if len(got) != digestSize(held.Keyed.Algorithm) {
			return DocumentCheck{}, errKeyedDigestFuncSize
		}
		*document.result = outcome(hmac.Equal(got, document.want))
	}
	return found, nil
}

func outcome(matched bool) Outcome {
	if matched {
		return Match
	}
	return Mismatch
}
