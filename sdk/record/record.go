// Package record is the decision record that sdk.Pack.EvaluateWithRecord
// and sdk.Pack.EvaluateWithKeyedRecord return beside one evaluation: what was
// evaluated, under which evaluator, and what it decided, as one canonical
// JSON text and that text's digest (ADR-0079, ADR-0087). This runtime writes
// the record nowhere. Only package sdk, from an evaluation, and Read, from
// bytes it holds to a record's form, compute a DecisionRecord's JSON and
// Digest. A DecisionRecord made any other way, the zero value or a struct
// literal, has a nil JSON and an empty Digest whatever its exported fields
// hold, and changing a field of a returned record changes neither.
// Everything in this package is experimental.
//
// # What a record says
//
// A record names the pack by its id, version, specVersion and the digest of
// the exact bytes loaded; the facts and evidence documents by a digest of
// each as evaluated, never by their content; the §8.3 disposition, as
// sdk.Evaluation.CanonicalDisposition returns it; the evaluator, by this
// runtime's version and the JPS Core evaluator contract applied; and, when
// the caller gave one, the caller's binding, as given. It holds no time, no
// random value and nothing read from outside those inputs, so the same
// inputs under the same runtime version give the same bytes and the same
// digest.
//
// A record is of one of two forms, and says which in its recordVersion. A
// record of the plain form, RecordVersion, names each document by the
// SHA-256 of its canonical text, which this runtime computes, in the members
// factsDigest and evidenceDigest. A record of the keyed form,
// KeyedRecordVersion, names them instead by keyed digests the caller
// computed over the same canonical texts with a key this runtime never
// receives, in the one member keyedDigests, with the algorithm's label and
// the caller's identifier for the key, and holds neither factsDigest nor
// evidenceDigest. No member is shared between the forms' digests, so a
// reader of one form finds no digest of the other where it looks.
// CanonicalFacts and CanonicalEvidence return the canonical texts, and
// docs/decision-records.md states their form with vectors.
//
// # What a record does not say
//
// A record is not an audit-trail entry: it is not appended to a trail,
// carries no run, time, sequence or link to a record before it, and nothing
// "jpack audit" reads covers it. Its form is not the trail record's, and its
// recordVersion is its own. It is unsigned: its digest names its bytes and
// says nothing about who wrote them, and anyone holding the same inputs and
// this runtime can write the same bytes. The binding is the caller's
// assertion: it is held to its shape and otherwise never read or
// recomputed, and neither the evaluation nor the disposition depends on it.
// The keyed digests are the caller's assertion too: this runtime cannot
// compute or check them, and a reader holding the key checks them with
// DecisionRecord.CheckDocuments. The facts and evidence appear by digest
// only, so a reader who needs them must keep them; the record cannot
// reproduce them. The runtime version is the program's build information's
// account of the module, not a check of the code that ran.
package record

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/Judgment-Pack/judgment-pack-runtime/sdk/internal/canonjson"
	"github.com/Judgment-Pack/judgment-pack-runtime/sdk/internal/recordhook"
)

// RecordVersion is the version of the plain record form: the members of
// DecisionRecord.JSON for a record that names its documents by plain
// digests, and the bytes each is written as, which the record carries in
// band as recordVersion. It is this form's own version and not the audit
// trail record's. It moves whenever the bytes for inputs an earlier record
// version could record would change: the major part when a member is removed
// or renamed or a member's value is taken over other bytes or written in
// another form, so that a reader of the earlier form would misread it; the
// minor part when a member is added, which such a reader can pass over; the
// patch part when the bytes change in any other way, every member and its
// meaning kept. It does not move when the runtime learns to record
// evaluations it refused before, such as those of a newly admitted
// specification version, or when the runtime version the record carries
// changes. A record of the keyed form carries KeyedRecordVersion instead.
// Experimental.
const RecordVersion = "1.0.0"

// KeyedRecordVersion is the version of the keyed record form (ADR-0087): the
// plain form's members with factsDigest and evidenceDigest removed and
// keyedDigests added. Removing a member is a major change by the rule
// RecordVersion states, so a reader of the plain form refuses a keyed record
// by its major part rather than reading it as plain. It moves by that same
// rule, and the major part of a record's version names its form: 1 the
// plain form, 2 the keyed form. Experimental.
const KeyedRecordVersion = "2.0.0"

// Form is the form a record names its documents in. Experimental.
type Form string

