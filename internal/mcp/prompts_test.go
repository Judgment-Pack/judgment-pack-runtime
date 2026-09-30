package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

type pinnedPrompt struct {
	name       string
	artifact   string
	boundaries []string
}

func pinnedPrompts() []pinnedPrompt {
	return []pinnedPrompt{
		{
			name:     "author_pack",
			artifact: "judgment pack",
			boundaries: []string{
				"this runtime decides nothing",
				`only "spec validate" / the "validate" tool decides conformance`,
			},
		},
		{
			name:     "test_pack",
			artifact: "instance matrix",
			boundaries: []string{
				"in full and only, in CONFORMANCE.md",
				"THE POLICY TEXT IS THE ARBITER",
			},
		},
		{
			name:     "fix_pack",
			artifact: "diagnostics",
			boundaries: []string{
				"validity does not test logic",
				"this runtime decides nothing",
			},
		},
		{
			name:     "explain_disposition",
			artifact: "disposition",
			boundaries: []string{
				"The disposition is authoritative",
				"the trace beside it is informative",
				"nothing about the wisdom of acting",
			},
		},
		{
			name:     "present_pack",
			artifact: "judgment pack",
			boundaries: []string{
				"The pack is the only statement of what it is",
				"an omission silent is a misrepresentation",
				"authorize acting on any disposition",
			},
		},
		{
			name:     "replay_history",
			artifact: "past decisions",
			boundaries: []string{
				"DOCUMENTS WRITE THE RULES, PAST DECISIONS TEST THEM",
				"every row is a rehearsal, and nothing is recorded as a decision",
				"None of it is a verdict",
			},
		},
		{
			name:     "author_graph",
			artifact: "graph document",
			boundaries: []string{
				"a PROPOSAL for a human to review",
				"a matrix row is a rehearsal, not a decision",
				"You propose; the human commits",
			},
		},
	}
}

// renderPinnedPrompt returns the text getPrompt serves for name with no
// arguments, failing the test when the name is not advertised.
func renderPinnedPrompt(t *testing.T, name string) string {
	t.Helper()
	params, err := json.Marshal(map[string]any{"name": name})
	if err != nil {
		t.Fatal(err)
	}
	got, rpcErr := getPrompt(params)
	if rpcErr != nil {
		t.Fatalf("getPrompt(%q) must succeed for an advertised prompt: %s", name, rpcErr.Message)
	}
	messages, ok := got["messages"].([]map[string]any)
	if !ok || len(messages) == 0 {
		t.Fatalf("getPrompt(%q) must return at least one message: %#v", name, got)
	}
	content, ok := messages[0]["content"].(map[string]any)
	if !ok {
		t.Fatalf("getPrompt(%q) message must carry content: %#v", name, messages[0])
	}
	text, ok := content["text"].(string)
	if !ok {
		t.Fatalf("getPrompt(%q) content must carry text: %#v", name, content)
	}
	return text
}

func TestPromptsPinClaimBoundaries(t *testing.T) {
	for _, pinned := range pinnedPrompts() {
		t.Run(pinned.name, func(t *testing.T) {
			text := renderPinnedPrompt(t, pinned.name)
			if strings.TrimSpace(text) == "" {
				t.Fatalf("%s must render non-empty text", pinned.name)
			}
			if !strings.Contains(strings.ToLower(text), strings.ToLower(pinned.artifact)) {
				t.Fatalf("%s must mention the artifact it is about (%q)", pinned.name, pinned.artifact)
			}
			for _, phrase := range pinned.boundaries {
				if !strings.Contains(text, phrase) {
					t.Fatalf("%s must carry its claim boundary %q", pinned.name, phrase)
				}
			}
		})
	}
}

// Every prompt the server advertises must have a pinning entry above, so a
// newly added prompt fails here until its claim boundary is pinned.
func TestEveryAdvertisedPromptIsPinned(t *testing.T) {
	pinned := map[string]bool{}
	for _, p := range pinnedPrompts() {
		pinned[p.name] = true
	}
	for _, entry := range listPrompts() {
		name, _ := entry["name"].(string)
		if name == "" {
			t.Fatal("listPrompts must advertise a name for every prompt")
		}
		if !pinned[name] {
			t.Fatalf("advertised prompt %q has no claim-boundary pin in pinnedPrompts", name)
		}
	}
}
