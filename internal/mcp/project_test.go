package mcp

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/graph"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/lock"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
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

// rawToolCall sends arguments verbatim, so a test can present a JSON value the
// Go type system would not let toolCall express -- a literal null, a wrongly
// typed member, a null array element.
func rawToolCall(t *testing.T, id int, name, rawArguments string) string {
	t.Helper()
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":%q,"arguments":%s}}`+"\n",
		id, name, rawArguments)
}

// newTestEngine builds the same validation engine the server uses, for tests
// that compare an MCP payload against the layer beneath it.
func newTestEngine(t *testing.T) *validation.Engine {
	t.Helper()
	engine, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	return engine
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
	if inventory.ConfigPath != configPath || inventory.ConfigVersion != "1" {
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
	// The same row the CLI serves, from the same derivation: the pointers the
	// pack's conditions read, sorted and deduplicated (ADR-0020).
	wantConsulted := []string{"/request/appropriateness", "/request/completeness",
		"/request/embargoedInformationToUnauthorizedRecipients", "/request/type"}
	if !slices.Equal(entry.ConsultedFactPaths, wantConsulted) {
		t.Fatalf("consultedFactPaths = %v, want %v", entry.ConsultedFactPaths, wantConsulted)
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
	if text := toolText(t, results[5]); !strings.Contains(text, `unknown member "packId"`) {
		t.Fatalf("an unknown argument key must be rejected, named exactly: %q", text)
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
	if err := os.WriteFile(configPath, []byte(`{"configVersion":"4","packs":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(project.ConfigEnv, configPath)

	responses := runServer(t, strings.Join([]string{
		toolCall(t, 1, "list_packs", nil),
		toolCall(t, 2, "experimental_test_packs", nil),
	}, ""))
	listed := responses[0]["result"].(map[string]any)
	if listed["isError"] != true {
		t.Fatalf("a broken configuration must be a tool error: %#v", listed)
	}
	if tested := responses[1]["result"].(map[string]any); tested["isError"] != true {
		t.Fatalf("a broken configuration must refuse the matrix run too: %#v", tested)
	}
	if text := toolText(t, listed); !strings.Contains(text, "It accepts: "+strings.Join(project.SupportedConfigVersions(), ", ")+".") {
		t.Fatalf("the refusal must name the versions this runtime accepts: %q", text)
	}
}

// A project that declares an audit directory is recorded on this surface too,
// with the pack passed as text as much as by id: the configuration says what is
// recorded, and which argument the client used is not part of that. A failure
// to write the record is a tool error and no disposition comes back with it.
func TestExperimentalEvaluateRecordsWhatTheProjectAskedFor(t *testing.T) {
	root := t.TempDir()
	pack, err := os.ReadFile(filepath.Join("..", "evaluation", "testdata", "data-request-intake-triage.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "packs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "packs", "intake-0.1.0.pack.json"), pack, 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, project.DefaultConfigName)
	config := `{"configVersion":"3","audit":{"dir":"audit"},"packs":{"intake":{"path":"packs/intake-0.1.0.pack.json"}}}`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(project.ConfigEnv, configPath)

	responses := runServer(t, strings.Join([]string{
		toolCall(t, 1, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts, "evidence": `{"intake-form":"present","sponsor-endorsement":"present"}`}),
		toolCall(t, 2, "experimental_evaluate", map[string]any{"pack": string(pack), "facts": projectFacts}),
		toolCall(t, 3, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": `{"request":{"type":"data-access"}}`, "evidence": `{"not-a-requirement":"present"}`}),
	}, ""))
	for index, response := range responses[:2] {
		if response["result"].(map[string]any)["isError"] != false {
			t.Fatalf("call %d must succeed: %#v", index+1, response)
		}
	}
	if responses[2]["result"].(map[string]any)["isError"] != true {
		t.Fatalf("the third call must be refused: %#v", responses[2])
	}

	data, err := os.ReadFile(filepath.Join(root, "audit", audit.FileName))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	// Two completed evaluations, two records. The refused one produced no
	// disposition at all (§8.4) and left no record.
	if len(lines) != 2 {
		t.Fatalf("records = %d, want 2: %q", len(lines), data)
	}
	var evaluation result.Evaluation
	decodeStructured(t, responses[0]["result"].(map[string]any), &evaluation)
	var record map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
		t.Fatalf("undecodable record %q: %v", lines[0], err)
	}
	if record["kind"] != audit.KindEvaluation || record["surface"] != "mcp experimental_evaluate" {
		t.Fatalf("record = %v", record)
	}
	if record["pack"].(map[string]any)["id"] != evaluation.PackID {
		t.Fatalf("record = %v, payload = %+v", record, evaluation)
	}
	canonical, err := evaluation.Disposition.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(lines[0], `"disposition":`+string(canonical)) {
		t.Fatalf("the record must embed the call's own canonical disposition %s: %q", canonical, lines[0])
	}

	// A record that cannot be written refuses the call, and the disposition
	// does not come back without it.
	occupied := t.TempDir()
	if err := os.WriteFile(filepath.Join(occupied, "packs.json"), pack, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(occupied, "audit"), []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(occupied, project.DefaultConfigName)
	if err := os.WriteFile(blocked, []byte(`{"configVersion":"3","audit":{"dir":"audit"},"packs":{"intake":{"path":"packs.json"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(project.ConfigEnv, blocked)
	refused := runServer(t, toolCall(t, 1, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts}))[0]["result"].(map[string]any)
	if refused["isError"] != true || toolText(t, refused) != audit.FailureMessage {
		t.Fatalf("a record that could not be written refuses the call: %#v", refused)
	}
	// The refusal carries the same code the CLI reports, so a client can tell
	// "your decision was not recorded" from an argument mistake — and carries no
	// disposition, which is the answer this refusal exists to withhold.
	var envelope struct {
		Status      string `json:"status"`
		Diagnostics []struct {
			Code string `json:"code"`
		} `json:"diagnostics"`
		Disposition any `json:"disposition"`
	}
	decodeStructured(t, refused, &envelope)
	if envelope.Status != "error" || len(envelope.Diagnostics) != 1 ||
		envelope.Diagnostics[0].Code != audit.FailureCode || envelope.Disposition != nil {
		t.Fatalf("refusal envelope = %+v (%#v)", envelope, refused["structuredContent"])
	}
}

// A configuration that is there and cannot be read refuses the call on this
// surface too, with the pack passed as text: the same fail-closed rule the CLI
// applies, for the same reason.
func TestABrokenConfigurationRefusesAPackPassedAsText(t *testing.T) {
	root := t.TempDir()
	pack, err := os.ReadFile(filepath.Join("..", "evaluation", "testdata", "data-request-intake-triage.json"))
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, project.DefaultConfigName)
	if err := os.WriteFile(configPath, []byte(`{"configVersion":"3","packs":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(project.ConfigEnv, configPath)

	outcome := runServer(t, toolCall(t, 1, "experimental_evaluate", map[string]any{"pack": string(pack), "facts": projectFacts}))[0]["result"].(map[string]any)
	if outcome["isError"] != true || !strings.Contains(toolText(t, outcome), project.DefaultConfigName) {
		t.Fatalf("a broken configuration refuses the call and says which file it is: %#v", outcome)
	}
}

// The reviewed-set lock reaches this surface exactly as it reaches the CLI
// (ADR-0019): a pack named by decision id is declared law and is held to what
// the project declared reviewed; a pack passed as text is a draft, evaluated and
// recorded as one; and law that drifted refuses the call before the evaluator is
// reached.
func TestExperimentalEvaluateHoldsDeclaredLawToTheReviewedSet(t *testing.T) {
	root := t.TempDir()
	pack, err := os.ReadFile(filepath.Join("..", "evaluation", "testdata", "data-request-intake-triage.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "packs"), 0o755); err != nil {
		t.Fatal(err)
	}
	packPath := filepath.Join(root, "packs", "intake.json")
	if err := os.WriteFile(packPath, pack, 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, project.DefaultConfigName)
	config := `{"configVersion":"3","audit":{"dir":"audit"},"packs":{"intake":{"path":"packs/intake.json"}}}`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(project.ConfigEnv, configPath)

	loaded, failure := project.Load(configPath)
	if failure != nil {
		t.Fatal(failure.Message)
	}
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
	loaded.Close()

	// Declared law that matches, and the same pack as text — a draft.
	responses := runServer(t, strings.Join([]string{
		toolCall(t, 1, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts}),
		toolCall(t, 2, "experimental_evaluate", map[string]any{"pack": string(pack), "facts": projectFacts}),
	}, ""))
	for index, response := range responses {
		if response["result"].(map[string]any)["isError"] != false {
			t.Fatalf("call %d must succeed: %#v", index+1, response)
		}
	}
	data, err := os.ReadFile(filepath.Join(root, "audit", audit.FileName))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("records = %d, want 2: %q", len(lines), data)
	}
	reviewed := make([]any, 0, 2)
	for _, line := range lines {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		reviewed = append(reviewed, record["reviewed"])
	}
	if reviewed[0] != true || reviewed[1] != false {
		t.Fatalf("reviewed = %v, want the declared run true and the draft false", reviewed)
	}
	// The reviewed run names the revision that made its claim true; the draft
	// names none, because it was judged under no reviewed set.
	var declaredRecord, draftRecord map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &declaredRecord); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &draftRecord); err != nil {
		t.Fatal(err)
	}
	named, ok := declaredRecord["reviewedSet"].(map[string]any)
	if !ok || named["lockDigest"] != lock.Digest(contents) || named["lockVersion"] != lock.Version {
		t.Fatalf("reviewedSet = %v", declaredRecord["reviewedSet"])
	}
	if draftRecord["reviewedSet"] != nil {
		t.Fatalf("a draft names no reviewed set: %v", draftRecord["reviewedSet"])
	}

	// Law that left the reviewed set refuses, carrying the same code the CLI
	// reports and no disposition.
	if err := os.WriteFile(packPath, append(pack, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	refused := runServer(t, toolCall(t, 1, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts}))[0]["result"].(map[string]any)
	if refused["isError"] != true {
		t.Fatalf("drifted law must refuse the call: %#v", refused)
	}
	if !strings.Contains(toolText(t, refused), "jpack packs lock") {
		t.Fatalf("the refusal steers: %q", toolText(t, refused))
	}
	var envelope struct {
		Diagnostics []struct {
			Code string `json:"code"`
		} `json:"diagnostics"`
		Disposition any `json:"disposition"`
	}
	decodeStructured(t, refused, &envelope)
	if len(envelope.Diagnostics) != 1 || envelope.Diagnostics[0].Code != lock.FailureCode || envelope.Disposition != nil {
		t.Fatalf("envelope = %+v", envelope)
	}
	// A draft is still evaluated over the same project: an undeclared document
	// is never refused for being unlocked.
	drafted := runServer(t, toolCall(t, 1, "experimental_evaluate", map[string]any{"pack": string(pack), "facts": projectFacts}))[0]["result"].(map[string]any)
	if drafted["isError"] != false {
		t.Fatalf("a draft is evaluated: %#v", drafted)
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

// passingMatrix is the CLI fixture's own two-row matrix: one row per judged
// path — a canonical-disposition comparison and an expected §8.4 refusal.
const passingMatrix = `{"matrixVersion":"1","cases":[
  {"id":"hard-fail","facts":` + projectFacts + `,"evidenceAvailability":{"intake-form":"present","sponsor-endorsement":"present"},"expectedDisposition":{"kind":"outcome","outcomeId":"decline-redirect","reasons":[],"handoff":{"state":"none"}}},
  {"id":"undeclared-key","facts":{"request":{"type":"data-access"}},"evidenceAvailability":{"not-a-requirement":"present"},"expectedErrorClass":"malformed-input","expectedErrorPhase":"preflight"}
]}`

// matrixProjectFixture lays out one project whose single pack declares a
// matrix, and points JPACK_CONFIG at it. projectFixture stays matrix-less on
// purpose: its callers pin today's inventory.
func matrixProjectFixture(t *testing.T, config, matrix string) string {
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
	if err := os.WriteFile(filepath.Join(root, "packs", "intake.matrix.json"), []byte(matrix), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, project.DefaultConfigName)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(project.ConfigEnv, configPath)
	return configPath
}

const matrixConfig = `{"configVersion":"1","packs":{"intake":{
  "path":"packs/intake-0.1.0.pack.json",
  "matrix":"packs/intake.matrix.json",
  "description":"Triage an inbound data-access request"
}}}`

// experimental_test_packs runs the declared matrix through the same evaluator
// and the same comparison packs test uses, and returns that command's payload
// (ADR-0021): every row's agreement or divergence, the derived coverage report
// beside the rows, informing and never gating.
func TestExperimentalTestPacksRunsTheDeclaredMatrix(t *testing.T) {
	matrixProjectFixture(t, matrixConfig, passingMatrix)
	outcome := runServer(t, toolCall(t, 1, "experimental_test_packs", nil))[0]["result"].(map[string]any)
	if outcome["isError"] != false {
		t.Fatalf("a passing run is a successful call: %#v", outcome)
	}
	var report result.PackTest
	decodeStructured(t, outcome, &report)
	if report.OutputVersion != "2" || report.Command != "mcp experimental_test_packs" || !report.Experimental {
		t.Fatalf("report = %+v", report)
	}
	if report.ConformanceClaimReference == "" {
		t.Fatalf("the payload must say where the claim is stated: %+v", report)
	}
	if report.EvaluatorSpecVersion != result.EvaluatorSpecVersion {
		t.Fatalf("the applied contract version stays in band: %+v", report)
	}
	if report.Status != "passed" || report.Summary.Total != 2 || report.Summary.Passed != 2 {
		t.Fatalf("summary = %+v status = %q", report.Summary, report.Status)
	}
	if len(report.Packs) != 1 || len(report.Packs[0].Coverage) == 0 {
		t.Fatalf("the derived coverage report travels with the rows: %+v", report.Packs)
	}
}

// A row that diverged is what the caller asked to be told: the call succeeds
// and the payload reports mismatch, exactly as packs test exits nonzero while
// still printing its report.
func TestExperimentalTestPacksReportsAMismatchAsASuccessfulCall(t *testing.T) {
	mismatching := `{"matrixVersion":"1","cases":[
	  {"id":"wrong-expectation","facts":` + projectFacts + `,"evidenceAvailability":{"intake-form":"present","sponsor-endorsement":"present"},"expectedDisposition":{"kind":"outcome","outcomeId":"approve-standard","reasons":[],"handoff":{"state":"none"}}}
	]}`
	matrixProjectFixture(t, matrixConfig, mismatching)
	outcome := runServer(t, toolCall(t, 1, "experimental_test_packs", nil))[0]["result"].(map[string]any)
	if outcome["isError"] != false {
		t.Fatalf("a mismatching run is a successful call: %#v", outcome)
	}
	var report result.PackTest
	decodeStructured(t, outcome, &report)
	if report.Status != "mismatch" || report.Summary.Mismatched != 1 {
		t.Fatalf("report = %+v", report)
	}
}

// A pack with no matrix is skipped and never passed, and the skip is a
// successful call: a green gate over zero rows would say a project was tested
// when nothing was, and the payload's own status is what says so here.
func TestExperimentalTestPacksReportsNoMatrixAsSkipped(t *testing.T) {
	projectFixture(t)
	outcome := runServer(t, toolCall(t, 1, "experimental_test_packs", nil))[0]["result"].(map[string]any)
	if outcome["isError"] != false {
		t.Fatalf("a skipped run is a successful call: %#v", outcome)
	}
	var report result.PackTest
	decodeStructured(t, outcome, &report)
	if report.Status != "skipped" || len(report.Packs) != 1 || report.Packs[0].Status != "skipped" {
		t.Fatalf("report = %+v", report)
	}
}

// pack_id selects one declared pack exactly as --id does, and an unknown id is
// a tool error naming the ids the project does declare.
func TestExperimentalTestPacksSelectsOnePackById(t *testing.T) {
	matrixProjectFixture(t, matrixConfig, passingMatrix)
	responses := runServer(t, strings.Join([]string{
		toolCall(t, 1, "experimental_test_packs", map[string]any{"pack_id": "intake"}),
		toolCall(t, 2, "experimental_test_packs", map[string]any{"pack_id": "no-such-pack"}),
	}, ""))
	selected := responses[0]["result"].(map[string]any)
	if selected["isError"] != false {
		t.Fatalf("selecting a declared pack must succeed: %#v", selected)
	}
	var report result.PackTest
	decodeStructured(t, selected, &report)
	if report.Status != "passed" || len(report.Packs) != 1 || report.Packs[0].ID != "intake" {
		t.Fatalf("report = %+v", report)
	}
	unknown := responses[1]["result"].(map[string]any)
	if unknown["isError"] != true || !strings.Contains(toolText(t, unknown), "intake") {
		t.Fatalf("an unknown id is refused naming the known ones: %#v", unknown)
	}
}

// Argument refusals precede the run: an unknown key, a null, a non-string, and
// a present-but-empty pack_id are each invocation failures, never a silently
// different run. Presence is the discriminator, so an empty id a client
// computed must not select every declared pack.
func TestExperimentalTestPacksRefusesBadArguments(t *testing.T) {
	matrixProjectFixture(t, matrixConfig, passingMatrix)
	responses := runServer(t, strings.Join([]string{
		toolCall(t, 1, "experimental_test_packs", map[string]any{"packId": "intake"}),
		toolCall(t, 2, "experimental_test_packs", map[string]any{"pack_id": nil}),
		toolCall(t, 3, "experimental_test_packs", map[string]any{"pack_id": 7}),
		toolCall(t, 4, "experimental_test_packs", map[string]any{"pack_id": ""}),
		// A JSON null for the whole arguments object is a malformed invocation,
		// not "run everything": decoded, it would silently select the tool's
		// most expensive operation.
		message(t, 5, "tools/call", map[string]any{"name": "experimental_test_packs", "arguments": nil}),
	}, ""))
	for index, response := range responses {
		outcome := response["result"].(map[string]any)
		if outcome["isError"] != true {
			t.Fatalf("call %d must be refused: %#v", index+1, outcome)
		}
	}
	if text := toolText(t, responses[3]["result"].(map[string]any)); !strings.Contains(text, "present but empty") {
		t.Fatalf("the empty-id refusal must say what is wrong: %q", text)
	}
	if text := toolText(t, responses[4]["result"].(map[string]any)); !strings.Contains(text, "must be an object") {
		t.Fatalf("the null-arguments refusal must say what is wrong: %q", text)
	}
}

// No configuration is a tool error here, unlike list_packs: an empty inventory
// answers "what can this project decide", but a skipped suite does not answer
// "run the suite", and a caller reading it as green is the misreading the
// refusal prevents. The error carries the same note the empty inventory does.
func TestExperimentalTestPacksWithNoConfigurationIsAToolError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), project.DefaultConfigName)
	t.Setenv(project.ConfigEnv, missing)
	outcome := runServer(t, toolCall(t, 1, "experimental_test_packs", nil))[0]["result"].(map[string]any)
	if outcome["isError"] != true {
		t.Fatalf("no configuration must refuse the run: %#v", outcome)
	}
	if text := toolText(t, outcome); !strings.Contains(text, "No project configuration was found") {
		t.Fatalf("the refusal must say where the runtime looked: %q", text)
	}
}

// A matrix run is a rehearsal, not a decision: in a project that declares an
// audit directory, experimental_test_packs appends nothing at all (ADR-0018,
// ADR-0021), where the same project's experimental_evaluate would append one
// record per completed call.
func TestExperimentalTestPacksRecordsNothing(t *testing.T) {
	auditConfig := `{"configVersion":"3","audit":{"dir":"audit"},"packs":{"intake":{
	  "path":"packs/intake-0.1.0.pack.json",
	  "matrix":"packs/intake.matrix.json"
	}}}`
	configPath := matrixProjectFixture(t, auditConfig, passingMatrix)
	outcome := runServer(t, toolCall(t, 1, "experimental_test_packs", nil))[0]["result"].(map[string]any)
	if outcome["isError"] != false {
		t.Fatalf("the run must succeed: %#v", outcome)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(configPath), "audit", audit.FileName)); !os.IsNotExist(err) {
		t.Fatalf("a rehearsal must leave no audit record: stat err = %v", err)
	}
}

// The reviewed set is not consulted: a matrix row rehearses law rather than
// deciding under it (ADR-0019, ADR-0021), so a declared pack that drifted from
// its lock still runs and reports what its rows did — where the same project's
// experimental_evaluate would refuse the drift fail-closed.
func TestExperimentalTestPacksConsultsNoReviewedSet(t *testing.T) {
	configPath := matrixProjectFixture(t, matrixConfig, passingMatrix)
	loaded, failure := project.Load(configPath)
	if failure != nil {
		t.Fatal(failure.Message)
	}
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
	loaded.Close()
	// Drift the declared pack's bytes without changing what it evaluates.
	packPath := filepath.Join(filepath.Dir(configPath), "packs", "intake-0.1.0.pack.json")
	drifted, err := os.ReadFile(packPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(packPath, append(drifted, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	outcome := runServer(t, toolCall(t, 1, "experimental_test_packs", nil))[0]["result"].(map[string]any)
	if outcome["isError"] != false {
		t.Fatalf("a drifted pack still rehearses: %#v", outcome)
	}
	var report result.PackTest
	decodeStructured(t, outcome, &report)
	if report.Status != "passed" {
		t.Fatalf("report = %+v", report)
	}
}

// The wire and the shell serve one report structure from one code path: the
// MCP structured payload equals a direct (*Project).Test run with only the
// command naming the surface. The JSON *text* encodings still differ — the CLI
// renderer disables HTML escaping and appends a newline, and configPath
// follows each surface's locator spelling — so this pins the structure, not
// the bytes.
func TestExperimentalTestPacksMatchesThePacksTestPayload(t *testing.T) {
	configPath := matrixProjectFixture(t, matrixConfig, passingMatrix)
	outcome := runServer(t, toolCall(t, 1, "experimental_test_packs", nil))[0]["result"].(map[string]any)
	var wire result.PackTest
	decodeStructured(t, outcome, &wire)

	engine, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	loaded, failure := project.Load(configPath)
	if failure != nil {
		t.Fatal(failure.Message)
	}
	defer loaded.Close()
	shell, projectFailure := loaded.Test(evaluation.NewEngine(engine), "", "packs test")
	if projectFailure != nil {
		t.Fatal(projectFailure.Message)
	}
	if wire.Command != "mcp experimental_test_packs" || shell.Command != "packs test" {
		t.Fatalf("commands name their surfaces: %q vs %q", wire.Command, shell.Command)
	}
	wire.Command = ""
	shell.Command = ""
	if !jsonEqual(t, wire, shell) {
		t.Fatalf("the two surfaces must report one run identically:\nwire  = %+v\nshell = %+v", wire, shell)
	}
}

// A declared matrix path that escapes the project root is refused at the read,
// through the same rooted handle every project file goes through — and the
// refusal is reported in-band exactly as the CLI reports it: the pack's entry
// is a mismatch whose detail names the read failure, and no escaped byte
// reaches the payload.
func TestExperimentalTestPacksRefusesAnEscapingMatrixPath(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "secret.json"), []byte(`{"secret":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "project")
	if err := os.MkdirAll(filepath.Join(root, "packs"), 0o755); err != nil {
		t.Fatal(err)
	}
	pack, err := os.ReadFile(filepath.Join("..", "evaluation", "testdata", "data-request-intake-triage.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "packs", "intake.json"), pack, 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, project.DefaultConfigName)
	config := `{"configVersion":"1","packs":{"intake":{"path":"packs/intake.json","matrix":"../secret.json"}}}`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(project.ConfigEnv, configPath)

	outcome := runServer(t, toolCall(t, 1, "experimental_test_packs", nil))[0]["result"].(map[string]any)
	if outcome["isError"] != false {
		t.Fatalf("the refusal is reported in-band, as the CLI reports it: %#v", outcome)
	}
	var report result.PackTest
	decodeStructured(t, outcome, &report)
	if report.Status != "mismatch" || len(report.Packs) != 1 || report.Packs[0].Detail == "" {
		t.Fatalf("report = %+v", report)
	}
	if strings.Contains(toolText(t, outcome), `"secret"`) {
		t.Fatalf("no escaped byte may reach the payload: %q", toolText(t, outcome))
	}
}

// A report over the response bound is refused with its size and the command
// that streams the same report — never truncated, because a truncated suite
// report under-reports silently (ADR-0021). The bound is lowered here so the
// refusal is testable without building a multi-gigabyte report.
func TestExperimentalTestPacksRefusesAnOversizedReport(t *testing.T) {
	matrixProjectFixture(t, matrixConfig, passingMatrix)
	bound := maxMatrixResultBytes
	maxMatrixResultBytes = 1024
	defer func() { maxMatrixResultBytes = bound }()
	outcome := runServer(t, toolCall(t, 1, "experimental_test_packs", nil))[0]["result"].(map[string]any)
	if outcome["isError"] != true {
		t.Fatalf("an oversized report must be refused: %#v", outcome)
	}
	if text := toolText(t, outcome); !strings.Contains(text, "jpack packs test --format json") {
		t.Fatalf("the refusal must name the command that can carry the report: %q", text)
	}
}

// The boundary family repeats a fact pointer and a decimal literal in a probe
// name and again in a missing sentence, and §2.1's carrier admits a megabyte of
// either. Rendered whole, a pack well inside every carrier limit — four
// maximum-length pointer/literal pairs, about 8 MiB, one passing row — pushed
// the report past this surface's response bound, turning a call that used to
// succeed into a refusal. An additive JSON member that removes an existing
// call is a compatibility break, so every authored string the family renders is
// capped at a fixed budget with a digest tail (ADR-0023): the report stays
// small, the call still succeeds, and the four names stay distinct and stable
// even though the pointers agree for their whole first megabyte.
func TestExperimentalTestPacksBoundsBoundaryProbesAtCarrierLimits(t *testing.T) {
	// Each pointer and each literal is exactly at the carrier's per-string
	// limit, and the pointers differ only in their final byte, so nothing but
	// the digest tail keeps the rendered names apart.
	shared := strings.Repeat("a", carrier.DefaultMaxStringBytes-2)
	digits := strings.Repeat("9", carrier.DefaultMaxStringBytes-1)
	rules := make([]any, 0, 4)
	for index := range 4 {
		distinguisher := string(rune('a' + index))
		rules = append(rules, map[string]any{
			"id": "rule-" + distinguisher, "description": "review threshold",
			"outcome": "review", "onUnknown": "ignore",
			"when": map[string]any{
				"op": "fact", "path": "/" + shared + distinguisher,
				"operator": "greater-than", "value": digits + strconv.Itoa(index),
			},
		})
	}
	pack, err := json.Marshal(map[string]any{
		"specVersion": "0.2.0-draft",
		"id":          "https://example.invalid/judgment-packs/carrier-bounds",
		"version":     "0.1.0", "title": "Carrier bounds",
		"decision": map[string]any{"intent": "Bound the report at the carrier's own limits.", "question": "Should this be reviewed?"},
		"outcomes": []any{map[string]any{"id": "review", "label": "Review"}, map[string]any{"id": "clear", "label": "Clear"}},
		"rules":    rules, "fallbackOutcome": "review",
	})
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(pack)) >= carrier.HardMaxBytes {
		t.Fatalf("the fixture must stay inside the carrier's own limit: %d bytes", len(pack))
	}

	root := t.TempDir()
	matrix := `{"matrixVersion":"1","cases":[{"id":"unresolved-pointers","facts":{},
	  "expectedDisposition":{"kind":"outcome","outcomeId":"review","reasons":[],"handoff":{"state":"none"}}}]}`
	configPath := filepath.Join(root, project.DefaultConfigName)
	for name, content := range map[string][]byte{
		"pack.json":               pack,
		"matrix.json":             []byte(matrix),
		project.DefaultConfigName: []byte(`{"configVersion":"1","packs":{"bounds":{"path":"pack.json","matrix":"matrix.json"}}}`),
	} {
		if err := os.WriteFile(filepath.Join(root, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(project.ConfigEnv, configPath)

	outcome := runServer(t, toolCall(t, 1, "experimental_test_packs", nil))[0]["result"].(map[string]any)
	if outcome["isError"] != false {
		t.Fatalf("a pack inside every carrier limit must still be reportable: %q", toolText(t, outcome))
	}
	payload := toolText(t, outcome)
	if len(payload) > maxMatrixResultBytes {
		t.Fatalf("the report is %d bytes, over the %d-byte response bound", len(payload), maxMatrixResultBytes)
	}
	// Not merely inside the bound: the rendered size no longer follows the
	// pack's, so an 8 MiB pack reports in kilobytes.
	if len(payload) > 64<<10 {
		t.Fatalf("the report grew with the pack: %d bytes", len(payload))
	}

	var report result.PackTest
	decodeStructured(t, outcome, &report)
	if report.Status != "passed" || len(report.Packs) != 1 {
		t.Fatalf("every row holds; only the boundaries are unstated: %+v", report)
	}
	var names []string
	for _, probe := range report.Packs[0].Coverage {
		if !strings.HasPrefix(probe.Probe, "boundary:") {
			continue
		}
		if slices.Contains(names, probe.Probe) {
			t.Fatalf("two boundaries rendered to one name: %q", probe.Probe)
		}
		if len(probe.Probe) > 1024 || len(probe.Detail) > 4096 {
			t.Fatalf("probe %d bytes, detail %d bytes: neither may follow the pack", len(probe.Probe), len(probe.Detail))
		}
		names = append(names, probe.Probe)
	}
	if len(names) != 4 {
		t.Fatalf("four compared pairs are four probes: %v", names)
	}
	// That the same pack renders the same names on every derivation is pinned
	// in the project package, where a second derivation costs no second
	// validation of an 8 MiB document.
}

// --- experimental_test_graphs (issue #95, ADR-0026) ------------------------

// graphProjectFixture copies the graph walk's own fixture project into a
// temporary tree and points the server at it, so these tests exercise the same
// declared matrix the CLI walks rather than a second fixture that could drift.
func graphProjectFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join("..", "graph", "testdata", "project")
	entries, err := os.ReadDir(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(source, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, entry.Name()), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	configPath := filepath.Join(root, project.DefaultConfigName)
	t.Setenv(project.ConfigEnv, configPath)
	return root
}

func TestExperimentalTestGraphsRunsTheDeclaredMatrix(t *testing.T) {
	graphProjectFixture(t)
	outcome := runServer(t, toolCall(t, 1, "experimental_test_graphs", nil))[0]["result"].(map[string]any)
	if outcome["isError"] != false {
		t.Fatalf("a declared graph matrix runs: %#v", outcome)
	}
	var report result.GraphSuite
	decodeStructured(t, outcome, &report)
	if report.Status != "passed" || len(report.Graphs) != 1 || report.Summary.Passed != 3 {
		t.Fatalf("report = %+v", report)
	}
	if report.Command != testGraphsCommand {
		t.Fatalf("the payload names this surface: %q", report.Command)
	}
	if report.ConformanceClaimReference == "" {
		t.Fatalf("the payload carries the claim reference: %+v", report)
	}
}

// The MCP payload is byte-identical to what the graph layer produces for the
// same project, so the transport adds and drops nothing.
//
// Named for what it does: it calls graph.TestProject directly, NOT the CLI, and
// supplies this surface's own command string. It is a layer-parity test, and
// the round-2 review was right that calling it a CLI-parity test claimed more
// than it checks -- the CLI's explicit-path walk reports a different type.
func TestExperimentalTestGraphsMatchesTheGraphLayerPayload(t *testing.T) {
	root := graphProjectFixture(t)
	configPath := filepath.Join(root, project.DefaultConfigName)
	loaded, failure := project.Load(configPath)
	if failure != nil {
		t.Fatal(failure.Message)
	}
	defer loaded.Close()
	direct, graphFailure := graph.TestProject(loaded, evaluation.NewEngine(newTestEngine(t)), "",
		graph.Options{Command: testGraphsCommand})
	if graphFailure != nil {
		t.Fatal(graphFailure.Message)
	}

	outcome := runServer(t, toolCall(t, 1, "experimental_test_graphs", nil))[0]["result"].(map[string]any)
	var overMCP result.GraphSuite
	decodeStructured(t, outcome, &overMCP)

	wantJSON, err := json.Marshal(direct)
	if err != nil {
		t.Fatal(err)
	}
	gotJSON, err := json.Marshal(overMCP)
	if err != nil {
		t.Fatal(err)
	}
	if string(wantJSON) != string(gotJSON) {
		t.Fatalf("MCP and the graph layer disagree:\n mcp         = %s\n graph layer = %s", gotJSON, wantJSON)
	}
}

func TestExperimentalTestGraphsSelectsOneGraphById(t *testing.T) {
	graphProjectFixture(t)
	outcome := runServer(t, toolCall(t, 1, "experimental_test_graphs",
		map[string]any{"graph_id": "onboarding"}))[0]["result"].(map[string]any)
	if outcome["isError"] != false {
		t.Fatalf("a configured graph id selects one graph: %#v", outcome)
	}
	var report result.GraphSuite
	decodeStructured(t, outcome, &report)
	if len(report.Graphs) != 1 || report.Graphs[0].ID != "onboarding" {
		t.Fatalf("report = %+v", report)
	}

	unknown := runServer(t, toolCall(t, 1, "experimental_test_graphs",
		map[string]any{"graph_id": "nope"}))[0]["result"].(map[string]any)
	if unknown["isError"] != true {
		t.Fatalf("an unknown graph id is a tool error: %#v", unknown)
	}
}

// The advertised schema is object with an optional string and an optional array
// of strings. Everything that is not that is refused rather than silently
// becoming a different run.
func TestExperimentalTestGraphsRefusesBadArguments(t *testing.T) {
	graphProjectFixture(t)
	for _, tt := range []struct {
		name string
		raw  string
	}{
		{name: "a literal null", raw: `null`},
		{name: "an unknown member", raw: `{"graphs":"onboarding"}`},
		{name: "a non-string graph id", raw: `{"graph_id":7}`},
		{name: "a present but empty graph id", raw: `{"graph_id":""}`},
		{name: "a null extension collection", raw: `{"supported_extensions":null}`},
		{name: "a null extension element", raw: `{"supported_extensions":[null]}`},
		{name: "a non-string extension element", raw: `{"supported_extensions":[7]}`},
		{name: "an extension collection that is not an array", raw: `{"supported_extensions":"a"}`},
		// encoding/json matches member names case-insensitively, so these bind
		// to the real fields and pass a decoder alone. additionalProperties:
		// false means the exact spelling (round-2 review).
		{name: "an upper-case graph id member", raw: `{"GRAPH_ID":"onboarding"}`},
		{name: "an upper-case extensions member", raw: `{"SUPPORTED_EXTENSIONS":[]}`},
		{name: "a camel-cased graph id member", raw: `{"graphId":"onboarding"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			outcome := runServer(t, rawToolCall(t, 1, "experimental_test_graphs", tt.raw))[0]["result"].(map[string]any)
			if outcome["isError"] != true {
				t.Fatalf("%s must be refused: %#v", tt.name, outcome)
			}
		})
	}
}

// Omitting supported_extensions and passing an empty array are the same run,
// and an empty string or a duplicate inside it is inert rather than an error:
// the evaluator treats capabilities as a set (design review, answer 1).
func TestExperimentalTestGraphsTreatsExtensionsAsASet(t *testing.T) {
	graphProjectFixture(t)
	payloads := map[string]string{
		"omitted":    `{}`,
		"empty":      `{"supported_extensions":[]}`,
		"duplicated": `{"supported_extensions":["x","x",""]}`,
	}
	reports := map[string]string{}
	for name, raw := range payloads {
		outcome := runServer(t, rawToolCall(t, 1, "experimental_test_graphs", raw))[0]["result"].(map[string]any)
		if outcome["isError"] != false {
			t.Fatalf("%s must be accepted: %#v", name, outcome)
		}
		var report result.GraphSuite
		decodeStructured(t, outcome, &report)
		encoded, err := json.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		reports[name] = string(encoded)
	}
	if reports["omitted"] != reports["empty"] {
		t.Fatal("an omitted and an empty supported_extensions must be the same run")
	}
	if reports["omitted"] != reports["duplicated"] {
		t.Fatal("an empty string and a duplicate must be inert")
	}
}

func TestExperimentalTestGraphsWithNoConfigurationIsAToolError(t *testing.T) {
	t.Setenv(project.ConfigEnv, filepath.Join(t.TempDir(), "absent.json"))
	outcome := runServer(t, toolCall(t, 1, "experimental_test_graphs", nil))[0]["result"].(map[string]any)
	if outcome["isError"] != true {
		t.Fatalf("no configuration is a tool error, not a skipped suite: %#v", outcome)
	}
}

// A report past the budget is refused as the suite accumulates, not after a
// multi-gigabyte one exists (design review F1). The bound is lowered so the
// refusal is testable without building one.
func TestExperimentalTestGraphsRefusesAnOversizedReport(t *testing.T) {
	graphProjectFixture(t)
	bound := maxMatrixResultBytes
	maxMatrixResultBytes = 512
	defer func() { maxMatrixResultBytes = bound }()
	outcome := runServer(t, toolCall(t, 1, "experimental_test_graphs", nil))[0]["result"].(map[string]any)
	if outcome["isError"] != true {
		t.Fatalf("an oversized report must be refused: %#v", outcome)
	}
	if text := toolText(t, outcome); !strings.Contains(text, "under-report silently") {
		t.Fatalf("the refusal must say why it is not truncated: %q", text)
	}
}

// Two independent invariants, tested separately because a future change could
// restore one without the other (design review F3): the run appends no audit
// record even where the project configures a trail, and it consults no
// reviewed-set lock even where one is present and wrong.
func TestExperimentalTestGraphsWritesNothingAndConsultsNoLock(t *testing.T) {
	t.Run("no audit record where the project configures a trail", func(t *testing.T) {
		root := graphProjectFixture(t)
		configPath := filepath.Join(root, project.DefaultConfigName)
		raw, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatal(err)
		}
		var config map[string]any
		if err := json.Unmarshal(raw, &config); err != nil {
			t.Fatal(err)
		}
		// configVersion 3 is what declares an audit trail; the key is "dir".
		config["configVersion"] = "3"
		config["audit"] = map[string]any{"dir": "audit"}
		amended, err := json.Marshal(config)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(configPath, amended, 0o600); err != nil {
			t.Fatal(err)
		}

		outcome := runServer(t, toolCall(t, 1, "experimental_test_graphs", nil))[0]["result"].(map[string]any)
		if outcome["isError"] != false {
			t.Fatalf("a configured audit trail must not change the run: %#v", outcome)
		}
		if entries, err := os.ReadDir(filepath.Join(root, "audit")); err == nil && len(entries) > 0 {
			t.Fatalf("a matrix run is a rehearsal and must append nothing: %d entries", len(entries))
		}
	})

	t.Run("no lock consultation where a wrong lock is present", func(t *testing.T) {
		root := graphProjectFixture(t)
		// A lock naming digests nothing in this project matches. A run that
		// consulted it would refuse; a rehearsal never looks.
		lock := `{"lockVersion":"1","packs":{"sanctions-screening":{"sha256":"` + strings.Repeat("0", 64) + `"}}}`
		if err := os.WriteFile(filepath.Join(root, "jpack.lock.json"), []byte(lock), 0o600); err != nil {
			t.Fatal(err)
		}
		outcome := runServer(t, toolCall(t, 1, "experimental_test_graphs", nil))[0]["result"].(map[string]any)
		if outcome["isError"] != false {
			t.Fatalf("a matrix run consults no reviewed set: %#v", outcome)
		}
		var report result.GraphSuite
		decodeStructured(t, outcome, &report)
		if report.Status != "passed" {
			t.Fatalf("a drifted lock must not touch a rehearsal: %+v", report)
		}
	})
}

// A call declaring rehearsal writes nothing in a project that asked for a
// trail, and its payload says what it is; the undeclared call beside it writes
// the one record. The declaration is held to its type: an explicit null is a
// bad invocation, never a silent false, because a declaration this member
// exists to make explicit must not be manufactured by a decoding accident
// (ADR-0028).
func TestExperimentalEvaluateRehearsalWritesNothing(t *testing.T) {
	root := t.TempDir()
	pack, err := os.ReadFile(filepath.Join("..", "evaluation", "testdata", "data-request-intake-triage.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "packs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "packs", "intake-0.1.0.pack.json"), pack, 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, project.DefaultConfigName)
	config := `{"configVersion":"3","audit":{"dir":"audit"},"packs":{"intake":{"path":"packs/intake-0.1.0.pack.json"}}}`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(project.ConfigEnv, configPath)

	responses := runServer(t, strings.Join([]string{
		toolCall(t, 1, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts, "rehearsal": true}),
		toolCall(t, 2, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts}),
		toolCall(t, 3, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts, "rehearsal": nil}),
	}, ""))
	if responses[0]["result"].(map[string]any)["isError"] != false {
		t.Fatalf("the rehearsal must succeed: %#v", responses[0])
	}
	var rehearsed result.Evaluation
	decodeStructured(t, responses[0]["result"].(map[string]any), &rehearsed)
	if !rehearsed.Rehearsal || rehearsed.Disposition.Kind == "" {
		t.Fatalf("a rehearsal is a labeled, complete evaluation: %+v", rehearsed)
	}
	if responses[1]["result"].(map[string]any)["isError"] != false {
		t.Fatalf("the undeclared call must succeed: %#v", responses[1])
	}
	var recorded result.Evaluation
	decodeStructured(t, responses[1]["result"].(map[string]any), &recorded)
	if recorded.Rehearsal {
		t.Fatalf("an undeclared call carries no rehearsal label: %+v", recorded)
	}
	if responses[2]["result"].(map[string]any)["isError"] != true {
		t.Fatalf("an explicit null is a bad invocation, never a silent false: %#v", responses[2])
	}

	data, err := os.ReadFile(filepath.Join(root, "audit", audit.FileName))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("the undeclared call is the one record, the rehearsal none: %q", data)
	}
}

// rehearsalFixture is one audited single-pack project for the rehearsal rows,
// returning its root and the pack bytes.
func rehearsalFixture(t *testing.T) (string, []byte) {
	t.Helper()
	root := t.TempDir()
	pack, err := os.ReadFile(filepath.Join("..", "evaluation", "testdata", "data-request-intake-triage.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "packs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "packs", "intake-0.1.0.pack.json"), pack, 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, project.DefaultConfigName)
	config := `{"configVersion":"3","audit":{"dir":"audit"},"packs":{"intake":{"path":"packs/intake-0.1.0.pack.json"}}}`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(project.ConfigEnv, configPath)
	return root, pack
}

// The rehearsal declaration is held to its exact spelling and its exact type,
// and an explicit false is the ordinary recorded call. encoding/json case-folds
// member names, so without the exactMembers hold a "REHEARSAL" key would bind
// to the rehearsal field and reach the guards that turn recording and the lock
// consult off — the one spelling the closed schema advertises is the one
// spelling that does anything, and every other shape is an error carrying no
// disposition at all.
func TestExperimentalEvaluateRehearsalArgumentIsHeldExactly(t *testing.T) {
	root, _ := rehearsalFixture(t)

	booleanError := `The "rehearsal" argument must be a JSON boolean; null and every other type are rejected. Omit the key to leave the argument unsupplied.`
	responses := runServer(t, strings.Join([]string{
		toolCall(t, 1, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts, "rehearsal": false}),
		toolCall(t, 2, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts, "rehearsal": nil}),
		toolCall(t, 3, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts, "REHEARSAL": true}),
		toolCall(t, 4, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts, "rehearsal": "true"}),
		toolCall(t, 5, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts, "rehearsal": 1}),
		toolCall(t, 6, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts, "rehearsal": map[string]any{}}),
		toolCall(t, 7, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts, "rehearsal": []any{}}),
		toolCall(t, 8, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts, "zzz": true, "aaa": true}),
	}, ""))
	if responses[0]["result"].(map[string]any)["isError"] != false {
		t.Fatalf("an explicit false is the ordinary call: %#v", responses[0])
	}
	var recorded result.Evaluation
	decodeStructured(t, responses[0]["result"].(map[string]any), &recorded)
	if recorded.Rehearsal {
		t.Fatalf("false is not a declaration: %+v", recorded)
	}
	// Full-message equality, not substrings: the diagnostic is part of the
	// contract, and call 8's two unknown members pin the sorted, deterministic
	// choice of which one the message names.
	for index, want := range map[int]string{
		1: booleanError,
		2: `The "experimental_evaluate" arguments carry an unknown member "REHEARSAL"; the accepted members are pack and pack_id and facts and evidence and supported_extensions and rehearsal, spelled exactly.`,
		3: booleanError,
		4: booleanError,
		5: booleanError,
		6: booleanError,
		7: `The "experimental_evaluate" arguments carry an unknown member "aaa"; the accepted members are pack and pack_id and facts and evidence and supported_extensions and rehearsal, spelled exactly.`,
	} {
		response := responses[index]["result"].(map[string]any)
		if response["isError"] != true {
			t.Fatalf("call %d must be refused: %#v", index+1, response)
		}
		if text := toolText(t, response); text != want {
			t.Fatalf("call %d error = %q, want %q", index+1, text, want)
		}
		if structured, present := response["structuredContent"]; present {
			if _, evaluated := structured.(map[string]any)["disposition"]; evaluated {
				t.Fatalf("call %d must carry no disposition: %#v", index+1, structured)
			}
		}
	}

	// The one recorded call above is the explicit-false one; every refused
	// shape recorded nothing because nothing was evaluated.
	data, err := os.ReadFile(filepath.Join(root, "audit", audit.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(strings.TrimSpace(string(data)), "\n"); len(lines) != 1 {
		t.Fatalf("records = %d, want the explicit-false call's one: %q", len(lines), data)
	}
}

// A rehearsal changes exactly one member of the tool payload — the label.
// Marshaling both structured payloads after deleting only "rehearsal" from the
// declared one discriminates any rehearsal-conditional change to any other
// member in one comparison.
func TestExperimentalEvaluateRehearsalChangesOnlyTheLabel(t *testing.T) {
	rehearsalFixture(t)

	responses := runServer(t, strings.Join([]string{
		toolCall(t, 1, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts, "rehearsal": true}),
		toolCall(t, 2, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts}),
	}, ""))
	rehearsed := responses[0]["result"].(map[string]any)["structuredContent"].(map[string]any)
	recorded := responses[1]["result"].(map[string]any)["structuredContent"].(map[string]any)
	if rehearsed["rehearsal"] != true {
		t.Fatalf("the declared call is labeled: %#v", rehearsed)
	}
	delete(rehearsed, "rehearsal")
	if !reflect.DeepEqual(rehearsed, recorded) {
		t.Fatalf("a rehearsal changes only the label:\nrehearsed %#v\nrecorded  %#v", rehearsed, recorded)
	}
}

// A rehearsal consults no reviewed set on this surface either: law that has
// left the reviewed set refuses the ordinary call and still rehearses. The
// refusal is asserted first, so the row cannot pass while the lock catches
// nothing, and the trail stays empty because neither run recorded — one was
// refused before evaluating, the other was declared a rehearsal.
func TestExperimentalEvaluateRehearsalSkipsTheLock(t *testing.T) {
	root, _ := rehearsalFixture(t)
	configPath := filepath.Join(root, project.DefaultConfigName)
	loaded, failure := project.Load(configPath)
	if failure != nil {
		t.Fatalf("loading the project: %+v", failure)
	}
	document, lockFailure := lock.Generate(loaded)
	if lockFailure != nil {
		t.Fatalf("generating the lock: %+v", lockFailure)
	}
	encoded, err := lock.Encode(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := loaded.WriteLock(encoded); err != nil {
		t.Fatal(err)
	}
	// The fixture's own project handle is done before the server opens its
	// own: a loaded project holds its directory descriptor open.
	if err := loaded.Close(); err != nil {
		t.Fatal(err)
	}
	packPath := filepath.Join(root, "packs", "intake-0.1.0.pack.json")
	body, err := os.ReadFile(packPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(packPath, append(body, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	responses := runServer(t, strings.Join([]string{
		toolCall(t, 1, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts}),
		toolCall(t, 2, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts, "rehearsal": true}),
	}, ""))
	if responses[0]["result"].(map[string]any)["isError"] != true {
		t.Fatalf("the ordinary call must be refused under drifted law: %#v", responses[0])
	}
	if responses[1]["result"].(map[string]any)["isError"] != false {
		t.Fatalf("the declared rehearsal evaluates: %#v", responses[1])
	}
	var rehearsed result.Evaluation
	decodeStructured(t, responses[1]["result"].(map[string]any), &rehearsed)
	if !rehearsed.Rehearsal || rehearsed.Disposition.Kind == "" {
		t.Fatalf("a rehearsal under drifted law is still a labeled, complete evaluation: %+v", rehearsed)
	}
	if _, err := os.Stat(filepath.Join(root, "audit", audit.FileName)); err == nil {
		t.Fatal("neither run records: the refusal evaluated nothing and the rehearsal declared nothing")
	}
}

// The graph inventory and document tools serve the configured graphs exactly
// as list_packs and get_pack serve the packs (ADR-0029): the configured id
// beside the document's own identity, the exact bytes as the text half, and
// listing that survives what validation would refuse.
func TestExperimentalGraphInventoryAndDocument(t *testing.T) {
	fixture, err := filepath.Abs(filepath.Join("..", "graph", "testdata", "project", "jpack.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(project.ConfigEnv, fixture)

	responses := runServer(t, strings.Join([]string{
		toolCall(t, 1, "experimental_list_graphs", map[string]any{}),
		toolCall(t, 2, "experimental_get_graph", map[string]any{"graph_id": "onboarding"}),
		toolCall(t, 3, "experimental_get_graph", map[string]any{"graph_id": "missing"}),
		toolCall(t, 4, "experimental_get_graph", map[string]any{"graph_id": nil}),
		toolCall(t, 5, "experimental_get_graph", map[string]any{"GRAPH_ID": "onboarding"}),
		toolCall(t, 6, "experimental_get_graph", map[string]any{}),
	}, ""))

	var inventory result.GraphInventory
	decodeStructured(t, responses[0]["result"].(map[string]any), &inventory)
	if inventory.Status != "resolved" || !inventory.Experimental || len(inventory.Graphs) != 1 {
		t.Fatalf("inventory = %+v", inventory)
	}
	row := inventory.Graphs[0]
	if row.ID != "onboarding" || row.GraphID != "vendor-onboarding-flow" || row.GraphVersion != "0.1.0" ||
		row.FormatVersion != "1" || row.ResultNode != "onboarding" ||
		row.NodeCount == nil || *row.NodeCount != 2 || row.EdgeCount == nil || *row.EdgeCount != 1 ||
		!row.RowsDeclared || row.RowsPath != "onboarding.rows.json" || row.Detail != "" {
		t.Fatalf("row = %+v", row)
	}
	// The wire keys themselves: present counts by their new names, and never
	// the array-typed names the walk payloads own.
	rawRow, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	var wireRow map[string]any
	if err := json.Unmarshal(rawRow, &wireRow); err != nil {
		t.Fatal(err)
	}
	if wireRow["nodeCount"] != float64(2) || wireRow["edgeCount"] != float64(1) || wireRow["rowsDeclared"] != true {
		t.Fatalf("wire row = %s", rawRow)
	}
	for _, legacy := range []string{"nodes", "edges", "rows"} {
		if _, present := wireRow[legacy]; present {
			t.Fatalf("the walk payloads own %q; the inventory must not reuse it: %s", legacy, rawRow)
		}
	}

	served := responses[1]["result"].(map[string]any)
	if served["isError"] != false {
		t.Fatalf("the configured graph must be served: %#v", served)
	}
	var meta result.GraphDocument
	decodeStructured(t, served, &meta)
	if meta.Status != "valid" || meta.ID != "onboarding" || meta.GraphID != "vendor-onboarding-flow" ||
		meta.GraphVersion != "0.1.0" || meta.FormatVersion != "1" || meta.ResultNode != "onboarding" {
		t.Fatalf("meta = %+v", meta)
	}
	disk, err := os.ReadFile(filepath.Join(filepath.Dir(fixture), "onboarding.graph.json"))
	if err != nil {
		t.Fatal(err)
	}
	if text := toolText(t, served); text != string(disk) {
		t.Fatalf("the served bytes are the file's exact bytes: %d vs %d", len(text), len(disk))
	}
	if meta.Bytes != len(disk) {
		t.Fatalf("bytes = %d, want %d", meta.Bytes, len(disk))
	}
	sum := sha256.Sum256(disk)
	if meta.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("sha256 = %q", meta.SHA256)
	}

	unknown := responses[2]["result"].(map[string]any)
	if unknown["isError"] != true || !strings.Contains(toolText(t, unknown), "onboarding") {
		t.Fatalf("an unknown id lists the configured ids: %#v", unknown)
	}
	if null := responses[3]["result"].(map[string]any); null["isError"] != true {
		t.Fatalf("an explicit null is a bad invocation: %#v", null)
	}
	alias := responses[4]["result"].(map[string]any)
	if alias["isError"] != true || !strings.Contains(toolText(t, alias), `unknown member "GRAPH_ID"`) {
		t.Fatalf("a case-folded alias is held to the exact spelling: %#v", alias)
	}
	if absent := responses[5]["result"].(map[string]any); absent["isError"] != true ||
		!strings.Contains(toolText(t, absent), `"graph_id" argument is required`) {
		t.Fatalf("an absent id is required: %#v", absent)
	}
}

// Listing is not validating, and serving is not validating either: a graph
// document that does not decode is listed with the reason and served with
// status undecodable — the mid-edit moment is the moment the tools must not
// go silent.
func TestExperimentalGraphToolsServeTheUndecodable(t *testing.T) {
	// The real fixture project, with only the graph document replaced by bytes
	// that do not decode: everything else must keep working around it.
	source := filepath.Join("..", "graph", "testdata", "project")
	root := t.TempDir()
	for _, name := range []string{"jpack.json", "onboarding.rows.json", "sanctions-screening-0.1.0.pack.json", "vendor-onboarding-0.1.0.pack.json"} {
		data, err := os.ReadFile(filepath.Join(source, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "onboarding.graph.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "jpack.json")
	t.Setenv(project.ConfigEnv, configPath)

	responses := runServer(t, strings.Join([]string{
		toolCall(t, 1, "experimental_list_graphs", map[string]any{}),
		toolCall(t, 2, "experimental_get_graph", map[string]any{"graph_id": "onboarding"}),
	}, ""))

	var inventory result.GraphInventory
	decodeStructured(t, responses[0]["result"].(map[string]any), &inventory)
	if len(inventory.Graphs) != 1 || inventory.Graphs[0].GraphID != "" {
		t.Fatalf("an undecodable graph is listed with identity empty: %+v", inventory.Graphs)
	}
	if detail := inventory.Graphs[0].Detail; !strings.HasPrefix(detail, `The file "onboarding.graph.json" could not be used:`) {
		t.Fatalf("the detail carries the pack-style path framing: %q", detail)
	}

	served := responses[1]["result"].(map[string]any)
	if served["isError"] != false {
		t.Fatalf("the undecodable graph is still served: %#v", served)
	}
	var meta result.GraphDocument
	decodeStructured(t, served, &meta)
	if meta.Status != "undecodable" || meta.Detail == "" || meta.GraphID != "" {
		t.Fatalf("meta = %+v", meta)
	}
	if toolText(t, served) != "{not json" {
		t.Fatalf("the served bytes are the file's exact bytes: %q", toolText(t, served))
	}
}

// With no configuration at all, the inventory answers empty with its own
// explanation and the fetch is an error that says where the runtime looked —
// the exact asymmetry list_packs and get_pack draw, for the same reasons.
func TestExperimentalGraphToolsWithoutConfiguration(t *testing.T) {
	t.Setenv(project.ConfigEnv, filepath.Join(t.TempDir(), "jpack.json"))

	responses := runServer(t, strings.Join([]string{
		toolCall(t, 1, "experimental_list_graphs", map[string]any{}),
		toolCall(t, 2, "experimental_get_graph", map[string]any{"graph_id": "onboarding"}),
	}, ""))

	var inventory result.GraphInventory
	decodeStructured(t, responses[0]["result"].(map[string]any), &inventory)
	if responses[0]["result"].(map[string]any)["isError"] != false || inventory.Status != "none" || inventory.Note == "" {
		t.Fatalf("no configuration is an empty answer with an explanation: %#v", responses[0])
	}
	fetch := responses[1]["result"].(map[string]any)
	if fetch["isError"] != true || !strings.Contains(toolText(t, fetch), "No project configuration was found") {
		t.Fatalf("a fetch by id cannot succeed emptily: %#v", fetch)
	}
}

// The wire shapes of the graph payloads, asserted on the raw JSON rather than
// through the production structs, so a renamed tag cannot survive by being
// renamed in both places at once — plus the shapes only a hostile fixture
// reaches: a future formatVersion served as declared, counts that go absent
// rather than zero, bytes no text frame can carry, a document past the byte
// limit, and a configured path that tries to leave the root.
func TestExperimentalGraphWireShapes(t *testing.T) {
	symlinks := runtime.GOOS != "windows"
	source := filepath.Join("..", "graph", "testdata", "project")
	root := t.TempDir()
	for _, name := range []string{"onboarding.rows.json", "sanctions-screening-0.1.0.pack.json", "vendor-onboarding-0.1.0.pack.json"} {
		data, err := os.ReadFile(filepath.Join(source, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Decodable, schema-invalid, and from the future: served as declared, with
	// counts absent because neither member is collection-shaped.
	future := `{"formatVersion":"future","id":"time-traveller","version":"9.9.9","nodes":[],"edges":{},"result":7}`
	if err := os.WriteFile(filepath.Join(root, "future.graph.json"), []byte(future), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "binary.graph.json"), []byte{0x7b, 0xff, 0xfe, 0x7d}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "huge.graph.json"), append(bytes.Repeat([]byte(" "), 1<<20), '{', '}'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "binary.pack.json"), []byte{0x7b, 0xff, 0xfe, 0x7d}, 0o600); err != nil {
		t.Fatal(err)
	}
	graphEntries := `"future":{"path":"future.graph.json"},` +
		`"binary":{"path":"binary.graph.json"},` +
		`"huge":{"path":"huge.graph.json"}`
	if symlinks {
		outside := t.TempDir()
		if err := os.WriteFile(filepath.Join(outside, "secret.graph.json"), []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(outside, "secret.graph.json"), filepath.Join(root, "escape.graph.json")); err != nil {
			t.Fatal(err)
		}
		graphEntries += `,"escape":{"path":"escape.graph.json"}`
	}
	config := `{"configVersion":"2","packs":{` +
		`"screening":{"path":"sanctions-screening-0.1.0.pack.json"},` +
		`"binpack":{"path":"binary.pack.json"}},"graphs":{` + graphEntries + `}}`
	configPath := filepath.Join(root, project.DefaultConfigName)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(project.ConfigEnv, configPath)

	calls := []string{
		toolCall(t, 1, "experimental_list_graphs", map[string]any{}),
		toolCall(t, 2, "experimental_get_graph", map[string]any{"graph_id": "future"}),
		toolCall(t, 3, "experimental_get_graph", map[string]any{"graph_id": "binary"}),
		toolCall(t, 4, "experimental_get_graph", map[string]any{"graph_id": "huge"}),
		toolCall(t, 5, "get_pack", map[string]any{"pack_id": "binpack"}),
	}
	if symlinks {
		calls = append(calls, toolCall(t, 6, "experimental_get_graph", map[string]any{"graph_id": "escape"}))
	}
	responses := runServer(t, strings.Join(calls, ""))

	// Raw wire keys, not struct round-trips.
	raw, err := json.Marshal(responses[0]["result"].(map[string]any)["structuredContent"])
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["experimental"] != true || wire["status"] != "resolved" {
		t.Fatalf("inventory wire = %s", raw)
	}
	wantRows := 3
	if symlinks {
		wantRows = 4
	}
	rows, ok := wire["graphs"].([]any)
	if !ok || len(rows) != wantRows {
		t.Fatalf(`"graphs" is a JSON array of the configured rows: %s`, raw)
	}
	byID := map[string]map[string]any{}
	for _, item := range rows {
		row := item.(map[string]any)
		byID[row["id"].(string)] = row
	}
	futureRow := byID["future"]
	if futureRow["formatVersion"] != "future" || futureRow["graphId"] != "time-traveller" {
		t.Fatalf("a declared future format is served as declared: %v", futureRow)
	}
	if _, present := futureRow["nodeCount"]; present {
		t.Fatalf("a non-collection nodes member has no count, not zero: %v", futureRow)
	}
	if _, present := futureRow["edgeCount"]; present {
		t.Fatalf("a non-collection edges member has no count, not zero: %v", futureRow)
	}
	if futureRow["rowsDeclared"] != false {
		t.Fatalf("rowsDeclared is the boolean wire member: %v", futureRow)
	}
	detailed := []string{"binary", "huge"}
	if symlinks {
		detailed = append(detailed, "escape")
	}
	for _, id := range detailed {
		row := byID[id]
		if detail, _ := row["detail"].(string); detail == "" {
			t.Fatalf("%s is listed with the reason: %v", id, row)
		}
	}
	if !strings.Contains(byID["huge"]["detail"].(string), "byte limit") {
		t.Fatalf("the oversized detail names the limit: %v", byID["huge"])
	}

	served := responses[1]["result"].(map[string]any)
	rawMeta, err := json.Marshal(served["structuredContent"])
	if err != nil {
		t.Fatal(err)
	}
	var meta map[string]any
	if err := json.Unmarshal(rawMeta, &meta); err != nil {
		t.Fatal(err)
	}
	if meta["experimental"] != true || meta["status"] != "valid" || meta["formatVersion"] != "future" ||
		meta["sha256"] == "" || meta["graphId"] != "time-traveller" {
		t.Fatalf("document wire = %s", rawMeta)
	}
	if toolText(t, served) != future {
		t.Fatalf("served bytes = %q", toolText(t, served))
	}

	// A text frame carries UTF-8 and nothing else: invalid bytes are refused
	// on BOTH fetch tools, naming the quoted configured path, never transcoded
	// into agreement-breaking replacements.
	binary := responses[2]["result"].(map[string]any)
	if binary["isError"] != true || !strings.Contains(toolText(t, binary), "not valid UTF-8") ||
		!strings.Contains(toolText(t, binary), `"binary.graph.json"`) {
		t.Fatalf("invalid UTF-8 is refused with the quoted path: %#v", binary)
	}
	if oversized := responses[3]["result"].(map[string]any); oversized["isError"] != true {
		t.Fatalf("an oversized document is a refusal: %#v", oversized)
	}
	binpack := responses[4]["result"].(map[string]any)
	if binpack["isError"] != true || !strings.Contains(toolText(t, binpack), "not valid UTF-8") ||
		!strings.Contains(toolText(t, binpack), `"binary.pack.json"`) {
		t.Fatalf("get_pack refuses invalid UTF-8 with the quoted path: %#v", binpack)
	}
	if symlinks {
		if escape := responses[5]["result"].(map[string]any); escape["isError"] != true {
			t.Fatalf("a path leaving the root is refused: %#v", escape)
		}
	}
}

// The two missing-configuration notes are one string, asserted exactly on both
// tools, so they cannot drift apart — and get_pack now refuses the case-folded
// alias exactly as the graph fetch does, closing the live PACK_ID bypass.
func TestFetchToolsHoldArgumentsAndNotesExactly(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "jpack.json")
	t.Setenv(project.ConfigEnv, missing)

	responses := runServer(t, strings.Join([]string{
		toolCall(t, 1, "experimental_list_graphs", map[string]any{}),
		toolCall(t, 2, "experimental_get_graph", map[string]any{"graph_id": "x"}),
		toolCall(t, 3, "get_pack", map[string]any{"PACK_ID": "intake"}),
	}, ""))

	var inventory result.GraphInventory
	decodeStructured(t, responses[0]["result"].(map[string]any), &inventory)
	if inventory.Note == "" || toolText(t, responses[1]["result"].(map[string]any)) != inventory.Note {
		t.Fatalf("the fetch error carries the listing's own note, byte for byte:\nnote  %q\nerror %q",
			inventory.Note, toolText(t, responses[1]["result"].(map[string]any)))
	}
	alias := responses[2]["result"].(map[string]any)
	if alias["isError"] != true ||
		toolText(t, alias) != `The "get_pack" arguments carry an unknown member "PACK_ID"; the accepted member is "pack_id", spelled exactly.` {
		t.Fatalf("the case-folded pack alias is held to the exact spelling: %#v", alias)
	}
}
