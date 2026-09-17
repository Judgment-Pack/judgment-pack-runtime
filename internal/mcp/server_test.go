package mcp

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/conformance"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/describe"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
	"github.com/santhosh-tekuri/jsonschema/v6"
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
	if serverInfo := initResult["serverInfo"].(map[string]any); serverInfo["name"] != "jpack" {
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
	for _, want := range []string{"experimental_validate_expectations", "validate", "test_conformance", "get_schema", "describe_runtime", "list_examples", "get_example", "list_packs", "get_pack", "experimental_evaluate", "experimental_test_packs", "experimental_test_graphs", "experimental_list_graphs", "experimental_get_graph"} {
		if _, ok := advertised[want]; !ok {
			t.Fatalf("tools/list must advertise %q; got %v", want, advertised)
		}
	}
	// The rehearsal declaration is advertised exactly as the handler holds it:
	// an optional boolean on a closed schema (ADR-0028). A schema that stopped
	// saying so, or said something looser, would let a bridge drop the member
	// or a client learn it only from an error.
	evaluateSchema := advertised["experimental_evaluate"]["inputSchema"].(map[string]any)
	if evaluateSchema["additionalProperties"] != false {
		t.Fatalf("the evaluate schema is closed: %v", evaluateSchema)
	}
	rehearsalProperty, ok := evaluateSchema["properties"].(map[string]any)["rehearsal"].(map[string]any)
	if !ok || rehearsalProperty["type"] != "boolean" {
		t.Fatalf("the evaluate schema advertises an optional boolean rehearsal: %v", evaluateSchema)
	}
	if required, ok := evaluateSchema["required"].([]any); !ok || len(required) != 1 || required[0] != "facts" {
		t.Fatalf(`"facts" is the one required member — optional means absent from required: %v`, evaluateSchema["required"])
	}
	// The include_traces declaration is advertised the same way (ADR-0031): an
	// optional boolean on a closed schema, with no required member at all.
	graphsSchema := advertised["experimental_test_graphs"]["inputSchema"].(map[string]any)
	if graphsSchema["additionalProperties"] != false {
		t.Fatalf("the test_graphs schema is closed: %v", graphsSchema)
	}
	tracesProperty, ok := graphsSchema["properties"].(map[string]any)["include_traces"].(map[string]any)
	if !ok || tracesProperty["type"] != "boolean" {
		t.Fatalf("the test_graphs schema advertises an optional boolean include_traces: %v", graphsSchema)
	}
	if _, present := graphsSchema["required"]; present {
		t.Fatalf("every test_graphs member is optional: %v", graphsSchema["required"])
	}
	if len(advertised) != 14 {
		t.Fatalf("expected 14 distinct tools, got %d", len(advertised))
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
	if structured["experimental"] != true || structured["conformanceClaimReference"] != result.EvaluationClaimReference {
		t.Fatalf("the payload must name the surface and reference the claim document: %#v", structured)
	}
	// The member is a locator and not a claim, and the removed one is gone: that
	// removal is the machine-output break outputVersion "2" accounts for.
	if structured["conformanceClaimReference"] != "CONFORMANCE.md" {
		t.Fatalf("the in-band reference is the claim document's path: %#v", structured)
	}
	if _, present := structured["conformanceClaim"]; present {
		t.Fatalf("the payload must not carry the removed conformanceClaim member: %#v", structured)
	}
	if structured["outputVersion"] != result.OutputVersion || structured["evaluatorSpecVersion"] != result.EvaluatorSpecVersion {
		t.Fatalf("the payload must name the protocol version and the contract version: %#v", structured)
	}
	disposition := structured["disposition"].(map[string]any)
	if disposition["kind"] != "outcome" || disposition["outcomeId"] != "clarify-return" {
		t.Fatalf("disposition = %#v", disposition)
	}

	refused := responses[1]["result"].(map[string]any)
	if refused["isError"] != true {
		t.Fatalf("a non-conformant pack must be an in-band tool error: %#v", refused)
	}
	text := refused["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "document conformance") {
		t.Fatalf("the refusal must be self-sufficient: %q", text)
	}
	// The refusal names the JPS §8.4 evaluation-error class, so a calling model
	// reads the same coarse identity the CLI reports.
	if !strings.Contains(text, "evaluation error class: pack-not-conformant") || !strings.Contains(text, "phase: preflight") {
		t.Fatalf("the refusal must name its §8.4 class: %q", text)
	}
	// And it carries that identity machine-readably, in the shared envelope, beside
	// the text: class, phase, and the version of the evaluator contract that
	// assigned them.
	assertEvaluationError(t, refused, "pack-not-conformant", "preflight")
}

// assertEvaluationError holds one refused MCP evaluation to the §8.4 contract:
// an in-band tool error whose structuredContent is the shared evaluation-error
// envelope naming the class, the phase, and the evaluator's contract version,
// with this runtime's finer JPS-* code beside them as the detail and no
// disposition anywhere in the payload.
func assertEvaluationError(t *testing.T, refused map[string]any, class, phase string) {
	t.Helper()
	if refused["isError"] != true {
		t.Fatalf("a refused evaluation must be an in-band tool error: %#v", refused)
	}
	structured, ok := refused["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("a refused evaluation must carry the structured envelope: %#v", refused)
	}
	evaluationError, ok := structured["evaluationError"].(map[string]any)
	if !ok {
		t.Fatalf("the envelope must carry evaluationError: %#v", structured)
	}
	if evaluationError["class"] != class || evaluationError["phase"] != phase {
		t.Fatalf("evaluationError = %#v, want class %q phase %q", evaluationError, class, phase)
	}
	if evaluationError["evaluatorSpecVersion"] != result.EvaluatorSpecVersion {
		t.Fatalf("evaluationError.evaluatorSpecVersion = %v, want %q", evaluationError["evaluatorSpecVersion"], result.EvaluatorSpecVersion)
	}
	if structured["command"] != "mcp experimental_evaluate" {
		t.Fatalf("the envelope must name this surface: %v", structured["command"])
	}
	diagnostics, ok := structured["diagnostics"].([]any)
	if !ok || len(diagnostics) == 0 {
		t.Fatalf("the envelope must keep this runtime's finer code as the detail: %#v", structured)
	}
	if code := diagnostics[0].(map[string]any)["code"].(string); !strings.HasPrefix(code, "JPS-") {
		t.Fatalf("diagnostic code = %q, want a JPS-* code", code)
	}
	// An evaluation error is never a disposition, in any member of the payload.
	encoded, err := json.Marshal(refused)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"disposition"`) {
		t.Fatalf("a refused evaluation must carry no disposition: %s", encoded)
	}
}

// §8.2 gives an omitted evidence document and a supplied empty one two different
// meanings, and §8.4 classes the difference. The MCP surface must keep them
// apart: omitted is the implicit empty object and evaluates, present-but-empty is
// malformed-input, an explicit null violates the declared string schema and is an
// argument error, and a present empty pack or facts document enters the preflight
// rather than being reported as an unclassified missing argument.
func TestExperimentalEvaluateDistinguishesOmittedFromEmptyDocuments(t *testing.T) {
	pack, err := os.ReadFile(filepath.Join("..", "evaluation", "testdata", "data-request-intake-triage.json"))
	if err != nil {
		t.Fatal(err)
	}
	// This pack declares two required evidence requirements, so an omitted evidence
	// document leaves both unknown and the evaluation is unresolved rather than an
	// outcome — which is what makes the omitted case visibly different from the
	// supplied one in the first test above.
	facts := `{"request":{"type":"one-time-extract","completeness":"incomplete","appropriateness":"pass","embargoedInformationToUnauthorizedRecipients":false}}`
	call := func(id int, arguments map[string]any) string {
		return message(t, id, "tools/call", map[string]any{"name": "experimental_evaluate", "arguments": arguments})
	}
	responses := runServer(t, strings.Join([]string{
		call(1, map[string]any{"pack": string(pack), "facts": facts}),
		call(2, map[string]any{"pack": string(pack), "facts": facts, "evidence": ""}),
		call(3, map[string]any{"pack": string(pack), "facts": facts, "evidence": nil}),
		call(4, map[string]any{"pack": string(pack), "facts": facts, "evidence": `{"no-such-requirement":"present"}`}),
		call(5, map[string]any{"pack": string(pack), "facts": facts, "evidence": `{"intake-form":"present","sponsor-endorsement":"present"}`}),
		call(6, map[string]any{"pack": "", "facts": facts}),
		call(7, map[string]any{"pack": string(pack), "facts": ""}),
		call(8, map[string]any{"facts": facts}),
		call(9, map[string]any{"pack": string(pack)}),
	}, ""))
	if len(responses) != 9 {
		t.Fatalf("expected 9 responses, got %d", len(responses))
	}
	results := make([]map[string]any, 0, len(responses))
	for _, response := range responses {
		results = append(results, response["result"].(map[string]any))
	}

	// 1. Omitted: the implicit empty object of §8.2, which is not an error. Every
	// declared requirement is unknown, so this pack's required evidence is unknown
	// and the disposition is unresolved.
	omitted := results[0]
	if omitted["isError"] != false {
		t.Fatalf("an omitted evidence document is not an error (§8.2): %#v", omitted)
	}
	disposition := omitted["structuredContent"].(map[string]any)["disposition"].(map[string]any)
	if disposition["kind"] != "unresolved" {
		t.Fatalf("with no evidence document every requirement is unknown: %#v", disposition)
	}

	// 2. Present but empty: a supplied document that is not a JSON text, which the
	// preflight classes malformed-input rather than treating as an absence.
	assertEvaluationError(t, results[1], "malformed-input", "preflight")
	if text := results[1]["content"].([]any)[0].(map[string]any)["text"].(string); !strings.Contains(text, "empty") {
		t.Fatalf("the refusal must say the supplied document was empty: %q", text)
	}

	// 3. Explicit null: the declared input schema says string, so this is an
	// argument-type failure and never a silent omission.
	null := results[2]
	if null["isError"] != true {
		t.Fatalf("an explicit null violates the declared string schema: %#v", null)
	}
	if text := null["content"].([]any)[0].(map[string]any)["text"].(string); !strings.Contains(text, "must be a JSON string") {
		t.Fatalf("a null argument must be reported as an argument-type error: %q", text)
	}
	if _, structured := null["structuredContent"]; structured {
		t.Fatalf("a bad argument never became an evaluation, so §8.4 does not class it: %#v", null)
	}

	// 4. Malformed: an undeclared member name is malformed-input (§8.2).
	assertEvaluationError(t, results[3], "malformed-input", "preflight")

	// 5. Valid: a supplied document naming declared requirements resolves an outcome.
	supplied := results[4]
	if supplied["isError"] != false {
		t.Fatalf("a valid evidence document is a successful call: %#v", supplied)
	}
	if kind := supplied["structuredContent"].(map[string]any)["disposition"].(map[string]any)["kind"]; kind != "outcome" {
		t.Fatalf("disposition kind = %v, want outcome", kind)
	}

	// 6. An empty pack is a supplied document that is not a conforming pack: §8.4's
	// pack-not-conformant, reached at the pack's own place in the preflight.
	assertEvaluationError(t, results[5], "pack-not-conformant", "preflight")

	// 7. An empty facts document is malformed-input, not an unclassified "argument
	// required" error.
	assertEvaluationError(t, results[6], "malformed-input", "preflight")

	// 8-9. An absent required key never became an evaluation: an invocation failure
	// with no §8.4 class at all.
	for _, absent := range []struct {
		index    int
		argument string
	}{{7, "pack"}, {8, "facts"}} {
		missing, argument := results[absent.index], absent.argument
		if missing["isError"] != true {
			t.Fatalf("a missing %q argument must be an in-band tool error: %#v", argument, missing)
		}
		if _, structured := missing["structuredContent"]; structured {
			t.Fatalf("a missing %q argument is not an evaluation error: %#v", argument, missing)
		}
		if text := missing["content"].([]any)[0].(map[string]any)["text"].(string); !strings.Contains(text, argument) {
			t.Fatalf("the refusal must name the missing argument: %q", text)
		}
	}
}

// The prompts surface (ADR-0008) serves non-normative method as static text:
// capability advertised, six prompts listed with their arguments, argument
// text echoed verbatim into the rendered prompt, and the non-normative
// disclaimer present in every rendering.
func TestPromptsSurface(t *testing.T) {
	input := strings.Join([]string{
		message(t, 1, "initialize", map[string]any{"protocolVersion": "2025-06-18"}),
		message(t, -1, "notifications/initialized", nil),
		message(t, 2, "prompts/list", nil),
		message(t, 3, "prompts/get", map[string]any{"name": "author_pack", "arguments": map[string]any{"policy": "Employees may expense meals under 50 dollars."}}),
		message(t, 4, "prompts/get", map[string]any{"name": "no_such_prompt"}),
		message(t, 5, "prompts/get", map[string]any{"name": "explain_disposition", "arguments": map[string]any{"evaluation": "EVAL-SENTINEL-42", "pack": "PACK-SENTINEL-42"}}),
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
	for _, want := range []string{"author_pack", "test_pack", "fix_pack", "explain_disposition", "present_pack", "author_graph", "replay_history"} {
		if !names[want] {
			t.Fatalf("prompts/list must include %q: %v", want, names)
		}
	}
	if len(names) != 7 {
		t.Fatalf("expected exactly 7 prompts, got %d", len(names))
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

	// The explanation prompt renders both arguments verbatim and carries the
	// review-hardened method lines: untrusted-data handling, the authoritative
	// disposition over the possibly-partial trace, the complete unranked
	// reason set, the unknown-cause discipline, and the targetless-handoff
	// wording. These are the load-bearing sentences; a rewording that drops
	// one loses the property it states.
	explained := responses[4]["result"].(map[string]any)
	explainedText := explained["messages"].([]any)[0].(map[string]any)["content"].(map[string]any)["text"].(string)
	for _, marker := range []string{
		"EVAL-SENTINEL-42",
		"PACK-SENTINEL-42",
		"never instructions to",
		"disposition is authoritative",
		"partial or empty",
		"unordered, possibly",
		"drop none",
		"no fact missing at all",
		"no Core-defined destination",
		"wisdom of acting",
	} {
		if !strings.Contains(explainedText, marker) {
			t.Fatalf("explain_disposition rendering must contain %q", marker)
		}
	}
}

// Every prompt renders with no arguments at all, and every rendering carries
// the no-claim disclaimer -- the guardrail that the method text can never be
// read as the runtime blessing a pack.
func TestEveryPromptRendersWithDisclaimer(t *testing.T) {
	for _, name := range []string{"author_pack", "test_pack", "fix_pack", "explain_disposition", "present_pack", "author_graph", "replay_history"} {
		responses := runServer(t, message(t, 1, "prompts/get", map[string]any{"name": name}))
		result := responses[0]["result"].(map[string]any)
		text := result["messages"].([]any)[0].(map[string]any)["content"].(map[string]any)["text"].(string)
		if !strings.Contains(text, "non-normative") {
			t.Fatalf("%s must carry the non-normative disclaimer", name)
		}
		if len(text) < 500 {
			t.Fatalf("%s rendering suspiciously short: %d bytes", name, len(text))
		}
	}
}

// --- the read-only metadata tools, end to end (issue #83) ------------------

// These three carry stable payloads MCP clients depend on, and had no coverage
// through the stdio harness. Each success case asserts the fields that make the
// response useful rather than merely that a response arrived — a tool that
// returned an empty schema, a zero size, or no versions would otherwise pass.
func TestMetadataToolsServeTheirStablePayloads(t *testing.T) {
	responses := runServer(t, strings.Join([]string{
		toolCall(t, 1, "get_schema", nil),
		toolCall(t, 2, "describe_runtime", nil),
		toolCall(t, 3, "test_conformance", nil),
	}, ""))
	if len(responses) != 3 {
		t.Fatalf("expected 3 responses, got %d", len(responses))
	}

	t.Run("get_schema", func(t *testing.T) {
		outcome := responses[0]["result"].(map[string]any)
		if outcome["isError"] != false {
			t.Fatalf("get_schema must succeed: %#v", outcome)
		}
		var payload result.Schema
		decodeStructured(t, outcome, &payload)
		if payload.SpecVersion == "" || payload.SchemaID == "" {
			t.Fatalf("the payload must name the version and schema it served: %+v", payload)
		}

		// The schema itself arrives as the tool's text content, and the
		// structured payload's bytes and sha256 must describe exactly those
		// bytes. A digest that does not describe what was served is worse than
		// no digest, because a caller pins against it.
		served := toolText(t, outcome)
		if len(served) != payload.Bytes {
			t.Fatalf("bytes = %d, served %d", payload.Bytes, len(served))
		}
		sum := sha256.Sum256([]byte(served))
		if got := hex.EncodeToString(sum[:]); got != payload.SHA256 {
			t.Fatalf("sha256 = %q, but the served bytes hash to %q", payload.SHA256, got)
		}
		if !json.Valid([]byte(served)) {
			t.Fatal("the served schema must itself be valid JSON")
		}
	})

	t.Run("describe_runtime", func(t *testing.T) {
		outcome := responses[1]["result"].(map[string]any)
		if outcome["isError"] != false {
			t.Fatalf("describe_runtime must succeed: %#v", outcome)
		}
		var payload struct {
			Tool struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"tool"`
			SupportedSpecVersions []string `json:"supportedSpecVersions"`
			ArtifactProvenance    string   `json:"artifactProvenance"`
		}
		decodeStructured(t, outcome, &payload)
		if payload.Tool.Name == "" || payload.Tool.Version == "" {
			t.Fatalf("the runtime must identify itself: %+v", payload.Tool)
		}
		if len(payload.SupportedSpecVersions) == 0 {
			t.Fatal("a runtime that supports no version cannot validate anything")
		}
		if payload.ArtifactProvenance == "" {
			t.Fatal("the bundled artifacts' provenance must be stated")
		}
		// The versions it claims must be the ones get_schema will actually serve.
		var schema result.Schema
		decodeStructured(t, responses[0]["result"].(map[string]any), &schema)
		if !slices.Contains(payload.SupportedSpecVersions, schema.SpecVersion) {
			t.Fatalf("get_schema served %q, which describe_runtime does not list: %v",
				schema.SpecVersion, payload.SupportedSpecVersions)
		}
	})

	t.Run("test_conformance", func(t *testing.T) {
		outcome := responses[2]["result"].(map[string]any)
		if outcome["isError"] != false {
			t.Fatalf("test_conformance must succeed on the bundled corpus: %#v", outcome)
		}
		var report result.Suite
		decodeStructured(t, outcome, &report)
		if report.Summary.Total == 0 {
			t.Fatal("a run over zero cases is not a conformance run")
		}
		if report.Summary.Mismatched != 0 {
			t.Fatalf("the bundled corpus must pass against its own runtime: %+v", report.Summary)
		}
		if report.Summary.Passed != report.Summary.Total {
			t.Fatalf("every bundled case must pass: %+v", report.Summary)
		}
		if report.CorpusDigest == "" || report.SpecVersion == "" {
			t.Fatalf("the run must name the corpus it ran and the version it targeted: %+v", report)
		}
	})
}