// The two forms. Experimental.
const (
	// FormPlain names the facts and evidence documents by the SHA-256 of
	// their canonical texts, in factsDigest and evidenceDigest.
	FormPlain Form = "plain"
	// FormKeyed names them by the caller's keyed digests of the same texts,
	// in keyedDigests.
	FormKeyed Form = "keyed"
)

// Binding is the caller's own commitments, carried on the record as given
// (ADR-0079): each name is the caller's label for some material of its own,
// and each value is a SHA-256 the caller computed over that material, in the
// form "sha256:" followed by 64 lower-case hexadecimal digits. A name is 1 to
// MaxBindingNameLength characters from a-z, 0-9, dot, underscore, slash and
// hyphen, the grammar of the kind of the audit record's binding (ADR-0050),
// and a binding holds at most MaxBindingNames names. A nil or empty Binding
// is no binding. This runtime never reads meaning into a name or a digest
// and never recomputes one. On a record of the keyed form it refuses a
// digest that is the plain SHA-256 of the facts or evidence document, as
// passed or as canonical text, because the binding would then carry beside
// the keyed digests the plain one they replace; whether a digest is the
// digest of anything else is for the caller's own reader to find.
// Experimental.
type Binding map[string]string

// The bounds of a Binding. Experimental.
const (
	MaxBindingNames      = 16
	MaxBindingNameLength = 128
)

// ErrBinding is wrapped by the error for a binding not of the shape Binding
// states, or one a keyed record must not carry. The error says which rule
// the binding breaks in a fixed sentence and repeats no name or digest of
// it. Experimental.
var ErrBinding = errors.New("record: malformed binding")

var (
	errBindingNames  = fmt.Errorf("%w: it holds more than %d names", ErrBinding, MaxBindingNames)
	errBindingName   = fmt.Errorf("%w: a name is not 1 to %d characters from a-z, 0-9, dot, underscore, slash and hyphen", ErrBinding, MaxBindingNameLength)
	errBindingDigest = fmt.Errorf("%w: a digest is not sha256: followed by 64 lower-case hexadecimal digits", ErrBinding)
	errBindingPlain  = fmt.Errorf("%w: a digest is the plain SHA-256 of the facts or evidence document, which a record with keyed digests must not carry", ErrBinding)
)

// DecisionRecord is the record of one evaluation, as typed data and as the
// canonical JSON text JSON returns, with that text's digest. Experimental.
//
// A DecisionRecord is the caller's own copy. JSON and Digest return the bytes
// and digest of the record as package sdk built it or Read read it; changing
// the fields afterwards changes neither. The zero DecisionRecord, and any
// DecisionRecord neither returned, has no bytes: JSON returns nil and Digest
// the empty string.
type DecisionRecord struct {
	// Pack names the evaluated pack.
	Pack Pack
	// FactsDigest is, on a record of the plain form, "sha256:" followed by
	// the 64 lower-case hexadecimal digits of the SHA-256 of the facts
	// document's canonical text, the bytes CanonicalFacts returns: the
	// document as the carrier decoder read it for the evaluation, written in
	// the form of view.PackView.JSON (ADR-0074) — object members ordered by
	// the UTF-16 code units of their names, RFC 8785's string escapes, no
	// white space, and every number as the literal token the document holds.
	// It is not the SHA-256 of the bytes the caller passed: two facts
	// documents that differ only in white space, member order or how a
	// string is escaped have one digest, and two that write one number
	// differently, 1.5 and 1.50, have two. It is empty on a record of the
	// keyed form, which holds no plain digest.
	FactsDigest string
	// EvidenceDigest is the same digest of the evidence-availability
	// document, the bytes CanonicalEvidence returns, and of the empty object
	// {} when the document was omitted, which JPS Core §8.2 evaluates as the
	// empty object: an omitted document and a supplied {} give one record.
	// It is empty on a record of the keyed form.
	EvidenceDigest string
	// Keyed is a copy of the caller's keyed digests on a record of the keyed
	// form, and nil on a record of the plain form.
	Keyed *KeyedDigests
	// Disposition is the §8.3 portable disposition in its canonical form, the
	// text of the bytes sdk.Evaluation.CanonicalDisposition returns for the
	// same evaluation.
	Disposition string
	// Evaluator names the evaluator.
	Evaluator Evaluator
	// Binding is a copy of the caller's binding, and nil when none was given.
	Binding Binding

	json   []byte
	digest string
	form   Form
}

// Pack names the evaluated pack: the id, version and specVersion members of
// the document, and the digest of the exact bytes loaded, as
// sdk.Pack.Identity returns them. Experimental.
type Pack struct {
	ID          string
	Version     string
	SpecVersion string
	// Digest is "sha256:" followed by the 64 lower-case hexadecimal digits of
	// the SHA-256 of the bytes the pack was loaded from.
	Digest string
}

