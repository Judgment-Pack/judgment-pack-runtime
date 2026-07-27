package mcp

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/conformance"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/describe"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

func message(t *testing.T, id int, method string, params any) string {
	t.Helper()
	msg := map[string]any{"jsonrpc": "2.0", "method": method}
	if id >= 0 {
		msg["id"] = id
	}
	if params != nil {
		msg["params"] = params
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	return string(data) + "\n"
}

func runServer(t *testing.T, input string) []map[string]any {
	t.Helper()
	engine, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(engine, conformance.NewRunner(engine))
	var out, logw bytes.Buffer
	if err := server.Serve(strings.NewReader(input), &out, &logw); err != nil {
		t.Fatalf("serve: %v (log %q)", err, logw.String())
	}
	var responses []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			t.Fatalf("undecodable response line %q: %v", line, err)
		}
		responses = append(responses, decoded)
	}
	return responses
}

func TestServerLifecycleToolsAndValidate(t *testing.T) {
	set, err := artifacts.Load(artifacts.DraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	validDoc, err := set.Case("valid/minimal-literal.json")
	if err != nil {
		t.Fatal(err)
	}

	input := strings.Join([]string{
		message(t, 1, "initialize", map[string]any{"protocolVersion": "2025-06-18"}),
		message(t, -1, "notifications/initialized", nil), // notification: no response
		message(t, 2, "tools/list", nil),
		message(t, 3, "tools/call", map[string]any{"name": "validate", "arguments": map[string]any{"document": string(validDoc)}}),
		message(t, 4, "tools/call", map[string]any{"name": "validate", "arguments": map[string]any{"document": `{"specVersion":"0.1.0-draft"}`}}),
		message(t, 5, "no/such/method", nil),
	}, "")

	responses := runServer(t, input)
	if len(responses) != 5 {
		t.Fatalf("expected 5 responses (the notification produces none), got %d: %#v", len(responses), responses)
	}

	// initialize
	initResult, ok := responses[0]["result"].(map[string]any)
	if !ok || initResult["protocolVersion"] != "2025-06-18" {
		t.Fatalf("initialize result: %#v", responses[0])
	}
	if serverInfo := initResult["serverInfo"].(map[string]any); serverInfo["name"] != "judgment-pack" {
		t.Fatalf("serverInfo: %#v", serverInfo)
	}
	if _, hasTools := initResult["capabilities"].(map[string]any)["tools"]; !hasTools {
		t.Fatalf("capabilities should advertise tools: %#v", initResult["capabilities"])
	}

	// tools/list advertises exactly the expected set by name, and get_example
	// requires "name" — a bare count would miss a rename or a dropped requirement.
	tools := responses[1]["result"].(map[string]any)["tools"].([]any)
	advertised := map[string]map[string]any{}
	for _, entry := range tools {
		tool := entry.(map[string]any)
		advertised[tool["name"].(string)] = tool
	}
	for _, want := range []string{"validate", "test_conformance", "get_schema", "describe_runtime", "list_examples", "get_example", "experimental_evaluate"} {
		if _, ok := advertised[want]; !ok {
			t.Fatalf("tools/list must advertise %q; got %v", want, advertised)
		}
	}
	if len(advertised) != 7 {
		t.Fatalf("expected 7 distinct tools, got %d", len(advertised))
	}
	required := advertised["get_example"]["inputSchema"].(map[string]any)["required"].([]any)
	if len(required) != 1 || required[0] != "name" {
		t.Fatalf("get_example must require exactly [name], got %v", required)
	}

	// validate (valid document)
	validCall := responses[2]["result"].(map[string]any)
	if validCall["isError"] != false {
		t.Fatalf("valid document should not be a tool error: %#v", validCall)
	}
	if structured := validCall["structuredContent"].(map[string]any); structured["status"] != "valid" {
		t.Fatalf("valid document status: %v", structured["status"])
	}

	// validate (invalid document) is still a successful call reporting "invalid"
	invalidCall := responses[3]["result"].(map[string]any)
	if invalidCall["isError"] != false {
		t.Fatalf("invalid document is a successful call, not a tool error: %#v", invalidCall)
	}
	if structured := invalidCall["structuredContent"].(map[string]any); structured["status"] != "invalid" {
		t.Fatalf("invalid document status: %v", structured["status"])
	}

	// unknown method -> JSON-RPC method-not-found
	rpcErr, ok := responses[4]["error"].(map[string]any)
	if !ok || rpcErr["code"].(float64) != codeMethodNotFound {
		t.Fatalf("unknown method should be a method-not-found error: %#v", responses[4])
	}
}

func TestValidateToolRequiresDocument(t *testing.T) {
	responses := runServer(t, message(t, 1, "tools/call", map[string]any{"name": "validate", "arguments": map[string]any{}}))
	result := responses[0]["result"].(map[string]any)
	if result["isError"] != true {
		t.Fatalf("a missing document should be an in-band tool error: %#v", result)
	}
}

// The example tools surface the embedded valid fixtures read-only: list_examples
// enumerates them, and get_example returns one document as text so a
// filesystem-less client can seed a Create by validating what it gets back.
func TestExampleToolsSurfaceEmbeddedFixtures(t *testing.T) {
	input := strings.Join([]string{
		message(t, 1, "tools/call", map[string]any{"name": "list_examples", "arguments": map[string]any{}}),
		message(t, 2, "tools/call", map[string]any{"name": "get_example", "arguments": map[string]any{"name": "minimal-literal"}}),
		message(t, 3, "tools/call", map[string]any{"name": "get_example", "arguments": map[string]any{"name": "no-such-example"}}),
	}, "")
	responses := runServer(t, input)

	// list_examples: a successful call whose structured payload is labeled a
	// fixture, not an authored template, and enumerates the bundled examples.
	list := responses[0]["result"].(map[string]any)
	if list["isError"] != false {
		t.Fatalf("list_examples should not be a tool error: %#v", list)
	}
	listed := list["structuredContent"].(map[string]any)
	if listed["kind"] != "version-pinned-conformance-fixture" {
		t.Fatalf("examples must be labeled a fixture, not a template: %#v", listed["kind"])
	}
	names := map[string]bool{}
	for _, entry := range listed["examples"].([]any) {
		names[entry.(map[string]any)["name"].(string)] = true
	}
	if !names["minimal-literal"] {
		t.Fatalf("list_examples should enumerate minimal-literal: %#v", listed["examples"])
	}

	// get_example: the document is returned as text and byte-compared to the
	// embedded fixture (a stronger check than re-validating it). Its structured
	// metadata must describe the text actually returned, and must be the exact
	// payload the shared describe seam produces — so the MCP surface cannot drift
	// from the CLI, which is pinned to the same seam in the cli package's tests.
	got := responses[1]["result"].(map[string]any)
	if got["isError"] != false {
		t.Fatalf("get_example should not be a tool error: %#v", got)
	}
	document := got["content"].([]any)[0].(map[string]any)["text"].(string)
	set, err := artifacts.Load(artifacts.DraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	want, err := set.Case("valid/minimal-literal.json")
	if err != nil {
		t.Fatal(err)
	}
	if document != string(want) {
		t.Fatalf("get_example returned bytes that differ from the embedded fixture")
	}
	// The reported digest and size must describe the returned text, at the wire boundary.
	structured := got["structuredContent"].(map[string]any)
	sum := sha256.Sum256([]byte(document))
	if structured["sha256"] != hex.EncodeToString(sum[:]) {
		t.Fatalf("structuredContent.sha256 does not describe the returned text: %v", structured["sha256"])
	}
	if int(structured["bytes"].(float64)) != len(document) {
		t.Fatalf("structuredContent.bytes = %v, want %d", structured["bytes"], len(document))
	}
	// The whole payload must equal the shared describe output verbatim.
	meta, _, err := describe.Example(set, "minimal-literal", "mcp get_example")
	if err != nil {
		t.Fatal(err)
	}
	var wantMeta map[string]any
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(metaBytes, &wantMeta); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(structured, wantMeta) {
		t.Fatalf("MCP structuredContent drifted from describe.Example:\n got=%v\nwant=%v", structured, wantMeta)
	}

	// get_example with an unknown name is an in-band tool error, not a crash.
	unknown := responses[2]["result"].(map[string]any)
	if unknown["isError"] != true {
		t.Fatalf("an unknown example name should be an in-band tool error: %#v", unknown)
	}
}

// experimental_evaluate is labeled, produces a disposition for a conformant
// pack, and refuses a non-conformant pack as an in-band tool error.
func TestExperimentalEvaluateTool(t *testing.T) {
	pack, err := os.ReadFile(filepath.Join("..", "evaluation", "testdata", "data-request-intake-triage.json"))
	if err != nil {
		t.Fatal(err)
	}
	facts := `{"request":{"type":"one-time-extract","completeness":"incomplete","appropriateness":"pass","embargoedInformationToUnauthorizedRecipients":false}}`
	evidence := `{"intake-form":"present","sponsor-endorsement":"present"}`
	input := strings.Join([]string{
		message(t, 1, "tools/call", map[string]any{"name": "experimental_evaluate", "arguments": map[string]any{"pack": string(pack), "facts": facts, "evidence": evidence}}),
		message(t, 2, "tools/call", map[string]any{"name": "experimental_evaluate", "arguments": map[string]any{"pack": `{"specVersion":"0.1.0-draft"}`, "facts": `{}`}}),
	}, "")
	responses := runServer(t, input)

	evaluated := responses[0]["result"].(map[string]any)
	if evaluated["isError"] != false {
		t.Fatalf("a produced disposition is a successful call: %#v", evaluated)
	}
	structured := evaluated["structuredContent"].(map[string]any)
	if structured["experimental"] != true || structured["conformanceClaim"] != "none" {
		t.Fatalf("the payload must be labeled experimental with no claim: %#v", structured)
	}
	disposition := structured["disposition"].(map[string]any)
	if disposition["kind"] != "outcome" || disposition["outcomeId"] != "clarify-return" {
		t.Fatalf("disposition = %#v", disposition)
	}

	refused := responses[1]["result"].(map[string]any)
	if refused["isError"] != true {
		t.Fatalf("a non-conformant pack must be an in-band tool error: %#v", refused)
	}
	if text := refused["content"].([]any)[0].(map[string]any)["text"].(string); !strings.Contains(text, "document conformance") {
		t.Fatalf("the refusal must be self-sufficient: %q", text)
	}
}

// The prompts surface (ADR-0008) serves non-normative authoring method as
// static text: capability advertised, three prompts listed with their
// arguments, argument text echoed verbatim into the rendered prompt, and the
// non-normative disclaimer present in every rendering.
func TestPromptsSurface(t *testing.T) {
	input := strings.Join([]string{
		message(t, 1, "initialize", map[string]any{"protocolVersion": "2025-06-18"}),
		message(t, -1, "notifications/initialized", nil),
		message(t, 2, "prompts/list", nil),
		message(t, 3, "prompts/get", map[string]any{"name": "author_pack", "arguments": map[string]any{"policy": "Employees may expense meals under 50 dollars."}}),
		message(t, 4, "prompts/get", map[string]any{"name": "no_such_prompt"}),
	}, "")
	responses := runServer(t, input)

	capabilities := responses[0]["result"].(map[string]any)["capabilities"].(map[string]any)
	if _, ok := capabilities["prompts"]; !ok {
		t.Fatalf("initialize must advertise the prompts capability: %#v", capabilities)
	}

	prompts := responses[1]["result"].(map[string]any)["prompts"].([]any)
	names := map[string]bool{}
	for _, entry := range prompts {
		names[entry.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"author_pack", "test_pack", "fix_pack"} {
		if !names[want] {
			t.Fatalf("prompts/list must include %q: %v", want, names)
		}
	}
	if len(names) != 3 {
		t.Fatalf("expected exactly 3 prompts, got %d", len(names))
	}

	rendered := responses[2]["result"].(map[string]any)
	text := rendered["messages"].([]any)[0].(map[string]any)["content"].(map[string]any)["text"].(string)
	if !strings.Contains(text, "Employees may expense meals under 50 dollars.") {
		t.Fatalf("the policy argument must be echoed verbatim into the prompt")
	}
	for _, marker := range []string{"non-normative", "validate", "onUnknown"} {
		if !strings.Contains(text, marker) {
			t.Fatalf("author_pack rendering must contain %q", marker)
		}
	}

	if _, isError := responses[3]["error"]; !isError {
		t.Fatalf("an unknown prompt must be a JSON-RPC error: %#v", responses[3])
	}
}

// Every prompt renders with no arguments at all, and every rendering carries
// the no-claim disclaimer -- the guardrail that the method text can never be
// read as the runtime blessing a pack.
func TestEveryPromptRendersWithDisclaimer(t *testing.T) {
	for _, name := range []string{"author_pack", "test_pack", "fix_pack"} {
		responses := runServer(t, message(t, 1, "prompts/get", map[string]any{"name": name}))
		result := responses[0]["result"].(map[string]any)
		text := result["messages"].([]any)[0].(map[string]any)["content"].(map[string]any)["text"].(string)
		if !strings.Contains(text, "does not make a\npack conformant") && !strings.Contains(text, "does not make a pack conformant") {
			t.Fatalf("%s must carry the non-normative disclaimer", name)
		}
		if len(text) < 500 {
			t.Fatalf("%s rendering suspiciously short: %d bytes", name, len(text))
		}
	}
}
