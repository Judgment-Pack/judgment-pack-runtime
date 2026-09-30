package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/lock"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// reviewProject lays out one project with the intake pack under config, points
// JPACK_CONFIG at it, and locks it when locked is set. It returns the pack's
// bytes and the lock's.
func reviewProject(t *testing.T, config string, locked bool) ([]byte, []byte) {
	t.Helper()
	root := t.TempDir()
	pack, err := os.ReadFile(filepath.Join("..", "evaluation", "testdata", "data-request-intake-triage.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "packs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "packs", "intake.json"), pack, 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, project.DefaultConfigName)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(project.ConfigEnv, configPath)
	if !locked {
		return pack, nil
	}
	loaded, failure := project.Load(configPath)
	if failure != nil {
		t.Fatal(failure.Message)
	}
	defer loaded.Close()
	document, lockFailure := lock.Generate(loaded)
	if lockFailure != nil {
		t.Fatal(lockFailure.Message)
	}
	contents, err := lock.Encode(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := loaded.WriteLock(contents); err != nil {
		t.Fatal(err)
	}
	return pack, contents
}

// reviewRefusal requires one call's result to be the review refusal, naming
// want, with no disposition.
func reviewRefusal(t *testing.T, outcome map[string]any, want string) {
	t.Helper()
	if outcome["isError"] != true {
		t.Fatalf("the call must be refused: %#v", outcome)
	}
	var envelope struct {
		Diagnostics []struct {
			Code string `json:"code"`
		} `json:"diagnostics"`
		Disposition any `json:"disposition"`
	}
	decodeStructured(t, outcome, &envelope)
	if len(envelope.Diagnostics) != 1 || envelope.Diagnostics[0].Code != lock.ReviewRequiredCode || envelope.Disposition != nil ||
		!strings.Contains(toolText(t, outcome), want) {
		t.Fatalf("refusal = %+v %q, want %s naming %q", envelope, toolText(t, outcome), lock.ReviewRequiredCode, want)
	}
}

// experimental_evaluate says which law the decision was judged under, as the
// CLI does (ADR-0044), and a project that requires reviewed law refuses a pack
// passed as text however its bytes compare, while a rehearsal of it is not
// refused.
func TestExperimentalEvaluateSaysAndCanRequireTheReviewedSet(t *testing.T) {
	pack, contents := reviewProject(t, `{"configVersion":"3","audit":{"dir":"audit"},"packs":{"intake":{"path":"packs/intake.json"}}}`, true)
	responses := runServer(t, strings.Join([]string{
		toolCall(t, 1, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts}),
		toolCall(t, 2, "experimental_evaluate", map[string]any{"pack": string(pack), "facts": projectFacts}),
		toolCall(t, 3, "experimental_evaluate", map[string]any{"pack": string(pack), "facts": projectFacts, "rehearsal": true}),
	}, ""))
	var declared, draft, rehearsed result.Evaluation
	decodeStructured(t, responses[0]["result"].(map[string]any), &declared)
	decodeStructured(t, responses[1]["result"].(map[string]any), &draft)
	decodeStructured(t, responses[2]["result"].(map[string]any), &rehearsed)
	if declared.Reviewed == nil || !*declared.Reviewed || declared.ReviewedSet == nil || declared.ReviewedSet.LockDigest != lock.Digest(contents) {
		t.Fatalf("declared law: reviewed=%v set=%+v", declared.Reviewed, declared.ReviewedSet)
	}
	if draft.Reviewed == nil || *draft.Reviewed || draft.ReviewedSet != nil {
		t.Fatalf("a pack passed as text is a draft: reviewed=%v set=%+v", draft.Reviewed, draft.ReviewedSet)
	}
	if !rehearsed.Rehearsal || rehearsed.Reviewed != nil {
		t.Fatalf("a rehearsal says nothing about a reviewed set: %+v", rehearsed)
	}
	// The payload says what the same call's record says, member by member.
	trail, err := os.ReadFile(filepath.Join(filepath.Dir(os.Getenv(project.ConfigEnv)), "audit", audit.FileName))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(trail)), "\n")
	if len(lines) != 2 {
		t.Fatalf("the two decisions are recorded and the rehearsal is not: %q", trail)
	}
	for index, payload := range []result.Evaluation{declared, draft} {
		var record struct {
			Reviewed    *bool               `json:"reviewed"`
			ReviewedSet *result.ReviewedSet `json:"reviewedSet"`
		}
		if err := json.Unmarshal([]byte(lines[index]), &record); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(record.Reviewed, payload.Reviewed) || !reflect.DeepEqual(record.ReviewedSet, payload.ReviewedSet) {
			t.Fatalf("call %d: payload %v %+v, record %v %+v", index+1, payload.Reviewed, payload.ReviewedSet, record.Reviewed, record.ReviewedSet)
		}
	}

	required := `{"configVersion":"4","requireReviewed":true,"packs":{"intake":{"path":"packs/intake.json"}}}`
	pack, _ = reviewProject(t, required, false)
	reviewRefusal(t, runServer(t, toolCall(t, 1, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts}))[0]["result"].(map[string]any), "there is no reviewed-set lock")

	pack, _ = reviewProject(t, required, true)
	responses = runServer(t, strings.Join([]string{
		toolCall(t, 1, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts}),
		toolCall(t, 2, "experimental_evaluate", map[string]any{"pack": string(pack), "facts": projectFacts}),
		toolCall(t, 3, "experimental_evaluate", map[string]any{"pack": string(pack), "facts": projectFacts, "rehearsal": true}),
	}, ""))
	if responses[0]["result"].(map[string]any)["isError"] != false {
		t.Fatalf("declared law that matched is evaluated: %#v", responses[0])
	}
	reviewRefusal(t, responses[1]["result"].(map[string]any), "this run applies a draft")
	if responses[2]["result"].(map[string]any)["isError"] != false {
		t.Fatalf("a rehearsal is not refused: %#v", responses[2])
	}
}
