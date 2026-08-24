package graph

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// canonicalCompliance is the RFC 8785 rendering of the target both fixture
// packs configure, which is what the report members carry within the budget.
const canonicalCompliance = `{"kind":"human-role","name":"Compliance"}`

const approveHeadline = `{"kind":"outcome","outcomeId":"approve","reasons":[],"handoff":{"state":"none"}}`

// The escalating fixture row: the target is reported exactly when the
// disposition requests a handoff, on the pack evaluation's own rule, so the
// assertion rides the row whose run escalates.
const unresolvedInputs = `{"screening":{"facts":{},"evidence":{"screening-record":"present"}}}`
const unresolvedDisposition = `{"kind":"unresolved","reasons":["unknown"],"handoff":{"state":"requested","triggeredBy":["unknown"]}}`

func targetRows(headline, nodeTarget string) Rows {
	row := RowCase{
		ID:                  "asserts-target",
		Inputs:              json.RawMessage(unresolvedInputs),
		ExpectedDisposition: json.RawMessage(unresolvedDisposition),
	}
	if headline != "" {
		row.ExpectedHandoffTarget = json.RawMessage(headline)
	}
	if nodeTarget != "" {
		row.ExpectedNodes = map[string]json.RawMessage{"screening": json.RawMessage(unresolvedDisposition)}
		row.ExpectedNodeHandoffTargets = map[string]json.RawMessage{"screening": json.RawMessage(nodeTarget)}
	}
	return Rows{Cases: []RowCase{row}}
}

// A graph row asserts the composite's reported handoff target — the result
// node's own — and the pair is reported rendered, exactly when asserted
// (ADR-0032). A row that asserts nothing carries neither member: the previous
// payload, byte for byte.
func TestGraphRowAssertsTheCompositeTarget(t *testing.T) {
	loaded := fixtureProject(t)
	output, failure := Test(loaded, newEngine(t), fixtureDocument(t), "g", "r", targetRows(canonicalCompliance, ""), Options{Command: "test"})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	row := output.Rows[0]
	if output.Status != "passed" || row.ExpectedHandoffTarget != canonicalCompliance || row.ActualHandoffTarget != canonicalCompliance {
		t.Fatalf("a matching assertion passes with the pair rendered: %s", rawJSON(t, row))
	}
	if !strings.Contains(rawJSON(t, row), `"expectedHandoffTarget"`) || !strings.Contains(rawJSON(t, row), `"actualHandoffTarget"`) {
		t.Fatalf("the pair reaches the wire: %s", rawJSON(t, row))
	}

	bare, failure := Test(loaded, newEngine(t), fixtureDocument(t), "g", "r", targetRows("", ""), Options{Command: "test"})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if strings.Contains(rawJSON(t, bare.Rows[0]), "HandoffTarget") || strings.Contains(rawJSON(t, bare.Rows[0]), "andoffTarget") {
		t.Fatalf("no assertion, no members: %s", rawJSON(t, bare.Rows[0]))
	}
}

// The defect class ADR-0025 closes for packs, closed for compositions: an
// edit reaching only escalation.target.name leaves every disposition byte
// identical, so the same suite that is green without the assertion goes red
// with it — and only the assertion tells the two apart.
func TestTargetOnlyPackEditIsCaughtExactlyWhenAsserted(t *testing.T) {
	files := fixtureFiles(t)
	files["vendor-onboarding-0.1.0.pack.json"] = strings.Replace(files["vendor-onboarding-0.1.0.pack.json"],
		`"name": "Compliance"`, `"name": "Compliance-2"`, 1)
	loaded := writeProject(t, files)

	blind, failure := Test(loaded, newEngine(t), fixtureDocument(t), "g", "r", targetRows("", ""), Options{Command: "test"})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if blind.Status != "passed" {
		t.Fatalf("without the assertion the edit is invisible — that is the defect: %+v", blind.Rows[0])
	}

	caught, failure := Test(loaded, newEngine(t), fixtureDocument(t), "g", "r", targetRows(canonicalCompliance, ""), Options{Command: "test"})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	row := caught.Rows[0]
	if caught.Status != "mismatch" || !strings.Contains(row.Detail, "reported handoff target") {
		t.Fatalf("the assertion catches the target-only edit: %+v", row)
	}
	if row.ExpectedHandoffTarget != canonicalCompliance || !strings.Contains(row.ActualHandoffTarget, "Compliance-2") {
		t.Fatalf("the pair names both destinations: %+v", row)
	}
}

