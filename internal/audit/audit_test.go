package audit

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// evaluated is one completed evaluation, assembled the way the engine assembles
// one. The disposition is an ordinary outcome, which is what most records carry.
func evaluated() result.Evaluation {
	return result.Evaluation{
		Command:              "experimental evaluate",
		SpecVersion:          "0.2.0-draft",
		EvaluatorSpecVersion: result.EvaluatorSpecVersion,
		PackID:               "expense-approval",
		PackVersion:          "1.2.0",
		Artifact: &result.Artifact{
			SpecVersion:  "0.2.0-draft",
			BundleDigest: "sha256:" + strings.Repeat("a", 64),
			Provenance:   "bundled",
		},
		Disposition: result.Disposition{
			Kind:      "outcome",
			OutcomeID: "auto-approve",
			Reasons:   []string{},
			Handoff:   result.Handoff{State: "none"},
		},
	}
}

// writerAt binds a writer to a fresh project directory and returns both.
func writerAt(t *testing.T, dir string) (*Writer, string) {
	t.Helper()
	root := t.TempDir()
	opened, err := fssecure.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { opened.Close() })
	return NewWriter(opened, dir), root
}

// decodeLines reads the trail as the lines it is: one record per line, each one
// decodable on its own, which is the whole point of the format.
func decodeLines(t *testing.T, root, dir string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(dir), FileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Fatalf("every record ends its own line: %q", data)
	}
	records := []map[string]any{}
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("undecodable record %q: %v", line, err)
		}
		records = append(records, record)
	}
	return records
}

// One completed evaluation leaves one record: the pack it evaluated, the
// documents it evaluated against, and the disposition in the one canonical form
// §8.3 compares.
func TestOneEvaluationLeavesOneRecord(t *testing.T) {
	writer, root := writerAt(t, "audit")
	before := time.Now().UTC().Add(-time.Second)
	facts := []byte(`{"expense":{"amountUsd":"12.00","note":"a <b> & c"}}`)
	if err := writer.Evaluation(evaluated(), Inputs{
		Facts:            facts,
		Evidence:         []byte(`{"itemised-receipt":"present"}`),
		EvidenceSupplied: true,
	}, []byte(`{"id":"expense-approval"}`), nil); err != nil {
		t.Fatal(err)
	}

	records := decodeLines(t, root, "audit")
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	record := records[0]
	if record["recordVersion"] != RecordVersion || record["kind"] != KindEvaluation ||
		record["surface"] != "experimental evaluate" || record["evaluatorSpecVersion"] != result.EvaluatorSpecVersion {
		t.Fatalf("record = %v", record)
	}
	at, err := time.Parse(time.RFC3339Nano, record["at"].(string))
	if err != nil || at.Before(before) || at.After(time.Now().UTC().Add(time.Second)) {
		t.Fatalf("at = %v (%v)", record["at"], err)
	}
	if !strings.HasSuffix(record["at"].(string), "Z") {
		t.Fatalf("the timestamp is UTC: %v", record["at"])
	}

	pack := record["pack"].(map[string]any)
	if pack["id"] != "expense-approval" || pack["version"] != "1.2.0" || pack["specVersion"] != "0.2.0-draft" {
		t.Fatalf("pack = %v", pack)
	}
	// The digest names the exact bytes evaluated, which no payload carries.
	if pack["digest"] != Digest([]byte(`{"id":"expense-approval"}`)) ||
		!strings.HasPrefix(pack["digest"].(string), "sha256:") || len(pack["digest"].(string)) != len("sha256:")+64 {
		t.Fatalf("digest = %v", pack["digest"])
	}

	inputs := record["inputs"].(map[string]any)
	if inputs["evidenceSupplied"] != true {
		t.Fatalf("inputs = %v", inputs)
	}
	if record["graph"] != nil {
		t.Fatalf("a single-pack evaluation names no graph: %v", record["graph"])
	}
	// The build that made the record and the artifacts it evaluated against:
	// without them a record names a contract version and nothing that ran it.
	tool := record["tool"].(map[string]any)
	if tool["name"] != result.CurrentTool().Name || tool["version"] != result.CurrentTool().Version {
		t.Fatalf("tool = %v", tool)
	}
	if record["artifact"] == nil {
		t.Fatalf("the record carries the evaluation's artifact provenance: %v", record)
	}
	// One id per invocation, on every record that invocation writes.
	run, ok := record["run"].(string)
	if !ok || len(run) != 16 {
		t.Fatalf("run = %v", record["run"])
	}
	// A second invocation is a second writer and a second run.
	other, _ := writerAt(t, "audit")
	if other.run == run {
		t.Fatalf("two invocations must not share a run id: %q", run)
	}

	line, err := os.ReadFile(filepath.Join(root, "audit", FileName))
	if err != nil {
		t.Fatal(err)
	}
	// The facts reach the trail as the value that was evaluated. This fixture is
	// already compact, so its text survives unchanged, and HTML escaping is off
	// so an angle bracket is written as one — a spelling choice, since "<" and
	// "<" are the same JSON string either way.
	if !strings.Contains(string(line), `"facts":`+string(facts)) {
		t.Fatalf("the record must embed the evaluated facts %s: %s", facts, line)
	}
	// The disposition is the §8.3 canonical byte sequence, not a re-serialization
	// of it: members ordered by name, no whitespace, absent members absent.
	canonical, err := evaluated().Disposition.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(line), `"disposition":`+string(canonical)) {
		t.Fatalf("the record must embed the canonical disposition %s: %s", canonical, line)
	}
}

