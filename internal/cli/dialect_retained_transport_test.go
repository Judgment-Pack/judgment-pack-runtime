package cli

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/dialect"
)

// These are handwritten structural families, not expected reports captured from
// Runtime. The old document is filled through an existing operation argument;
// projection preserves every declaration and changes only the copy leaves.
func retainedTransportJSON(t *testing.T, value any, html bool) string {
	t.Helper()
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(html)
	if err := e.Encode(value); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSuffix(b.String(), "\n")
}
func retainedTransportDocument(t *testing.T) (string, string) {
	t.Helper()
	facts, evidence, sources := []any{}, []any{}, []any{}
	params := map[string]any{}
	for i := 0; i < 63; i++ {
		params[fmt.Sprintf("p%02d", i)] = map[string]any{"pointer": "/original", "type": "string"}
	}
	for i := 0; i < 256; i++ {
		target := fmt.Sprintf("/f%03d", i)
		if i == 0 {
			target = "/expense"
		}
		facts = append(facts, map[string]any{"target": target, "source": "/retained"})
	}
	for i := 0; i < 128; i++ {
		name := fmt.Sprintf("e%03d", i)
		if i == 0 {
			name = "receipt"
		}
		if i == 1 {
			name = "cost-center"
		}
		evidence = append(evidence, map[string]any{"requirement": name, "source": "/retained"})
	}
	for i := 0; i < 16; i++ {
		sources = append(sources, map[string]any{"name": fmt.Sprintf("source%02d", i), "kind": "operation", "profile": "original-profile", "profileDigest": "sha256:" + strings.Repeat("a", 64), "maxAge": 86400, "arguments": map[string]any{"provenance": ""}, "read": map[string]any{"copy": map[string]any{"facts": []any{}, "evidence": []any{}}}})
	}
	d := map[string]any{"version": "1.0.0", "formatVersion": "3.0.0", "id": strings.Repeat("i", 1024), "case": map[string]any{"parameters": params, "facts": facts, "evidence": evidence}, "sources": sources}
	metadata := sources[0].(map[string]any)["arguments"].(map[string]any)
	metadata["provenance"] = strings.Repeat("x", dialect.MaxDocumentBytes-len(retainedTransportJSON(t, d, true)))
	old := retainedTransportJSON(t, d, true)
	if len(old) != dialect.MaxDocumentBytes {
		t.Fatal("old complete document budget", len(old))
	}
	for _, f := range facts {
		f.(map[string]any)["conversion"] = map[string]any{"contractVersion": "1", "column": strings.Repeat("\x01", 160), "kind": "boolean", "trueValue": strings.Repeat("\x02", 160), "falseValue": strings.Repeat("\x03", 160)}
	}
	for _, e := range evidence {
		e.(map[string]any)["availability"] = map[string]any{"contractVersion": "1", "column": strings.Repeat("\x01", 160)}
	}
	projected := retainedTransportJSON(t, d, true)
	if len(projected) > 1026688 || len(projected) < 900000 {
		t.Fatal("projected structural family outside proved growth", len(projected))
	}
	return old, projected
}