// A named node's target is asserted beside its disposition, and a target-only
// edit of that node's pack is caught on the comparison, with the pair on the
// node and the detail naming it.
func TestNodeTargetAssertionCatchesTheNodesOwnPack(t *testing.T) {
	loaded := fixtureProject(t)
	output, failure := Test(loaded, newEngine(t), fixtureDocument(t), "g", "r", targetRows("", canonicalCompliance), Options{Command: "test"})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	comparison := output.Rows[0].Nodes[0]
	if output.Status != "passed" || comparison.ExpectedHandoffTarget != canonicalCompliance || comparison.ActualHandoffTarget != canonicalCompliance {
		t.Fatalf("a matching node assertion passes with the pair on the comparison: %s", rawJSON(t, comparison))
	}

	files := fixtureFiles(t)
	files["sanctions-screening-0.1.0.pack.json"] = strings.Replace(files["sanctions-screening-0.1.0.pack.json"],
		`"name": "Compliance"`, `"name": "Compliance-2"`, 1)
	perturbed := writeProject(t, files)
	caught, failure := Test(perturbed, newEngine(t), fixtureDocument(t), "g", "r", targetRows("", canonicalCompliance), Options{Command: "test"})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	row := caught.Rows[0]
	if caught.Status != "mismatch" || !strings.Contains(row.Detail, `Node "screening"`) || !strings.Contains(row.Detail, "reported handoff target") {
		t.Fatalf("the node assertion catches its own pack's edit: %+v", row)
	}
	if len(row.Nodes) != 1 || row.Nodes[0].Status != "mismatch" ||
		!strings.Contains(row.Nodes[0].ActualHandoffTarget, "Compliance-2") {
		t.Fatalf("the mismatching comparison names both destinations: %s", rawJSON(t, row.Nodes[0]))
	}
}

// The assertion holds the target the run reported, which is the configured
// target exactly when the disposition requested a handoff: a non-escalating
// row reports null, so null passes there and a named target mismatches — and
// on the escalating row the same two assertions swap verdicts.
func TestTargetAssertionReadsTheReportedTarget(t *testing.T) {
	loaded := fixtureProject(t)
	happyRow := func(target string) Rows {
		return Rows{Cases: []RowCase{{
			ID:                    "happy",
			Inputs:                json.RawMessage(happyInputs),
			ExpectedDisposition:   json.RawMessage(approveHeadline),
			ExpectedHandoffTarget: json.RawMessage(target),
		}}}
	}
	nullOnHappy, failure := Test(loaded, newEngine(t), fixtureDocument(t), "g", "r", happyRow("null"), Options{Command: "test"})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if nullOnHappy.Status != "passed" || nullOnHappy.Rows[0].ExpectedHandoffTarget != "null" || nullOnHappy.Rows[0].ActualHandoffTarget != "null" {
		t.Fatalf("a non-escalating run reports no target, and null asserts that: %+v", nullOnHappy.Rows[0])
	}
	namedOnHappy, failure := Test(loaded, newEngine(t), fixtureDocument(t), "g", "r", happyRow(canonicalCompliance), Options{Command: "test"})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if namedOnHappy.Status != "mismatch" || namedOnHappy.Rows[0].ActualHandoffTarget != "null" {
		t.Fatalf("a named target mismatches a run that reported none: %+v", namedOnHappy.Rows[0])
	}
	nullOnEscalating, failure := Test(loaded, newEngine(t), fixtureDocument(t), "g", "r", targetRows("null", ""), Options{Command: "test"})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if nullOnEscalating.Status != "mismatch" || nullOnEscalating.Rows[0].ActualHandoffTarget != canonicalCompliance {
		t.Fatalf("null mismatches a run that reported a target: %+v", nullOnEscalating.Rows[0])
	}
}

