package mcp

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
)

// citesNoTrail is the sentence a call citing receipts in a project that keeps
// no trail is refused with (ADR-0051).
const citesNoTrail = `The "cites" argument is not taken here: the project keeps no trail, so citations would be accepted and not recorded.`

// The values of markedCites, each distinctive enough that a refusal repeating
// any of them would show it.
var (
	citesMarkerSession   = "s-NOECHO-7f3a"
	citesMarkerSignature = strings.Repeat("c0ffee", 21) + "c0"
)

const citesMarkerIndex = 8675309

// markedCites is a well-formed cites argument whose every value is a marker.
func markedCites() []map[string]any {
	return []map[string]any{{"sessionId": citesMarkerSession, "callIndex": citesMarkerIndex, "signature": citesMarkerSignature}}
}

// noTrailProject writes the binding project's pack under a configuration that
// declares no audit directory, points the server at it, and returns its
// directory.
func noTrailProject(t *testing.T) string {
	t.Helper()
	root := bindingProject(t)
	if err := os.WriteFile(filepath.Join(root, project.DefaultConfigName), []byte(`{"configVersion":"1","packs":{"intake":{"path":"packs/intake-0.1.0.pack.json"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

// refusedForCitesWithoutATrail holds one call to the no-trail refusal: a tool
// error carrying exactly the sentence, which repeats no value of the
// citations.
func refusedForCitesWithoutATrail(t *testing.T, label string, response map[string]any) {
	t.Helper()
	outcome := response["result"].(map[string]any)
	text := toolText(t, outcome)
	if outcome["isError"] != true || text != citesNoTrail {
		t.Fatalf("%s: %#v", label, outcome)
	}
	for _, marker := range []string{citesMarkerSession, "8675309", citesMarkerSignature} {
		if strings.Contains(text, marker) {
			t.Fatalf("%s: the refusal repeats a value of the citations (%s): %q", label, marker, text)
		}
	}
	if structured, _ := outcome["structuredContent"].(map[string]any); structured["disposition"] != nil {
		t.Fatalf("%s: nothing is evaluated: %#v", label, outcome)
	}
}

// Citations passed in a project that keeps no trail would be accepted and
// recorded nowhere (ADR-0051): the call is refused before anything is
// evaluated, whether the pack is named by id or passed as text, and where no
// configuration is present at all, and nothing is created. Without citations,
// with an empty array, which cites nothing, and on a rehearsal, which keeps its
// rule, the call runs.
func TestExperimentalEvaluateRefusesCitationsWhereNoTrailIsKept(t *testing.T) {
	root := noTrailProject(t)
	pack, err := os.ReadFile(filepath.Join(root, "packs", "intake-0.1.0.pack.json"))
	if err != nil {
		t.Fatal(err)
	}
	responses := runServer(t, strings.Join([]string{
		toolCall(t, 1, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts, "cites": markedCites()}),
		toolCall(t, 2, "experimental_evaluate", map[string]any{"pack": string(pack), "facts": projectFacts, "cites": markedCites()}),
		toolCall(t, 3, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts}),
		toolCall(t, 4, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts, "cites": []any{}}),
		toolCall(t, 5, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts, "cites": markedCites(), "rehearsal": true}),
	}, ""))
	refusedForCitesWithoutATrail(t, "pack by id", responses[0])
	refusedForCitesWithoutATrail(t, "pack as text", responses[1])
	for index, label := range map[int]string{2: "without citations", 3: "an empty array", 4: "a rehearsal with citations"} {
		outcome := responses[index]["result"].(map[string]any)
		if outcome["isError"] != false || !strings.Contains(toolText(t, outcome), `"disposition"`) {
			t.Fatalf("%s runs: %#v", label, outcome)
		}
	}
	if rehearsed := toolText(t, responses[4]["result"].(map[string]any)); !strings.Contains(rehearsed, `"rehearsal":true`) {
		t.Fatalf("the rehearsal says what it is: %q", rehearsed)
	}
	if _, err := os.Stat(filepath.Join(root, "audit")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("nothing is created: %v", err)
	}

	// No configuration at all keeps no trail either.
	t.Setenv(project.ConfigEnv, filepath.Join(t.TempDir(), project.DefaultConfigName))
	responses = runServer(t, toolCall(t, 6, "experimental_evaluate", map[string]any{"pack": string(pack), "facts": projectFacts, "cites": markedCites()}))
	refusedForCitesWithoutATrail(t, "no configuration at all", responses[0])
}

// The refusal sits where the binding's does: after the configuration is read,
// so a configuration that does not load is what is reported; before anything
// is evaluated, so a facts document the evaluation would refuse is not; and,
// given a binding too, the citations are what is refused, as their shape is
// checked first.
func TestExperimentalEvaluateRefusesCitationsWhereNoTrailIsKeptInTheBindingsPlace(t *testing.T) {
	root := noTrailProject(t)
	binding := map[string]any{"kind": "k", "digest": bindingDigest}
	bindingNoTrail := `The "binding" argument is not taken here: the project keeps no trail, so a binding would be accepted and not recorded.`
	responses := runServer(t, strings.Join([]string{
		toolCall(t, 1, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts, "cites": markedCites(), "binding": binding}),
		toolCall(t, 2, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts, "binding": binding}),
		toolCall(t, 3, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": "{", "cites": markedCites()}),
		toolCall(t, 4, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": "{"}),
	}, ""))
	refusedForCitesWithoutATrail(t, "citations and a binding", responses[0])
	if outcome := responses[1]["result"].(map[string]any); outcome["isError"] != true || toolText(t, outcome) != bindingNoTrail {
		t.Fatalf("a binding alone is refused by its own sentence: %#v", outcome)
	}
	refusedForCitesWithoutATrail(t, "citations with facts the evaluation would refuse", responses[2])
	if outcome := responses[3]["result"].(map[string]any); outcome["isError"] != true || !strings.Contains(toolText(t, outcome), "malformed-input") {
		t.Fatalf("without citations, the evaluation refuses the facts: %#v", outcome)
	}

	// A configuration that does not load is reported first, for citations as
	// for a binding.
	if err := os.WriteFile(filepath.Join(root, project.DefaultConfigName), []byte(`{"configVersion":"1",`), 0o600); err != nil {
		t.Fatal(err)
	}
	responses = runServer(t, strings.Join([]string{
		toolCall(t, 5, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts, "cites": markedCites()}),
		toolCall(t, 6, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts, "binding": binding}),
	}, ""))
	for index, label := range []string{"citations", "a binding"} {
		outcome := responses[index]["result"].(map[string]any)
		if text := toolText(t, outcome); outcome["isError"] != true || text == citesNoTrail || text == bindingNoTrail || strings.Contains(text, "not taken here") {
			t.Fatalf("%s under a broken configuration: %#v", label, outcome)
		}
	}
}

// The review's probes, kept as regressions (cross-vendor round 1, at 7ce4891).
// The pack named by id is resolved before the citations are judged: a
// configured pack that cannot be read is what is reported, as for a binding.
func TestExperimentalEvaluateResolvesThePackBeforeRefusingCitations(t *testing.T) {
	root := noTrailProject(t)
	if err := os.Remove(filepath.Join(root, "packs", "intake-0.1.0.pack.json")); err != nil {
		t.Fatal(err)
	}
	responses := runServer(t, toolCall(t, 1, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts, "cites": markedCites()}))
	outcome := responses[0]["result"].(map[string]any)
	message := toolText(t, outcome)
	if outcome["isError"] != true || message == citesNoTrail || !strings.Contains(message, "read") {
		t.Fatalf("the unreadable pack is reported first: %q", message)
	}
}

// A rehearsal with citations runs, says it is a rehearsal, and claims nothing
// about the citations: no "cites" member and no word of recording.
func TestExperimentalEvaluateRehearsalWithCitationsClaimsNothingRecorded(t *testing.T) {
	noTrailProject(t)
	responses := runServer(t, toolCall(t, 1, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts, "cites": markedCites(), "rehearsal": true}))
	outcome := responses[0]["result"].(map[string]any)
	message := toolText(t, outcome)
	if outcome["isError"] != false || !strings.Contains(message, `"rehearsal":true`) || strings.Contains(message, `"cites"`) || strings.Contains(message, `"recorded":true`) {
		t.Fatalf("a rehearsal's payload: %q", message)
	}
}
