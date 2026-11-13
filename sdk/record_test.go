package sdk_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/sdk"
	"github.com/Judgment-Pack/judgment-pack-runtime/sdk/record"
	"github.com/Judgment-Pack/judgment-pack-runtime/sdk/view"
)

// annotatedPack is the Core 0.5.0 decision-record fixture, with annotations on
// the outcome its rule selects; the example reads it under this name.
const annotatedPack = sdk.RecordPack050

const (
	recordFacts    = `{"amount":"1250"}`
	recordEvidence = `{"invoice":"present"}`
	digestA        = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	digestB        = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func evaluateWithRecord(t *testing.T, pack *sdk.Pack, facts, evidence string, omitEvidence bool, binding record.Binding) (*sdk.Evaluation, record.DecisionRecord) {
	t.Helper()
	var supplied []byte
	if !omitEvidence {
		supplied = []byte(evidence)
	}
	evaluated, decided, err := pack.EvaluateWithRecord(context.Background(), []byte(facts), supplied, binding)
	if err != nil {
		t.Fatalf("EvaluateWithRecord: %v", err)
	}
	return evaluated, decided
}

// The evaluation EvaluateWithRecord returns is the one Evaluate returns for
// the same inputs, and the record's typed fields say what its bytes say.
func TestEvaluateWithRecordIsEvaluateAndItsRecord(t *testing.T) {
	description, err := sdk.Describe()
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{sdk.RecordPack030, sdk.RecordPack050, routedPack, thresholdPack} {
		pack := load(t, text, sdk.LoadOptions{})
		for _, facts := range []string{recordFacts, `{"amount":"10"}`, `{"score":2}`, `{"request":{"amount":"1000"}}`} {
			plain, err := pack.Evaluate(context.Background(), []byte(facts), nil)
			if err != nil {
				t.Fatal(err)
			}
			evaluated, decided := evaluateWithRecord(t, pack, facts, "", true, record.Binding{"settings": digestA})
			plainJSON, _ := plain.JSON()
			recordedJSON, _ := evaluated.JSON()
			if !bytes.Equal(plainJSON, recordedJSON) || !reflect.DeepEqual(plain.Disposition, evaluated.Disposition) {
				t.Fatalf("the evaluation differs from Evaluate's:\n%s\n%s", plainJSON, recordedJSON)
			}
			identity := pack.Identity()
			if decided.Pack != (record.Pack{ID: identity.ID, Version: identity.Version, SpecVersion: identity.SpecVersion, Digest: identity.Digest}) {
				t.Fatalf("record pack = %#v, identity %#v", decided.Pack, identity)
			}
			if decided.Disposition != string(plain.CanonicalDisposition()) {
				t.Fatalf("record disposition = %s, want %s", decided.Disposition, plain.CanonicalDisposition())
			}
			if decided.Evaluator != (record.Evaluator{RuntimeVersion: description.RuntimeVersion, SpecVersion: plain.EvaluatorSpecVersion}) {
				t.Fatalf("record evaluator = %#v", decided.Evaluator)
			}
			if decided.Digest() != digestOf(decided.JSON()) {
				t.Fatalf("digest %s is not the digest of the record's bytes", decided.Digest())
			}
			var members struct {
				Binding     map[string]string `json:"binding"`
				Disposition json.RawMessage   `json:"disposition"`
				Evaluator   struct {
					RuntimeVersion *string `json:"runtimeVersion"`
					SpecVersion    string  `json:"specVersion"`
				} `json:"evaluator"`
				EvidenceDigest string `json:"evidenceDigest"`
				FactsDigest    string `json:"factsDigest"`
				Pack           struct {
					Digest, ID, SpecVersion, Version string
				} `json:"pack"`
				RecordVersion string `json:"recordVersion"`
			}
			decoder := json.NewDecoder(bytes.NewReader(decided.JSON()))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&members); err != nil {
				t.Fatalf("record %s: %v", decided.JSON(), err)
			}
			if members.RecordVersion != record.RecordVersion || record.RecordVersion != "1.0.0" ||
				string(members.Disposition) != decided.Disposition ||
				members.FactsDigest != decided.FactsDigest || members.EvidenceDigest != decided.EvidenceDigest ||
				members.Pack.Digest != decided.Pack.Digest || members.Pack.ID != decided.Pack.ID ||
				members.Pack.SpecVersion != decided.Pack.SpecVersion || members.Pack.Version != decided.Pack.Version ||
				members.Evaluator.SpecVersion != decided.Evaluator.SpecVersion ||
				(members.Evaluator.RuntimeVersion == nil) != (decided.Evaluator.RuntimeVersion == "") ||
				!reflect.DeepEqual(members.Binding, map[string]string(decided.Binding)) {
				t.Fatalf("the record's bytes and fields disagree: %s\n%#v", decided.JSON(), decided)
			}
		}
	}
}