// A run refused where the row expected a composite reports the pack triple's
// honest third state: the expected rendering beside "unavailable", told from
// the other causes by the actual error class being set (ADR-0025's rule,
// carried over).
func TestRefusedRunReportsTheTargetUnavailable(t *testing.T) {
	loaded := fixtureProject(t)
	refusable := Rows{Cases: []RowCase{{
		ID:                    "refused",
		Inputs:                json.RawMessage(happyInputs),
		ExpectedDisposition:   json.RawMessage(approveHeadline),
		ExpectedHandoffTarget: json.RawMessage(canonicalCompliance),
	}}}
	output, failure := Test(loaded, newEngine(t), fixtureDocument(t), "g", "r", refusable, Options{Command: "test", injectionBudget: 1})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	row := output.Rows[0]
	if output.Status != "mismatch" || row.ActualErrorClass == "" {
		t.Fatalf("the refusal is the mismatch: %+v", row)
	}
	if row.ExpectedHandoffTarget != canonicalCompliance || row.ActualHandoffTarget != "unavailable" {
		t.Fatalf("the pair degrades honestly: %+v", row)
	}
}

// One row carrying every assertion at once: headline target, node target, and
// (ADR-0031) traces — the pair and the trace coexist on the same comparison,
// and a node asserting null passes when that node reported none.
func TestAllAssertionsCoexistOnOneRow(t *testing.T) {
	loaded := fixtureProject(t)
	clearNode := `{"kind":"outcome","outcomeId":"clear","reasons":[],"handoff":{"state":"none"}}`
	rows := Rows{Cases: []RowCase{{
		ID:                         "everything",
		Inputs:                     json.RawMessage(happyInputs),
		ExpectedDisposition:        json.RawMessage(approveHeadline),
		ExpectedHandoffTarget:      json.RawMessage("null"),
		ExpectedNodes:              map[string]json.RawMessage{"screening": json.RawMessage(clearNode)},
		ExpectedNodeHandoffTargets: map[string]json.RawMessage{"screening": json.RawMessage("null")},
	}}}
	output, failure := Test(loaded, newEngine(t), fixtureDocument(t), "g", "r", rows, Options{Command: "test", IncludeTraces: true})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	row := output.Rows[0]
	if output.Status != "passed" || row.ExpectedHandoffTarget != "null" || row.ActualHandoffTarget != "null" {
		t.Fatalf("the headline null assertion passes on a non-escalating run: %+v", row)
	}
	comparison := row.Nodes[0]
	if comparison.ExpectedHandoffTarget != "null" || comparison.ActualHandoffTarget != "null" || comparison.Trace == nil {
		t.Fatalf("the node pair and the trace coexist on one comparison: %s", rawJSON(t, comparison))
	}

	// A node disposition mismatch still reports the pair set before it: the
	// reader is never told less by a comparison that failed earlier.
	wrongDisposition := Rows{Cases: []RowCase{{
		ID:                         "wrong-node",
		Inputs:                     json.RawMessage(happyInputs),
		ExpectedDisposition:        json.RawMessage(approveHeadline),
		ExpectedNodes:              map[string]json.RawMessage{"screening": json.RawMessage(`{"kind":"outcome","outcomeId":"not-clear","reasons":[],"handoff":{"state":"none"}}`)},
		ExpectedNodeHandoffTargets: map[string]json.RawMessage{"screening": json.RawMessage("null")},
	}}}
	output, failure = Test(loaded, newEngine(t), fixtureDocument(t), "g", "r", wrongDisposition, Options{Command: "test"})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	comparison = output.Rows[0].Nodes[0]
	if comparison.Status != "mismatch" || comparison.ExpectedHandoffTarget != "null" || comparison.ActualHandoffTarget != "null" {
		t.Fatalf("a disposition mismatch keeps the pair beside it: %s", rawJSON(t, comparison))
	}
}