// Inputs are recorded as JSON values, not as source bytes, and the package doc
// says so rather than claiming a byte-exactness the encoder does not deliver:
// whitespace is dropped and an escape may be rewritten in an equivalent form.
// What is preserved is the value, which is all evaluation is a function of.
func TestInputsAreRecordedAsValuesAndNotAsSourceBytes(t *testing.T) {
	spellings := map[string]string{
		"insignificant whitespace":   "{\n  \"request\": { \"type\": \"data-access\" }\n}",
		"an escaped code point":      `{"request":{"type":"data\u002daccess"}}`,
		"already the compacted form": `{"request":{"type":"data-access"}}`,
	}
	want := map[string]any{"request": map[string]any{"type": "data-access"}}
	for name, spelling := range spellings {
		t.Run(name, func(t *testing.T) {
			writer, root := writerAt(t, "audit")
			if err := writer.Evaluation(evaluated(), Inputs{Facts: []byte(spelling)}, []byte(`{}`), nil); err != nil {
				t.Fatal(err)
			}
			// Every spelling decodes back to the one value that was evaluated,
			// which is what a reader replaying the record needs.
			recorded := decodeLines(t, root, "audit")[0]["inputs"].(map[string]any)["facts"]
			if !reflect.DeepEqual(recorded, want) {
				t.Fatalf("recorded %v, want the same value %v", recorded, want)
			}
			// And the text on the line is the source compacted — insignificant
			// whitespace gone, escapes left exactly as the caller wrote them.
			// So it is neither the source bytes nor a normalization of them, and
			// the package doc claims only the value.
			var compacted bytes.Buffer
			if err := json.Compact(&compacted, []byte(spelling)); err != nil {
				t.Fatal(err)
			}
			line, err := os.ReadFile(filepath.Join(root, "audit", FileName))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(line), `"facts":`+compacted.String()) {
				t.Fatalf("the record holds %s compacted: %s", spelling, line)
			}
		})
	}
}

