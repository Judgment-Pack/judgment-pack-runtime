package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"slices"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
)

// Facts contracts and registry entries as facts (spec RFC 0037 §1–§5,
// ADR-0095). A facts document is built from one resolved entry of a valid
// registry: numbers become decimal strings in one normal form, a secret
// reference becomes its store and scope, and each entry reference carries its
// target's kind, id and mapped value, one level deep. Nothing is fetched.

// DocumentFacts names a facts document in a location. The pack and contract
// documents of a check are named DocumentPack and DocumentContract.
const (
	DocumentFacts    = "facts"
	DocumentContract = "contract"
	DocumentPack     = "pack"
)

// FactsCodes is the closed list of codes the facts commands report, with
// each one's severity and fixed message. RFC 0037 §5 names the three
// FACTS-*-advisories; the others are this runtime's (ADR-0095), for cases the
// RFC names no code for.
var FactsCodes = map[string]codeInfo{
	"FACTS-POINTER-UNKNOWN":   {SeverityWarning, "The pack reads a fact pointer the facts contract does not list."},
	"FACTS-TYPE-ORDERED":      {SeverityWarning, "The pack uses an ordered comparison on a fact pointer that the contract does not type decimal."},
	"FACTS-VALUE-UNREACHABLE": {SeverityWarning, "The pack compares a fact with a value outside the pointer's values in the contract, or of another type: an equals or not-equals operand, or at least one item of an in, that no fact at the pointer can equal."},
	"FACTS-DOCUMENT-LIMIT":    {SeverityError, "The entry's facts document exceeds the runtime's facts-document limit: 10 MiB, depth 128, 250000 nodes, 1 MiB per string."},
	"FACTS-CONTRACT-LIMIT":    {SeverityError, "The facts contract exceeds the contract bounds: 8 MiB, 65536 facts, or the generation work limit."},
	"FACTS-CONTRACT-FORM":     {SeverityError, "A member of the facts contract is missing, not defined by the format, or has the wrong form."},
	"FACTS-CONTRACT-ORDER":    {SeverityError, "The contract's facts are not in strictly increasing order of pointer, then type."},
	"FACTS-CHECK-LIMIT":       {SeverityError, "The check exceeds the runtime's check bounds: 4194304 contract positions and facts visited, or a fact pointer deeper than 128. This condition and the ones after it are not checked."},
}

func factsDiagnostic(code string, location Location) Diagnostic {
	info, ok := FactsCodes[code]
	if !ok {
		panic("registry: unlisted facts code " + code)
	}
	return Diagnostic{Code: code, Severity: info.severity, Location: location, Message: info.message}
}

// Errors of the facts commands that are not findings.
var (
	ErrNoKind        = errors.New("registry: the registry pins no kind of that name")
	ErrNoEnvironment = errors.New("registry: the registry declares no environment of that name")
	// ErrWrite wraps a failure of the writer a batch is written to.
	ErrWrite = errors.New("registry: the facts batch could not be written")
	// errUnmapped is a resolved value that a valid registry cannot hold: a
	// secret position without a secret reference, or a reference that does
	// not resolve. It is never written around.
	errUnmapped = errors.New("registry: a resolved value has no facts mapping")
)