// Use the actual binary when supplied by the integration coordinator; ordinary
// repository tests exercise the same public command entry directly.
func retainedTransportCLI(t *testing.T, argv []string, input string) (int, string, string) {
	t.Helper()
	binary := os.Getenv("JPS_RETAINED_TEST_BINARY")
	if binary == "" {
		return runTest(t, argv, input)
	}
	if !filepath.IsAbs(binary) {
		t.Fatal("candidate binary must be an absolute path")
	}
	cmd := exec.Command(binary, argv...)
	cmd.Stdin = strings.NewReader(input)
	var out, errout bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errout
	code := 0
	if err := cmd.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		} else {
			t.Fatal(err)
		}
	}
	return code, out.String(), errout.String()
}
func retainedTransportCall(t *testing.T, operation, document, pack string, frame bool) (map[string]any, int, int) {
	t.Helper()
	args := map[string]any{"draftContract": dialect.RetainedColumnDraftSelector, "draftProfile": "base", "document": document}
	if pack != "" {
		args["pack"] = pack
	}
	if frame {
		args["retainedFrame"] = true
	}
	request := retainedTransportJSON(t, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "dialect_" + operation, "arguments": args}}, true) + "\n"
	if len(request) > 16*1024*1024 {
		t.Fatal("Base complete escaped request exceeded stdio envelope", len(request))
	}
	code, out, stderr := retainedTransportCLI(t, []string{"mcp"}, request)
	if code != 0 || stderr != "" {
		t.Fatal("actual MCP entry failed", code, stderr)
	}
	t.Logf("complete serialized MCP request=%d response=%d", len(request), len(out))
	// Consume the complete output through a bounded line reader, just as a stdio
	// client must. Both structuredContent and the second, quoted report are real.
	scanner := bufio.NewScanner(strings.NewReader(out))
	scanner.Buffer(make([]byte, 65536), 16*1024*1024)
	if !scanner.Scan() {
		t.Fatal("complete MCP response was not consumable", len(out), scanner.Err())
	}
	var response struct {
		Result struct {
			Structured map[string]any                `json:"structuredContent"`
			Content    []struct{ Type, Text string } `json:"content"`
			IsError    bool                          `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(scanner.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if scanner.Scan() || scanner.Err() != nil {
		t.Fatal("extra or oversized response", scanner.Err())
	}
	if len(response.Result.Content) != 1 || response.Result.Content[0].Type != "text" {
		t.Fatal("missing report representation")
	}
	var text map[string]any
	if json.Unmarshal([]byte(response.Result.Content[0].Text), &text) != nil || !reflect.DeepEqual(text, response.Result.Structured) {
		t.Fatal("structured and text report differ")
	}
	if response.Result.IsError != (response.Result.Structured["status"] == "refused") {
		t.Fatal("wrong refusal status")
	}
	return response.Result.Structured, len(request), len(out)
}
func retainedTransportAssert(t *testing.T, operation, raw, pack string, frame bool, want string) {
	t.Helper()
	argv := []string{"dialect", operation, "-", "--format", "json", "--draft-contract", dialect.RetainedColumnDraftSelector, "--draft-profile", "base"}
	if pack != "" {
		file := writeFixture(t, []byte(pack))
		argv = append(argv, "--pack", file)
	}
	if frame {
		argv = append(argv, "--retained-frame")
	}
	code, out, stderr := retainedTransportCLI(t, argv, raw)
	var cli map[string]any
	if stderr != "" || json.Unmarshal([]byte(out), &cli) != nil || cli["status"] != want || (code == 0) != (want != "refused") {
		t.Fatal("CLI complete artifact boundary", code, len(raw), first(out, 500), stderr)
	}
	mcp, requestBytes, responseBytes := retainedTransportCall(t, operation, raw, pack, frame)
	delete(cli, "command")
	delete(mcp, "command")
	if !reflect.DeepEqual(cli, mcp) {
		t.Fatal("serialized CLI and MCP reports differ", len(raw))
	}
	if frame && want != "refused" {
		if mcp["document"] != raw || mcp["digest"] != fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(raw))) {
			t.Fatal("retained frame bytes or independent digest changed")
		}
		var original map[string]any
		if json.Unmarshal([]byte(raw), &original) != nil {
			t.Fatal("handwritten frame is not JSON")
		}
		var reconstructed map[string]any
		if json.Unmarshal([]byte(mcp["document"].(string)), &reconstructed) != nil || !reflect.DeepEqual(original, reconstructed) {
			t.Fatal("complete frame values/statuses/declarations/escapes cannot be reconstructed")
		}
		if _, duplicate := mcp["frame"]; duplicate {
			t.Fatal("redundant complete frame was emitted on the wire")
		}
		summary := map[string]any{"contractVersion": original["contractVersion"], "origin": original["origin"], "valueCount": float64(len(original["values"].(map[string]any))), "declarationCount": float64(0), "fieldCount": float64(0)}
		if research, ok := original["research"].(map[string]any); ok {
			summary["fieldCount"] = float64(len(research["fields"].(map[string]any)))
			if declaration, ok := research["declaration"].(map[string]any); ok {
				summary["declarationCount"] = float64(len(declaration["fields"].([]any)))
			}
		}
		if !reflect.DeepEqual(mcp["frameSummary"], summary) {
			t.Fatal("frame summary differs from the complete original data")
		}
	}
	if want == "refused" {
		diagnostics := mcp["diagnostics"].([]any)
		code := diagnostics[0].(map[string]any)["code"]
		if code != "input-over-bound" && code != "JPS-DIALECT-PACK-BYTE-LIMIT" {
			t.Fatal("wrong one-over refusal", code)
		}
	}
	t.Logf("%s frame=%v artifact=%d pack=%d escapedRequest=%d cliReport=%d dualMCPResponse=%d status=%s", operation, frame, len(raw), len(pack), requestBytes, len(out), responseBytes, want)
}
func TestRetainedColumnTransportBaseDocumentAndPack(t *testing.T) {
	old, projected := retainedTransportDocument(t)
	code, out, stderr := retainedTransportCLI(t, []string{"dialect", "validate", "-", "--format", "json"}, old)
	if code != 0 || stderr != "" || !strings.Contains(out, `"status":"document-valid"`) {
		t.Fatal("source family was not admitted before projection", code, first(out, 500), stderr)
	}
	b, err := os.ReadFile("../artifacts/jps/0.2.0-draft/cases/valid/minimal-expense-approval.json")
	if err != nil {
		t.Fatal(err)
	}
	var p map[string]any
	if json.Unmarshal(b, &p) != nil {
		t.Fatal("pack fixture")
	}
	requirements := []any{}
	for i := 0; i < 128; i++ {
		name := fmt.Sprintf("e%03d", i)
		if i == 0 {
			name = "receipt"
		}
		if i == 1 {
			name = "cost-center"
		}
		requirements = append(requirements, map[string]any{"id": name, "description": "Original requirement", "required": true, "kind": "fact"})
	}
	p["evidenceRequirements"] = requirements
	p["description"] = ""
	// Literal HTML is legal pack metadata. Go quoting the complete pack expands
	// almost all of its 1MiB bytes sixfold in the actual JSON-RPC request.
	p["description"] = strings.Repeat("<", (1<<20)-len(retainedTransportJSON(t, p, false)))
	pack := retainedTransportJSON(t, p, false)
	if len(pack) != 1<<20 {
		t.Fatal("pack boundary fixture", len(pack))
	}
	exact := projected + strings.Repeat(" ", dialect.RetainedColumnDraftDocumentBytes-len(projected))
	for _, tc := range []struct{ name, raw, pack, want string }{{"projected-family", projected, pack, "review-required"}, {"both-exact", exact, pack, "review-required"}, {"document-over", exact + " ", pack, "refused"}, {"desk-pack-size-is-not-runtime-limit", exact, pack + " ", "review-required"}, {"runtime-pack-over", exact, strings.Repeat(" ", int(dialect.MaxPackBytes)+1), "refused"}} {
		t.Run(tc.name, func(t *testing.T) { retainedTransportAssert(t, "check", tc.raw, tc.pack, false, tc.want) })
	}
}
func TestRetainedColumnTransportBaseFrames(t *testing.T) {
	// Asserted: all 100 old CSV columns, 160-byte names, maximal coupled old
	// values-object budget. The complete original case still fits 2MiB with a
	// 4096-byte reserve for its mapping, provenance and fixed wrapper.
	values := map[string]any{}
	for i := 0; i < 100; i++ {
		values[fmt.Sprintf("C%03d", i)+strings.Repeat("n", 156)] = strings.Repeat("\x01", 3450)
	}
	asserted := map[string]any{"contractVersion": "1", "origin": "asserted", "values": values, "research": nil}
	raw := retainedTransportJSON(t, asserted, true)
	key := "C000" + strings.Repeat("n", 156)
	values[key] = values[key].(string) + strings.Repeat("a", dialect.RetainedColumnDraftFrameBytes-4096-len(raw))
	raw = retainedTransportJSON(t, asserted, true)
	if len(raw) != dialect.RetainedColumnDraftFrameBytes-4096 || len(values[key].(string)) > 16000 {
		t.Fatal("asserted old-domain witness", len(raw))
	}
	// Generated: all 30 original declarations, all eight original value types,
	// absent/blank distinctions and complete opaque field/declaration text.
	declarations := []any{}
	originalFields := []any{}
	originalTyped := map[string]any{}
	fields := map[string]any{}
	generatedValues := map[string]any{"Blank": "", "AbsentTyped": "opaque"}
	types := []string{"text", "integer", "decimal", "boolean", "text-list", "range", "string", "number"}
	tokens := []string{`"<\\\"\u2028>"`, `-0`, `"-0.00"`, `false`, `["a","<"]`, `{"min":"-0.00","max":"2.00"}`, `"legacy"`, `-0`}
	for i := 0; i < 30; i++ {
		name := fmt.Sprintf("Field%02d", i)
		field := map[string]any{"name": name, "description": strings.Repeat("<", 1000), "type": types[i%8]}
		originalFields = append(originalFields, field)
		originalTyped[name] = map[string]any{"status": "supported", "value": json.RawMessage(tokens[i%8]), "sources": []any{}}
		declarations = append(declarations, map[string]any{"name": name, "fieldJson": retainedTransportJSON(t, field, true)})
		fields[name] = map[string]any{"status": "supported", "valueJson": tokens[i%8]}
		generatedValues[name] = strings.Repeat("\x01", 900)
	}
	fields["AbsentTyped"] = map[string]any{"status": "missing"}
	generated := map[string]any{"contractVersion": "1", "origin": "generated", "values": generatedValues, "research": map[string]any{"recordRecipeRevision": "1", "declaration": map[string]any{"recipeRevision": "1", "fields": declarations}, "fields": fields}}
	generatedRaw := retainedTransportJSON(t, generated, true)
	// Source-derived snapshot shape from Desk's reviewed-input handoff: keep
	// record.values AND the available values, the typed entity, full recipe,
	// preparation/qualification, reviews, model attempts and source provenance.
	// Those duplicate values count against the SAME 512KiB old limit.
	provenance := map[string]any{"id": "source1", "kind": "page", "url": "https://example.com/original", "at": "2026-01-01T00:00:00Z", "digest": "sha256:" + strings.Repeat("a", 64), "proofDigest": "sha256:" + strings.Repeat("b", 64), "verification": "verified"}
	availableValues := map[string]any{}
	for k, v := range generatedValues {
		if v != "" {
			availableValues[k] = v
		}
	}
	snapshotObject := map[string]any{"version": 1, "class": "generated", "preparationId": "preparation1", "record": map[string]any{"id": "record1", "values": generatedValues, "research": map[string]any{"entity": map[string]any{"fields": originalTyped}}}, "recipeVersion": 1, "qualification": map[string]any{"mapping": map[string]any{"case": map[string]any{"facts": []any{}, "evidence": []any{}}}}, "prepared": map[string]any{}, "values": availableValues, "research": map[string]any{"id": "research1", "recipeVersion": 1, "recipe": map[string]any{"fields": originalFields}, "intent": "Preserve original inventory", "createdAt": "2026-01-01T00:00:00Z", "approvedLimits": map[string]any{"records": 1, "modelCalls": 1}, "budgetApprovals": []any{}, "workflowApproval": map[string]any{"decision": "approved"}, "modelResponses": []any{map[string]any{"id": "attempt1", "requestDigest": "sha256:" + strings.Repeat("c", 64), "resultDigest": "sha256:" + strings.Repeat("d", 64), "finishedAt": "2026-01-01T00:00:00Z"}}, "sources": []any{provenance}}, "reviews": []any{map[string]any{"review": map[string]any{"decision": "accepted", "recordId": "record1", "recipeVersion": 1}}}}
	snapshot := retainedTransportJSON(t, snapshotObject, true)
	if len(snapshot) > 524288 {
		t.Fatal("generated family exceeds old snapshot budget", len(snapshot))
	}
	// Unfiltered opaque origin metadata is retained at source rather than dropped
	// to manufacture space. This exact source budget is independent of Runtime's
	// frame check and does not claim a new producer/authenticity implementation.
	provenance["query"] = strings.Repeat("p", 524288-len(snapshot)-len(`,"query":""`))
	snapshot = retainedTransportJSON(t, snapshotObject, true)
	if len(snapshot) != 524288 {
		t.Fatal("generated complete snapshot byte accounting", len(snapshot))
	}
	t.Logf("handwritten old generated snapshot=%d projected frame=%d declarations=%d", len(snapshot), len(generatedRaw), len(declarations))
	for _, tc := range []struct{ name, raw string }{{"asserted", raw}, {"generated", generatedRaw}} {
		t.Run(tc.name, func(t *testing.T) {
			retainedTransportAssert(t, "validate", tc.raw, "", true, "retained-frame-valid")
			exact := tc.raw + strings.Repeat(" ", dialect.RetainedColumnDraftFrameBytes-len(tc.raw))
			retainedTransportAssert(t, "validate", exact, "", true, "retained-frame-valid")
			retainedTransportAssert(t, "validate", exact+" ", "", true, "refused")
		})
	}
}

// This admitted frame-grammar stress case is deliberately outside the pinned
// Desk snapshot projection. It checks the full public Base frame envelope, where
// valueJson is inspectable opaque text and must not amplify a second HTML pass.
func TestRetainedColumnTransportBaseOpaqueHTMLFrame(t *testing.T) {
	for _, tc := range []struct{ name, unit string }{{"HTML", "<"}, {"mixed", "<>&\"\\\x00\t\u2028\u2029é😀"}} {
		t.Run(tc.name, func(t *testing.T) {
			field := map[string]any{"status": "supported", "valueJson": ""}
			frame := map[string]any{"contractVersion": "1", "origin": "generated", "values": map[string]any{}, "research": map[string]any{"recordRecipeRevision": "r1", "declaration": nil, "fields": map[string]any{"Column": field}}}
			overhead := len(retainedTransportJSON(t, frame, false))
			unitBytes := len(retainedTransportJSON(t, tc.unit, false)) - 2
			field["valueJson"] = strings.Repeat(tc.unit, (dialect.RetainedColumnDraftFrameBytes-overhead)/unitBytes)
			raw := retainedTransportJSON(t, frame, false)
			field["valueJson"] = field["valueJson"].(string) + strings.Repeat("<", dialect.RetainedColumnDraftFrameBytes-len(raw))
			exact := retainedTransportJSON(t, frame, false)
			if len(exact) != dialect.RetainedColumnDraftFrameBytes {
				t.Fatal("opaque frame grammar boundary", len(exact))
			}
			retainedTransportAssert(t, "validate", exact, "", true, "retained-frame-valid")
			retainedTransportAssert(t, "validate", exact+" ", "", true, "refused")
		})
	}
}

// Construct the complete frame by hand. Go's JSON encoder would pre-escape
// literal U+2028/U+2029 and hide the admitted three-byte UTF-8 input boundary.
func TestRetainedColumnTransportBaseLiteralSeparators(t *testing.T) {
	for _, tc := range []struct{ name, unit string }{
		{"line", "\u2028"},
		{"mandatory-quote", `\"`},
		{"mandatory-backslash", `\\`},
		{"paragraph", "\u2029"},
		{"escaped-separators", `\u2028\u2029`},
		{"literal-backslash-u", `\\u2028\\u2029\\\\u2028\\\\u2029`},
		{"mixed-escaping", "\u2028" + `\\u2028\\u2029\\\\u2028\u0000\n\t\"\\` + "\u2029é😀"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prefix := `{"contractVersion":"1","origin":"generated","values":{},"research":{"recordRecipeRevision":"r","declaration":null,"fields":{"Column":{"status":"supported","valueJson":"`
			suffix := `"}}}}`
			room := dialect.RetainedColumnDraftFrameBytes - len(prefix) - len(suffix)
			payload := strings.Repeat(tc.unit, room/len(tc.unit)) + strings.Repeat("x", room%len(tc.unit))
			exact := prefix + payload + suffix
			if len(exact) != dialect.RetainedColumnDraftFrameBytes {
				t.Fatal("handwritten raw UTF-8 frame boundary", len(exact))
			}
			retainedTransportAssert(t, "validate", exact, "", true, "retained-frame-valid")
			retainedTransportAssert(t, "validate", exact+" ", "", true, "refused")
		})
	}
}

