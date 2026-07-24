package mcp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/conformance"
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

	// tools/list
	tools := responses[1]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 4 {
		t.Fatalf("expected 4 tools, got %d", len(tools))
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