// decimalGrammar is Core §2.2's decimal.
var decimalGrammar = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?$`)

// normalDecimal writes an admitted JSON number in RFC 0037 §2's normal form:
// an integer in base 10 with no leading zero, and a non-integer with no
// exponent, no trailing zero after the point and a digit on each side of it.
// It works on the literal's exact value, which admission (RFC 0034 §1) has
// already held to a finite decimal, so no double is involved.
func normalDecimal(number json.Number) (string, bool) {
	exact, ok := new(big.Rat).SetString(number.String())
	if !ok {
		return "", false
	}
	// The value is n / 2^a 5^b in lowest terms; max(a, b) digits after the
	// point write it exactly, and the last of them is not zero.
	denominator := new(big.Int).Set(exact.Denom())
	twos := denominator.TrailingZeroBits()
	denominator.Rsh(denominator, twos)
	fives := uint(0)
	five, remainder := big.NewInt(5), new(big.Int)
	for denominator.Cmp(big.NewInt(1)) != 0 {
		denominator.QuoRem(denominator, five, remainder)
		if remainder.Sign() != 0 {
			return "", false
		}
		fives++
	}
	return exact.FloatString(int(max(twos, fives))), true
}

// factValue maps one resolved value of a kind: a deep copy in which every
// number is its normal decimal string and every secret position holds
// {"store", "scope"} in place of its reference.
func factValue(kind *Kind, value any) (any, error) {
	mapped, err := mapNumbers(value)
	if err != nil {
		return nil, err
	}
	for _, secret := range kind.secrets {
		for _, place := range secret.positions(value) {
			reference, code := parseSecretReference(place.value)
			if code != "" {
				return nil, errUnmapped
			}
			mapped = setAt(mapped, place.path, map[string]any{"store": reference.store, "scope": reference.project})
		}
	}
	return mapped, nil
}

func mapNumbers(value any) (any, error) {
	switch typed := value.(type) {
	case json.Number:
		text, ok := normalDecimal(typed)
		if !ok {
			return nil, errUnmapped
		}
		return text, nil
	case map[string]any:
		out := make(map[string]any, len(typed))
		for name, item := range typed {
			mapped, err := mapNumbers(item)
			if err != nil {
				return nil, err
			}
			out[name] = mapped
		}
		return out, nil
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			mapped, err := mapNumbers(item)
			if err != nil {
				return nil, err
			}
			out[i] = mapped
		}
		return out, nil
	}
	return value, nil
}

// factsBuilder builds the facts documents of one environment.
type factsBuilder struct {
	result      *Result
	environment string
	entries     map[EntryName]resolvedEntry
	mapped      map[EntryName]any
	// sizes holds the encoded size of each reference target's value, by
	// target and pointer, so that a document's size is known before it is
	// encoded.
	sizes map[string]int64
}

func (r *Result) factsBuilder(environment string) *factsBuilder {
	entries := map[EntryName]resolvedEntry{}
	for _, resolved := range r.resolved[environment] {
		entries[resolved.entry.name] = resolved
	}
	return &factsBuilder{result: r, environment: environment, entries: entries, mapped: map[EntryName]any{}, sizes: map[string]int64{}}
}

// mappedValue is a target's mapped value, made once per environment and
// shared, read-only, by every facts document that refers to it.
func (b *factsBuilder) mappedValue(target resolvedEntry) (any, error) {
	if value, ok := b.mapped[target.entry.name]; ok {
		return value, nil
	}
	value, err := factValue(target.entry.kind, target.value)
	if err != nil {
		return nil, err
	}
	b.mapped[target.entry.name] = value
	return value, nil
}

// encodedSize is the size of a reference target's value in its RFC 8785
// form, encoded once per target and pointer.
func (b *factsBuilder) encodedSize(name EntryName, reference entryReference, value any) (int64, error) {
	key := name.Kind + "\x00" + name.ID + "\x00" + pointerOf(reference.pointer)
	if !reference.hasPointer {
		key = name.Kind + "\x00" + name.ID
	}
	if size, ok := b.sizes[key]; ok {
		return size, nil
	}
	encoded, err := jcs.EncodeDocument(value)
	if err != nil {
		return 0, err
	}
	size := int64(len(encoded))
	b.sizes[key] = size
	return size, nil
}

// errOverLimit is a facts document known to be over the hard byte limit
// before it is encoded.
var errOverLimit = errors.New("registry: the facts document is over the facts-document limit")

// document returns one entry's facts document in its RFC 8785 form, or
// errOverLimit once the parts it holds already pass the hard byte limit. An
// RFC 8785 document holds each member value's own RFC 8785 form, so the sum of
// those sizes is a lower bound on its size: a document reported here is over
// the limit, and none is encoded whole first.
func (b *factsBuilder) document(resolved resolvedEntry) ([]byte, error) {
	value, err := factValue(resolved.entry.kind, resolved.value)
	if err != nil {
		return nil, err
	}
	encodedValue, err := jcs.EncodeDocument(value)
	if err != nil {
		return nil, err
	}
	size := int64(len(encodedValue))
	if size > carrier.HardMaxBytes {
		return nil, errOverLimit
	}
	references := map[string]any{}
	for _, pattern := range resolved.entry.kind.references {
		for _, place := range pattern.pattern.positions(resolved.value) {
			text, _ := place.value.(string)
			reference, ok := parseEntryReference(text)
			if !ok {
				return nil, errUnmapped
			}
			name := EntryName{Kind: reference.kind, ID: reference.id}
			target, found := b.entries[name]
			if !found {
				return nil, errUnmapped
			}
			// One level: the target's value is mapped, and its own
			// references stay the strings they are.
			addressed, err := b.mappedValue(target)
			if err != nil {
				return nil, err
			}
			if reference.hasPointer {
				if addressed, found = lookup(addressed, reference.pointer); !found {
					return nil, errUnmapped
				}
			}
			targetSize, err := b.encodedSize(name, reference, addressed)
			if err != nil {
				return nil, err
			}
			if size += targetSize; size > carrier.HardMaxBytes {
				return nil, errOverLimit
			}
			references[pointerOf(place.path)] = map[string]any{"kind": name.Kind, "id": name.ID, "value": addressed}
		}
	}
	document := map[string]any{
		"registry":    b.result.registryMember(),
		"environment": b.environment,
		"entry":       map[string]any{"kind": resolved.entry.name.Kind, "id": resolved.entry.name.ID, "owner": resolved.entry.owner},
		"value":       value,
		"references":  references,
	}
	return jcs.EncodeDocument(document)
}

// registryMember is a facts document's "registry": the registry's id,
// version and digest (RFC 0037 §2 rule 5).
func (r *Result) registryMember() map[string]any {
	identity := r.registry.identity
	return map[string]any{"id": identity.ID, "version": identity.Version, "digest": identity.Digest}
}

// withinFactsLimit says whether a facts document is one evaluate admits: at
// most the hard byte limit, and within the carrier limits it decodes facts
// under. A document over either is reported, never truncated.
func withinFactsLimit(document []byte) bool {
	if int64(len(document)) > carrier.HardMaxBytes {
		return false
	}
	_, failure := carrier.Decode(document, carrier.DefaultLimits())
	return failure == nil
}

// factsEntries returns the resolved entries of kind in environment, in
// canonical order.
func (r *Result) factsEntries(kind, environment string) []resolvedEntry {
	var out []resolvedEntry
	for _, resolved := range r.resolved[environment] {
		if resolved.entry.name.Kind == kind {
			out = append(out, resolved)
		}
	}
	return out
}

func (r *Result) checkFactsRequest(kind string, environments []string) error {
	if !r.Report.Valid || r.registry == nil {
		return ErrInvalid
	}
	if r.pinnedKind(kind) == nil {
		return ErrNoKind
	}
	if len(environments) == 0 {
		return ErrNoEnvironment
	}
	for _, environment := range environments {
		if !slices.Contains(r.registry.environments, environment) {
			return ErrNoEnvironment
		}
	}
	return nil
}

func (r *Result) pinnedKind(name string) *Kind {
	if r.registry == nil {
		return nil
	}
	for _, p := range r.registry.pins {
		if p.kind == name && p.usable != nil {
			return p.usable
		}
	}
	return nil
}

// WriteFacts writes the batch of RFC 0037 §3: one line per entry of kind that
// applies in each environment, environments in the order given and entries in
// canonical order, each line {"facts": <facts document>, "id": <id>} in RFC
// 8785 form and a newline. The id is "<kind>/<entry-id>", prefixed with
// "<environment>:" when qualified.
//
// It writes nothing unless every line can be written: it first builds every
// facts document and checks it against the facts-document limit, and returns
// the diagnostics of those over it without writing a byte. Only then does it
// call ready (which may write a companion file and refuse) and write the
// lines, building each again so that no batch is held in memory.
func (r *Result) WriteFacts(kind string, environments []string, qualified bool, out io.Writer, ready func() error) ([]Diagnostic, error) {
	if err := r.checkFactsRequest(kind, environments); err != nil {
		return nil, err
	}
	var diagnostics []Diagnostic
	for _, environment := range environments {
		builder := r.factsBuilder(environment)
		for _, resolved := range r.factsEntries(kind, environment) {
			document, err := builder.document(resolved)
			if err != nil && !errors.Is(err, errOverLimit) {
				return nil, err
			}
			if err != nil || !withinFactsLimit(document) {
				diagnostics = append(diagnostics, factsDiagnostic("FACTS-DOCUMENT-LIMIT", Location{Document: DocumentFacts, Entry: locatable(resolved.entry.name), Environment: environment}))
			}
		}
	}
	if len(diagnostics) > 0 {
		return diagnostics, nil
	}
	if ready != nil {
		if err := ready(); err != nil {
			return nil, err
		}
	}
	for _, environment := range environments {
		builder := r.factsBuilder(environment)
		for _, resolved := range r.factsEntries(kind, environment) {
			document, err := builder.document(resolved)
			if err != nil {
				return nil, err
			}
			id := resolved.entry.name.Kind + "/" + resolved.entry.name.ID
			if qualified {
				id = environment + ":" + id
			}
			encodedID, err := jcs.EncodeDocument(id)
			if err != nil {
				return nil, err
			}
			line := make([]byte, 0, len(document)+len(encodedID)+18)
			line = append(line, `{"facts":`...)
			line = append(line, document...)
			line = append(line, `,"id":`...)
			line = append(line, encodedID...)
			line = append(line, "}\n"...)
			if _, err := out.Write(line); err != nil {
				return nil, fmt.Errorf("%w: %v", ErrWrite, err)
			}
		}
	}
	return nil, nil
}
