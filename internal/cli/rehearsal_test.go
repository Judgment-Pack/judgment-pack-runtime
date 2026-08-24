package cli

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// A declared rehearsal writes no record in a project that asked for a trail,
// and its payload says what it is; the same run without the declaration writes
// the record and carries no label. The label is present exactly when declared
// (ADR-0028): a decoding accident or a default can never manufacture either
// side.
func TestRehearsalWritesNoRecordAndLabelsThePayload(t *testing.T) {
	configPath := auditProject(t)
	facts := writeDocument(t, "facts.json", hardFailFacts)
	evidence := writeDocument(t, "evidence.json", presentEvidence)

	code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake", "--rehearsal",
		"--config", configPath, "--facts", facts, "--evidence", evidence, "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	var rehearsed result.Evaluation
	if err := json.Unmarshal([]byte(stdout), &rehearsed); err != nil {
		t.Fatal(err)
	}
	if !rehearsed.Rehearsal || rehearsed.Disposition.Kind == "" {
		t.Fatalf("a rehearsal is a labeled, complete evaluation: %+v", rehearsed)
	}
	noAuditTrail(t, configPath)

	// The undeclared run is the ordinary recorded call, and its payload carries
	// no rehearsal member at all — absent, not false — so a reader of the bytes
	// sees the label exactly where a declaration was made.
	code, stdout, stderr = runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake",
		"--config", configPath, "--facts", facts, "--evidence", evidence, "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	if strings.Contains(stdout, `"rehearsal"`) {
		t.Fatalf("an undeclared run carries no rehearsal member: %q", stdout)
	}
	records := auditRecords(t, configPath)
	if len(records) != 1 {
		t.Fatalf("one undeclared evaluation is one record, the rehearsal none: %v", records)
	}
}

// The human rendering says the same thing the payload's member says, in one
// line under the experimental headline, and says nothing when nothing was
// declared.
func TestRehearsalIsNamedInHumanOutput(t *testing.T) {
	configPath := auditProject(t)
	facts := writeDocument(t, "facts.json", hardFailFacts)
	evidence := writeDocument(t, "evidence.json", presentEvidence)

	code, stdout, _ := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake", "--rehearsal",
		"--config", configPath, "--facts", facts, "--evidence", evidence}, "")
	if code != 0 || !strings.Contains(stdout, "REHEARSAL: declared not a decision") {
		t.Fatalf("exit=%d, the human rendering names the declaration: %q", code, stdout)
	}
	noAuditTrail(t, configPath)
}

// A rehearsal consults no reviewed set: law the lock would refuse to decide
// under still rehearses, because the run is declared, in band, to decide
// nothing — the standing a matrix row has always had (ADR-0021). The refusal
// on the undeclared run is asserted first, so this row cannot pass while the
// lock catches nothing.
func TestRehearsalEvaluatesLawTheLockWouldRefuse(t *testing.T) {
	configPath := auditProject(t)
	mustLock(t, configPath)
	body, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, append(body, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	facts := writeDocument(t, "facts.json", hardFailFacts)

	code, _, stderr := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake",
		"--config", configPath, "--facts", facts}, "")
	if code != result.ExitInvalid || !strings.Contains(stderr, "jpack packs lock") {
		t.Fatalf("the undeclared run must be refused under drifted law: exit=%d stderr=%q", code, stderr)
	}

	code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake", "--rehearsal",
		"--config", configPath, "--facts", facts, "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("the declared rehearsal evaluates: exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	var rehearsed result.Evaluation
	if err := json.Unmarshal([]byte(stdout), &rehearsed); err != nil {
		t.Fatal(err)
	}
	if !rehearsed.Rehearsal || rehearsed.Disposition.Kind == "" {
		t.Fatalf("a rehearsal under drifted law is still a labeled, complete evaluation: %+v", rehearsed)
	}
	noAuditTrail(t, configPath)
}

