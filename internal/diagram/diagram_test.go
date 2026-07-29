package diagram

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The renderer's whole contract is byte determinism, so the tests are golden
// files: the same fixture yields the same bytes, and a reviewed golden is the
// specification of the mapping. Regenerate deliberately with
// UPDATE_DIAGRAM_GOLDENS=1 and re-review the diff — a golden that changed is a
// mapping that changed.
func TestMermaidGoldens(t *testing.T) {
	for _, name := range []string{"intake-triage", "minimal", "branches"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", name+".pack.json"))
			if err != nil {
				t.Fatal(err)
			}
			var document map[string]any
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatal(err)
			}
			rendered := Mermaid(document)

			goldenPath := filepath.Join("testdata", name+".golden.mmd")
			if os.Getenv("UPDATE_DIAGRAM_GOLDENS") == "1" {
				if err := os.WriteFile(goldenPath, []byte(rendered), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			golden, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatal(err)
			}
			if rendered != string(golden) {
				t.Fatalf("rendering diverged from the reviewed golden;\n--- got ---\n%s\n--- want ---\n%s", rendered, golden)
			}
		})
	}
}

// Determinism holds across repeated renders of the same decoded document —
// nothing in the walk depends on map iteration order.
func TestMermaidDeterministic(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "intake-triage.pack.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	first := Mermaid(document)
	for i := 0; i < 16; i++ {
		if Mermaid(document) != first {
			t.Fatalf("render %d differed", i)
		}
	}
}

// Labels never leak Mermaid syntax: quotes, angle brackets, and newlines in
// document strings arrive as entities or <br/>, and hostile member text stays
// inside its quoted label.
func TestMermaidEscapesLabels(t *testing.T) {
	document := map[string]any{
		"id":      "https://example.invalid/x",
		"version": "0.0.1",
		"outcomes": []any{map[string]any{
			"id":    "quote",
			"label": `he said "hi" --> <script>` + "\nline",
		}},
	}
	rendered := Mermaid(document)
	for _, banned := range []string{`"hi"`, "<script>"} {
		if strings.Contains(rendered, banned) {
			t.Fatalf("label leaked %q:\n%s", banned, rendered)
		}
	}
	for _, wanted := range []string{"#quot;hi#quot;", "#lt;script#gt;", "<br/>"} {
		if !strings.Contains(rendered, wanted) {
			t.Fatalf("expected escaped form %q:\n%s", wanted, rendered)
		}
	}
}

// The renderer-killing byte sequences found in review stay neutralized: a
// leading backtick would switch Mermaid's lexer into its markdown-string
// state, %%{ opens a directive scanned outside string state, an empty label
// derives nothing in the grammar, and a hostile id would break the YAML
// frontmatter as a plain scalar or smuggle lines through \r.
func TestMermaidNeutralizesRendererHazards(t *testing.T) {
	document := map[string]any{
		"id":      "a: b #c\r---\rflowchart LR",
		"version": "0.0.1",
		"outcomes": []any{
			map[string]any{"id": "tick", "label": "`Proceed`"},
			map[string]any{"id": "directive", "label": "%%{init: {}}%%"},
			map[string]any{},
		},
	}
	rendered := Mermaid(document)
	for _, banned := range []string{"`", "%%", "\r", "([\"\"])"} {
		if strings.Contains(rendered, banned) {
			t.Fatalf("output still carries %q:\n%s", banned, rendered)
		}
	}
	for _, wanted := range []string{
		`title: "a: b #c\r---\rflowchart LR 0.0.1"`,
		"#96;Proceed#96;",
		"#37;#37;{init: {}}#37;#37;",
		`(["(unnamed)"])`,
	} {
		if !strings.Contains(rendered, wanted) {
			t.Fatalf("expected %q:\n%s", wanted, rendered)
		}
	}
}

// Authored text that looks like a Mermaid entity survives rendering as
// itself: the raw # is escaped first, so #quot; in a document displays as
// the seven characters the author wrote, not as a decoded quote.
func TestMermaidRoundTripsAuthoredEntities(t *testing.T) {
	document := map[string]any{
		"id":      "https://example.invalid/x",
		"version": "0.0.1",
		"outcomes": []any{map[string]any{"id": "o", "label": "wrote #quot; literally"}},
	}
	rendered := Mermaid(document)
	if !strings.Contains(rendered, "wrote #35;quot; literally") {
		t.Fatalf("authored entity text must be neutralized:\n%s", rendered)
	}
}

// Sanitization is many-to-one; the allocator is not. Two rule ids that
// sanitize identically stay two vertices, and every reference resolves to
// the vertex of the member it names.
func TestMermaidKeepsCollidingIDsDistinct(t *testing.T) {
	document := map[string]any{
		"id":      "https://example.invalid/x",
		"version": "0.0.1",
		"outcomes": []any{
			map[string]any{"id": "a-b", "label": "First"},
			map[string]any{"id": "a_b", "label": "Second"},
		},
		"rules": []any{
			map[string]any{"id": "r", "when": map[string]any{"op": "fact", "path": "/x", "operator": "equals", "value": true}, "outcome": "a_b"},
		},
	}
	rendered := Mermaid(document)
	for _, wanted := range []string{`out_a_b(["First"])`, `out_a_b_2(["Second"])`, "rule_r --> out_a_b_2"} {
		if !strings.Contains(rendered, wanted) {
			t.Fatalf("expected %q:\n%s", wanted, rendered)
		}
	}
}
