package project

import (
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// lintCheckNamed finds one named check on one pack's lint entry.
func lintCheckNamed(t *testing.T, report result.PackProducersLint, id, name string) result.PackCheck {
	t.Helper()
	for _, pack := range report.Packs {
		if pack.ID != id {
			continue
		}
		for _, check := range pack.Checks {
			if check.Name == name {
				return check
			}
		}
		t.Fatalf("pack %q has no %q check: %+v", id, name, pack.Checks)
	}
	t.Fatalf("no pack %q in %+v", id, report.Packs)
	return result.PackCheck{}
}

// The hints mode: the configuration's own hints are the producer
// declaration, and the lint is the inverse of validate's hint-key check — a
// consulted pointer with no hint fails here, where a hint with no consulted
// pointer fails there.
func TestLintHoldsEveryConsultedPointerToAProducer(t *testing.T) {
	pack := string(packFixture(t))
	// The fixture consults /request/appropriateness, /request/completeness,
	// /request/embargoedInformationToUnauthorizedRecipients, /request/type,
	// and declares intake-form, sponsor-endorsement, sensitive-data-approvals.
	configPath := writeProject(t, `{"configVersion":"1","packs":{
	  "complete":{"path":"packs/a.json",
	    "facts":{"/request":{"source":"the intake system"}},
	    "evidence":{"intake-form":{"source":"SharePoint"},"sponsor-endorsement":{"source":"mail"},"sensitive-data-approvals":{"source":"OneTrust"}}},
	  "starved":{"path":"packs/a.json",
	    "facts":{"/request/type":{"source":"the intake system"}},
	    "evidence":{"intake-form":{"source":"SharePoint"}}},
	  "unhinted":{"path":"packs/a.json"}
	}}`, map[string]string{"packs/a.json": pack})
	report, failure := mustLoad(t, configPath).Lint(nil, "", "packs lint")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if report.ProducersSource != "hints" {
		t.Fatalf("without a manifest the hints are the declaration: %+v", report)
	}

	// An ancestor hint produces every pointer beneath it.
	if got := lintCheckNamed(t, report, "complete", CheckFactProducers); got.Status != result.PackCheckPassed {
		t.Fatalf("a producer at /request supplies every consulted /request/... pointer: %+v", got)
	}
	if got := lintCheckNamed(t, report, "complete", CheckEvidenceProducers); got.Status != result.PackCheckPassed {
		t.Fatalf("declared and supplied evidence agree: %+v", got)
	}

	// A consulted pointer no producer supplies fails, and the detail names it.
	starved := lintCheckNamed(t, report, "starved", CheckFactProducers)
	if starved.Status != result.PackCheckFailed {
		t.Fatalf("an unproduced consulted pointer must fail: %+v", starved)
	}
	for _, required := range []string{`"/request/appropriateness"`, `"/request/completeness"`} {
		if !strings.Contains(starved.Detail, required) {
			t.Fatalf("the detail must name %s: %q", required, starved.Detail)
		}
	}
	// Both directions of the evidence check.
	starvedEvidence := lintCheckNamed(t, report, "starved", CheckEvidenceProducers)
	if starvedEvidence.Status != result.PackCheckFailed || !strings.Contains(starvedEvidence.Detail, `"sponsor-endorsement"`) {
		t.Fatalf("a declared requirement with no supplier must fail: %+v", starvedEvidence)
	}

	// No hints at all: everything consulted is unproduced.
	if got := lintCheckNamed(t, report, "unhinted", CheckFactProducers); got.Status != result.PackCheckFailed {
		t.Fatalf("no producers means every consulted pointer is unproduced: %+v", got)
	}

	if report.Status != "failed" || report.Summary.Failed != 2 || report.Summary.Passed != 1 {
		t.Fatalf("summary = %+v status = %q", report.Summary, report.Status)
	}
}

// The manifest mode: an explicit producer set replaces the hints, for an
// application whose producers are wider than its hints.
func TestLintAcceptsAnExplicitProducerManifest(t *testing.T) {
	pack := string(packFixture(t))
	configPath := writeProject(t, `{"configVersion":"1","packs":{
	  "intake":{"path":"packs/a.json","facts":{"/request/type":{"source":"x"}}}
	}}`, map[string]string{"packs/a.json": pack})
	manifest, err := DecodeProducers([]byte(`{"producersVersion":"1",
	  "facts":["/request"],
	  "evidence":["intake-form","sponsor-endorsement","sensitive-data-approvals"]}`))
	if err != nil {
		t.Fatal(err)
	}
	report, failure := mustLoad(t, configPath).Lint(manifest, "", "packs lint")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if report.ProducersSource != "manifest" {
		t.Fatalf("the manifest is the declaration when supplied: %+v", report)
	}
	if got := lintCheckNamed(t, report, "intake", CheckFactProducers); got.Status != result.PackCheckPassed {
		t.Fatalf("the manifest's producers supply the consulted set: %+v", got)
	}
	if report.Status != "passed" {
		t.Fatalf("report = %+v", report)
	}

	// The reverse evidence direction is project-wide in manifest mode: an id
	// nothing declares fails once, as a configuration-level check, not once
	// per pack that happens not to declare it.
	orphaned, err := DecodeProducers([]byte(`{"producersVersion":"1",
	  "facts":["/request"],
	  "evidence":["intake-form","sponsor-endorsement","sensitive-data-approvals","ghost-evidence"]}`))
	if err != nil {
		t.Fatal(err)
	}
	report, failure = mustLoad(t, configPath).Lint(orphaned, "", "packs lint")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if report.Status != "failed" || len(report.Checks) != 1 || report.Checks[0].Status != result.PackCheckFailed {
		t.Fatalf("an orphaned supplied id fails at the configuration level: %+v", report)
	}
	if got := lintCheckNamed(t, report, "intake", CheckEvidenceProducers); got.Status != result.PackCheckPassed {
		t.Fatalf("the per-pack evidence check must not double-report the orphan: %+v", got)
	}
}

// The manifest is a closed input: unknown members, wrong versions, and
// trailing content are refused rather than silently ignored.
func TestDecodeProducersIsStrict(t *testing.T) {
	for name, text := range map[string]string{
		"unknown member":    `{"producersVersion":"1","facts":[],"evidence":[],"extra":true}`,
		"wrong version":     `{"producersVersion":"2","facts":[],"evidence":[]}`,
		"missing version":   `{"facts":[],"evidence":[]}`,
		"trailing":          `{"producersVersion":"1","facts":[],"evidence":[]}{"again":true}`,
		"not an object":     `[]`,
		"duplicate members": `{"producersVersion":"1","producersVersion":"1","facts":[],"evidence":[]}`,
		"cased alias":       `{"ProducersVersion":"1","facts":[],"evidence":[]}`,
		"non-pointer fact":  `{"producersVersion":"1","facts":["not-a-pointer"],"evidence":[]}`,
		"non-local-id":      `{"producersVersion":"1","facts":[],"evidence":["UPPER"]}`,
		"non-string entry":  `{"producersVersion":"1","facts":[7],"evidence":[]}`,
	} {
		if _, err := DecodeProducers([]byte(text)); err == nil {
			t.Fatalf("%s must be refused", name)
		}
	}
	manifest, err := DecodeProducers([]byte(`{"producersVersion":"1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Facts == nil || manifest.Evidence == nil {
		t.Fatalf("absent lists decode as empty, never nil: %+v", manifest)
	}
	deduped, err := DecodeProducers([]byte(`{"producersVersion":"1","evidence":["ghost","ghost"],"facts":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(deduped.Evidence) != 1 {
		t.Fatalf("duplicate supplied ids collapse to one: %+v", deduped.Evidence)
	}
}

// A pack using draft-RFC collection quantifiers reports its fact half as
// skipped, never checked and never silently passed: element-relative pointers
// cannot be held to a flat producer set (ADR-0020's recorded narrowness).
func TestLintSkipsTheFactHalfOfAQuantifierPack(t *testing.T) {
	document := `{
	  "id": "https://example.invalid/judgment-packs/quantified", "version": "0.1.0",
	  "rules": [{"id": "r1", "when": {"op": "exists", "path": "/items", "where": {"op": "fact", "path": "/available"}}}]
	}`
	configPath := writeProject(t, `{"configVersion":"1","packs":{"quantified":{"path":"packs/q.json"}}}`,
		map[string]string{"packs/q.json": document})
	report, failure := mustLoad(t, configPath).Lint(nil, "", "packs lint")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	scope := lintCheckNamed(t, report, "quantified", CheckQuantifierScope)
	if scope.Status != result.PackCheckSkipped || !strings.Contains(scope.Detail, "quantifier") {
		t.Fatalf("the quantifier scope must be a named skip: %+v", scope)
	}
	for _, pack := range report.Packs {
		for _, check := range pack.Checks {
			if check.Name == CheckFactProducers {
				t.Fatalf("the fact half must not be checked on a quantifier pack: %+v", check)
			}
		}
	}
}

// An unreadable document FAILS, on packs test's own discipline: skipping it
// would let a broken pack lint clean behind a passing sibling. validate is
// still where the read failure itself is diagnosed.
func TestLintFailsWhatItCannotRead(t *testing.T) {
	configPath := writeProject(t, `{"configVersion":"1","packs":{"broken":{"path":"packs/broken.json"}}}`,
		map[string]string{"packs/broken.json": `{ not json`})
	report, failure := mustLoad(t, configPath).Lint(nil, "", "packs lint")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if got := lintCheckNamed(t, report, "broken", CheckFactProducers); got.Status != result.PackCheckFailed {
		t.Fatalf("an unreadable declared document must fail the lint: %+v", got)
	}
	if report.Status != "failed" || report.Summary.Failed != 1 {
		t.Fatalf("report = %q %+v", report.Status, report.Summary)
	}
}

// A pack whose fact half is quantifier-skipped counts as skipped in the
// summary, visibly — never folded into passed or failed.
func TestLintSummaryCountsSkippedPacks(t *testing.T) {
	document := `{
	  "id": "https://example.invalid/judgment-packs/quantified", "version": "0.1.0",
	  "rules": [{"id": "r1", "when": {"op": "exists", "path": "/items", "where": {"op": "fact", "path": "/available"}}}]
	}`
	configPath := writeProject(t, `{"configVersion":"1","packs":{"quantified":{"path":"packs/q.json"}}}`,
		map[string]string{"packs/q.json": document})
	report, failure := mustLoad(t, configPath).Lint(nil, "", "packs lint")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if report.Summary.Skipped != 1 || report.Summary.Total != 1 {
		t.Fatalf("summary = %+v", report.Summary)
	}
}

// A data literal that merely carries `where` must not suppress the fact gate:
// the quantifier detection is keyed to the known quantifier operators, so the
// impersonation falls into the flat check and its unproduced pointer fails
// visibly there.
func TestADataLiteralCannotImpersonateAQuantifier(t *testing.T) {
	document := `{
	  "id": "https://example.invalid/judgment-packs/impersonator", "version": "0.1.0",
	  "rules": [{"id": "r1", "when": {"op": "fact", "path": "/real", "operator": "equals",
	    "value": {"op": "payload", "where": null}}}]
	}`
	configPath := writeProject(t, `{"configVersion":"1","packs":{"imp":{"path":"packs/i.json"}}}`,
		map[string]string{"packs/i.json": document})
	report, failure := mustLoad(t, configPath).Lint(nil, "", "packs lint")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	if got := lintCheckNamed(t, report, "imp", CheckFactProducers); got.Status != result.PackCheckFailed {
		t.Fatalf("the impersonator must not suppress the fact gate: %+v", got)
	}
}

// An unknown decision id is refused exactly as every packs command refuses it.
func TestLintRefusesAnUnknownDecisionId(t *testing.T) {
	configPath := writeProject(t, `{"configVersion":"1","packs":{"intake":{"path":"packs/a.json"}}}`,
		map[string]string{"packs/a.json": string(packFixture(t))})
	if _, failure := mustLoad(t, configPath).Lint(nil, "no-such-pack", "packs lint"); failure == nil {
		t.Fatal("an unknown id must be refused, not zero results")
	}
}

func TestProducesPointerDirections(t *testing.T) {
	producers := []string{"/request", "/dataset/classification"}
	if !producesPointer(producers, "/request/type") {
		t.Fatal("an ancestor producer declares its subtree")
	}
	if !producesPointer(producers, "/dataset/classification") {
		t.Fatal("an exact producer supplies its pointer")
	}
	if producesPointer(producers, "/requestX") {
		t.Fatal("/request must not be read as a prefix of /requestX")
	}
	// Structurally true: writing /dataset/classification creates /dataset,
	// so a condition reading the ancestor resolves.
	if !producesPointer(producers, "/dataset") {
		t.Fatal("a descendant producer makes its ancestor resolvable")
	}
	if producesPointer(producers, "/vendor") {
		t.Fatal("an unrelated pointer is not covered")
	}
	// The root pointer: covered by any producer, and covering everything.
	if !producesPointer(producers, "") {
		t.Fatal("any producer makes the root resolvable")
	}
	if !producesPointer([]string{""}, "/anything/at/all") {
		t.Fatal("a root producer declares the whole document")
	}
	if producesPointer([]string{}, "") {
		t.Fatal("no producers means even the root is unproduced")
	}
}
