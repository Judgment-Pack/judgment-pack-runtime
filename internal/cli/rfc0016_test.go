package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

const (
	refundPack    = "refund-pass-through.json"
	tiersPack     = "credit-limit-tiers.json"
	refundOutcome = `{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"approve-refund","reasons":[],"value":{"currency":"CAD","refundAmount":"149.50"}}`
)

func rfc0016Fixture(name string) string {
	return filepath.Join("..", "evaluation", "testdata", "rfc0016", name)
}

// dispositionOf cuts the disposition member out of one JSON payload as the
// bytes the payload carries, which is what §8.3's byte agreement is about. A
// decoded and re-encoded member would compare what this test wrote.
func dispositionOf(t *testing.T, payload string) string {
	t.Helper()
	var output struct {
		Disposition json.RawMessage `json:"disposition"`
	}
	if err := json.Unmarshal([]byte(payload), &output); err != nil {
		t.Fatalf("output is not JSON: %v (%q)", err, payload)
	}
	return string(output.Disposition)
}

func TestExperimentalEvaluateRFC0016OutcomeValuesFlag(t *testing.T) {
	pack := rfc0016Fixture(refundPack)
	facts := writeDocument(t, "facts.json", `{"customer":{"goodStanding":true},"proposed":{"refundAmount":"149.50"}}`)

	// spec validate is untouched: the name is reserved and the pack is refused.
	for _, fixture := range []string{refundPack, tiersPack} {
		code, stdout, stderr := runTest(t, []string{"spec", "validate", rfc0016Fixture(fixture), "--format", "json"}, "")
		if code != result.ExitInvalid || stderr != "" {
			t.Fatalf("spec validate must still reject %s: exit=%d stderr=%q", fixture, code, stderr)
		}
		assertDiagnosticCode(t, stdout, "JPS-STRUCTURE-EXTENSION-NAME")
	}

	// Without the flag the evaluator refuses it exactly as it does today, and
	// under the other draft's flag too.
	for _, flags := range [][]string{nil, {"--rfc0008-quantifiers"}, {"--supported-extension", "org.judgmentpack.outcome-values"}} {
		code, stdout, _ := runTest(t, append([]string{"experimental", "evaluate", pack, "--facts", facts, "--format", "json"}, flags...), "")
		if code != result.ExitInvalid {
			t.Fatalf("with %v the pack must be refused, got exit=%d", flags, code)
		}
		assertDiagnosticCode(t, stdout, "JPS-EVALUATION-PACK-NOT-CONFORMANT")
	}

	// With the flag it evaluates, the disposition carries the values in its
	// canonical form, and the marker travels with the result.
	code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", pack, "--facts", facts, "--rfc0016-outcome-values", "--format", "json"}, "")
	if code != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	if got := dispositionOf(t, stdout); got != refundOutcome {
		t.Fatalf("disposition = %s, want %s", got, refundOutcome)
	}
	var output struct {
		SpecVersion    string `json:"specVersion"`
		Experimental   bool   `json:"experimental"`
		DraftPrototype *struct {
			RFC                       string   `json:"rfc"`
			Status                    string   `json:"status"`
			Operators                 []string `json:"operators"`
			Outcomes                  []string `json:"outcomes"`
			PackValidUnderSpecVersion bool     `json:"packValidUnderSpecVersion"`
			Note                      string   `json:"note"`
		} `json:"draftPrototype"`
	}
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatal(err)
	}
	marker := output.DraftPrototype
	if marker == nil || marker.RFC != "0016" || marker.Status != "draft-rfc-prototype" || marker.PackValidUnderSpecVersion || !strings.Contains(marker.Note, "NOT valid") {
		t.Fatalf("marker = %+v", marker)
	}
	if len(marker.Outcomes) != 1 || marker.Outcomes[0] != "approve-refund" || marker.Operators == nil || len(marker.Operators) != 0 {
		t.Fatalf("marker = %+v", marker)
	}
	if !output.Experimental {
		t.Fatal("the payload is the experimental surface's")
	}

	// The human surface carries the same warning above the disposition, and
	// the values below it, one to a line in the order of their names.
	code, stdout, stderr = runTest(t, []string{"experimental", "evaluate", pack, "--facts", facts, "--rfc0016-outcome-values"}, "")
	if code != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	want := "DRAFT-RFC PROTOTYPE: RFC 0016 outcome values declared by approve-refund; this pack is NOT valid under JPS " + artifacts.EvaluatorDraftVersion + " and spec validate rejects it\n" +
		"disposition: outcome approve-refund\n" +
		"value: currency = \"CAD\"\n" +
		"value: refundAmount = \"149.50\"\n" +
		"trace: exception goodwill-override: unknown onUnknown=ignore [fact /override/goodwill absent]\n" +
		"trace: rule small-refund-in-good-standing: true outcome=approve-refund\n" +
		"trace: outcome-value currency: resolved outcome=approve-refund\n" +
		"trace: outcome-value refundAmount: resolved outcome=approve-refund\n"
	if !strings.Contains(stdout, want) {
		t.Fatalf("human output = %q, want it to hold %q", stdout, want)
	}
}