// Evaluator names the evaluator that decided. Experimental.
type Evaluator struct {
	// RuntimeVersion is this module's version, sdk.Description.RuntimeVersion:
	// whatever version the program's build information records for the
	// module, which may be a release tag or a pseudo-version, and may carry a
	// +dirty suffix for a build from a modified source tree. It is empty, and
	// the record has no runtimeVersion member, when the build records none,
	// for example with the module replaced by a local directory.
	RuntimeVersion string
	// SpecVersion is the JPS Core evaluator contract applied, as
	// sdk.Evaluation.EvaluatorSpecVersion names it.
	SpecVersion string
}

// JSON returns the record in its canonical JSON form, with no trailing
// newline: one object of the members binding (only when a binding was given:
// each name with its digest), disposition (the §8.3 disposition as a JSON
// value), evaluator (runtimeVersion, only when known, and specVersion), the
// document digests, pack (digest, id, specVersion and version) and
// recordVersion. The document digests are, on a record of the plain form,
// evidenceDigest and factsDigest, with recordVersion RecordVersion; on a
// record of the keyed form, keyedDigests (algorithm, evidence, facts and
// keyId, each digest as lower-case hexadecimal digits), with recordVersion
// KeyedRecordVersion. It is written in the form of view.PackView.JSON:
// object members ordered by the UTF-16 code units of their names, RFC 8785's
// string escapes and no white space; it holds no number. The caller owns the
// returned slice. Experimental.
func (r DecisionRecord) JSON() []byte {
	return bytes.Clone(r.json)
}

// Digest returns "sha256:" followed by the 64 lower-case hexadecimal digits
// of the SHA-256 of the bytes JSON returns. Experimental.
func (r DecisionRecord) Digest() string {
	return r.digest
}

// Form returns the form the bytes JSON returns are of, FormPlain or
// FormKeyed, and the empty Form for a record with no bytes. It follows the
// bytes, not the exported fields. Experimental.
func (r DecisionRecord) Form() Form {
	return r.form
}

// check holds a binding to the shape Binding states. The names are checked
// in sorted order, so the same binding is refused for the same reason every
// time.
func check(binding map[string]string) error {
	if len(binding) > MaxBindingNames {
		return errBindingNames
	}
	for _, name := range slices.Sorted(maps.Keys(binding)) {
		if !bindingName(name) {
			return errBindingName
		}
		if !bindingDigest(binding[name]) {
			return errBindingDigest
		}
	}
	return nil
}

func bindingName(name string) bool {
	if len(name) == 0 || len(name) > MaxBindingNameLength {
		return false
	}
	for index := 0; index < len(name); index++ {
		switch character := name[index]; {
		case 'a' <= character && character <= 'z', '0' <= character && character <= '9':
		case character == '.', character == '_', character == '/', character == '-':
		default:
			return false
		}
	}
	return true
}

func bindingDigest(digest string) bool {
	const prefix = "sha256:"
	return len(digest) == len(prefix)+64 && digest[:len(prefix)] == prefix && lowerHex(digest[len(prefix):])
}

// lowerHex reports whether text is a non-empty run of lower-case hexadecimal
// digits.
func lowerHex(text string) bool {
	if text == "" {
		return false
	}
	for index := 0; index < len(text); index++ {
		if character := text[index]; !('0' <= character && character <= '9' || 'a' <= character && character <= 'f') {
			return false
		}
	}
	return true
}