// A graph-layer refusal carries no §8.4 class, and the pair still degrades
// honestly: expected rendering beside "unavailable", the classless cause told
// by the detail naming the refusal rather than by an error class.
func TestClasslessRefusalReportsTheTargetUnavailable(t *testing.T) {
	loaded := fixtureProject(t)
	colliding := `{"screening":{"facts":{"screening":{"matches":"0"}},"evidence":{"screening-record":"present"}},"onboarding":{"facts":{"screening":{"status":"precleared"}}}}`
	rows := Rows{Cases: []RowCase{{
		ID:                    "collides",
		Inputs:                json.RawMessage(colliding),
		ExpectedDisposition:   json.RawMessage(approveHeadline),
		ExpectedHandoffTarget: json.RawMessage(canonicalCompliance),
	}}}
	output, failure := Test(loaded, newEngine(t), fixtureDocument(t), "g", "r", rows, Options{Command: "test"})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	row := output.Rows[0]
	if row.Status != "mismatch" || row.ActualErrorClass != "" || !strings.Contains(row.Detail, "refused") {
		t.Fatalf("a classless refusal is told by the detail: %+v", row)
	}
	if row.ExpectedHandoffTarget != canonicalCompliance || row.ActualHandoffTarget != "unavailable" {
		t.Fatalf("the pair degrades honestly without a class: %+v", row)
	}
}

// A row-defect mismatch reports the defect and no pair: the members appear
// exactly when a well-formed assertion rode a run this walk performed.
func TestRowDefectMismatchCarriesNoPair(t *testing.T) {
	loaded := fixtureProject(t)
	malformedDisposition := Rows{Cases: []RowCase{{
		ID:                    "bad-disposition",
		Inputs:                json.RawMessage(happyInputs),
		ExpectedDisposition:   json.RawMessage(`{"kind":"outcome","outcomeId":"approve","reasons":["unknown"],"handoff":{"state":"none"}}`),
		ExpectedHandoffTarget: json.RawMessage(canonicalCompliance),
	}}}
	output, failure := Test(loaded, newEngine(t), fixtureDocument(t), "g", "r", malformedDisposition, Options{Command: "test"})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	row := output.Rows[0]
	if row.Status != "mismatch" || !strings.Contains(row.Detail, "defect in the row") ||
		strings.Contains(rawJSON(t, row), "andoffTarget") {
		t.Fatalf("a row defect reports the defect, not a pair: %s", rawJSON(t, row))
	}

	ghost := Rows{Cases: []RowCase{{
		ID:                         "ghost-node-target",
		Inputs:                     json.RawMessage(happyInputs),
		ExpectedDisposition:        json.RawMessage(approveHeadline),
		ExpectedNodes:              map[string]json.RawMessage{"aaa-ghost": json.RawMessage(approveHeadline)},
		ExpectedNodeHandoffTargets: map[string]json.RawMessage{"aaa-ghost": json.RawMessage("null")},
	}}}
	output, failure = Test(loaded, newEngine(t), fixtureDocument(t), "g", "r", ghost, Options{Command: "test"})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	row = output.Rows[0]
	if row.Status != "mismatch" || !strings.Contains(row.Detail, `"aaa-ghost"`) ||
		strings.Contains(rawJSON(t, row.Nodes[0]), "andoffTarget") {
		t.Fatalf("an undeclared node's comparison carries no pair: %s", rawJSON(t, row))
	}
}

// Renderings are charged like every retained member: a budget the bare run
// fits exactly is a budget the asserting run overruns — ADR-0031's boundary
// argument, applied to this member.
func TestTargetRenderingsAreChargedByTheReportBudget(t *testing.T) {
	loaded := fixtureProject(t)
	engine := newEngine(t)
	document := fixtureDocument(t)
	bare, failure := Test(loaded, engine, document, "g", "r", targetRows("", ""), Options{Command: "test"})
	if failure != nil {
		t.Fatal(failure.Message)
	}
	exact := len(rawJSON(t, bare))
	if _, failure = Test(loaded, engine, document, "g", "r", targetRows("", ""), Options{Command: "test", ReportBudget: exact}); failure != nil {
		t.Fatalf("the bare run fits its own bytes exactly: %s", failure.Message)
	}
	_, failure = Test(loaded, engine, document, "g", "r", targetRows(canonicalCompliance, canonicalCompliance), Options{Command: "test", ReportBudget: exact})
	if failure == nil || failure.Code != CodeReportBudget {
		t.Fatalf("the asserting run overruns the bare boundary, by the budget refusal: %+v", failure)
	}
}