// A declared value that does not resolve withholds the outcome. The run is a
// completed evaluation and exits 0: unresolved is a disposition.
func TestExperimentalEvaluateRFC0016WithholdsAnOutcome(t *testing.T) {
	pack := rfc0016Fixture(refundPack)
	// The goodwill override forces the outcome without any rule reading the
	// amount, which is the case the RFC's Problem section names.
	facts := writeDocument(t, "facts.json", `{"customer":{"goodStanding":true},"override":{"goodwill":true}}`)
	code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", pack, "--facts", facts, "--rfc0016-outcome-values", "--format", "json"}, "")
	if code != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	if got, want := dispositionOf(t, stdout), `{"handoff":{"state":"requested","triggeredBy":["unknown"]},"kind":"unresolved","reasons":["unknown"]}`; got != want {
		t.Fatalf("disposition = %s, want %s", got, want)
	}
	if !strings.Contains(stdout, `"handoffTarget":{"kind":"queue","name":"refund-review"}`) {
		t.Fatalf("the target is reported beside the disposition: %s", stdout)
	}
	code, stdout, stderr = runTest(t, []string{"experimental", "evaluate", pack, "--facts", facts, "--rfc0016-outcome-values"}, "")
	if code != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	for _, line := range []string{
		"disposition: unresolved (unknown)\n",
		"handoff: requested -> queue \"refund-review\" (triggered by unknown)\n",
		"trace: outcome-value currency: resolved outcome=approve-refund\n",
		"trace: outcome-value refundAmount: unresolved outcome=approve-refund\n",
	} {
		if !strings.Contains(stdout, line) {
			t.Errorf("human output must hold %q: %q", line, stdout)
		}
	}
	if strings.Contains(stdout, "value:") {
		t.Fatalf("no value is reported without an outcome: %q", stdout)
	}
}

// Constants on authored tiers, and an outcome that declares none.
func TestExperimentalEvaluateRFC0016Tiers(t *testing.T) {
	pack := rfc0016Fixture(tiersPack)
	for score, want := range map[string]string{
		"720": `{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"limit-high","reasons":[],"value":{"creditLimit":"10000"}}`,
		"719": `{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"limit-standard","reasons":[],"value":{"creditLimit":"5000"}}`,
		"649": `{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"decline","reasons":[]}`,
	} {
		facts := writeDocument(t, "facts.json", `{"applicant":{"score":"`+score+`"}}`)
		code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", pack, "--facts", facts, "--rfc0016-outcome-values", "--format", "json"}, "")
		if code != result.ExitSuccess || stderr != "" {
			t.Fatalf("score %s: exit=%d stderr=%q stdout=%q", score, code, stderr, stdout)
		}
		if got := dispositionOf(t, stdout); got != want {
			t.Errorf("score %s: disposition = %s, want %s", score, got, want)
		}
	}
}