// The same inputs give the same bytes: on every call, from every Pack loaded
// from the same bytes, and from concurrent calls on one Pack. A facts or
// evidence document spelled another way, with other white space, member order
// or string escapes, is the same document as evaluated, and gives the same
// record.
func TestTheRecordIsByteStable(t *testing.T) {
	binding := record.Binding{"settings": digestA, "judgment-pack-runner/preparation/1": digestB}
	first := load(t, sdk.RecordPack050, sdk.LoadOptions{})
	_, want := evaluateWithRecord(t, first, recordFacts, recordEvidence, false, binding)
	second := load(t, sdk.RecordPack050, sdk.LoadOptions{})
	spellings := [][2]string{
		{recordFacts, recordEvidence},
		{"{\n  \"amount\": \"1250\"\n}\n", "{ \"invoice\" : \"present\" }"},
		{`{"amount":"\u0031250"}`, `{"invoice":"pr\u0065sent"}`},
	}
	for _, pack := range []*sdk.Pack{first, second} {
		for _, spelling := range spellings {
			_, got := evaluateWithRecord(t, pack, spelling[0], spelling[1], false, binding)
			if !bytes.Equal(got.JSON(), want.JSON()) || got.Digest() != want.Digest() {
				t.Fatalf("facts %q, evidence %q:\n got %s\nwant %s", spelling[0], spelling[1], got.JSON(), want.JSON())
			}
		}
	}
	// Member order inside the facts is not part of the document as evaluated.
	_, left := evaluateWithRecord(t, first, `{"amount":"1250","b":{"y":1,"x":[2,1]}}`, "", true, nil)
	_, right := evaluateWithRecord(t, first, `{"b":{"x":[2,1],"y":1},"amount":"1250"}`, "", true, nil)
	if left.Digest() != right.Digest() {
		t.Fatalf("member order moved the digest:\n%s\n%s", left.JSON(), right.JSON())
	}
	var group sync.WaitGroup
	digests := make([]string, 16)
	for index := range digests {
		group.Add(1)
		go func() {
			defer group.Done()
			_, decided, err := first.EvaluateWithRecord(context.Background(), []byte(recordFacts), []byte(recordEvidence), binding)
			if err == nil {
				digests[index] = decided.Digest()
			}
		}()
	}
	group.Wait()
	for index, digest := range digests {
		if digest != want.Digest() {
			t.Fatalf("concurrent call %d: digest %q, want %s", index, digest, want.Digest())
		}
	}
}