// An omitted evidence document and an empty one are two different evaluations
// under §8.2, so they are two different records.
func TestAnOmittedEvidenceDocumentIsRecordedAsOmitted(t *testing.T) {
	writer, root := writerAt(t, "audit")
	if err := writer.Evaluation(evaluated(), Inputs{Facts: []byte(`{}`)}, []byte(`{}`), nil); err != nil {
		t.Fatal(err)
	}
	inputs := decodeLines(t, root, "audit")[0]["inputs"].(map[string]any)
	if inputs["evidence"] != nil || inputs["evidenceSupplied"] != false {
		t.Fatalf("inputs = %v", inputs)
	}
	// The member is written rather than omitted: a reader must not have to tell
	// "no evidence" from "this writer did not know".
	line, err := os.ReadFile(filepath.Join(root, "audit", FileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(line), `"evidence":null`) {
		t.Fatalf("an unsupplied document is recorded as null: %s", line)
	}
}

// A graph run leaves one record per node and one for the composite, and the
// composite repeats no node's inputs.
func TestAGraphRunLeavesNodeRecordsAndOneComposite(t *testing.T) {
	writer, root := writerAt(t, "audit")
	node := evaluated()
	node.Command = "experimental graph evaluate"
	if err := writer.Evaluation(node, Inputs{Facts: []byte(`{"screening":{"matches":"0"}}`)}, []byte(`{}`),
		&Graph{ID: "vendor-onboarding-flow", Version: "0.1.0", Node: "screening"}); err != nil {
		t.Fatal(err)
	}
	composite, err := CompositeRecord(result.GraphEvaluation{
		Command:              "experimental graph evaluate",
		EvaluatorSpecVersion: result.EvaluatorSpecVersion,
		GraphID:              "vendor-onboarding-flow",
		GraphVersion:         "0.1.0",
		FormatVersion:        "1",
		ResultNode:           "onboarding",
		Disposition:          evaluated().Disposition,
	}, "sha256:"+strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Append(composite); err != nil {
		t.Fatal(err)
	}

	records := decodeLines(t, root, "audit")
	if len(records) != 2 {
		t.Fatalf("records = %d, want 2", len(records))
	}
	nodeGraph := records[0]["graph"].(map[string]any)
	if records[0]["kind"] != KindEvaluation || nodeGraph["node"] != "screening" || nodeGraph["id"] != "vendor-onboarding-flow" {
		t.Fatalf("node record = %v", records[0])
	}
	compositeGraph := records[1]["graph"].(map[string]any)
	if records[1]["kind"] != KindGraphComposite || compositeGraph["resultNode"] != "onboarding" {
		t.Fatalf("composite record = %v", records[1])
	}
	if records[1]["inputs"] != nil || records[1]["pack"] != nil {
		t.Fatalf("the composite repeats no node's inputs or pack: %v", records[1])
	}
	// A graph's id and version are as mutable as a pack's, so the composite
	// names the document's exact bytes and the format it was written in.
	if compositeGraph["formatVersion"] != "1" || compositeGraph["digest"] != "sha256:"+strings.Repeat("b", 64) {
		t.Fatalf("composite graph = %v", compositeGraph)
	}
	// Both lines came from one writer, so both carry one run id: the composite
	// is what says that run finished.
	if records[0]["run"] != records[1]["run"] {
		t.Fatalf("one invocation writes one run id: %v vs %v", records[0]["run"], records[1]["run"])
	}
}

// The reader rule the package doc states, applied to the two shapes a trail can
// be found in after an interrupted run. Neither is produced by refusing before
// the open — that writes nothing — but an I/O failure partway through a write
// cannot be undone by an appender, so a reader has to be able to tell.
func TestAnUnfinishedRunIsIdentifiableInTheTrail(t *testing.T) {
	writer, root := writerAt(t, "audit")
	node, err := EvaluationRecord(evaluated(), Inputs{Facts: []byte(`{"n":1}`)}, []byte(`{}`),
		&Graph{ID: "g", Version: "0.1.0", Node: "first"})
	if err != nil {
		t.Fatal(err)
	}
	// A run whose composite never arrived: its node line is there, its run id
	// appears on no "graph-composite" line.
	if err := writer.AppendAll([]Record{node}); err != nil {
		t.Fatal(err)
	}
	// A second, complete run over the same trail.
	finished, _ := writerAt(t, "audit")
	finished.root, finished.dir = writer.root, writer.dir
	second, err := EvaluationRecord(evaluated(), Inputs{Facts: []byte(`{"n":2}`)}, []byte(`{}`),
		&Graph{ID: "g", Version: "0.1.0", Node: "first"})
	if err != nil {
		t.Fatal(err)
	}
	composite, err := CompositeRecord(result.GraphEvaluation{
		Command:              "experimental graph evaluate",
		EvaluatorSpecVersion: result.EvaluatorSpecVersion,
		GraphID:              "g",
		GraphVersion:         "0.1.0",
		ResultNode:           "first",
		Disposition:          evaluated().Disposition,
	}, "sha256:"+strings.Repeat("c", 64))
	if err != nil {
		t.Fatal(err)
	}
	if err := finished.AppendAll([]Record{second, composite}); err != nil {
		t.Fatal(err)
	}

	// The rule: a run is finished exactly when a composite carries its id.
	records := decodeLines(t, root, "audit")
	committed := map[string]bool{}
	for _, record := range records {
		if record["kind"] == KindGraphComposite {
			committed[record["run"].(string)] = true
		}
	}
	orphans := 0
	for _, record := range records {
		if record["kind"] == KindEvaluation && !committed[record["run"].(string)] {
			orphans++
		}
	}
	if len(committed) != 1 || orphans != 1 {
		t.Fatalf("committed=%v orphans=%d over %d records", committed, orphans, len(records))
	}
	if !committed[finished.run] || committed[writer.run] {
		t.Fatalf("the composite commits its own run and no other: %v", committed)
	}

	// The other shape: a write that stopped partway leaves a trailing line that
	// is not a complete JSON text, and it can only be the last one.
	trail := filepath.Join(root, "audit", FileName)
	data, err := os.ReadFile(trail)
	if err != nil {
		t.Fatal(err)
	}
	torn := append(append([]byte{}, data...), []byte(`{"recordVersion":"1","run":"beef`)...)
	if err := os.WriteFile(trail, torn, 0o600); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(torn), "\n"), "\n")
	for index, line := range lines {
		var decoded map[string]any
		err := json.Unmarshal([]byte(line), &decoded)
		if index < len(lines)-1 && err != nil {
			t.Fatalf("line %d of a torn trail must still decode: %q", index, line)
		}
		if index == len(lines)-1 && err == nil {
			t.Fatalf("the torn line must be identifiable as incomplete: %q", line)
		}
	}
}