// The version gate's exact contract: code, exit class, and sentence, pinned
// per input, because round 1 found the gate's strictness changing silently.
func TestGraphMatrixVersionGateContract(t *testing.T) {
	for name, tt := range map[string]struct {
		document string
		code     string
		exit     int
		message  string
	}{
		"wrong type": {`{"graphMatrixVersion":2,"cases":[{"id":"r","inputs":{},"expectedErrorClass":"c"}]}`,
			"JPS-GRAPH-ROWS-SHAPE", result.ExitInvalid,
			"The graphMatrixVersion of the graph matrix rows.json must be a string; this runtime accepts: 1, 2."},
		"null": {`{"graphMatrixVersion":null,"cases":[{"id":"r","inputs":{},"expectedErrorClass":"c"}]}`,
			"JPS-GRAPH-ROWS-SHAPE", result.ExitInvalid,
			"The graphMatrixVersion of the graph matrix rows.json must be a string; this runtime accepts: 1, 2."},
		"unsupported": {`{"graphMatrixVersion":"3","cases":[{"id":"r","inputs":{},"expectedErrorClass":"c"}]}`,
			"JPS-GRAPH-ROWS-VERSION", result.ExitUnsupported,
			`The graph matrix rows.json declares graphMatrixVersion "3", which this runtime does not support; it accepts: 1, 2.`},
		"empty string": {`{"graphMatrixVersion":"","cases":[{"id":"r","inputs":{},"expectedErrorClass":"c"}]}`,
			"JPS-GRAPH-ROWS-VERSION", result.ExitUnsupported,
			`The graph matrix rows.json declares graphMatrixVersion "", which this runtime does not support; it accepts: 1, 2.`},
	} {
		t.Run(name, func(t *testing.T) {
			_, failure := LoadRows([]byte(tt.document), "rows.json")
			if failure == nil || failure.Code != tt.code || failure.ExitCode != tt.exit || failure.Message != tt.message {
				t.Fatalf("got %+v", failure)
			}
		})
	}
}

// Every member the introduction map version-gates names a version the
// supported list actually ranks — a version missing from the list would rank
// -1 and, unguarded, silently admit the members it introduced (round-1
// review; the rank check also fails closed on its own).
func TestVersionIntroductionMapNamesOnlySupportedVersions(t *testing.T) {
	for member, since := range graphRowsVersionMembers {
		found := false
		for _, version := range SupportedGraphMatrixVersions() {
			if version == since {
				found = true
			}
		}
		if !found {
			t.Fatalf("member %q is introduced by unranked version %q", member, since)
		}
	}
}

// The loader admits the version-2 members through LoadRows itself, so the
// direct-construction rows above are not asserting a shape the wire refuses.
func TestLoadRowsAdmitsVersionTwoTargetAssertions(t *testing.T) {
	document := `{"graphMatrixVersion":"2","cases":[{"id":"r","inputs":` + happyInputs +
		`,"expectedDisposition":` + unresolvedDisposition +
		`,"expectedNodes":{"screening":` + unresolvedDisposition + `}` +
		`,"expectedHandoffTarget":` + canonicalCompliance +
		`,"expectedNodeHandoffTargets":{"screening":null}}]}`
	rows, failure := LoadRows([]byte(document), "rows.json")
	if failure != nil {
		t.Fatalf("version-2 assertions load: %+v", failure)
	}
	if rows.Cases[0].ExpectedHandoffTarget == nil || len(rows.Cases[0].ExpectedNodeHandoffTargets) != 1 {
		t.Fatalf("the members decode: %+v", rows.Cases[0])
	}
}