// A bad argument is a tool error carried inside a SUCCESSFUL JSON-RPC response,
// not a top-level protocol error. That distinction is what lets a client tell
// "your call was wrong" from "the transport broke".
func TestMetadataToolsRejectBadArgumentsAsToolErrors(t *testing.T) {
	for _, tt := range []struct {
		name string
		raw  string
	}{
		{name: "test_conformance with a non-object", raw: rawToolCall(t, 1, "test_conformance", `"suite"`)},
		{name: "get_schema with a non-object", raw: rawToolCall(t, 1, "get_schema", `["0.2.0-draft"]`)},
		{name: "get_schema with an unsupported version", raw: toolCall(t, 1, "get_schema", map[string]any{"spec_version": "9.9.9-nope"})},
	} {
		t.Run(tt.name, func(t *testing.T) {
			response := runServer(t, tt.raw)[0]
			if _, isProtocolError := response["error"]; isProtocolError {
				t.Fatalf("a bad argument is a tool error, not a JSON-RPC error: %#v", response)
			}
			outcome, ok := response["result"].(map[string]any)
			if !ok {
				t.Fatalf("expected a result: %#v", response)
			}
			if outcome["isError"] != true {
				t.Fatalf("expected isError true: %#v", outcome)
			}
			if toolText(t, outcome) == "" {
				t.Fatal("a refusal must say something a caller can act on")
			}
		})
	}
}