// The flag on a pack that declares no value changes nothing about it, and both
// markers say so.
func TestExperimentalEvaluateRFC0016OnAPlainPack(t *testing.T) {
	pack := filepath.Join("..", "evaluation", "testdata", "rfc0008", "airline-cancellation-prepared.json")
	facts := writeDocument(t, "facts.json", `{"reservation":{"anySegmentCancelledByAirline":true}}`)
	code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", pack, "--facts", facts, "--rfc0016-outcome-values"}, "")
	if code != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "DRAFT-RFC PROTOTYPE: RFC 0016 outcome values enabled; no outcome of this pack declares a value and it remains a plain JPS "+artifacts.EvaluatorDraftVersion+" pack\n") || strings.Contains(stdout, "NOT valid") {
		t.Fatalf("a pack that declares no value must not be called invalid: %q", stdout)
	}
	code, under, stderr := runTest(t, []string{"experimental", "evaluate", pack, "--facts", facts, "--rfc0016-outcome-values", "--format", "json"}, "")
	if code != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	code, without, stderr := runTest(t, []string{"experimental", "evaluate", pack, "--facts", facts, "--format", "json"}, "")
	if code != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	start := strings.Index(under, `"draftPrototype":`)
	end := strings.Index(under, `"disposition":`)
	if start < 0 || end < start {
		t.Fatalf("the payload must carry the marker before the disposition: %s", under)
	}
	if !strings.Contains(under[start:end], `"rfc":"0016"`) || !strings.Contains(under[start:end], `"operators":[],"packValidUnderSpecVersion":true`) {
		t.Fatalf("marker = %s", under[start:end])
	}
	if under[:start]+under[end:] != without {
		t.Fatalf("the flag must add the marker and nothing else:\nunder   %s\nwithout %s", under, without)
	}
}

// One evaluation runs under one draft. The pair is an invocation error, and it
// is refused before the filesystem: the pack named here does not exist.
func TestExperimentalEvaluateRFC0016AndRFC0008AreMutuallyExclusive(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-pack.json")
	code, stdout, _ := runTest(t, []string{"experimental", "evaluate", missing, "--facts", missing, "--rfc0016-outcome-values", "--rfc0008-quantifiers", "--format", "json"}, "")
	if code != result.ExitInvocation {
		t.Fatalf("exit=%d stdout=%q", code, stdout)
	}
	assertDiagnosticCode(t, stdout, "JPS-INVOCATION-DRAFT-RFC")
	if strings.Contains(stdout, `"evaluationError"`) || strings.Contains(stdout, `"disposition"`) {
		t.Fatalf("an invocation error has no §8.4 class and no disposition: %s", stdout)
	}
	// Each flag alone reads the pack, and reports that it is not there.
	for _, flag := range []string{"--rfc0016-outcome-values", "--rfc0008-quantifiers"} {
		code, stdout, _ = runTest(t, []string{"experimental", "evaluate", missing, "--facts", missing, flag, "--format", "json"}, "")
		if code == result.ExitInvocation && strings.Contains(stdout, "JPS-INVOCATION-DRAFT-RFC") {
			t.Fatalf("%s alone is no pair: %s", flag, stdout)
		}
	}
}