// A rehearsal changes exactly one thing in the payload — the label — and
// exactly one line in the human rendering. Byte-comparing the two runs after
// deleting only `"rehearsal":true,` from the declared one discriminates any
// rehearsal-conditional change to any other member: disposition, trace,
// handoff, draftPrototype, artifact, all of them at once. The second case runs
// the same comparison under the draft-quantifier opt-in, so the draftPrototype
// member sits inside the compared bytes rather than outside them.
func TestRehearsalChangesOnlyTheLabel(t *testing.T) {
	configPath := auditProject(t)
	facts := writeDocument(t, "facts.json", hardFailFacts)
	evidence := writeDocument(t, "evidence.json", presentEvidence)

	quantifierPack := writeDocument(t, "quantifier.pack.json", `{
  "specVersion": "0.2.0-draft",
  "id": "https://example.invalid/judgment-packs/rehearsal-identity",
  "version": "0.1.0",
  "title": "Synthetic rehearsal-identity row",
  "description": "Invented content for specification testing; it authorizes nothing.",
  "decision": {
    "intent": "Exercise the rehearsal identity under the draft-quantifier opt-in.",
    "question": "Did any element hold?"
  },
  "outcomes": [
    {"id": "held", "label": "Held"},
    {"id": "did-not-hold", "label": "Did not hold"}
  ],
  "rules": [
    {
      "id": "the-rule",
      "description": "One draft-RFC quantifier, so the payload carries draftPrototype.",
      "when": {"op": "exists", "path": "/items", "where": {"op": "literal", "value": true}},
      "outcome": "held",
      "onUnknown": "ignore"
    }
  ],
  "fallbackOutcome": "did-not-hold"
}`)
	quantifierFacts := writeDocument(t, "quantifier-facts.json", `{"items":[null]}`)

	cases := []struct {
		name string
		args []string
	}{
		{name: "core", args: []string{"experimental", "evaluate", "--pack-id", "intake",
			"--config", configPath, "--facts", facts, "--evidence", evidence}},
		{name: "draft-quantifiers", args: []string{"experimental", "evaluate", quantifierPack, "--rfc0008-quantifiers",
			"--config", configPath, "--facts", quantifierFacts}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			code, recorded, stderr := runTest(t, append(append([]string{}, testCase.args...), "--format", "json"), "")
			if code != 0 || stderr != "" {
				t.Fatalf("recorded run: exit=%d stderr=%q", code, stderr)
			}
			code, rehearsed, stderr := runTest(t, append(append([]string{}, testCase.args...), "--rehearsal", "--format", "json"), "")
			if code != 0 || stderr != "" {
				t.Fatalf("rehearsal run: exit=%d stderr=%q", code, stderr)
			}
			stripped := strings.Replace(rehearsed, `"rehearsal":true,`, "", 1)
			if stripped == rehearsed {
				t.Fatalf("the rehearsal payload must carry the label: %q", rehearsed)
			}
			if stripped != recorded {
				t.Fatalf("a rehearsal changes only the label:\nrecorded  %q\nstripped  %q", recorded, stripped)
			}

			code, recordedHuman, stderr := runTest(t, append([]string{}, testCase.args...), "")
			if code != 0 || stderr != "" {
				t.Fatalf("recorded human run: exit=%d stderr=%q", code, stderr)
			}
			code, rehearsedHuman, stderr := runTest(t, append(append([]string{}, testCase.args...), "--rehearsal"), "")
			if code != 0 || stderr != "" {
				t.Fatalf("rehearsal human run: exit=%d stderr=%q", code, stderr)
			}
			strippedHuman := strings.Replace(rehearsedHuman,
				"REHEARSAL: declared not a decision; no audit record was appended and no reviewed set was consulted\n", "", 1)
			if strippedHuman == rehearsedHuman || strippedHuman != recordedHuman {
				t.Fatalf("the human renderings differ by exactly the rehearsal line:\nrecorded  %q\nrehearsal %q", recordedHuman, rehearsedHuman)
			}
		})
	}
}