// Changing any one input changes the record's digest: the pack's bytes, even
// where its identity members stay, the facts, the evidence and the binding.
// Two inputs that the record says are one evaluation do not: an omitted
// evidence document and a supplied {}, and a nil and an empty binding.
func TestEachInputMovesTheDigest(t *testing.T) {
	base := load(t, sdk.RecordPack030, sdk.LoadOptions{})
	retitled := load(t, strings.Replace(sdk.RecordPack030, "Synthetic decision-record fixture", "Retitled fixture", 1), sdk.LoadOptions{})
	newline := load(t, sdk.RecordPack030+"\n", sdk.LoadOptions{})
	other := load(t, sdk.RecordPack050, sdk.LoadOptions{})
	binding := record.Binding{"settings": digestA}
	rows := []struct {
		name               string
		pack               *sdk.Pack
		facts, evidence    string
		omitEvidence       bool
		binding            record.Binding
		samePack, sameFact bool
	}{
		{name: "base", pack: base, facts: recordFacts, evidence: recordEvidence, binding: binding},
		{name: "pack bytes with a trailing newline", pack: newline, facts: recordFacts, evidence: recordEvidence, binding: binding},
		{name: "pack retitled, its identity members kept", pack: retitled, facts: recordFacts, evidence: recordEvidence, binding: binding},
		{name: "pack under another specVersion", pack: other, facts: recordFacts, evidence: recordEvidence, binding: binding},
		{name: "facts with another value deciding the same", pack: base, facts: `{"amount":"1251"}`, evidence: recordEvidence, binding: binding},
		{name: "facts with a member the pack does not read", pack: base, facts: `{"amount":"1250","extra":true}`, evidence: recordEvidence, binding: binding},
		{name: "facts 1.5", pack: base, facts: `{"amount":"1250","n":1.5}`, evidence: recordEvidence, binding: binding},
		{name: "facts 1.50", pack: base, facts: `{"amount":"1250","n":1.50}`, evidence: recordEvidence, binding: binding},
		{name: "evidence absent", pack: base, facts: recordFacts, evidence: `{"invoice":"absent"}`, binding: binding},
		{name: "evidence {}", pack: base, facts: recordFacts, evidence: `{}`, binding: binding},
		{name: "no binding", pack: base, facts: recordFacts, evidence: recordEvidence},
		{name: "binding with another digest", pack: base, facts: recordFacts, evidence: recordEvidence, binding: record.Binding{"settings": digestB}},
		{name: "binding under another name", pack: base, facts: recordFacts, evidence: recordEvidence, binding: record.Binding{"setting": digestA}},
		{name: "binding with one more name", pack: base, facts: recordFacts, evidence: recordEvidence, binding: record.Binding{"settings": digestA, "mapping": digestB}},
	}
	seen := map[string]string{}
	var baseRecord record.DecisionRecord
	for _, row := range rows {
		_, decided := evaluateWithRecord(t, row.pack, row.facts, row.evidence, row.omitEvidence, row.binding)
		if earlier, found := seen[decided.Digest()]; found {
			t.Fatalf("%s gives the digest of %s: %s", row.name, earlier, decided.JSON())
		}
		seen[decided.Digest()] = row.name
		if row.name == "base" {
			baseRecord = decided
			continue
		}
		if decided.Disposition != baseRecord.Disposition && row.pack == base {
			t.Fatalf("%s decided otherwise, so the row does not isolate its input: %s", row.name, decided.Disposition)
		}
	}
	// An omitted evidence document is the empty object §8.2 evaluates it as.
	_, supplied := evaluateWithRecord(t, base, recordFacts, `{}`, false, binding)
	_, omitted := evaluateWithRecord(t, base, recordFacts, "", true, binding)
	if supplied.Digest() != omitted.Digest() || omitted.EvidenceDigest != digestOf([]byte(`{}`)) {
		t.Fatalf("omitted evidence %s, supplied {} %s", omitted.JSON(), supplied.JSON())
	}
	// A nil and an empty binding are both no binding.
	_, none := evaluateWithRecord(t, base, recordFacts, recordEvidence, false, nil)
	_, empty := evaluateWithRecord(t, base, recordFacts, recordEvidence, false, record.Binding{})
	if none.Digest() != empty.Digest() || empty.Binding != nil || bytes.Contains(empty.JSON(), []byte(`"binding"`)) {
		t.Fatalf("nil binding %s, empty binding %s (%#v)", none.JSON(), empty.JSON(), empty.Binding)
	}
}