// A refusal under the flag is the ordinary evaluation-error envelope: it names
// the §8.4 class and the draft's code, and carries no marker and no disposition.
func TestExperimentalEvaluateRFC0016RefusalCarriesNoMarker(t *testing.T) {
	data, err := os.ReadFile(rfc0016Fixture(refundPack))
	if err != nil {
		t.Fatal(err)
	}
	malformed := strings.Replace(string(data), `"type": "decimal", "fromFact"`, `"type": "money", "fromFact"`, 1)
	if malformed == string(data) {
		t.Fatal("the fixture no longer holds the text this test replaces")
	}
	pack := writeDocument(t, "pack.json", malformed)
	facts := writeDocument(t, "facts.json", `{"customer":{"goodStanding":true},"proposed":{"refundAmount":"149.50"}}`)
	code, stdout, _ := runTest(t, []string{"experimental", "evaluate", pack, "--facts", facts, "--rfc0016-outcome-values", "--format", "json"}, "")
	if code != result.ExitInvalid {
		t.Fatalf("exit=%d stdout=%q", code, stdout)
	}
	assertDiagnosticCode(t, stdout, "JPS-EVALUATION-RFC0016-GRAMMAR")
	if !strings.Contains(stdout, `"evaluationError":{"class":"pack-not-conformant","phase":"preflight"`) {
		t.Fatalf("the refusal must name its class: %s", stdout)
	}
	if strings.Contains(stdout, `"draftPrototype"`) || strings.Contains(stdout, `"disposition"`) {
		t.Fatalf("a refusal carries no marker and no disposition: %s", stdout)
	}
}

// A value drawn from a fact is text somebody else wrote. The human surface
// writes it with its controls taken out. The JSON surface writes what RFC 8785
// writes: a control below U+0020 as its escape, and a direction control as
// itself. That is the canonical form and is not changed here, so whatever
// shows the JSON to a person has to treat the value as it treats any string it
// did not write.
func TestExperimentalEvaluateRFC0016ValuesAreWrittenSafely(t *testing.T) {
	data, err := os.ReadFile(rfc0016Fixture(refundPack))
	if err != nil {
		t.Fatal(err)
	}
	asText := strings.Replace(string(data), `"type": "decimal", "fromFact"`, `"type": "string", "fromFact"`, 1)
	asText = strings.Replace(asText, `{ "op": "fact", "path": "/proposed/refundAmount", "operator": "less-than-or-equal", "value": "200" }`, `{ "op": "literal", "value": true }`, 1)
	pack := writeDocument(t, "pack.json", asText)
	facts := writeDocument(t, "facts.json", `{"customer":{"goodStanding":true},"proposed":{"refundAmount":"1\u001b[31m\u202e\n2"}}`)
	code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", pack, "--facts", facts, "--rfc0016-outcome-values"}, "")
	if code != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "value: refundAmount = \"1?[31m??2\"\n") {
		t.Fatalf("human output = %q", stdout)
	}
	if strings.ContainsAny(stdout, "\x1b\u202e") {
		t.Fatalf("the human surface must write no control: %q", stdout)
	}
	code, stdout, stderr = runTest(t, []string{"experimental", "evaluate", pack, "--facts", facts, "--rfc0016-outcome-values", "--format", "json"}, "")
	if code != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	want := `{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"approve-refund","reasons":[],"value":{"currency":"CAD","refundAmount":"1\u001b[31m` + "\u202e" + `\n2"}}`
	if got := dispositionOf(t, stdout); got != want {
		t.Fatalf("disposition = %q, want %q", got, want)
	}
}