// --- transport edge cases (issue #85) --------------------------------------

// The server reads one JSON-RPC message per line. These pin what must and must
// NOT produce a response, which is the half a test can silently get wrong: a
// notification that answered would break clients that do not read one.
func TestTransportHandlesLineAndRequestEdgeCases(t *testing.T) {
	for _, tt := range []struct {
		name    string
		input   string
		want    int
		inspect func(*testing.T, []map[string]any)
	}{
		{
			name:  "malformed JSON is a parse error",
			input: "{not json\n",
			want:  1,
			inspect: func(t *testing.T, responses []map[string]any) {
				failure := responses[0]["error"].(map[string]any)
				if int(failure["code"].(float64)) != codeParse {
					t.Fatalf("code = %v, want %d", failure["code"], codeParse)
				}
				if responses[0]["id"] != nil {
					t.Fatalf("a message that never parsed has no id to echo: %#v", responses[0]["id"])
				}
			},
		},
		{
			name:  "blank and whitespace-only lines are skipped",
			input: "\n   \n\t\n" + message(t, 1, "ping", nil),
			want:  1,
		},
		{
			name:  "ping answers with an empty result",
			input: message(t, 7, "ping", nil),
			want:  1,
			inspect: func(t *testing.T, responses []map[string]any) {
				if int(responses[0]["id"].(float64)) != 7 {
					t.Fatalf("ping must echo its id: %#v", responses[0]["id"])
				}
				// MCP's ping carries an EMPTY result object. Echoing an id only
				// shows something answered; a ping that returned a payload would
				// still pass that and would still be wrong. Assertion from
				// Tethys0's independent coverage of this case in PR #110.
				result, ok := responses[0]["result"].(map[string]any)
				if !ok || len(result) != 0 {
					t.Fatalf("ping result = %#v, want an empty object", responses[0]["result"])
				}
			},
		},
		{
			name:  "initialize returns the default protocol version",
			input: message(t, 1, "initialize", map[string]any{}),
			want:  1,
			inspect: func(t *testing.T, responses []map[string]any) {
				got := responses[0]["result"].(map[string]any)["protocolVersion"]
				if got != protocolVersion {
					t.Fatalf("protocolVersion = %v, want %q", got, protocolVersion)
				}
			},
		},
		{
			name:  "a notification produces no response",
			input: message(t, -1, "notifications/initialized", nil),
			want:  0,
		},
		{
			name:  "an unknown notification is ignored",
			input: message(t, -1, "notifications/somethingElse", nil),
			want:  0,
		},
		{
			name:  "a malformed line does not end the stream",
			input: "{not json\n" + message(t, 2, "ping", nil),
			want:  2,
			inspect: func(t *testing.T, responses []map[string]any) {
				if _, isError := responses[0]["error"]; !isError {
					t.Fatalf("the first line must still report a parse error: %#v", responses[0])
				}
				if int(responses[1]["id"].(float64)) != 2 {
					t.Fatalf("the request after it must still be answered: %#v", responses[1])
				}
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			responses := runServer(t, tt.input)
			if len(responses) != tt.want {
				t.Fatalf("got %d responses, want %d: %#v", len(responses), tt.want, responses)
			}
			if tt.inspect != nil {
				tt.inspect(t, responses)
			}
		})
	}
}