// A draft-RFC evaluation is labeled in its record exactly as it is in its
// payload. A record that dropped the label would say a disposition produced by
// operators no published JPS version defines was an ordinary one.
func TestTheDraftPrototypeLabelSurvivesIntoTheRecord(t *testing.T) {
	for name, prototype := range map[string]*result.DraftPrototype{
		"a pack using the draft operators": {
			RFC:                       "0008",
			Status:                    "draft-rfc-prototype",
			Operators:                 []string{"exists"},
			PackValidUnderSpecVersion: false,
			Note:                      "prototype",
		},
		// The payload carries the label under the opt-in whatever the pack
		// uses, saying so in PackValidUnderSpecVersion; the record mirrors it
		// rather than deciding for itself.
		"a core-only pack under the same flag": {
			RFC:                       "0008",
			Status:                    "draft-rfc-prototype",
			Operators:                 []string{},
			PackValidUnderSpecVersion: true,
			Note:                      "prototype, unused",
		},
		"no opt-in at all": nil,
	} {
		t.Run(name, func(t *testing.T) {
			writer, root := writerAt(t, "audit")
			labeled := evaluated()
			labeled.DraftPrototype = prototype
			if err := writer.Evaluation(labeled, Inputs{Facts: []byte(`{}`)}, []byte(`{}`), nil); err != nil {
				t.Fatal(err)
			}
			record := decodeLines(t, root, "audit")[0]
			if prototype == nil {
				if record["draftPrototype"] != nil {
					t.Fatalf("an evaluation with no label records none: %v", record["draftPrototype"])
				}
				return
			}
			recorded, ok := record["draftPrototype"].(map[string]any)
			if !ok {
				t.Fatalf("the label must reach the record: %v", record)
			}
			if recorded["rfc"] != prototype.RFC || recorded["status"] != prototype.Status ||
				recorded["packValidUnderSpecVersion"] != prototype.PackValidUnderSpecVersion ||
				recorded["note"] != prototype.Note {
				t.Fatalf("recorded = %v, want %+v", recorded, prototype)
			}
		})
	}
}

// A writer a project never asked for writes nothing and reports nothing: it is
// the value every surface holds when the configuration declares no audit
// directory, and a surface that treated it as a failure would refuse every
// evaluation in every project that opted out.
func TestTheNilWriterWritesNothing(t *testing.T) {
	var writer *Writer
	if err := writer.Evaluation(evaluated(), Inputs{Facts: []byte(`{}`)}, []byte(`{}`), nil); err != nil {
		t.Fatalf("a project that asked for no trail has no failure to report: %v", err)
	}
	if err := writer.Append(Record{}); err != nil {
		t.Fatal(err)
	}
	if err := writer.AppendAll([]Record{{}, {}}); err != nil {
		t.Fatal(err)
	}
}

