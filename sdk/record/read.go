package record

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/uriformat"
	"github.com/Judgment-Pack/judgment-pack-runtime/sdk/internal/canonjson"
)

// ErrRecord is wrapped by the error for bytes Read does not admit as a
// decision record. The error says which rule the bytes break in a fixed
// sentence and repeats nothing of them. Experimental.
var ErrRecord = errors.New("record: not a decision record this reader admits")

var (
	errRecordJSON       = fmt.Errorf("%w: it is not one JSON object the carrier admits", ErrRecord)
	errRecordCanonical  = fmt.Errorf("%w: it is not in the record's canonical form", ErrRecord)
	errRecordVersion    = fmt.Errorf("%w: its recordVersion is not %s or %s, the versions this reader admits", ErrRecord, RecordVersion, KeyedRecordVersion)
	errRecordMembers    = fmt.Errorf("%w: its members are not those of its recordVersion", ErrRecord)
	errRecordPack       = fmt.Errorf("%w: its pack is not an object of a sha256: digest, an id that is an absolute URI, a version of three numbers and a specVersion of the version grammar", ErrRecord)
	errRecordEvaluator  = fmt.Errorf("%w: its evaluator is not an object of a specVersion naming an evaluator contract this reader knows and an optional runtimeVersion", ErrRecord)
	errRecordDigest     = fmt.Errorf("%w: a document digest is not sha256: followed by 64 lower-case hexadecimal digits", ErrRecord)
	errRecordKeyed      = fmt.Errorf("%w: its keyedDigests is not an object of algorithm, evidence, facts and keyId of the shape KeyedDigests states", ErrRecord)
	errRecordBinding    = fmt.Errorf("%w: its binding is not a non-empty object of the shape Binding states", ErrRecord)
	errRecordDispMember = fmt.Errorf("%w: its disposition holds a member this runtime never writes, such as the draft value member", ErrRecord)
	errRecordDisp       = fmt.Errorf("%w: its disposition is not a JPS Core §8.3 disposition of the members and vocabularies its evaluator contract writes, in canonical form", ErrRecord)
)

// The members of each form, beside the optional binding.
var (
	plainMembers = []string{"disposition", "evaluator", "evidenceDigest", "factsDigest", "pack", "recordVersion"}
	keyedMembers = []string{"disposition", "evaluator", "keyedDigests", "pack", "recordVersion"}
)