// The record of an evaluation made under the flag carries the disposition as it
// was produced, value included, and the marker. It already carries the facts
// whole, so the value adds nothing to what the trail holds. A rehearsal under
// the flag writes nothing, as any rehearsal does.
func TestExperimentalEvaluateRFC0016IsRecordedWithItsValues(t *testing.T) {
	configPath := auditProject(t)
	pack := rfc0016Fixture(refundPack)
	facts := writeDocument(t, "facts.json", `{"customer":{"goodStanding":true},"proposed":{"refundAmount":"149.50"}}`)
	args := []string{"experimental", "evaluate", pack, "--config", configPath, "--facts", facts, "--rfc0016-outcome-values", "--format", "json"}

	code, rehearsed, stderr := runTest(t, append(append([]string{}, args...), "--rehearsal"), "")
	if code != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	noAuditTrail(t, configPath)

	code, recorded, stderr := runTest(t, args, "")
	if code != result.ExitSuccess || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if strings.Replace(rehearsed, `"rehearsal":true,`, "", 1) != recorded {
		t.Fatalf("a rehearsal changes only the label:\nrecorded  %q\nrehearsed %q", recorded, rehearsed)
	}
	line, err := os.ReadFile(filepath.Join(filepath.Dir(configPath), "audit", audit.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(line), "\n") != 1 {
		t.Fatalf("one evaluation leaves one record: %q", line)
	}
	if !strings.Contains(string(line), `"disposition":`+refundOutcome+`}`) {
		t.Fatalf("the record must carry the disposition as it was produced: %s", line)
	}
	if !strings.Contains(string(line), `"draftPrototype":{"rfc":"0016","status":"draft-rfc-prototype","operators":[],"outcomes":["approve-refund"],"packValidUnderSpecVersion":false,`) {
		t.Fatalf("the record must carry the marker: %s", line)
	}
	if !strings.Contains(string(line), `"refundAmount":"149.50"`) {
		t.Fatalf("the record carries the facts: %s", line)
	}
}

// A matrix row is held to §8.3 as it is published. A row that expects the
// draft's member is reported as a mismatch, under the sentence that says why,
// and is not compared with anything. The rows beside it run as they did.
func TestPacksTestRefusesARowThatExpectsAValue(t *testing.T) {
	expected := strings.TrimSuffix(declineRedirect, "}") + `,"value":{"currency":"CAD"}}`
	matrix := `{"matrixVersion":"1","cases":[
	  {"id":"expects-a-value","facts":` + hardFailFacts + `,"evidenceAvailability":` + presentEvidence + `,"expectedDisposition":` + expected + `},
	  {"id":"expects-none","facts":` + hardFailFacts + `,"evidenceAvailability":` + presentEvidence + `,"expectedDisposition":` + declineRedirect + `}
	]}`
	configPath := writeProjectFixture(t, `{"configVersion":"1","packs":{"intake":{
	  "path":"packs/intake-0.1.0.pack.json",
	  "matrix":"packs/intake.matrix.json",
	  "expectedVersion":"0.1.0"
	}}}`, map[string]string{
		"packs/intake-0.1.0.pack.json": evaluatorPack(t),
		"packs/intake.matrix.json":     matrix,
	})
	code, stdout, _ := runTest(t, []string{"packs", "test", "--config", configPath, "--format", "json"}, "")
	if code == result.ExitSuccess {
		t.Fatalf("a matrix with such a row must not pass: %s", stdout)
	}
	var report struct {
		Status string `json:"status"`
		Packs  []struct {
			Rows []struct {
				ID       string `json:"id"`
				Status   string `json:"status"`
				Expected string `json:"expected"`
				Actual   string `json:"actual"`
				Detail   string `json:"detail"`
			} `json:"rows"`
		} `json:"packs"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("output is not JSON: %v (%q)", err, stdout)
	}
	if report.Status != "mismatch" || len(report.Packs) != 1 || len(report.Packs[0].Rows) != 2 {
		t.Fatalf("report = %+v", report)
	}
	row, beside := report.Packs[0].Rows[0], report.Packs[0].Rows[1]
	if row.ID != "expects-a-value" || row.Status != "mismatch" || row.Expected != "" || row.Actual != "" || !strings.Contains(row.Detail, "draft RFC 0016 and no member of §8.3") {
		t.Fatalf("row = %+v", row)
	}
	if beside.ID != "expects-none" || beside.Status != "passed" || beside.Expected == "" || beside.Expected != beside.Actual || strings.Contains(beside.Expected, "value") {
		t.Fatalf("the row beside it = %+v", beside)
	}
}