// A run's records are written together or not at all, in one open, and each one
// carries its own moment rather than the moment the batch was written.
func TestAppendAllWritesEveryRecordOrNone(t *testing.T) {
	writer, root := writerAt(t, "audit")
	first, err := EvaluationRecord(evaluated(), Inputs{Facts: []byte(`{"n":1}`)}, []byte(`{}`), &Graph{ID: "g", Version: "0.1.0", Node: "a"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := EvaluationRecord(evaluated(), Inputs{Facts: []byte(`{"n":2}`)}, []byte(`{}`), &Graph{ID: "g", Version: "0.1.0", Node: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.AppendAll([]Record{first, second}); err != nil {
		t.Fatal(err)
	}
	records := decodeLines(t, root, "audit")
	if len(records) != 2 {
		t.Fatalf("records = %d, want 2", len(records))
	}
	// File order is the ordering key within a run; the stamp is informational
	// time at nanosecond resolution. Adjacent records may share a stamp — no
	// wall clock promises distinct ticks — but time may never run backwards
	// across a batch composed in order.
	var stamps []time.Time
	for _, record := range records {
		at, err := time.Parse(time.RFC3339Nano, record["at"].(string))
		if err != nil {
			t.Fatalf("at = %v: %v", record["at"], err)
		}
		stamps = append(stamps, at)
	}
	if stamps[1].Before(stamps[0]) {
		t.Fatalf("a batch composed in order must not carry time running backwards: %v then %v",
			records[0]["at"], records[1]["at"])
	}

	// Nothing at all is written when the batch cannot be opened.
	blocked, blockedRoot := writerAt(t, "audit")
	if err := os.WriteFile(filepath.Join(blockedRoot, "audit"), []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := blocked.AppendAll([]Record{first, second}); err == nil {
		t.Fatal("a batch that could not be opened must be reported")
	}
	if _, err := os.Stat(filepath.Join(blockedRoot, "audit", FileName)); err == nil {
		t.Fatal("a refused batch leaves no partial trail")
	}
	// An empty batch is not a write: a run that recorded nothing must not make
	// the directory or the file.
	empty, emptyRoot := writerAt(t, "audit")
	if err := empty.AppendAll(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(emptyRoot, "audit")); err == nil {
		t.Fatal("an empty batch creates nothing")
	}
}

// A write that cannot happen is reported, never swallowed: the surfaces refuse
// the run on this error, so it must actually arrive.
func TestAFailedWriteIsReported(t *testing.T) {
	writer, root := writerAt(t, "audit")
	// Something else is already at the declared directory's name, so no
	// directory can be made there and no record can be written under it.
	if err := os.WriteFile(filepath.Join(root, "audit"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writer.Evaluation(evaluated(), Inputs{Facts: []byte(`{}`)}, []byte(`{}`), nil); err == nil {
		t.Fatal("a record that could not be written must be reported")
	}

	// A path leaving the project is refused before anything is opened.
	escaping, _ := writerAt(t, "../outside")
	if err := escaping.Evaluation(evaluated(), Inputs{Facts: []byte(`{}`)}, []byte(`{}`), nil); err == nil {
		t.Fatal("an audit directory outside the project must be refused")
	}
}

// A disposition that is not a legal §8.3 value is not written as one. The
// canonicalizer is the gate, and the record uses it rather than a serializer of
// its own.
func TestAnIllegalDispositionIsNotRecorded(t *testing.T) {
	writer, root := writerAt(t, "audit")
	illegal := evaluated()
	illegal.Disposition = result.Disposition{Kind: "approved", Reasons: []string{}, Handoff: result.Handoff{State: "none"}}
	if err := writer.Evaluation(illegal, Inputs{Facts: []byte(`{}`)}, []byte(`{}`), nil); err == nil {
		t.Fatal("a value that is not a §8.3 disposition must not be recorded as one")
	}
	if _, err := os.Stat(filepath.Join(root, "audit", FileName)); err == nil {
		t.Fatal("nothing is written for a record that could not be composed")
	}
}
