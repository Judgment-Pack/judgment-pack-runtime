package graph

import (
	"encoding/json"
	"strings"
	"testing"
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

// A graph row asserts the composite's configured handoff target — the result
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
	if caught.Status != "mismatch" || !strings.Contains(row.Detail, "configured handoff target") {
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
	if caught.Status != "mismatch" || !strings.Contains(row.Detail, `Node "screening"`) || !strings.Contains(row.Detail, "configured handoff target") {
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