func TestTransportAcceptsLineUnderMaxMessageBytes(t *testing.T) {
	base := message(t, 1, "ping", nil)
	pad := maxMessageBytes - len(base) - 64
	big := `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"pad":"` + strings.Repeat("x", pad) + `"}}` + "\n"
	if len(big) >= maxMessageBytes {
		t.Fatalf("under-bound fixture is %d bytes, want less than %d", len(big), maxMessageBytes)
	}

	responses := runServer(t, big)
	if len(responses) != 1 {
		t.Fatalf("got %d responses, want 1", len(responses))
	}
	if int(responses[0]["id"].(float64)) != 1 {
		t.Fatalf("under-bound ping response = %#v, want id 1", responses[0])
	}
}

func TestTransportRefusesOversizedLineAndEndsStream(t *testing.T) {
	engine, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(engine, conformance.NewRunner(engine))

	var out, logw bytes.Buffer
	input := strings.Repeat("x", maxMessageBytes+1) + "\n" + message(t, 1, "ping", nil)
	err = server.Serve(strings.NewReader(input), &out, &logw)
	if err == nil {
		t.Fatal("Serve returned nil for an oversized input line")
	}
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Fatalf("Serve error = %v, want scanner ErrTooLong", err)
	}
	if out.String() != "" {
		t.Fatalf("oversized line must emit no response and stop before later requests, got %q", out.String())
	}
	if !strings.Contains(logw.String(), "mcp: input error:") {
		t.Fatalf("log = %q, want input error diagnostic", logw.String())
	}
}

// The two transport refusals ADR-0037 adds, quoted here so a change to either
// wording is a change to a test and not a silent change to the wire.
const (
	transportNotUTF8   = "Message is not valid UTF-8 JSON."
	transportSurrogate = "Message contains an unpaired surrogate escape at byte offset %d. RFC 8785 §3.2.2.2 makes such a value invalid rather than replaceable, and this runtime refuses it rather than substituting U+FFFD."
)

// wireCall writes one tools/call line byte for byte rather than through
// message(): the defects under test are exactly what a Go encoder repairs or
// re-escapes on the way out, so a marshalled fixture could not carry them.
func wireCall(id int, tool, arguments string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":"%s","arguments":%s}}`, id, tool, arguments) + "\n"
}

