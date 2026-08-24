package mcp

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/conformance"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/describe"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
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
	for _, want := range []string{"validate", "test_conformance", "get_schema", "describe_runtime", "list_examples", "get_example", "list_packs", "get_pack", "experimental_evaluate", "experimental_test_packs", "experimental_test_graphs"} {
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
	if len(advertised) != 11 {
		t.Fatalf("expected 11 distinct tools, got %d", len(advertised))
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
	for _, want := range []string{"author_pack", "test_pack", "fix_pack", "explain_disposition", "present_pack", "author_graph"} {
		if !names[want] {
			t.Fatalf("prompts/list must include %q: %v", want, names)
		}
	}
	if len(names) != 6 {
		t.Fatalf("expected exactly 6 prompts, got %d", len(names))
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
	for _, name := range []string{"author_pack", "test_pack", "fix_pack", "explain_disposition", "present_pack", "author_graph"} {
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
