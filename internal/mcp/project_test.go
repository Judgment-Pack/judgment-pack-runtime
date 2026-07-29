package mcp

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

const projectFacts = `{"request":{"type":"data-access","completeness":"complete","appropriateness":"hard-fail","embargoedInformationToUnauthorizedRecipients":false}}`

// projectFixture lays out one project and points JPACK_CONFIG at it. The
// environment variable is how a client configures the server: there is no path
// argument over the wire, for the reason no tool takes a document by path.
func projectFixture(t *testing.T) string {
	t.Helper()
	pack, err := os.ReadFile(filepath.Join("..", "evaluation", "testdata", "data-request-intake-triage.json"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "packs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "packs", "intake-0.1.0.pack.json"), pack, 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, project.DefaultConfigName)
	config := `{"configVersion":"1","packs":{"intake":{
	  "path":"packs/intake-0.1.0.pack.json",
	  "description":"Triage an inbound data-access request",
	  "expectedVersion":"0.1.0",
	  "facts":{"/request/type":{"source":"Snowflake ANALYTICS.REQUESTS","hint":"request_kind, lowercased"}},
	  "evidence":{"intake-form":{"source":"SharePoint /Intake","hint":"one PDF per request id"}}
	}}}`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(project.ConfigEnv, configPath)
	return configPath
}

func toolCall(t *testing.T, id int, name string, arguments map[string]any) string {
	t.Helper()
	params := map[string]any{"name": name}
	if arguments != nil {
		params["arguments"] = arguments
	}
	return message(t, id, "tools/call", params)
}

// list_packs and get_pack serve the project's own inventory and documents,
// read-only. The two names a pack has stay two members, and the hints travel
// with the inventory so an agent learns where to look without a second call.
func TestProjectToolsServeTheInventoryAndOneDocument(t *testing.T) {
	configPath := projectFixture(t)

	responses := runServer(t, strings.Join([]string{
		toolCall(t, 1, "list_packs", nil),
		toolCall(t, 2, "get_pack", map[string]any{"pack_id": "intake"}),
		toolCall(t, 3, "get_pack", map[string]any{"pack_id": "no-such-pack"}),
		toolCall(t, 4, "get_pack", map[string]any{"pack_id": nil}),
		toolCall(t, 5, "get_pack", nil),
		toolCall(t, 6, "get_pack", map[string]any{"packId": "intake"}),
	}, ""))
	if len(responses) != 6 {
		t.Fatalf("expected 6 responses, got %d", len(responses))
	}
	results := make([]map[string]any, 0, len(responses))
	for _, response := range responses {
		results = append(results, response["result"].(map[string]any))
	}

	listed := results[0]
	if listed["isError"] != false {
		t.Fatalf("list_packs must be a successful call: %#v", listed)
	}
	var inventory result.PackInventory
	decodeStructured(t, listed, &inventory)
	if inventory.Status != "valid" || inventory.Command != "mcp list_packs" || inventory.Kind != result.ProjectKind {
		t.Fatalf("inventory = %+v", inventory)
	}
	if inventory.ConfigPath != configPath || inventory.ConfigVersion != project.ConfigVersion {
		t.Fatalf("the inventory names the configuration it read: %+v", inventory)
	}
	if len(inventory.Packs) != 1 {
		t.Fatalf("packs = %+v", inventory.Packs)
	}
	entry := inventory.Packs[0]
	if entry.ID != "intake" || entry.PackID != "https://example.invalid/judgment-packs/data-request-intake-triage" {
		t.Fatalf("the decision id and the document's own id are two members: %+v", entry)
	}
	if entry.PackVersion != "0.1.0" || entry.ExpectedVersionStatus != result.PackVersionMatches || entry.Matrix {
		t.Fatalf("entry = %+v", entry)
	}
	if len(entry.EvidenceRequirements) != 3 {
		t.Fatalf("the inventory carries the pack's declared evidence ids: %v", entry.EvidenceRequirements)
	}
	if len(entry.Facts) != 1 || entry.Facts[0].Key != "/request/type" || entry.Facts[0].Source == "" {
		t.Fatalf("the fact hints travel with the inventory: %+v", entry.Facts)
	}
	if len(entry.Evidence) != 1 || entry.Evidence[0].Key != "intake-form" {
		t.Fatalf("the evidence hints travel with the inventory: %+v", entry.Evidence)
	}

	// get_pack returns the file's exact bytes as text, with its metadata beside them.
	fetched := results[1]
	if fetched["isError"] != false {
		t.Fatalf("get_pack must be a successful call: %#v", fetched)
	}
	text := fetched["content"].([]any)[0].(map[string]any)["text"].(string)
	onDisk, err := os.ReadFile(filepath.Join(filepath.Dir(configPath), "packs", "intake-0.1.0.pack.json"))
	if err != nil {
		t.Fatal(err)
	}
	if text != string(onDisk) {
		t.Fatal("get_pack must return the project's own bytes, unaltered")
	}
	var document result.PackDocument
	decodeStructured(t, fetched, &document)
	if document.ID != "intake" || document.PackVersion != "0.1.0" || document.SpecVersion != result.EvaluatorSpecVersion {
		t.Fatalf("document = %+v", document)
	}
	if document.Bytes != len(onDisk) || document.SHA256 == "" {
		t.Fatalf("the metadata describes the exact bytes: %+v", document)
	}

	// An unknown id is refused and the refusal lists what is configured.
	unknown := results[2]
	if unknown["isError"] != true || !strings.Contains(toolText(t, unknown), "Configured ids: intake.") {
		t.Fatalf("an unknown decision id must be refused with the known ones: %#v", unknown)
	}

	// The argument discipline of every other tool applies here: an explicit null is
	// an argument-type error, an absent key is a missing-argument error, and an
	// unknown key is rejected rather than ignored.
	if text := toolText(t, results[3]); !strings.Contains(text, "must be a JSON string") {
		t.Fatalf("an explicit null must be an argument-type error: %q", text)
	}
	if text := toolText(t, results[4]); !strings.Contains(text, "required") {
		t.Fatalf("an absent pack_id must be a missing-argument error: %q", text)
	}
	if text := toolText(t, results[5]); !strings.Contains(text, "unknown keys are rejected") {
		t.Fatalf("an unknown argument key must be rejected: %q", text)
	}
}

// With no configuration, list_packs answers empty with an explanation. It is a
// result and not an error: a project that does not use the convention is an
// ordinary project, and a model handed "error" retries while a model handed the
// explanation passes documents directly instead.
func TestListPacksWithNoConfigurationIsAnEmptyAnswerWithAnExplanation(t *testing.T) {
	t.Setenv(project.ConfigEnv, "")
	responses := runServer(t, strings.Join([]string{
		toolCall(t, 1, "list_packs", nil),
		toolCall(t, 2, "get_pack", map[string]any{"pack_id": "intake"}),
	}, ""))

	listed := responses[0]["result"].(map[string]any)
	if listed["isError"] != false {
		t.Fatalf("an absent configuration is not a tool error: %#v", listed)
	}
	var inventory result.PackInventory
	decodeStructured(t, listed, &inventory)
	if inventory.Status != "none" || len(inventory.Packs) != 0 {
		t.Fatalf("inventory = %+v", inventory)
	}
	for _, required := range []string{project.DefaultConfigName, project.ConfigEnv} {
		if !strings.Contains(inventory.Note, required) {
			t.Fatalf("the note must say where the runtime looked and how to point it elsewhere (%q): %q", required, inventory.Note)
		}
	}

	// Asking for a document, by contrast, cannot be answered at all.
	fetched := responses[1]["result"].(map[string]any)
	if fetched["isError"] != true || !strings.Contains(toolText(t, fetched), project.DefaultConfigName) {
		t.Fatalf("get_pack with no configuration must say where the runtime looked: %#v", fetched)
	}
}

// experimental_evaluate takes the pack one way or the other. The two arguments
// are mutually exclusive, exactly one is required, and the payload echoes the
// identity of the document that was actually evaluated.
func TestExperimentalEvaluateAcceptsAPackId(t *testing.T) {
	configPath := projectFixture(t)
	pack, err := os.ReadFile(filepath.Join(filepath.Dir(configPath), "packs", "intake-0.1.0.pack.json"))
	if err != nil {
		t.Fatal(err)
	}
	evidence := `{"intake-form":"present","sponsor-endorsement":"present"}`

	responses := runServer(t, strings.Join([]string{
		toolCall(t, 1, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts, "evidence": evidence}),
		toolCall(t, 2, "experimental_evaluate", map[string]any{"pack": string(pack), "facts": projectFacts, "evidence": evidence}),
		toolCall(t, 3, "experimental_evaluate", map[string]any{"pack": string(pack), "pack_id": "intake", "facts": projectFacts}),
		toolCall(t, 4, "experimental_evaluate", map[string]any{"facts": projectFacts}),
		toolCall(t, 5, "experimental_evaluate", map[string]any{"pack_id": nil, "facts": projectFacts}),
		toolCall(t, 6, "experimental_evaluate", map[string]any{"pack_id": "", "facts": projectFacts}),
		toolCall(t, 7, "experimental_evaluate", map[string]any{"pack_id": "no-such-pack", "facts": projectFacts}),
	}, ""))
	if len(responses) != 7 {
		t.Fatalf("expected 7 responses, got %d", len(responses))
	}
	results := make([]map[string]any, 0, len(responses))
	for _, response := range responses {
		results = append(results, response["result"].(map[string]any))
	}

	byID := results[0]
	if byID["isError"] != false {
		t.Fatalf("evaluation by decision id must succeed: %#v", byID)
	}
	var evaluation result.Evaluation
	decodeStructured(t, byID, &evaluation)
	if evaluation.Disposition.Kind != "outcome" || evaluation.Disposition.OutcomeID != "decline-redirect" {
		t.Fatalf("disposition = %+v", evaluation.Disposition)
	}
	if evaluation.PackID != "https://example.invalid/judgment-packs/data-request-intake-triage" || evaluation.PackVersion != "0.1.0" {
		t.Fatalf("the payload echoes the evaluated document's own identity: %+v", evaluation)
	}

	// Naming the same pack as text produces the same payload, byte for byte: the id
	// is a way of finding the document, never a different evaluation.
	if !jsonEqual(t, byID["structuredContent"], results[1]["structuredContent"]) {
		t.Fatal("by-id and by-text evaluation must produce the same payload")
	}

	for _, testCase := range []struct {
		index  int
		reason string
		want   string
	}{
		{2, "both arguments", "mutually exclusive"},
		{3, "neither argument", "A pack is required"},
		{4, "an explicit null", "must be a JSON string"},
		{5, "a present but empty id", "present but empty"},
		{6, "an unknown id", "Configured ids: intake."},
	} {
		outcome := results[testCase.index]
		if outcome["isError"] != true {
			t.Fatalf("%s must be refused: %#v", testCase.reason, outcome)
		}
		if _, structured := outcome["structuredContent"]; structured {
			t.Fatalf("%s never became an evaluation, so §8.4 classes nothing about it: %#v", testCase.reason, outcome)
		}
		if text := toolText(t, outcome); !strings.Contains(text, testCase.want) {
			t.Fatalf("%s: message = %q, want it to contain %q", testCase.reason, text, testCase.want)
		}
	}
}

// A configured path that leaves the configuration's own directory is refused on
// the wire as it is everywhere else, and the refusal never returns the bytes.
// Both escapes are exercised: the lexical one, and the one only canonicalizing
// the containing directory can see.
func TestProjectToolsRefuseAConfiguredPathThatEscapesTheRoot(t *testing.T) {
	for name, declared := range map[string]string{
		"a parent traversal":      "../secret.json",
		"a symlinked component":   "out/secret.json",
		"a symlinked final file":  "alias.json",
		"an absolute path":        "/etc/passwd",
		"an absolute-looking one": "/secret.json",
	} {
		t.Run(name, func(t *testing.T) {
			base := t.TempDir()
			if err := os.WriteFile(filepath.Join(base, "secret.json"), []byte(`{"secret":true}`), 0o600); err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(base, "project")
			if err := os.MkdirAll(root, 0o755); err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(declared, "out/") || declared == "alias.json" {
				if runtime.GOOS == "windows" {
					t.Skip("the symlink escapes are POSIX filesystem behavior")
				}
				// "out" is a directory symlink leaving the root; "alias.json" is a final
				// symlink, which the lexical check and the canonicalized directory both
				// accept and only the O_NOFOLLOW open refuses.
				if err := os.Symlink(base, filepath.Join(root, "out")); err != nil {
					t.Skipf("cannot create symlink: %v", err)
				}
				if err := os.Symlink(filepath.Join(base, "secret.json"), filepath.Join(root, "alias.json")); err != nil {
					t.Skipf("cannot create symlink: %v", err)
				}
			}
			configPath := filepath.Join(root, project.DefaultConfigName)
			config := `{"configVersion":"1","packs":{"escaping":{"path":` + quoteJSON(t, declared) + `}}}`
			if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv(project.ConfigEnv, configPath)

			responses := runServer(t, strings.Join([]string{
				toolCall(t, 1, "get_pack", map[string]any{"pack_id": "escaping"}),
				toolCall(t, 2, "experimental_evaluate", map[string]any{"pack_id": "escaping", "facts": "{}"}),
				toolCall(t, 3, "list_packs", nil),
			}, ""))

			for index, tool := range []string{"get_pack", "experimental_evaluate"} {
				outcome := responses[index]["result"].(map[string]any)
				if outcome["isError"] != true {
					t.Fatalf("%s must refuse this path: %#v", tool, outcome)
				}
				// The refusal names the declared path, which is the project's own text; it
				// never carries a byte of the file that path pointed at.
				if text := toolText(t, outcome); strings.Contains(text, `{"secret":true}`) {
					t.Fatalf("%s must not return the file it refused: %q", tool, text)
				}
			}
			// The inventory still lists the entry, with the reason and no invented identity.
			var inventory result.PackInventory
			decodeStructured(t, responses[2]["result"].(map[string]any), &inventory)
			if len(inventory.Packs) != 1 || inventory.Packs[0].PackID != "" || inventory.Packs[0].Detail == "" {
				t.Fatalf("inventory = %+v", inventory.Packs)
			}
		})
	}
}

// A pack document that was read and did not decode is served — the bytes are the
// project's and the caller asked for them — but the payload says so instead of
// reporting an empty identity under a "valid" status with nothing to explain it.
func TestGetPackReportsADocumentThatDidNotDecode(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "packs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "packs", "broken.json"), []byte(`{ this is not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, project.DefaultConfigName)
	if err := os.WriteFile(configPath, []byte(`{"configVersion":"1","packs":{"broken":{"path":"packs/broken.json"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(project.ConfigEnv, configPath)

	fetched := runServer(t, toolCall(t, 1, "get_pack", map[string]any{"pack_id": "broken"}))[0]["result"].(map[string]any)
	var document result.PackDocument
	decodeStructured(t, fetched, &document)
	if document.Status == "valid" {
		t.Fatalf("bytes that did not decode are not a valid document: %+v", document)
	}
	if document.Detail == "" {
		t.Fatalf("the payload must say why the identity members are empty: %+v", document)
	}
	if document.PackID != "" || document.SpecVersion != "" {
		t.Fatalf("no identity is invented: %+v", document)
	}
	// list_packs says the same thing about the same file: two project tools must
	// not disagree about one document.
	var inventory result.PackInventory
	decodeStructured(t, runServer(t, toolCall(t, 1, "list_packs", nil))[0]["result"].(map[string]any), &inventory)
	if inventory.Packs[0].Detail != document.Detail {
		t.Fatalf("the two tools must report one file identically: %q vs %q", inventory.Packs[0].Detail, document.Detail)
	}
}

// One byte-limit boundary for every surface: an oversized pack reached by
// decision id is the §8.2 preflight condition the engine classes, not a read
// failure with no class. The CLI reports it as pack-not-conformant, and the wire
// has to report the same thing about the same file.
func TestAnOversizedPackReachedByIdIsClassedLikeAnyOtherOversizedPack(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "packs"), 0o755); err != nil {
		t.Fatal(err)
	}
	oversized := append([]byte(`{"pad":"`), bytes.Repeat([]byte("x"), int(carrier.HardMaxBytes))...)
	if err := os.WriteFile(filepath.Join(root, "packs", "huge.json"), append(oversized, '"', '}'), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, project.DefaultConfigName)
	if err := os.WriteFile(configPath, []byte(`{"configVersion":"1","packs":{"huge":{"path":"packs/huge.json"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(project.ConfigEnv, configPath)

	outcome := runServer(t, toolCall(t, 1, "experimental_evaluate", map[string]any{"pack_id": "huge", "facts": "{}"}))[0]["result"].(map[string]any)
	if outcome["isError"] != true {
		t.Fatalf("an oversized pack is refused: %#v", outcome)
	}
	var envelope result.OperationalError
	decodeStructured(t, outcome, &envelope)
	if envelope.EvaluationError == nil {
		t.Fatalf("the refusal must carry the §8.4 envelope, as it does for oversized pack text: %#v", outcome)
	}
	if envelope.EvaluationError.Class != result.ClassPackNotConformant || envelope.EvaluationError.Phase != "preflight" {
		t.Fatalf("evaluationError = %+v", envelope.EvaluationError)
	}
}

func quoteJSON(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

// A configuration that exists and is broken is a tool error, unlike an absent
// one: that is a defect someone has to fix, and reporting it as an empty project
// would hide it.
func TestABrokenConfigurationIsAToolError(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, project.DefaultConfigName)
	if err := os.WriteFile(configPath, []byte(`{"configVersion":"2","packs":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(project.ConfigEnv, configPath)

	listed := runServer(t, toolCall(t, 1, "list_packs", nil))[0]["result"].(map[string]any)
	if listed["isError"] != true {
		t.Fatalf("a broken configuration must be a tool error: %#v", listed)
	}
	if text := toolText(t, listed); !strings.Contains(text, "It accepts: "+project.ConfigVersion+".") {
		t.Fatalf("the refusal must name the versions this runtime accepts: %q", text)
	}
}

func toolText(t *testing.T, outcome map[string]any) string {
	t.Helper()
	content, ok := outcome["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("tool result carries no content: %#v", outcome)
	}
	return content[0].(map[string]any)["text"].(string)
}

func decodeStructured(t *testing.T, outcome map[string]any, into any) {
	t.Helper()
	data, err := json.Marshal(outcome["structuredContent"])
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, into); err != nil {
		t.Fatalf("undecodable structured content %s: %v", data, err)
	}
}

func jsonEqual(t *testing.T, left, right any) bool {
	t.Helper()
	leftBytes, err := json.Marshal(left)
	if err != nil {
		t.Fatal(err)
	}
	rightBytes, err := json.Marshal(right)
	if err != nil {
		t.Fatal(err)
	}
	return string(leftBytes) == string(rightBytes)
}

// get_pack_diagram serves the deterministic Mermaid rendering of one declared
// pack: the same bytes the CLI's packs diagram emits, a valid flowchart with
// the document's members as nodes, and the standard refusals for a missing or
// unknown id.
func TestGetPackDiagramServesTheDeterministicRendering(t *testing.T) {
	projectFixture(t)

	responses := runServer(t, strings.Join([]string{
		toolCall(t, 1, "get_pack_diagram", map[string]any{"pack_id": "intake"}),
		toolCall(t, 2, "get_pack_diagram", map[string]any{"pack_id": "no-such-pack"}),
		toolCall(t, 3, "get_pack_diagram", nil),
	}, ""))
	if len(responses) != 3 {
		t.Fatalf("expected 3 responses, got %d", len(responses))
	}

	rendered := responses[0]["result"].(map[string]any)
	if rendered["isError"] == true {
		t.Fatalf("get_pack_diagram must succeed for a declared pack: %#v", rendered)
	}
	text := rendered["content"].([]any)[0].(map[string]any)["text"].(string)
	for _, marker := range []string{"flowchart TD", "out_", "subgraph rules"} {
		if !strings.Contains(text, marker) {
			t.Fatalf("the rendering must contain %q:\n%s", marker, text)
		}
	}
	// Same document, same bytes: a second call returns the identical text.
	again := runServer(t, toolCall(t, 1, "get_pack_diagram", map[string]any{"pack_id": "intake"}))
	if other := again[0]["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string); other != text {
		t.Fatalf("two renderings of one document differed")
	}

	for i, wantError := range map[int]string{1: "no-such-pack", 2: "pack_id"} {
		result := responses[i]["result"].(map[string]any)
		if result["isError"] != true {
			t.Fatalf("response %d must be a tool error: %#v", i, result)
		}
		message := result["content"].([]any)[0].(map[string]any)["text"].(string)
		if !strings.Contains(message, wantError) {
			t.Fatalf("error %d must mention %q: %q", i, wantError, message)
		}
	}
}