// validateArguments is the minimal-literal pack as validate's one string
// argument, carrying title's bytes exactly as given in place of its title.
func validateArguments(t *testing.T, title string) string {
	t.Helper()
	set, err := artifacts.Load(artifacts.DraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	document, err := set.Case("valid/minimal-literal.json")
	if err != nil {
		t.Fatal(err)
	}
	quoted, err := json.Marshal(string(document))
	if err != nil {
		t.Fatal(err)
	}
	const authored = "Minimal literal decision"
	if !bytes.Contains(quoted, []byte(authored)) {
		t.Fatalf("the fixture no longer carries the title this helper rewrites: %s", quoted)
	}
	return `{"document":` + strings.Replace(string(quoted), authored, title, 1) + `}`
}

// expectationArguments is one complete §8.3 disposition as the single string
// element of an expectations batch, carrying outcome's bytes exactly as given.
// The escapes are written doubled because the disposition is itself JSON text
// inside a JSON string: what reaches the wire is one backslash.
func expectationArguments(outcome string) string {
	return `{"spec_version":"` + expectationSpec + `","expectations":["` +
		`{\"kind\":\"outcome\",\"outcomeId\":\"` + outcome + `\",\"reasons\":[],\"handoff\":{\"state\":\"none\"}}"]}`
}

// Before ADR-0037 a request line was admitted on json.Valid alone, and
// json.Valid does not judge UTF-8: encoding/json states that "when unmarshaling
// quoted strings, invalid UTF-8 or invalid UTF-16 surrogate pairs are not
// treated as an error. Instead, they are replaced by the Unicode replacement
// character U+FFFD." Measured against d891b05, validate answered "valid" with
// all three layers passed for a pack whose title carried a raw 0x80 byte and
// again for one carrying a lone \ud800 escape written at the argument level,
// and experimental_validate_expectations answered "valid" with a canonical
// outcomeId reading "allow" followed by U+FFFD — a disposition no caller sent.
// Both defects are parse errors at the transport now, for every string argument
// of every tool, and a refusal is not a session end (issue #153).
func TestTransportRefusesMalformedUnicodeInEveryStringArgument(t *testing.T) {
	for _, tt := range []struct {
		name    string
		line    string
		message string // the exact message expected, when no offset is named
		escape  string // when set, the message must name this escape's own offset
	}{
		{
			name:    "a raw 0x80 byte in validate's document",
			line:    wireCall(1, "validate", validateArguments(t, "Minimal \x80 decision")),
			message: transportNotUTF8,
		},
		{
			name:    "a truncated three-byte sequence in validate's document",
			line:    wireCall(1, "validate", validateArguments(t, "Minimal \xe2\x82 decision")),
			message: transportNotUTF8,
		},
		{
			name:    "a raw 0x80 byte in an expectation",
			line:    wireCall(1, expectationTool, expectationArguments("allow\x80")),
			message: transportNotUTF8,
		},
		{
			name:    "a truncated three-byte sequence in an expectation",
			line:    wireCall(1, expectationTool, expectationArguments("allow\xe2\x82")),
			message: transportNotUTF8,
		},
		{
			name:   "a lone high surrogate escape in validate's document",
			line:   wireCall(1, "validate", validateArguments(t, `Minimal \ud800 decision`)),
			escape: `\ud800`,
		},
		{
			name:   "a lone low surrogate escape in validate's document",
			line:   wireCall(1, "validate", validateArguments(t, `Minimal \udc00 decision`)),
			escape: `\udc00`,
		},
		{
			name:   "a lone high surrogate escape in an expectation",
			line:   wireCall(1, expectationTool, expectationArguments(`allow\ud800`)),
			escape: `\ud800`,
		},
		{
			name:   "a lone low surrogate escape in an expectation",
			line:   wireCall(1, expectationTool, expectationArguments(`allow\udc00`)),
			escape: `\udc00`,
		},
		{
			// The scan reads a backslash as an escape only inside a string, so
			// over bytes that are not JSON its string tracking is a guess: this
			// line's last quote opens a string that never closes. The JSON
			// defect is the one this runtime can locate, so json.Valid is
			// settled first and names it; the scan never sees the line.
			name:    "an unterminated string carrying a lone escape reports the JSON defect",
			line:    `{"jsonrpc":"2.0","id":7,"method":"ping","params":{"a":"\ud800` + "\n",
			message: "Message is not valid JSON.",
		},
		{
			// The mirror image, and the only kind of line whose answer the
			// UTF-8 check's position decides: this one is neither valid UTF-8
			// nor valid JSON, so whichever check runs first names its defect.
			// utf8.Valid runs first, so the encoding defect is named. Both
			// diagnostics are true of this line; which one it gets is the
			// choice ADR-0037 records, and this case is what holds it.
			name:    "a line that is neither valid UTF-8 nor valid JSON reports the encoding defect",
			line:    "{\"a\":\"x\x80\n",
			message: transportNotUTF8,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			refuseAndContinue(t, tt.line, wantRefusal(t, tt.line, tt.escape, tt.message))
		})
	}
}

// Both checks read the whole request line, before the envelope is read, so the
// defect is refused wherever it is written; a tool argument is where it was
// found, not the limit of the rule. That widens what the parse-error branch
// answers, measured against d891b05: {"jsonrpc":"2.0","id":1,"method":
// "pi\ud800ng"} was answered -32601 "Unknown method: pi<U+FFFD>ng" under id 1,
// and the same line with a raw 0x80 in place of the escape the same way;
// {"jsonrpc":"2.0","id":"a\ud800b","method":"ping"} was answered as an ordinary
// ping result; ["\ud800"] was answered -32600 "The request is not a JSON
// object; batches are not supported." under null. All four are -32700 under
// null now, so a client correlating by id sees a null-id parse error where an
// id-bearing error, or a result, used to come back (ADR-0037). These cases are
// also what pins the scope: narrowing either check to lines that carry tool
// arguments leaves every other test in this package green.
func TestTransportRefusesMalformedUnicodeOutsideToolArguments(t *testing.T) {
	for _, tt := range []struct {
		name    string
		line    string
		message string // the exact message expected, when no offset is named
		escape  string // when set, the message must name this escape's own offset
	}{
		{
			name:   "a lone escape in the method name",
			line:   `{"jsonrpc":"2.0","id":1,"method":"pi\ud800ng"}` + "\n",
			escape: `\ud800`,
		},
		{
			name:    "a raw 0x80 byte in the method name",
			line:    "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"pi\x80ng\"}\n",
			message: transportNotUTF8,
		},
		{
			name:   "a lone escape in a string id",
			line:   `{"jsonrpc":"2.0","id":"a\ud800b","method":"ping"}` + "\n",
			escape: `\ud800`,
		},
		{
			name:   "a lone escape in a line that is not an object",
			line:   `["\ud800"]` + "\n",
			escape: `\ud800`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			refuseAndContinue(t, tt.line, wantRefusal(t, tt.line, tt.escape, tt.message))
		})
	}
}

// wantRefusal is the message a case expects: the one it names, or the surrogate
// sentence carrying the offset of the escape in the line the case built, so a
// wrong offset fails.
func wantRefusal(t *testing.T, line, escape, message string) string {
	t.Helper()
	if escape == "" {
		return message
	}
	offset := strings.Index(line, escape)
	if offset < 0 {
		t.Fatalf("the fixture does not carry %s: %q", escape, line)
	}
	return fmt.Sprintf(transportSurrogate, offset)
}