// Read reads the bytes of a decision record, of either form, as JSON wrote
// them, and returns the record, whose JSON is a copy of data and whose
// Digest is its digest. Experimental.
//
// It admits exactly the record versions RecordVersion and
// KeyedRecordVersion, each with exactly its own members: a record of the
// plain form holds factsDigest and evidenceDigest and no keyedDigests, one
// of the keyed form holds keyedDigests and neither of the others, and a
// member of another form or of no form is refused rather than passed over,
// so a record is never read as of a form it is not. The bytes must be the
// record's canonical text, and each member of the shape JSON states: the
// pack's id an absolute URI (RFC 3986), its version three numbers, its
// specVersion of that grammar with an optional label such as -draft; the
// evaluator's specVersion naming an evaluator contract this reader knows;
// and the disposition a JPS Core §8.3 disposition of the members, closed
// vocabularies and presence rules that contract writes, in canonical form,
// with no draft value member. Any other bytes are refused with an error
// wrapping ErrRecord. A record of a later version is refused, not read as
// one this reader knows, and so is a record naming an evaluator contract
// this reader does not know, whose disposition it cannot hold to that
// contract.
//
// Read holds bytes to a record's form and nothing more: it does not
// evaluate, cannot tell whether the runtime it names wrote them, and does
// not check a digest against a document; DecisionRecord.CheckDocuments does
// that.
func Read(data []byte) (DecisionRecord, error) {
	if len(data) == 0 || int64(len(data)) > carrier.HardMaxBytes {
		return DecisionRecord{}, errRecordJSON
	}
	value, failure := carrier.Decode(data, carrier.DefaultLimits())
	if failure != nil {
		return DecisionRecord{}, errRecordJSON
	}
	root, ok := value.(map[string]any)
	if !ok {
		return DecisionRecord{}, errRecordJSON
	}
	if text, err := canonjson.Text(value); err != nil || text != string(data) {
		return DecisionRecord{}, errRecordCanonical
	}
	read := DecisionRecord{}
	var members []string
	switch version, _ := root["recordVersion"].(string); version {
	case RecordVersion:
		read.form, members = FormPlain, plainMembers
	case KeyedRecordVersion:
		read.form, members = FormKeyed, keyedMembers
	default:
		return DecisionRecord{}, errRecordVersion
	}
	names := slices.Collect(maps.Keys(root))
	if binding, given := root["binding"]; given {
		names = slices.DeleteFunc(names, func(name string) bool { return name == "binding" })
		object, ok := binding.(map[string]any)
		if !ok || len(object) == 0 {
			return DecisionRecord{}, errRecordBinding
		}
		read.Binding = Binding{}
		for name, digest := range object {
			text, ok := digest.(string)
			if !ok {
				return DecisionRecord{}, errRecordBinding
			}
			read.Binding[name] = text
		}
		if check(read.Binding) != nil {
			return DecisionRecord{}, errRecordBinding
		}
	}
	slices.Sort(names)
	if !slices.Equal(names, members) {
		return DecisionRecord{}, errRecordMembers
	}

	pack, ok := stringObject(root["pack"], []string{"digest", "id", "specVersion", "version"}, nil)
	if !ok || !bindingDigest(pack["digest"]) || !uriformat.Valid(pack["id"]) || !versionText(pack["version"], false) || !versionText(pack["specVersion"], true) {
		return DecisionRecord{}, errRecordPack
	}
	read.Pack = Pack{ID: pack["id"], Version: pack["version"], SpecVersion: pack["specVersion"], Digest: pack["digest"]}
	evaluator, ok := stringObject(root["evaluator"], []string{"specVersion"}, []string{"runtimeVersion"})
	if !ok {
		return DecisionRecord{}, errRecordEvaluator
	}
	contract, ok := contractOf(evaluator["specVersion"])
	if !ok {
		return DecisionRecord{}, errRecordEvaluator
	}
	read.Evaluator = Evaluator{RuntimeVersion: evaluator["runtimeVersion"], SpecVersion: evaluator["specVersion"]}
	disposition, err := readDisposition(root["disposition"], contract)
	if err != nil {
		return DecisionRecord{}, err
	}
	read.Disposition = disposition

	if read.form == FormPlain {
		facts, factsOK := root["factsDigest"].(string)
		evidence, evidenceOK := root["evidenceDigest"].(string)
		if !factsOK || !evidenceOK || !bindingDigest(facts) || !bindingDigest(evidence) {
			return DecisionRecord{}, errRecordDigest
		}
		read.FactsDigest, read.EvidenceDigest = facts, evidence
	} else {
		keyed, ok := stringObject(root["keyedDigests"], []string{"algorithm", "evidence", "facts", "keyId"}, nil)
		if !ok {
			return DecisionRecord{}, errRecordKeyed
		}
		size := digestSize(keyed["algorithm"])
		if size == 0 || !keyID(keyed["keyId"]) {
			return DecisionRecord{}, errRecordKeyed
		}
		digests := make([][]byte, 2)
		for index, name := range []string{"facts", "evidence"} {
			digest := keyed[name]
			if len(digest) != 2*size || !lowerHex(digest) {
				return DecisionRecord{}, errRecordKeyed
			}
			digests[index], _ = hex.DecodeString(digest)
		}
		read.Keyed = &KeyedDigests{Algorithm: keyed["algorithm"], KeyID: keyed["keyId"], Facts: digests[0], Evidence: digests[1]}
	}
	read.json = bytes.Clone(data)
	read.digest = digestOf(read.json)
	return read, nil
}

// versionText reports whether text is three numbers with no leading zero
// separated by dots, the grammar of a pack's version, followed, when
// prerelease is true, by an optional hyphen and dot-separated identifiers of
// letters, digits and hyphens, as in the selector 0.2.0-draft.
func versionText(text string, prerelease bool) bool {
	core, label, labelled := strings.Cut(text, "-")
	if labelled && !prerelease {
		return false
	}
	numbers := strings.Split(core, ".")
	if len(numbers) != 3 {
		return false
	}
	for _, number := range numbers {
		if number == "" || len(number) > 1 && number[0] == '0' || strings.Trim(number, "0123456789") != "" {
			return false
		}
	}
	if !labelled {
		return true
	}
	for _, identifier := range strings.Split(label, ".") {
		if identifier == "" || strings.Trim(identifier, "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz-") != "" {
			return false
		}
	}
	return true
}

