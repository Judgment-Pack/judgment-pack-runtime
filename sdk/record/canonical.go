package record

import (
	"errors"
	"fmt"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/sdk/internal/canonjson"
)

// ErrDocument is wrapped by the error for a facts or evidence document that
// CanonicalFacts, CanonicalEvidence or DecisionRecord.CheckDocuments refuses.
// The error names the document and the rule it breaks, with the carrier's
// code where the carrier refused it, in a fixed sentence that repeats nothing
// of the document: no member name, value or position. Experimental.
var ErrDocument = errors.New("record: document refused")

// CanonicalFacts returns the canonical text of a facts document: the exact
// bytes whose SHA-256 a record of the plain form carries as factsDigest, and
// over which a caller computes the facts digest of KeyedDigests. For every
// facts document sdk.Pack.Evaluate admits, the SHA-256 of these bytes is the
// factsDigest sdk.Pack.EvaluateWithRecord records for it.
//
// The document is read as the evaluation reads it, by the carrier decoder
// under the same limits, and written in the form docs/decision-records.md
// states: object members ordered by the UTF-16 code units of their names,
// RFC 8785's string escapes, no white space, and every number as the literal
// token the document holds. It refuses, with an error wrapping ErrDocument,
// an empty or nil document, one over the hard 10 MiB byte limit, one that is
// not one JSON text in valid UTF-8, one with a duplicated member name or an
// unpaired surrogate escape, and one over the carrier's nesting, node,
// string or number-token limits. It does not hold the document to a pack:
// that a facts document is an object, and what its members hold, are the
// evaluation's to judge. The caller owns the returned slice. Experimental.
func CanonicalFacts(facts []byte) ([]byte, error) {
	text, err := canonicalText("facts", facts)
	if err != nil {
		return nil, err
	}
	return []byte(text), nil
}

// CanonicalEvidence returns the canonical text of an evidence-availability
// document, as CanonicalFacts does for a facts document: the bytes whose
// SHA-256 a record of the plain form carries as evidenceDigest. A nil
// document is an omitted one, which JPS Core §8.2 evaluates as the empty
// object, and its text is {}; an empty non-nil document is a supplied one and
// is refused, as sdk.Pack.Evaluate refuses it. It does not hold the document
// to a pack's declared evidence requirements. The caller owns the returned
// slice. Experimental.
func CanonicalEvidence(evidence []byte) ([]byte, error) {
	text, err := canonicalEvidenceText(evidence)
	if err != nil {
		return nil, err
	}
	return []byte(text), nil
}

func canonicalEvidenceText(evidence []byte) (string, error) {
	if evidence == nil {
		return "{}", nil
	}
	return canonicalText("evidence", evidence)
}

// canonicalText is the one reading of a document the record's digests and
// the public calls share: the hard byte limit and the carrier decoder under
// its default limits, which is what sdk.Pack.Evaluate applies, and the
// canonical writer the view and the record share.
func canonicalText(name string, document []byte) (string, error) {
	if len(document) == 0 {
		return "", fmt.Errorf("%w: the %s document is empty", ErrDocument, name)
	}
	if int64(len(document)) > carrier.HardMaxBytes {
		return "", fmt.Errorf("%w: the %s document exceeds the hard %d-byte limit (JPS-RESOURCE-INPUT-BYTE-LIMIT)", ErrDocument, name, carrier.HardMaxBytes)
	}
	value, failure := carrier.Decode(document, carrier.DefaultLimits())
	if failure != nil {
		return "", fmt.Errorf("%w: the %s document %s (%s)", ErrDocument, name, carrierReason(failure.Diagnostic.Code), failure.Diagnostic.Code)
	}
	text, err := canonjson.Text(value)
	if err != nil {
		return "", fmt.Errorf("%w: the %s document holds a value the canonical form cannot write", ErrDocument, name)
	}
	return text, nil
}

// carrierReason is a fixed sentence for each code the carrier decoder
// refuses a document with. The decoder's own message is not repeated: it can
// quote a member name or a character of the document.
func carrierReason(code string) string {
	switch code {
	case "JPS-CARRIER-DUPLICATE-MEMBER":
		return "has an object with a member name twice"
	case "JPS-RESOURCE-DEPTH-LIMIT":
		return "exceeds the carrier's nesting limit"
	case "JPS-RESOURCE-NODE-LIMIT":
		return "exceeds the carrier's node limit"
	case "JPS-RESOURCE-STRING-LIMIT":
		return "has a string or member name over the carrier's string limit"
	case "JPS-RESOURCE-NUMBER-LIMIT":
		return "has a number token over the carrier's limit"
	default:
		return "is not one JSON text in valid UTF-8 without an unpaired surrogate escape"
	}
}