// refuseAndContinue sends one malformed line with an ordinary ping behind it,
// and holds the answer to the shape both transport refusals share: a -32700
// parse error under a null id, carrying no tool result and the exact message —
// with the ping behind it still answered, because a refusal is one message
// refused and not the end of the session.
func refuseAndContinue(t *testing.T, line, want string) {
	t.Helper()
	responses := runServer(t, line+message(t, 99, "ping", nil))
	if len(responses) != 2 {
		t.Fatalf("got %d responses, want the refusal and the request after it: %#v", len(responses), responses)
	}
	refusal := responses[0]
	if _, served := refusal["result"]; served {
		t.Fatalf("a refused line must reach no tool: %#v", refusal)
	}
	id, present := refusal["id"]
	if !present || id != nil {
		t.Fatalf("a parse error is answered under a null id (JSON-RPC §5): %#v", refusal)
	}
	failure, ok := refusal["error"].(map[string]any)
	if !ok || failure["code"].(float64) != codeParse {
		t.Fatalf("refusal = %#v, want a %d parse error", refusal, codeParse)
	}
	if failure["message"] != want {
		t.Fatalf("message = %q, want %q", failure["message"], want)
	}
	if int(responses[1]["id"].(float64)) != 99 {
		t.Fatalf("the request after the refusal must still be answered: %#v", responses[1])
	}
	if _, answered := responses[1]["result"]; !answered {
		t.Fatalf("the request after the refusal must be answered normally: %#v", responses[1])
	}
}

// escapedPair writes one astral character as the two \u escapes JSON gives it,
// built from its own UTF-16 code units rather than spelled out. A literal
// "😀" written in a source file is one careless editor away from
// being folded into the character it names — which would silently turn the
// escape cases below into copies of the literal one beside them, and leave the
// scan's pairing arm untested. Built this way it cannot be folded.
func escapedPair(t *testing.T, astral rune) string {
	t.Helper()
	high, low := utf16.EncodeRune(astral)
	if high == utf8.RuneError || low == utf8.RuneError {
		t.Fatalf("%q is in the basic plane, so it has no surrogate pair", astral)
	}
	return fmt.Sprintf(`\u%04x\u%04x`, high, low)
}

// The other half of the rule: what is well-formed is admitted unchanged. A
// surrogate pair is a character, not a defect, and U+FFFD is an ordinary
// character when a client authored it — only the repair of bytes that were
// never sent is refused.
func TestTransportAcceptsWellFormedUnicodeArguments(t *testing.T) {
	pair, character := escapedPair(t, '\U0001F600'), string(rune(0x1F600))
	topPair, topCharacter := escapedPair(t, '\U0010FFFF'), string(rune(0x10FFFF))
	replacement := string(utf8.RuneError)

	validDocument := func(t *testing.T, response map[string]any) {
		t.Helper()
		result, ok := response["result"].(map[string]any)
		if !ok || result["isError"] != false {
			t.Fatalf("want a successful validate call: %#v", response)
		}
		if structured := result["structuredContent"].(map[string]any); structured["status"] != "valid" {
			t.Fatalf("status = %v, want valid: %#v", structured["status"], structured)
		}
	}

	for _, tt := range []struct {
		name    string
		line    string
		carries string // bytes the composed line must hold, or the case tests something else
		absent  string // bytes it must not hold, which is what keeps these cases apart
		inspect func(*testing.T, map[string]any)
	}{
		{
			name:    "a well-formed surrogate pair escape in validate's document",
			line:    wireCall(1, "validate", validateArguments(t, "Minimal "+pair+" decision")),
			carries: pair,
			absent:  character,
			inspect: validDocument,
		},
		{
			name:    "the same character written as literal UTF-8",
			line:    wireCall(1, "validate", validateArguments(t, "Minimal "+character+" decision")),
			carries: character,
			absent:  `\u`,
			inspect: validDocument,
		},
		{
			// U+10FFFF is the largest scalar value there is, and one of the
			// 1,024 (U+10FC00–U+10FFFF) whose high unit is 0xDBFF, the last
			// high surrogate: the boundary the scan splits high from low on.
			// utf16.EncodeRune reports DBFF DFFF for it and DBFF DC00 for
			// U+10FC00, the bottom of that range. A pair whose high unit is
			// lower is admitted even if the split is off by one, so without a
			// case from this range the split is untested; the value >= 0xDBFF
			// mutation refuses every pair in it, this one included — and this
			// record is what makes it a refusal on the wire.
			name:    "the highest pair there is, U+10FFFF, in validate's document",
			line:    wireCall(1, "validate", validateArguments(t, "Minimal "+topPair+" decision")),
			carries: topPair,
			absent:  topCharacter,
			inspect: validDocument,
		},
		{
			name:    "a literal U+FFFD character in validate's document",
			line:    wireCall(1, "validate", validateArguments(t, "Minimal "+replacement+" decision")),
			carries: replacement,
			absent:  `\u`,
			inspect: validDocument,
		},
		{
			name:    "a well-formed surrogate pair escape in an expectation",
			line:    wireCall(1, expectationTool, expectationArguments("allow"+pair)),
			carries: pair,
			absent:  character,
			inspect: func(t *testing.T, response map[string]any) {
				t.Helper()
				result, ok := response["result"].(map[string]any)
				if !ok || result["isError"] != false {
					t.Fatalf("want a successful expectation call: %#v", response)
				}
				report := result["structuredContent"].(map[string]any)
				row := report["results"].([]any)[0].(map[string]any)
				if row["status"] != "valid" {
					t.Fatalf("row = %#v, want valid", row)
				}
				if canonical := row["canonical"].(string); !strings.Contains(canonical, `"outcomeId":"allow`+character+`"`) {
					t.Fatalf("canonical = %q, want the character the pair the caller sent names", canonical)
				}
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(tt.line, tt.carries) {
				t.Fatalf("the composed line does not carry %q: %q", tt.carries, tt.line)
			}
			if strings.Contains(tt.line, tt.absent) {
				t.Fatalf("the composed line carries %q, so it is not the case it names: %q", tt.absent, tt.line)
			}

			responses := runServer(t, tt.line)
			if len(responses) != 1 {
				t.Fatalf("got %d responses, want 1: %#v", len(responses), responses)
			}
			tt.inspect(t, responses[0])
		})
	}
}