// A binding not of its shape is refused before anything is evaluated: with
// facts the evaluator would refuse, the binding's error is the one returned.
// The error wraps record.ErrBinding, is no *Refusal and no internal fault, and
// repeats no name or digest of the binding.
func TestAMalformedBindingIsRefusedBeforeEvaluating(t *testing.T) {
	pack := load(t, sdk.RecordPack030, sdk.LoadOptions{})
	many := record.Binding{}
	for index := 0; index <= record.MaxBindingNames; index++ {
		many["name-"+string(rune('a'+index))] = digestA
	}
	rows := []struct {
		name    string
		binding record.Binding
		reason  string
	}{
		{"seventeen names", many, "it holds more than 16 names"},
		{"an empty name", record.Binding{"": digestA}, "a name is not 1 to 128 characters"},
		{"a name of 129 characters", record.Binding{strings.Repeat("n", 129): digestA}, "a name is not 1 to 128 characters"},
		{"an upper-case name", record.Binding{"Settings": digestA}, "a name is not 1 to 128 characters"},
		{"a name with a space", record.Binding{"my settings": digestA}, "a name is not 1 to 128 characters"},
		{"a name with a colon", record.Binding{"settings:1": digestA}, "a name is not 1 to 128 characters"},
		{"a name outside ASCII", record.Binding{"r\u00e9glage": digestA}, "a name is not 1 to 128 characters"},
		{"an empty digest", record.Binding{"settings": ""}, "a digest is not sha256:"},
		{"a digest without its prefix", record.Binding{"settings": strings.TrimPrefix(digestA, "sha256:")}, "a digest is not sha256:"},
		{"a digest of another algorithm", record.Binding{"settings": "sha512:" + strings.TrimPrefix(digestA, "sha256:")}, "a digest is not sha256:"},
		{"a digest in upper case", record.Binding{"settings": strings.ToUpper(digestA)}, "a digest is not sha256:"},
		{"an upper-case hex digit", record.Binding{"settings": digestA[:70] + "A"}, "a digest is not sha256:"},
		{"a digest one digit short", record.Binding{"settings": digestA[:70]}, "a digest is not sha256:"},
		{"a digest one digit long", record.Binding{"settings": digestA + "a"}, "a digest is not sha256:"},
		{"a digest with a non-hex letter", record.Binding{"settings": digestA[:70] + "g"}, "a digest is not sha256:"},
		{"a digest with white space", record.Binding{"settings": digestA[:70] + " "}, "a digest is not sha256:"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			evaluated, decided, err := pack.EvaluateWithRecord(context.Background(), []byte(`{"amount":`), nil, row.binding)
			var refusal *sdk.Refusal
			if !errors.Is(err, record.ErrBinding) || errors.As(err, &refusal) || errors.Is(err, sdk.ErrInternal) {
				t.Fatalf("error %v (%T), want one wrapping record.ErrBinding", err, err)
			}
			if !strings.Contains(err.Error(), row.reason) {
				t.Fatalf("error %q does not say %q", err, row.reason)
			}
			for name, digest := range row.binding {
				if (name != "" && strings.Contains(err.Error(), name)) || (len(digest) > 8 && strings.Contains(err.Error(), digest[7:])) {
					t.Fatalf("error %q repeats the binding", err)
				}
			}
			if evaluated != nil || decided.JSON() != nil {
				t.Fatalf("a refused binding returned an evaluation or a record")
			}
		})
	}
	// The bounds themselves are admitted.
	full := record.Binding{}
	for index := 0; index < record.MaxBindingNames; index++ {
		full["name-"+string(rune('a'+index))] = digestB
	}
	full["name-a"] = digestA
	long := record.Binding{strings.Repeat("a", record.MaxBindingNameLength): digestA, "0._/-": digestB}
	for _, binding := range []record.Binding{full, long} {
		_, decided := evaluateWithRecord(t, pack, recordFacts, "", true, binding)
		if !reflect.DeepEqual(decided.Binding, binding) {
			t.Fatalf("binding %v recorded as %v", binding, decided.Binding)
		}
	}
}

// The record keeps its own copy of the binding, and the caller's copy of the
// record is the caller's: editing either changes neither the bytes nor the
// digest.
func TestTheRecordIsTheCallersCopy(t *testing.T) {
	pack := load(t, sdk.RecordPack030, sdk.LoadOptions{})
	binding := record.Binding{"settings": digestA}
	_, decided := evaluateWithRecord(t, pack, recordFacts, "", true, binding)
	bytesBefore, digestBefore := bytes.Clone(decided.JSON()), decided.Digest()
	binding["settings"] = digestB
	binding["mapping"] = digestB
	if decided.Binding["settings"] != digestA || len(decided.Binding) != 1 {
		t.Fatalf("the record's binding follows the caller's map: %v", decided.Binding)
	}
	decided.Binding["settings"] = digestB
	decided.FactsDigest = digestB
	returned := decided.JSON()
	returned[0] = 'X'
	if !bytes.Equal(decided.JSON(), bytesBefore) || decided.Digest() != digestBefore || digestOf(decided.JSON()) != digestBefore {
		t.Fatalf("editing the record changed its bytes: %s", decided.JSON())
	}
	var zero record.DecisionRecord
	if zero.JSON() != nil || zero.Digest() != "" {
		t.Fatalf("the zero record has bytes %q and digest %q", zero.JSON(), zero.Digest())
	}
}