func digestOf(text []byte) string {
	sum := sha256.Sum256(text)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// build is the record of one evaluation. Every error but one wrapping
// ErrBinding or ErrKeyedDigests is a fault of this runtime: package sdk held
// the binding and the keyed digests to their shape before evaluating, and
// the documents decoded for the evaluation that has completed.
func build(input recordhook.Input) (DecisionRecord, error) {
	if err := check(input.Binding); err != nil {
		return DecisionRecord{}, err
	}
	if input.Keyed != nil {
		if err := checkKeyed(*input.Keyed); err != nil {
			return DecisionRecord{}, err
		}
	}
	for _, member := range []string{input.PackID, input.PackVersion, input.PackSpecVersion, input.PackDigest, input.EvaluatorSpecVersion} {
		if member == "" {
			return DecisionRecord{}, errors.New("the record was given an empty identity member")
		}
	}
	facts, err := canonicalText("facts", input.Facts)
	if err != nil {
		return DecisionRecord{}, fmt.Errorf("the facts document does not encode: %w", err)
	}
	evidence, err := canonicalEvidenceText(input.Evidence)
	if err != nil {
		return DecisionRecord{}, fmt.Errorf("the evidence document does not encode: %w", err)
	}
	disposition, err := canonicalDisposition(input.Disposition)
	if err != nil {
		return DecisionRecord{}, err
	}

	built := DecisionRecord{
		Pack:        Pack{ID: input.PackID, Version: input.PackVersion, SpecVersion: input.PackSpecVersion, Digest: input.PackDigest},
		Disposition: disposition,
		Evaluator:   Evaluator{RuntimeVersion: input.RuntimeVersion, SpecVersion: input.EvaluatorSpecVersion},
		form:        FormPlain,
	}
	evaluator := map[string]any{"specVersion": built.Evaluator.SpecVersion}
	if built.Evaluator.RuntimeVersion != "" {
		evaluator["runtimeVersion"] = built.Evaluator.RuntimeVersion
	}
	document := map[string]any{
		"disposition": canonjson.Raw(disposition),
		"evaluator":   evaluator,
		"pack": map[string]any{
			"digest":      built.Pack.Digest,
			"id":          built.Pack.ID,
			"specVersion": built.Pack.SpecVersion,
			"version":     built.Pack.Version,
		},
	}
	if input.Keyed == nil {
		built.FactsDigest = digestOf([]byte(facts))
		built.EvidenceDigest = digestOf([]byte(evidence))
		document["factsDigest"] = built.FactsDigest
		document["evidenceDigest"] = built.EvidenceDigest
		document["recordVersion"] = RecordVersion
	} else {
		// The plain digests are computed here only to be refused where the
		// caller would carry one beside the keyed digests; none is kept.
		plain := plainDigests(input.Facts, input.Evidence, facts, evidence)
		for _, keyedDigest := range [][]byte{input.Keyed.Facts, input.Keyed.Evidence} {
			if slices.Contains(plain, "sha256:"+hex.EncodeToString(keyedDigest)) {
				return DecisionRecord{}, errKeyedPlain
			}
		}
		for _, digest := range input.Binding {
			if slices.Contains(plain, digest) {
				return DecisionRecord{}, errBindingPlain
			}
		}
		keyed := copyKeyed(*input.Keyed)
		built.Keyed = &keyed
		built.form = FormKeyed
		document["keyedDigests"] = map[string]any{
			"algorithm": keyed.Algorithm,
			"evidence":  hex.EncodeToString(keyed.Evidence),
			"facts":     hex.EncodeToString(keyed.Facts),
			"keyId":     keyed.KeyID,
		}
		document["recordVersion"] = KeyedRecordVersion
	}
	if len(input.Binding) > 0 {
		built.Binding = maps.Clone(Binding(input.Binding))
		members := make(map[string]any, len(built.Binding))
		for name, digest := range built.Binding {
			members[name] = digest
		}
		document["binding"] = members
	}
	encoded, err := canonjson.Text(document)
	if err != nil {
		return DecisionRecord{}, fmt.Errorf("the record does not encode: %w", err)
	}
	built.json = []byte(encoded)
	built.digest = digestOf(built.json)
	return built, nil
}

// plainDigests are the plain SHA-256 digests, in the "sha256:" form, that a
// record of the keyed form must not carry: of the facts and evidence
// documents as passed and of their canonical texts.
func plainDigests(facts, evidence []byte, canonicalFacts, canonicalEvidence string) []string {
	plain := []string{digestOf(facts), digestOf([]byte(canonicalFacts)), digestOf([]byte(canonicalEvidence))}
	if evidence != nil {
		plain = append(plain, digestOf(evidence))
	}
	return plain
}

// canonicalDisposition returns the disposition's text when the bytes are one
// JSON value already in canonical form, as the evaluator writes it, so the
// record carries exactly the bytes CanonicalDisposition returns.
func canonicalDisposition(disposition []byte) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(disposition))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", errors.New("the disposition is not a JSON value")
	}
	if _, isObject := value.(map[string]any); !isObject {
		return "", errors.New("the disposition is not a JSON object")
	}
	text, err := canonjson.Text(value)
	if err != nil || text != string(disposition) {
		return "", errors.New("the disposition is not in canonical form")
	}
	return text, nil
}

func init() {
	recordhook.Check = check
	recordhook.CheckKeyed = checkKeyed
	recordhook.Build = func(input recordhook.Input) (any, error) {
		built, err := build(input)
		if err != nil {
			return nil, err
		}
		return built, nil
	}
}