// encoding/json matches struct tags case-insensitively, so DisallowUnknownFields
// alone accepts PACK_ID for pack_id. callTool holds every tool's arguments to
// the spelling its schema advertises, before any handler decodes (issue #115).
func TestToolsRefuseCaseFoldedArgumentMembers(t *testing.T) {
	projectFixture(t)

	cases := []struct {
		name      string
		tool      string
		arguments map[string]any
		member    string
	}{
		{
			name:      "get_pack PACK_ID",
			tool:      "get_pack",
			arguments: map[string]any{"PACK_ID": "intake"},
			member:    "PACK_ID",
		},
		{
			name:      "get_pack Pack_Id",
			tool:      "get_pack",
			arguments: map[string]any{"Pack_Id": "intake"},
			member:    "Pack_Id",
		},
		{
			name:      "experimental_evaluate PACK_ID",
			tool:      "experimental_evaluate",
			arguments: map[string]any{"PACK_ID": "intake", "facts": projectFacts},
			member:    "PACK_ID",
		},
		{
			name:      "experimental_test_packs PACK_ID",
			tool:      "experimental_test_packs",
			arguments: map[string]any{"PACK_ID": "intake"},
			member:    "PACK_ID",
		},
		{
			name:      "experimental_test_packs Pack_Id",
			tool:      "experimental_test_packs",
			arguments: map[string]any{"Pack_Id": "intake"},
			member:    "Pack_Id",
		},
		// The tools that decode without a member list of their own are
		// held by the same central check, from their schemas' names.
		{name: "validate DOCUMENT", tool: "validate", arguments: map[string]any{"DOCUMENT": "{}"}, member: "DOCUMENT"},
		{name: "test_conformance SPEC_VERSION", tool: "test_conformance", arguments: map[string]any{"SPEC_VERSION": "0.2.0-draft"}, member: "SPEC_VERSION"},
		{name: "get_schema Spec_Version", tool: "get_schema", arguments: map[string]any{"Spec_Version": "0.2.0-draft"}, member: "Spec_Version"},
		{name: "get_example NAME", tool: "get_example", arguments: map[string]any{"NAME": "minimal-expense-approval"}, member: "NAME"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			responses := runServer(t, toolCall(t, 1, tc.tool, tc.arguments))
			if len(responses) != 1 {
				t.Fatalf("got %d responses, want 1", len(responses))
			}
			if _, protocol := responses[0]["error"]; protocol {
				t.Fatalf("refusal must be an in-band tool error, not a protocol error: %#v", responses[0])
			}
			result, ok := responses[0]["result"].(map[string]any)
			if !ok || result["isError"] != true {
				t.Fatalf("want isError tool result, got %#v", responses[0])
			}
			want := spelledError(tc.tool, tc.member, strings.ToLower(tc.member))
			if text := toolText(t, result); text != want {
				t.Fatalf("error = %q, want %q", text, want)
			}
		})
	}

	t.Run("get_pack spelled correctly still succeeds", func(t *testing.T) {
		outcome := runServer(t, toolCall(t, 1, "get_pack", map[string]any{"pack_id": "intake"}))[0]["result"].(map[string]any)
		if outcome["isError"] != false {
			t.Fatalf("correct spelling must succeed: %#v", outcome)
		}
	})

	t.Run("experimental_evaluate spelled correctly still succeeds", func(t *testing.T) {
		outcome := runServer(t, toolCall(t, 1, "experimental_evaluate", map[string]any{"pack_id": "intake", "facts": projectFacts}))[0]["result"].(map[string]any)
		if outcome["isError"] != false {
			t.Fatalf("correct spelling must succeed: %#v", outcome)
		}
	})
}

func TestExperimentalTestPacksExactMembersLeavesOmittedOptionalUntouched(t *testing.T) {
	matrixProjectFixture(t, matrixConfig, passingMatrix)
	outcome := runServer(t, toolCall(t, 1, "experimental_test_packs", nil))[0]["result"].(map[string]any)
	if outcome["isError"] != false {
		t.Fatalf("omitted arguments must still run every declared pack: %#v", outcome)
	}
	t.Run("correct pack_id still succeeds", func(t *testing.T) {
		selected := runServer(t, toolCall(t, 1, "experimental_test_packs", map[string]any{"pack_id": "intake"}))[0]["result"].(map[string]any)
		if selected["isError"] != false {
			t.Fatalf("correct spelling must succeed: %#v", selected)
		}
	})
}

// The cites argument's advertised schema states what the handler enforces
// (ADR-0033): a client that generates arguments from the schema cannot
// produce a citation the handler refuses for its shape.
func TestTheCitesSchemaStatesWhatTheHandlerEnforces(t *testing.T) {
	responses := runServer(t, message(t, 1, "tools/list", nil))
	tools := responses[0]["result"].(map[string]any)["tools"].([]any)
	var cites map[string]any
	for _, tool := range tools {
		if tool.(map[string]any)["name"] == "experimental_evaluate" {
			cites = tool.(map[string]any)["inputSchema"].(map[string]any)["properties"].(map[string]any)["cites"].(map[string]any)
		}
	}
	if cites == nil {
		t.Fatal("experimental_evaluate advertises no cites argument")
	}
	items := cites["items"].(map[string]any)
	properties := items["properties"].(map[string]any)
	session, index, signature := properties["sessionId"].(map[string]any), properties["callIndex"].(map[string]any), properties["signature"].(map[string]any)
	if cites["type"] != "array" || items["type"] != "object" || items["additionalProperties"] != false ||
		session["minLength"] != float64(1) || session["maxLength"] != float64(128) || session["pattern"] != "^[A-Za-z0-9._-]+$" ||
		index["type"] != "integer" || index["minimum"] != float64(0) || index["maximum"] != float64(9007199254740991) ||
		signature["pattern"] != "^[0-9a-f]{128}$" {
		t.Fatalf("the cites schema does not state the handler's shape: %v", cites)
	}
	required, _ := items["required"].([]any)
	if len(required) != 3 || required[0] != "sessionId" || required[1] != "callIndex" || required[2] != "signature" {
		t.Fatalf("exactly the three members are required: %v", required)
	}
	excluded, _ := session["not"].(map[string]any)["enum"].([]any)
	if len(excluded) != 2 || excluded[0] != "." || excluded[1] != ".." {
		t.Fatalf("the schema excludes . and ..: %v", session["not"])
	}
	// The advertised schema itself, compiled by the validator this runtime
	// validates packs with, evaluated on each value: what it admits, the
	// handler admits, and the reverse.
	schemaBytes, err := json.Marshal(cites)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := validation.CompileSchema(schemaBytes, "urn:judgmentpack:test:cites")
	if err != nil {
		t.Fatalf("the advertised cites schema does not compile: %v", err)
	}
	admits := func(document string) bool {
		instance, err := jsonschema.UnmarshalJSON(strings.NewReader(document))
		if err != nil {
			return false
		}
		return compiled.Validate(instance) == nil
	}
	// What the schema admits, the handler admits, and the reverse, on the
	// values the patterns and bounds decide.
	for _, value := range []string{
		`[{"sessionId":"s-1","callIndex":0,"signature":"` + strings.Repeat("a", 128) + `"}]`,
		`[{"sessionId":"` + strings.Repeat("s", 128) + `","callIndex":9007199254740991,"signature":"` + strings.Repeat("f", 128) + `"}]`,
		`[{"sessionId":"","callIndex":0,"signature":"` + strings.Repeat("a", 128) + `"}]`,
		`[{"sessionId":".","callIndex":0,"signature":"` + strings.Repeat("a", 128) + `"}]`,
		`[{"sessionId":"..","callIndex":0,"signature":"` + strings.Repeat("a", 128) + `"}]`,
		`[{"sessionId":"...","callIndex":0,"signature":"` + strings.Repeat("a", 128) + `"}]`,
		`[{"sessionId":"s/1","callIndex":0,"signature":"` + strings.Repeat("a", 128) + `"}]`,
		`[{"sessionId":"` + strings.Repeat("s", 129) + `","callIndex":0,"signature":"` + strings.Repeat("a", 128) + `"}]`,
		`[{"sessionId":"s-1","callIndex":-1,"signature":"` + strings.Repeat("a", 128) + `"}]`,
		`[{"sessionId":"s-1","callIndex":-0,"signature":"` + strings.Repeat("a", 128) + `"}]`,
		`[{"sessionId":"s-1","callIndex":9007199254740992,"signature":"` + strings.Repeat("a", 128) + `"}]`,
		`[{"sessionId":"s-1","callIndex":0,"signature":"` + strings.Repeat("A", 128) + `"}]`,
		`[{"sessionId":"s-1","callIndex":0,"signature":"` + strings.Repeat("a", 127) + `"}]`,
		`[{"sessionId":"s-1","callIndex":0,"signature":""}]`,
		`[{"sessionId":"s-1","callIndex":0,"signature":"` + strings.Repeat("a", 128) + `","more":1}]`,
		`[{"sessionId":"s-1","signature":"` + strings.Repeat("a", 128) + `"}]`,
	} {
		_, err := audit.ParseCites([]byte(value))
		if (err == nil) != admits(value) {
			t.Errorf("%s: handler admits=%v, schema admits=%v", value, err == nil, admits(value))
		}
	}
}