// Every other error is Evaluate's: a refused input is the same *Refusal, a
// done context is ctx.Err(), and a misuse is a plain error.
func TestEvaluateWithRecordErrorsAreEvaluates(t *testing.T) {
	pack := load(t, sdk.RecordPack030, sdk.LoadOptions{})
	for _, inputs := range [][2][]byte{
		{[]byte(`{"amount":`), nil},
		{[]byte(recordFacts), []byte(`{"unknown":"present"}`)},
		{[]byte(recordFacts), []byte{}},
		{nil, nil},
	} {
		_, plain := pack.Evaluate(context.Background(), inputs[0], inputs[1])
		evaluated, decided, err := pack.EvaluateWithRecord(context.Background(), inputs[0], inputs[1], record.Binding{"settings": digestA})
		if plain == nil || evaluated != nil || decided.JSON() != nil {
			t.Fatalf("inputs %q: Evaluate %v, EvaluateWithRecord %v", inputs, plain, evaluated)
		}
		if !reflect.DeepEqual(refusalOf(t, err), refusalOf(t, plain)) {
			t.Fatalf("inputs %q: refusal %#v, Evaluate's %#v", inputs, err, plain)
		}
	}
	done, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := pack.EvaluateWithRecord(done, []byte(recordFacts), nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("done context: %v", err)
	}
	var nilContext context.Context
	if _, _, err := pack.EvaluateWithRecord(nilContext, []byte(recordFacts), nil, nil); err == nil || errors.Is(err, sdk.ErrInternal) {
		t.Fatalf("nil context: %v", err)
	}
	var unloaded *sdk.Pack
	if _, _, err := unloaded.EvaluateWithRecord(context.Background(), []byte(recordFacts), nil, nil); err == nil || errors.Is(err, sdk.ErrInternal) {
		t.Fatalf("a Pack Load did not return: %v", err)
	}
}

// The annotations of the chosen outcome are reachable from the evaluation,
// as the view carries them, without the caller reading the pack again; they
// are absent for a disposition that is not an outcome, for an outcome without
// annotations and under a specVersion without them, and the caller owns what
// is returned.
func TestOutcomeAnnotationsAreTheChosenOutcomes(t *testing.T) {
	annotated := load(t, sdk.RecordPack050, sdk.LoadOptions{})
	evaluated, _ := evaluateWithRecord(t, annotated, recordFacts, "", true, nil)
	want := []view.Annotation{{Key: "follow-up", List: []string{"call", "write"}}, {Key: "team", Value: "intake"}}
	got, err := evaluated.OutcomeAnnotations()
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("annotations = %#v, %v; want %#v", got, err, want)
	}
	described, err := annotated.View()
	if err != nil || !reflect.DeepEqual(described.Outcomes[0].Annotations, want) {
		t.Fatalf("the view carries %#v, %v", described.Outcomes[0].Annotations, err)
	}
	got[0].List[0] = "edited"
	got[1].Value = "edited"
	plain, err := annotated.Evaluate(context.Background(), []byte(recordFacts), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, from := range []*sdk.Evaluation{evaluated, plain} {
		if again, err := from.OutcomeAnnotations(); err != nil || !reflect.DeepEqual(again, want) {
			t.Fatalf("annotations after editing a returned copy = %#v, %v", again, err)
		}
	}
	routed := load(t, routedPack, sdk.LoadOptions{})
	for _, row := range []struct {
		name  string
		pack  *sdk.Pack
		facts string
	}{
		{"an outcome without annotations", annotated, `{"amount":"10"}`},
		{"a Core 0.3.0 pack", load(t, sdk.RecordPack030, sdk.LoadOptions{}), recordFacts},
		{"an unresolved disposition", routed, `{"score":2}`},
		{"a Core 0.3.0 outcome", routed, `{"score":1}`},
	} {
		evaluated, err := row.pack.Evaluate(context.Background(), []byte(row.facts), nil)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := evaluated.OutcomeAnnotations(); got != nil || err != nil {
			t.Fatalf("%s: annotations %#v, %v", row.name, got, err)
		}
	}
	var zero sdk.Evaluation
	if _, err := zero.OutcomeAnnotations(); err == nil || errors.Is(err, sdk.ErrInternal) {
		t.Fatalf("the zero Evaluation: %v", err)
	}
}