// Full-document reports already retain only the canonical document. Exercise
// their separate Base envelope with raw UTF-8/mandatory escaping as well.
func TestRetainedColumnTransportBaseRawDocumentEscapes(t *testing.T) {
	for _, tc := range []struct{ name, unit string }{{"line", "\u2028"}, {"paragraph", "\u2029"}, {"quote", `\"`}, {"backslash", `\\`}} {
		t.Run(tc.name, func(t *testing.T) {
			prefix := `{"version":3,"case":{"facts":[],"evidence":[]},"sources":[{"name":"original","kind":"operation","profile":"profile1","profileDigest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","maxAge":86400,"arguments":{"payload":[`
			suffix := `]},"read":{"copy":{"facts":[],"evidence":[]}}}]}`
			chunks := make([]string, 16)
			room := dialect.RetainedColumnDraftDocumentBytes - len(prefix) - len(suffix) - (16*2 + 15)
			perChunk := room / 16
			for i := range chunks {
				n := perChunk
				if i < room%16 {
					n++
				}
				chunks[i] = `"` + strings.Repeat(tc.unit, n/len(tc.unit)) + strings.Repeat("x", n%len(tc.unit)) + `"`
			}
			exact := prefix + strings.Join(chunks, ",") + suffix
			if len(exact) != dialect.RetainedColumnDraftDocumentBytes {
				t.Fatal("handwritten full document byte boundary", len(exact))
			}
			retainedTransportAssert(t, "validate", exact, "", false, "document-valid")
			retainedTransportAssert(t, "validate", exact+" ", "", false, "refused")
		})
	}
}

func TestRetainedColumnTransportFrameReconstruction(t *testing.T) {
	// Deliberately noncanonical source spacing/order, a historical null, an
	// absent member, opaque malformed typed JSON, and ordered declarations.
	raw := ` { "origin":"generated", "contractVersion":"1", "values":{"Null":null,"Empty":"","Escaped":"\\u2028","Literal":"` + "\u2028" + `"},"research":{"recordRecipeRevision":"v1","declaration":{"recipeRevision":"v1","fields":[{"name":"Second","fieldJson":" {\"name\":\"Second\",\"type\":\"future\",\"n\":-0} "},{"name":"First","fieldJson":"{\"name\":\"First\",\"type\":\"text\"}"}]},"fields":{"Null":{"status":"supported","valueJson":"null"},"Missing":{"status":"missing"},"Control":{"status":"conflicting","valueJson":"\\u2028 \u0000 \" \\\\"},"Malformed":{"status":"interpretation","valueJson":"\"\\ud800\""}}}} `
	retainedTransportAssert(t, "validate", raw, "", true, "retained-frame-valid")
}