// The central check refuses a member spelled as a known one by another case;
// what a schema forbids by additionalProperties:false is a wholly unknown
// member too, and every tool holds that itself: the four that decode with
// a member list of their own, and the three whose schemas advertise none.
func TestEveryToolRefusesAWhollyUnknownMember(t *testing.T) {
	projectFixture(t)
	for _, tc := range []struct {
		tool      string
		arguments map[string]any
		accepted  string
	}{
		{"validate", map[string]any{"document": "{}", "typo": 1}, "the accepted members are document and through"},
		{"test_conformance", map[string]any{"typo": 1}, "the accepted members are suite and spec_version"},
		{"get_schema", map[string]any{"typo": 1}, `the accepted member is "spec_version"`},
		{"get_example", map[string]any{"name": "minimal-expense-approval", "typo": 1}, `the accepted member is "name"`},
		{"describe_runtime", map[string]any{"typo": true}, "it accepts no members"},
		{"list_examples", map[string]any{"typo": true}, "it accepts no members"},
		{"list_packs", map[string]any{"typo": true}, "it accepts no members"},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			responses := runServer(t, toolCall(t, 1, tc.tool, tc.arguments))
			if len(responses) != 1 {
				t.Fatalf("got %d responses, want 1", len(responses))
			}
			if _, protocol := responses[0]["error"]; protocol {
				t.Fatalf("refusal must be an in-band tool error, not a protocol error: %#v", responses[0])
			}
			result, ok := responses[0]["result"].(map[string]any)
			if !ok || result["isError"] != true {
				t.Fatalf("want isError tool result, got %#v", responses[0])
			}
			want := `The "` + tc.tool + `" arguments carry an unknown member "typo"; ` + tc.accepted + `, spelled exactly.`
			if text := toolText(t, result); text != want {
				t.Fatalf("error = %q, want %q", text, want)
			}
		})
	}
	// An empty object, or none, is what an argument-less tool takes.
	for _, arguments := range []map[string]any{{}, nil} {
		responses := runServer(t, toolCall(t, 1, "list_examples", arguments))
		if len(responses) != 1 || responses[0]["result"].(map[string]any)["isError"] != false {
			t.Fatalf("list_examples with %v arguments: %#v", arguments, responses)
		}
	}
}

// The replay_history prompt (ADR-0034) renders its method -- the two lanes,
// the held-out slice, the policy owner as the only arbiter -- carries the
// draft pack when one is given and not otherwise, and ends with the
// disclaimer every prompt carries.
func TestReplayHistoryPromptRendersItsMethod(t *testing.T) {
	render := func(args map[string]any) string {
		t.Helper()
		params := map[string]any{"name": "replay_history"}
		if args != nil {
			params["arguments"] = args
		}
		responses := runServer(t, message(t, 1, "prompts/get", params))
		result, ok := responses[0]["result"].(map[string]any)
		if !ok {
			t.Fatalf("prompts/get replay_history: %#v", responses[0])
		}
		return result["messages"].([]any)[0].(map[string]any)["content"].(map[string]any)["text"].(string)
	}
	bare := render(nil)
	for _, sentence := range []string{
		"DOCUMENTS WRITE THE RULES, PAST DECISIONS TEST THEM",
		"Do not read the past decisions while drafting",
		"expectedDisposition is what was recorded, never what the draft produces",
		`matrixVersion "3"`,
		"Hold out a slice of the history",
		"Only the policy owner can say which",
		"Never weaken a rule to make history pass",
		"Nothing moves a threshold but the policy owner's answer",
		"experimental_test_packs",
		"non-normative",
	} {
		if !strings.Contains(bare, sentence) {
			t.Fatalf("the method must say %q:\n%s", sentence, bare)
		}
	}
	if strings.Contains(bare, "The draft pack:") {
		t.Fatalf("no pack given, no pack block:\n%s", bare)
	}
	withPack := render(map[string]any{"pack": `{"id":"draft"}`})
	if !strings.Contains(withPack, "The draft pack:") || !strings.Contains(withPack, `{"id":"draft"}`) {
		t.Fatalf("the given pack is rendered in its block:\n%s", withPack)
	}
	if !strings.HasSuffix(strings.TrimSpace(withPack), strings.TrimSpace(authoringDisclaimer)) {
		t.Fatalf("the disclaimer ends the rendering:\n%s", withPack)
	}
}
