package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/conformance"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

// protocolVersion is the MCP revision this server implements. When a client
// requests a version, initialize echoes it back, since this server's tool-only
// surface is version-agnostic; absent one, it answers with this value.
const protocolVersion = "2025-06-18"

// maxMessageBytes bounds one JSON-RPC line. It sits above the 10 MiB document
// limit so a validate call carrying a maximal document still fits, while
// refusing an unbounded line from a misbehaving client.
const maxMessageBytes = 16 * 1024 * 1024

const (
	codeParse          = -32700
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

// Server maps MCP tool calls onto the offline validation core. It holds no
// network connection and no credential.
type Server struct {
	engine *validation.Engine
	runner *conformance.Runner
}

// NewServer builds a server over an already-constructed engine and runner.
func NewServer(engine *validation.Engine, runner *conformance.Runner) *Server {
	return &Server{engine: engine, runner: runner}
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Serve reads newline-delimited JSON-RPC messages from in, dispatches each, and
// writes one response line per request to out. Notifications receive no
// response. Diagnostic text goes to logw so it never mixes with protocol output
// on out. Serve returns when in reaches EOF.
func (s *Server) Serve(in io.Reader, out, logw io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), maxMessageBytes)
	encoder := json.NewEncoder(out)
	encoder.SetEscapeHTML(false)

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var request rpcRequest
		if err := json.Unmarshal(line, &request); err != nil {
			writeMessage(encoder, logw, map[string]any{"jsonrpc": "2.0", "id": nil, "error": &rpcError{Code: codeParse, Message: "Message is not valid JSON."}})
			continue
		}
		if response, ok := s.handle(&request); ok {
			writeMessage(encoder, logw, response)
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(logw, "mcp: input error:", err)
		return err
	}
	return nil
}

func writeMessage(encoder *json.Encoder, logw io.Writer, response map[string]any) {
	if err := encoder.Encode(response); err != nil {
		fmt.Fprintln(logw, "mcp: write error:", err)
	}
}

// handle routes one message. The bool is false for notifications, which never
// receive a response.
func (s *Server) handle(request *rpcRequest) (map[string]any, bool) {
	switch request.Method {
	case "notifications/initialized":
		return nil, false
	case "initialize":
		return s.reply(request, s.initializeResult(request.Params), nil)
	case "ping":
		return s.reply(request, map[string]any{}, nil)
	case "tools/list":
		return s.reply(request, map[string]any{"tools": toolDefinitions()}, nil)
	case "tools/call":
		res, rerr := s.callTool(request.Params)
		return s.reply(request, res, rerr)
	default:
		if len(request.ID) == 0 {
			return nil, false // ignore unknown notifications
		}
		return s.reply(request, nil, &rpcError{Code: codeMethodNotFound, Message: "Unknown method: " + request.Method})
	}
}

// reply builds a response for a request, or signals no response for a
// notification (a message with no id).
func (s *Server) reply(request *rpcRequest, res any, rerr *rpcError) (map[string]any, bool) {
	if len(request.ID) == 0 {
		return nil, false
	}
	response := map[string]any{"jsonrpc": "2.0", "id": request.ID}
	if rerr != nil {
		response["error"] = rerr
	} else {
		response["result"] = res
	}
	return response, true
}

func (s *Server) initializeResult(rawParams json.RawMessage) map[string]any {
	version := protocolVersion
	var params struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if len(rawParams) > 0 {
		if err := json.Unmarshal(rawParams, &params); err == nil && params.ProtocolVersion != "" {
			version = params.ProtocolVersion
		}
	}
	return map[string]any{
		"protocolVersion": version,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo": map[string]any{
			"name":    result.CLIName,
			"version": result.CLIVersion,
		},
	}
}
