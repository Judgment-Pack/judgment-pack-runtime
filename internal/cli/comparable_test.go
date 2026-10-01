package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// The facts the comparable-facts fixture reads, every one of its comparison's
// type, and the same with one detector's flag sent as the string "true" -- the
// case the spec's field guide shape answers permitted for.
const (
	comparableFactsGood   = `{"amount":{"exceedsCap":false,"value":"10"},"request":{"exempt":false,"outsideWindow":false,"region":"domestic","verified":true}}`
	comparableFactsString = `{"amount":{"exceedsCap":"true","value":"10"},"request":{"exempt":false,"outsideWindow":false,"region":"domestic","verified":true}}`
	comparableFactsAbsent = `{"amount":{"value":"10"},"request":{"exempt":false,"outsideWindow":false,"region":"domestic","verified":true}}`
	permittedDisposition  = `{"kind":"outcome","outcomeId":"permitted","reasons":[],"handoff":{"state":"none"}}`
	stringFlagFinding     = `"/amount/exceedsCap" is a string, and equals can match only a boolean`
)

func comparableFixture(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "evaluation", "testdata", "comparable-facts.json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// comparableProject lays out a project around the fixture under the given
// configuration head -- the members before "packs" -- with an audit trail and
// a matrix whose one row probes the string flag on purpose, expecting the
// permitted answer Core gives it.
func comparableProject(t *testing.T, head string) string {
	t.Helper()
	matrix := `{"matrixVersion":"1","cases":[{"id":"string-flag","facts":` + comparableFactsString + `,"expectedDisposition":` + permittedDisposition + `}]}`
	return writeProjectFixture(t, `{`+head+`,"audit":{"dir":"audit"},"packs":{"detector":{"path":"packs/detector.json","matrix":"packs/detector.matrix.json"}}}`, map[string]string{
		"packs/detector.json":        comparableFixture(t),
		"packs/detector.matrix.json": matrix,
	})
}

// refusedAsIncomparable runs one command and requires the comparable-facts
// refusal: exit 1, the code, a message naming want, no disposition, and no
// value of the facts quoted.
func refusedAsIncomparable(t *testing.T, want string, args ...string) {
	t.Helper()
	code, stdout, stderr := runTest(t, append(args, "--format", "json"), "")
	if code != result.ExitInvalid {
		t.Fatalf("exit=%d, want %d (stdout %q stderr %q)", code, result.ExitInvalid, stdout, stderr)
	}
	var envelope struct {
		Diagnostics []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"diagnostics"`
		EvaluationError any `json:"evaluationError"`
		Disposition     any `json:"disposition"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("undecodable refusal %q: %v", stdout, err)
	}
	if len(envelope.Diagnostics) != 1 || envelope.Diagnostics[0].Code != evaluation.ComparableFactsCode || envelope.Disposition != nil ||
		envelope.EvaluationError != nil || !strings.Contains(envelope.Diagnostics[0].Message, want) || strings.Contains(envelope.Diagnostics[0].Message, `"true"`) {
		t.Fatalf("refusal = %+v, want %s naming %q", envelope, evaluation.ComparableFactsCode, want)
	}
}

// A project under configVersion "5" that sets requireComparableFacts refuses,
// before evaluating and without a record, an evaluation whose facts hold a
// type no comparison reading them can match -- a decision and a rehearsal
// alike, and a pack named by id or by path. An absent fact is evaluated, as
// unknown. packs test, whose rows may probe such a fact on purpose, is not
// refused.
func TestAProjectThatRequiresComparableFactsRefusesOneThatCannotMatch(t *testing.T) {
	configPath := comparableProject(t, `"configVersion":"5","requireComparableFacts":true`)
	packPath := filepath.Join(filepath.Dir(configPath), "packs", "detector.json")
	trail := filepath.Join(filepath.Dir(configPath), "audit", audit.FileName)
	stringFlag := writeDocument(t, "facts.json", comparableFactsString)

	refusedAsIncomparable(t, stringFlagFinding, "experimental", "evaluate", "--pack-id", "detector", "--config", configPath, "--facts", stringFlag)
	refusedAsIncomparable(t, stringFlagFinding, "experimental", "evaluate", packPath, "--config", configPath, "--facts", stringFlag)
	refusedAsIncomparable(t, stringFlagFinding, "experimental", "evaluate", packPath, "--rehearsal", "--config", configPath, "--facts", stringFlag)
	if _, err := os.Stat(trail); err == nil {
		t.Fatal("a refused evaluation leaves no record")
	}
	_, human, stderr := runTest(t, []string{"experimental", "evaluate", packPath, "--config", configPath, "--facts", stringFlag}, "")
	if !strings.Contains(stderr, stringFlagFinding) || strings.Contains(human, "disposition") {
		t.Fatalf("the human refusal names the finding and no disposition: stdout=%q stderr=%q", human, stderr)
	}

	good := evaluateJSON(t, "--pack-id", "detector", "--config", configPath, "--facts", writeDocument(t, "good.json", comparableFactsGood))
	if good.Disposition.OutcomeID != "permitted" {
		t.Fatalf("facts of their comparisons' types are evaluated: %+v", good.Disposition)
	}
	absent := evaluateJSON(t, "--pack-id", "detector", "--config", configPath, "--facts", writeDocument(t, "absent.json", comparableFactsAbsent))
	if absent.Disposition.Kind != "unresolved" || absent.Disposition.Handoff.State != "requested" {
		t.Fatalf("an absent fact is unknown, and onUnknown escalates it: %+v", absent.Disposition)
	}
	if records := auditRecords(t, configPath); len(records) != 2 {
		t.Fatalf("the two evaluations are recorded: %d", len(records))
	}

	code, stdout, stderr := runTest(t, []string{"packs", "test", "--config", configPath, "--format", "json"}, "")
	if code != 0 || stderr != "" || !strings.Contains(stdout, `"string-flag"`) || strings.Contains(stdout, evaluation.ComparableFactsCode) {
		t.Fatalf("packs test runs the row that probes the string flag: exit=%d stdout=%.400q stderr=%q", code, stdout, stderr)
	}
}

// configVersion "5" without the member behaves as before: the string flag is
// evaluated to the permitted answer Core gives it, and the record says what the
// trace said -- the comparison that crossed types -- without the value.
func TestWithoutTheMemberTheStringFlagIsEvaluatedAndTheRecordSaysSo(t *testing.T) {
	for _, head := range []string{`"configVersion":"5"`, `"configVersion":"5","requireComparableFacts":false`} {
		configPath := comparableProject(t, head)
		evaluated := evaluateJSON(t, "--pack-id", "detector", "--config", configPath, "--facts", writeDocument(t, "facts.json", comparableFactsString))
		if evaluated.Disposition.OutcomeID != "permitted" {
			t.Fatalf("%s: the string flag falls through to the fallback: %+v", head, evaluated.Disposition)
		}
		record := lastRecord(t, configPath)
		mismatches, _ := json.Marshal(record["typeMismatches"])
		if string(mismatches) != `[{"factType":"string","operandTypes":["boolean"],"operator":"equals","path":"/amount/exceedsCap"}]` {
			t.Fatalf("%s: the record names the comparison that crossed types: %s", head, mismatches)
		}
		if _, has := record["unknownCauses"]; has {
			t.Fatalf("%s: nothing was unknown: %v", head, record)
		}

		evaluateJSON(t, "--pack-id", "detector", "--config", configPath, "--facts", writeDocument(t, "absent.json", comparableFactsAbsent))
		record = lastRecord(t, configPath)
		causes, _ := json.Marshal(record["unknownCauses"])
		if string(causes) != `[{"cause":"absent","path":"/amount/exceedsCap"}]` {
			t.Fatalf("%s: the record names what left the result unknown: %s", head, causes)
		}
		if _, has := record["typeMismatches"]; has {
			t.Fatalf("%s: nothing crossed types: %v", head, record)
		}

		evaluateJSON(t, "--pack-id", "detector", "--config", configPath, "--facts", writeDocument(t, "good.json", comparableFactsGood))
		record = lastRecord(t, configPath)
		if _, has := record["typeMismatches"]; has {
			t.Fatalf("%s: a record with no notes carries no member: %v", head, record)
		}
		if _, has := record["unknownCauses"]; has {
			t.Fatalf("%s: a record with no notes carries no member: %v", head, record)
		}
	}
}

// detectorGraphProject is a two-node graph over the fixture, the first node
// feeding the second a fact the pack does not read, under the given
// configuration head. Its one row probes the string flag in the second node on
// purpose, expecting the permitted answer Core gives it.
func detectorGraphProject(t *testing.T, head string) (string, string) {
	t.Helper()
	graph := `{"formatVersion":"1","id":"detector-flow","version":"0.1.0","result":"second",
	  "nodes":{"first":{"pack":"detector"},"second":{"pack":"detector"}},
	  "edges":[{"from":"first","to":"second","fact":"/upstream/outcome"}]}`
	rows := `{"graphMatrixVersion":"1","cases":[{"id":"string-flag","inputs":{"first":{"facts":` + comparableFactsGood + `},"second":{"facts":` + comparableFactsString + `}},"expectedDisposition":` + permittedDisposition + `}]}`
	configPath := writeProjectFixture(t, `{`+head+`,"audit":{"dir":"audit"},"packs":{"detector":{"path":"packs/detector.json"}},"graphs":{"flow":{"path":"flow.graph.json","rows":"flow.rows.json"}}}`, map[string]string{
		"packs/detector.json": comparableFixture(t),
		"flow.graph.json":     graph,
		"flow.rows.json":      rows,
	})
	return configPath, filepath.Join(filepath.Dir(configPath), "flow.graph.json")
}

// The graph surface checks every node against the facts that node would be
// evaluated against, rehearsal or not: a wrong-typed fact in the second node's
// inputs refuses the run with that node named and records nothing. experimental
// graph test, whose row probes that fact on purpose, is not refused. Without the
// member the run completes, the node whose facts crossed types records the
// mismatch, and the composite, which carries no node's inputs, records none.
func TestTheGraphSurfaceChecksEveryNode(t *testing.T) {
	inputs := writeGraphInputs(t, `{"first":{"facts":`+comparableFactsGood+`},"second":{"facts":`+comparableFactsString+`}}`)
	configPath, graphPath := detectorGraphProject(t, `"configVersion":"5","requireComparableFacts":true`)
	for _, extra := range [][]string{nil, {"--rehearsal"}} {
		code, stdout, stderr := runTest(t, append([]string{"experimental", "graph", "evaluate", graphPath, "--config", configPath, "--inputs", inputs}, extra...), "")
		if code != result.ExitInvalid || !strings.Contains(stderr, `Node "second" (pack "detector")`) || !strings.Contains(stderr, stringFlagFinding) || strings.Contains(stdout, "disposition") {
			t.Fatalf("%v: the run is refused with the node named: exit=%d stdout=%q stderr=%q", extra, code, stdout, stderr)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(configPath), "audit", audit.FileName)); err == nil {
		t.Fatal("a refused graph run records nothing")
	}
	code, stdout, stderr := runTest(t, []string{"experimental", "graph", "test", "--config", configPath, "--format", "json"}, "")
	if code != 0 || stderr != "" || !strings.Contains(stdout, `"string-flag"`) || strings.Contains(stdout, evaluation.ComparableFactsCode) {
		t.Fatalf("experimental graph test runs the row that probes the string flag: exit=%d stdout=%.400q stderr=%q", code, stdout, stderr)
	}

	configPath, graphPath = detectorGraphProject(t, `"configVersion":"5"`)
	code, stdout, stderr = runTest(t, []string{"experimental", "graph", "evaluate", graphPath, "--config", configPath, "--inputs", inputs}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("without the member the run completes: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	records := auditRecords(t, configPath)
	if len(records) != 3 {
		t.Fatalf("two node records and a composite: %v", records)
	}
	if _, has := records[0]["typeMismatches"]; has {
		t.Fatalf("the first node crossed no type: %v", records[0])
	}
	mismatches, _ := json.Marshal(records[1]["typeMismatches"])
	if records[1]["graph"].(map[string]any)["node"] != "second" || string(mismatches) != `[{"factType":"string","operandTypes":["boolean"],"operator":"equals","path":"/amount/exceedsCap"}]` {
		t.Fatalf("the second node's record names its mismatch: %v", records[1])
	}
	if records[2]["kind"] != audit.KindGraphComposite || records[2]["typeMismatches"] != nil || records[2]["unknownCauses"] != nil {
		t.Fatalf("the composite carries no notes: %v", records[2])
	}
	if _, has := records[2]["typeMismatches"]; has {
		t.Fatalf("the composite carries no notes: %v", records[2])
	}
}
