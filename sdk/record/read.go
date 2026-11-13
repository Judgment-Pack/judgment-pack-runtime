package record

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/sdk/internal/canonjson"
)

// ErrRecord is wrapped by the error for bytes Read does not admit as a
// decision record. The error says which rule the bytes break in a fixed
// sentence and repeats nothing of them. Experimental.
var ErrRecord = errors.New("record: not a decision record this reader admits")

var (
	errRecordJSON      = fmt.Errorf("%w: it is not one JSON object the carrier admits", ErrRecord)
	errRecordCanonical = fmt.Errorf("%w: it is not in the record's canonical form", ErrRecord)
	errRecordVersion   = fmt.Errorf("%w: its recordVersion is not %s or %s, the versions this reader admits", ErrRecord, RecordVersion, KeyedRecordVersion)
	errRecordMembers   = fmt.Errorf("%w: its members are not those of its recordVersion", ErrRecord)
	errRecordPack      = fmt.Errorf("%w: its pack is not an object of digest, id, specVersion and version", ErrRecord)
	errRecordEvaluator = fmt.Errorf("%w: its evaluator is not an object of specVersion and an optional runtimeVersion", ErrRecord)
	errRecordDigest    = fmt.Errorf("%w: a document digest is not sha256: followed by 64 lower-case hexadecimal digits", ErrRecord)
	errRecordKeyed     = fmt.Errorf("%w: its keyedDigests is not an object of algorithm, evidence, facts and keyId of the shape KeyedDigests states", ErrRecord)
	errRecordBinding   = fmt.Errorf("%w: its binding is not a non-empty object of the shape Binding states", ErrRecord)
	errRecordDisp      = fmt.Errorf("%w: its disposition is not an object", ErrRecord)
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
// record's canonical text, and each member of the shape JSON states. Any
// other bytes are refused with an error wrapping ErrRecord. A record of a
// later version is refused, not read as one this reader knows.
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
	if !ok || !bindingDigest(pack["digest"]) {
		return DecisionRecord{}, errRecordPack
	}
	read.Pack = Pack{ID: pack["id"], Version: pack["version"], SpecVersion: pack["specVersion"], Digest: pack["digest"]}
	evaluator, ok := stringObject(root["evaluator"], []string{"specVersion"}, []string{"runtimeVersion"})
	if !ok {
		return DecisionRecord{}, errRecordEvaluator
	}
	read.Evaluator = Evaluator{RuntimeVersion: evaluator["runtimeVersion"], SpecVersion: evaluator["specVersion"]}
	disposition, ok := root["disposition"].(map[string]any)
	if !ok {
		return DecisionRecord{}, errRecordDisp
	}
	text, err := canonjson.Text(disposition)
	if err != nil {
		return DecisionRecord{}, errRecordDisp
	}
	read.Disposition = text

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
