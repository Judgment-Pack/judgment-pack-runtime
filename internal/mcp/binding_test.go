package mcp

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// bindingDigest is a well-formed binding digest.
var bindingDigest = "sha256:" + strings.Repeat("0123456789abcdef", 4)

// bindingProject writes a project that keeps a trail and points the server
// at it, returning its directory.
func bindingProject(t *testing.T) string {
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
	if err := os.WriteFile(configPath, []byte(`{"configVersion":"3","audit":{"dir":"audit"},"packs":{"intake":{"path":"packs/intake-0.1.0.pack.json"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(project.ConfigEnv, configPath)
	return root
}

// The binding argument is accepted and refused as the CLI's --binding is
// (ADR-0050): the object itself, held to its shape and recorded as given; a
// call that passes one not of the shape, or passes one while declaring a
// rehearsal, is a tool error before anything is evaluated, and leaves
// nothing.
func TestExperimentalEvaluateRecordsTheBindingAsGiven(t *testing.T) {
	refused := bindingProject(t)
	shape := `The "binding" argument must be one JSON object of exactly {"kind": string, "digest": string}: `
	refusals := []struct {
		binding   any
		rehearsal bool
		want      string
	}{
		{map[string]any{"kind": "k", "digest": bindingDigest, "note": "secret-value"}, false, shape + "it does not have exactly the two members kind and digest."},
		{map[string]any{"kind": "k"}, false, shape + "it does not have exactly the two members kind and digest."},
		{"not an object", false, shape + "it is not one JSON object with each member named once."},
		{nil, false, shape + "it is not one JSON object with each member named once."},
		{[]any{}, false, shape + "it is not one JSON object with each member named once."},
		{map[string]any{"kind": "Secret Value", "digest": bindingDigest}, false, shape + "kind must be a string of 1 to 128 characters from a-z, 0-9, dot, underscore, slash and hyphen."},
		{map[string]any{"kind": "k", "digest": "sha256:SECRET"}, false, shape + "digest must be a string of sha256: and 64 lowercase hexadecimal characters."},
		{map[string]any{"kind": strings.Repeat("a", 128), "digest": bindingDigest, "pad": strings.Repeat(" ", 900)}, false, shape + "it is longer than 1024 bytes."},
		{map[string]any{"kind": "k", "digest": bindingDigest}, true, `The "binding" argument is not taken with "rehearsal": true: a rehearsal records nothing, so there is no record for the binding to be on.`},
	}
	calls := []string{}
	for index, refusal := range refusals {
		arguments := map[string]any{"pack_id": "intake", "facts": projectFacts, "binding": refusal.binding}
		if refusal.rehearsal {
			arguments["rehearsal"] = true
		}
		calls = append(calls, toolCall(t, index+1, "experimental_evaluate", arguments))
	}
	responses := runServer(t, strings.Join(calls, ""))
	for index, refusal := range refusals {
		outcome := responses[index]["result"].(map[string]any)
		text := toolText(t, outcome)
		if outcome["isError"] != true || text != refusal.want {
			t.Errorf("call %d: %v %q, want %q", index+1, outcome["isError"], text, refusal.want)
		}
		if strings.Contains(text, "secret-value") || strings.Contains(text, "Secret Value") || strings.Contains(text, "SECRET") {
			t.Errorf("call %d repeats the argument: %q", index+1, text)
		}
	}
	if _, err := os.Stat(filepath.Join(refused, "audit")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a refused call creates nothing: %v", err)
	}

	root := bindingProject(t)
	given := map[string]any{"digest": bindingDigest, "kind": "judgment-pack-runner/preparation/1"}
	responses = runServer(t, strings.Join([]string{
		toolCall(t, 1, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts, "binding": given}),
		toolCall(t, 2, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts}),
		toolCall(t, 3, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts, "binding": given, "rehearsal": false}),
	}, ""))
	for index, response := range responses {
		if outcome := response["result"].(map[string]any); outcome["isError"] != false {
			t.Fatalf("call %d: %#v", index+1, outcome)
		}
	}
	data, err := os.ReadFile(filepath.Join(root, "audit", audit.FileName))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	member := `"binding":{"kind":"judgment-pack-runner/preparation/1","digest":"` + bindingDigest + `"}`
	if len(lines) != 3 || strings.Count(lines[0], member) != 1 || strings.Contains(lines[1], `"binding"`) || strings.Count(lines[2], member) != 1 {
		t.Fatalf("the binding as given on the bound calls' records, and none on the other: %q", data)
	}
}

// The binding argument's advertised schema states what the handler enforces:
// a client that generates arguments from the schema cannot produce a binding
// the handler refuses for its shape, and the reverse.
func TestTheBindingSchemaStatesWhatTheHandlerEnforces(t *testing.T) {
	responses := runServer(t, message(t, 1, "tools/list", nil))
	var schema map[string]any
	for _, tool := range responses[0]["result"].(map[string]any)["tools"].([]any) {
		if tool.(map[string]any)["name"] == "experimental_evaluate" {
			schema, _ = tool.(map[string]any)["inputSchema"].(map[string]any)["properties"].(map[string]any)["binding"].(map[string]any)
		}
	}
	if schema == nil {
		t.Fatal("experimental_evaluate advertises no binding argument")
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := validation.CompileSchema(encoded, "urn:judgmentpack:test:binding")
	if err != nil {
		t.Fatalf("the advertised binding schema does not compile: %v", err)
	}
	admits := func(document string) bool {
		instance, err := jsonschema.UnmarshalJSON(strings.NewReader(document))
		return err == nil && compiled.Validate(instance) == nil
	}
	for _, value := range []string{
		`{"kind":"judgment-pack-runner/preparation/1","digest":"` + bindingDigest + `"}`,
		`{"digest":"` + bindingDigest + `","kind":"a.b_c/d-0"}`,
		`{"kind":"` + strings.Repeat("a", 128) + `","digest":"` + bindingDigest + `"}`,
		`{"kind":"` + strings.Repeat("a", 129) + `","digest":"` + bindingDigest + `"}`,
		`{"kind":"","digest":"` + bindingDigest + `"}`,
		`{"kind":"A","digest":"` + bindingDigest + `"}`,
		`{"kind":"a b","digest":"` + bindingDigest + `"}`,
		`{"kind":"a:b","digest":"` + bindingDigest + `"}`,
		`{"kind":1,"digest":"` + bindingDigest + `"}`,
		`{"kind":"k","digest":"sha256:` + strings.Repeat("A", 64) + `"}`,
		`{"kind":"k","digest":"sha256:` + strings.Repeat("a", 63) + `"}`,
		`{"kind":"k","digest":"sha256:` + strings.Repeat("a", 65) + `"}`,
		`{"kind":"k","digest":"` + strings.Repeat("a", 64) + `"}`,
		`{"kind":"k"}`,
		`{"digest":"` + bindingDigest + `"}`,
		`{"kind":"k","digest":"` + bindingDigest + `","x":1}`,
		`{}`,
		`[]`,
		`null`,
	} {
		_, err := audit.ParseBinding([]byte(value))
		if (err == nil) != admits(value) {
			t.Errorf("%s: handler admits=%v, schema admits=%v", value, err == nil, admits(value))
		}
	}
}