// contractOf is the evaluator contract a record's evaluator specVersion
// names, selected as the evaluator selects it, and false for a specVersion
// that names no contract this reader knows: its disposition cannot be held
// to a contract this reader does not have.
func contractOf(specVersion string) (result.EvaluatorContract, bool) {
	contract := result.EvaluatorContract{SpecVersion: specVersion}
	if contract.Core030Rules() {
		contract.Profile = "base"
	}
	return contract, specVersion != "" && contract.Validate() == nil
}

// The members a disposition and its handoff may hold, as package sdk writes
// them: the §8.3 members, and never the draft value member (ADR-0039), which
// package sdk does not offer.
var (
	dispositionMembers = []string{"handoff", "kind", "outcomeId", "reasons"}
	handoffMembers     = []string{"routes", "state", "triggeredBy"}
	routeMembers       = []string{"kind", "name"}
)

// readDisposition returns the canonical text of value when it is a §8.3
// disposition this runtime writes under contract: an object of no member but
// those above, refused with errRecordDispMember otherwise, each of its type,
// that the disposition's own canonical form, which holds it to the closed
// vocabularies and the presence rules of §8.3, writes byte for byte, refused
// with errRecordDisp otherwise.
func readDisposition(value any, contract result.EvaluatorContract) (string, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return "", errRecordDisp
	}
	handoff, ok := object["handoff"].(map[string]any)
	if !ok {
		return "", errRecordDisp
	}
	if !membersOf(object, dispositionMembers) || !membersOf(handoff, handoffMembers) {
		return "", errRecordDispMember
	}
	var disposition result.Disposition
	kind, kindOK := object["kind"].(string)
	state, stateOK := handoff["state"].(string)
	reasons, reasonsOK := stringList(object["reasons"])
	if !kindOK || !stateOK || !reasonsOK {
		return "", errRecordDisp
	}
	disposition.Kind, disposition.Handoff.State, disposition.Reasons = kind, state, reasons
	if outcome, given := object["outcomeId"]; given {
		if disposition.OutcomeID, ok = outcome.(string); !ok {
			return "", errRecordDisp
		}
	}
	if triggers, given := handoff["triggeredBy"]; given {
		if disposition.Handoff.TriggeredBy, ok = stringList(triggers); !ok {
			return "", errRecordDisp
		}
	}
	if routes, given := handoff["routes"]; given {
		items, ok := routes.([]any)
		if !ok {
			return "", errRecordDisp
		}
		disposition.Handoff.Routes = make([]result.HandoffTarget, 0, len(items))
		for _, item := range items {
			route, ok := stringObject(item, routeMembers, nil)
			if !ok {
				return "", errRecordDisp
			}
			disposition.Handoff.Routes = append(disposition.Handoff.Routes, result.HandoffTarget{Kind: route["kind"], Name: route["name"]})
		}
	}
	canonical, err := disposition.CanonicalWith(contract)
	if err != nil {
		return "", errRecordDisp
	}
	text, err := canonjson.Text(object)
	if err != nil || text != string(canonical) {
		return "", errRecordDisp
	}
	return text, nil
}

// membersOf reports whether object holds no member outside allowed.
func membersOf(object map[string]any, allowed []string) bool {
	for name := range object {
		if !slices.Contains(allowed, name) {
			return false
		}
	}
	return true
}

// stringList returns value as a list of strings, empty and not nil for an
// empty array.
func stringList(value any) ([]string, bool) {
	items, ok := value.([]any)
	if !ok {
		return nil, false
	}
	texts := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			return nil, false
		}
		texts = append(texts, text)
	}
	return texts, true
}

// stringObject returns value as an object of non-empty strings when it holds
// every required member, any of the optional ones, and nothing else.
func stringObject(value any, required, optional []string) (map[string]string, bool) {
	object, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	texts := make(map[string]string, len(object))
	for name, member := range object {
		text, ok := member.(string)
		if !ok || text == "" || !slices.Contains(required, name) && !slices.Contains(optional, name) {
			return nil, false
		}
		texts[name] = text
	}
	for _, name := range required {
		if _, given := texts[name]; !given {
			return nil, false
		}
	}
	return texts, true
}
