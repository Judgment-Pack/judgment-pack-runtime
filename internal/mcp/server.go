package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

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
	codeInvalidRequest = -32600
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
		// JSON's own whitespace around a message is passed over; any
		// other -- a vertical tab, a no-break space -- is part of the
		// message, and not JSON.
		line := bytes.Trim(scanner.Bytes(), " \t\r\n")
		if len(line) == 0 {
			continue
		}
		// Syntax first: what is not JSON at all is a parse error under null,
		// the one case JSON-RPC answers without an id (§5).
		if !json.Valid(line) {
			writeMessage(encoder, logw, map[string]any{"jsonrpc": "2.0", "id": nil, "error": &rpcError{Code: codeParse, Message: "Message is not valid JSON."}})
			continue
		}
		// A message that is not an object -- an array, which this server
		// does not batch, or a scalar -- is not a request at all: it is an
		// invalid request, answered under null (JSON-RPC §4.2, §5).
		if line[0] != '{' {
			writeMessage(encoder, logw, map[string]any{"jsonrpc": "2.0", "id": nil, "error": &rpcError{Code: codeInvalidRequest, Message: "The request is not a JSON object; batches are not supported."}})
			continue
		}
		// Then the envelope, judged in the order exactEnvelope states,
		// before any typed decoding is trusted: encoding/json would bind
		// "Params" to params and keep the last of two, so a first params
		// or arguments object -- one declaring a rehearsal, or citing
		// nothing -- could vanish behind a second; it would bind a
		// "Method" of the wrong type to method and fail before anything
		// held the envelope; and it would read null into a string.
		env, request := exactEnvelope(line)
		if env.err != nil {
			// An invalid request (-32600) is answered whether or not an
			// id is there: §4.1 exempts a notification, which is a
			// Request object without an id, and an object that is not a
			// Request object is not one -- §5's own example answers
			// {"method": 1, "params": "bar"} under null. It is answered
			// under the one id when a member spelled exactly "id" is
			// there once, valid, and spelled by nothing else; under null
			// otherwise. A valid request with no id is a notification,
			// answered by nothing, its errors included: a refusal of its
			// params, an unknown method.
			if env.err.Code != codeInvalidRequest && !env.idPresent {
				continue
			}
			var id any
			if env.idUnique {
				id = env.id
			}
			writeMessage(encoder, logw, map[string]any{"jsonrpc": "2.0", "id": id, "error": env.err})
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
		// A notification by name; carrying an id, it is a request for a
		// method that is not one, and a request is answered (§5).
		if len(request.ID) == 0 {
			return nil, false
		}
		return s.reply(request, nil, &rpcError{Code: codeMethodNotFound, Message: "notifications/initialized is a notification, not a request method."})
	case "initialize":
		return s.reply(request, s.initializeResult(request.Params), nil)
	case "ping":
		return s.reply(request, map[string]any{}, nil)
	case "tools/list":
		return s.reply(request, map[string]any{"tools": toolDefinitions()}, nil)
	case "tools/call":
		res, rerr := s.callTool(request.Params)
		return s.reply(request, res, rerr)
	case "prompts/list":
		return s.reply(request, map[string]any{"prompts": listPrompts()}, nil)
	case "prompts/get":
		res, rerr := getPrompt(request.Params)
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
		"capabilities":    map[string]any{"tools": map[string]any{}, "prompts": map[string]any{}},
		"serverInfo": map[string]any{
			"name":    result.CLIName,
			"version": result.CLIVersion,
		},
	}
}

// envelope is what the exact walk found of a request: the refusal, if
// any, and the id as the walk saw it -- present when a member spelled
// exactly "id" is there, unique when it is there once and no member is
// spelled as it by another case, and then its value.
type envelope struct {
	err       *rpcError
	idPresent bool
	idUnique  bool
	id        json.RawMessage
}

// exactEnvelope judges a request object in the order JSON-RPC's answers
// need. The id first, since it is what any refusal is answered under: the
// member spelled exactly "id", once, a string or an integer. Then the
// top-level members, once and spelled exactly: a member twice, or one that
// differs from a known member only by case, is refused before any decoder
// reads the last of two or folds the case. Then what a Request object must
// have (§4): "jsonrpc" the string "2.0", "method" a string, "params" an
// object or an array when there -- null is none of those. Each of these
// refuses an invalid request (-32600). Last, the params object's own
// members, once and spelled exactly, which refuses invalid params
// (-32602): a request's own fault, which a notification carries
// unanswered. The request returned is the one to dispatch when nothing
// was refused.
func exactEnvelope(line []byte) (envelope, rpcRequest) {
	env := envelope{}
	env.id, env.idPresent, env.idUnique = exactID(line)
	if env.idPresent && env.idUnique && !validID(env.id) {
		env.idUnique = false
		env.err = &rpcError{Code: codeInvalidRequest, Message: "The request's id is not a string or an integer."}
		return env, rpcRequest{}
	}
	if message := membersOnce(line, []string{"jsonrpc", "id", "method", "params"}); message != "" {
		env.err = &rpcError{Code: codeInvalidRequest, Message: "The request " + message}
		return env, rpcRequest{}
	}
	var shape struct {
		JSONRPC *string         `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  *string         `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(line, &shape); err != nil {
		env.err = &rpcError{Code: codeInvalidRequest, Message: "The request's members are not of their types."}
		return env, rpcRequest{}
	}
	if shape.JSONRPC == nil || *shape.JSONRPC != "2.0" {
		env.err = &rpcError{Code: codeInvalidRequest, Message: `The request's "jsonrpc" is not the string "2.0".`}
		return env, rpcRequest{}
	}
	if shape.Method == nil {
		env.err = &rpcError{Code: codeInvalidRequest, Message: `The request's "method" is not a string.`}
		return env, rpcRequest{}
	}
	if len(shape.Params) > 0 && shape.Params[0] != '{' && shape.Params[0] != '[' {
		env.err = &rpcError{Code: codeInvalidRequest, Message: `The request's "params" is not an object or an array.`}
		return env, rpcRequest{}
	}
	request := rpcRequest{JSONRPC: *shape.JSONRPC, ID: shape.ID, Method: *shape.Method, Params: shape.Params}
	if len(shape.Params) > 0 && shape.Params[0] == '{' {
		if message := membersOnce(shape.Params, []string{"name", "arguments", "_meta", "protocolVersion", "capabilities", "clientInfo", "cursor", "uri"}); message != "" {
			env.err = &rpcError{Code: codeInvalidParams, Message: "The params " + message}
		}
	}
	return env, request
}

// exactID walks an object for its id: the value of the member spelled
// exactly "id", whether one is there, and whether it is there once with
// nothing spelled as it by another case -- judged on its own, whatever
// else in the object is refused, so that a request with one id is
// answered under it.
func exactID(object []byte) (id json.RawMessage, present bool, unique bool) {
	decoder := json.NewDecoder(bytes.NewReader(object))
	if tok, err := decoder.Token(); err != nil || tok != json.Delim('{') {
		return nil, false, false
	}
	exact, folded := 0, 0
	for decoder.More() {
		tok, err := decoder.Token()
		if err != nil {
			return nil, false, false
		}
		key, _ := tok.(string)
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, false, false
		}
		switch {
		case key == "id":
			exact++
			id = value
		case strings.EqualFold(key, "id"):
			folded++
		}
	}
	return id, exact > 0, exact == 1 && folded == 0
}

// membersOnce walks one JSON object's members as tokens and reports the
// first given twice, or the first spelled as a known member by another
// case; "" when the object is not one (a later decoder says so) or is in
// order.
func membersOnce(object []byte, known []string) string {
	decoder := json.NewDecoder(bytes.NewReader(object))
	if tok, err := decoder.Token(); err != nil || tok != json.Delim('{') {
		return ""
	}
	seen := map[string]bool{}
	for decoder.More() {
		tok, err := decoder.Token()
		if err != nil {
			return ""
		}
		name, _ := tok.(string)
		if seen[name] {
			return fmt.Sprintf("carries the member %q twice; each member is given once.", name)
		}
		seen[name] = true
		for _, k := range known {
			if name != k && strings.EqualFold(name, k) {
				return fmt.Sprintf("carries %q, which is not the member %q spelled exactly.", name, k)
			}
		}
		var skip json.RawMessage
		if err := decoder.Decode(&skip); err != nil {
			return ""
		}
	}
	return ""
}

// integerLiteral is a JSON integer: no fraction, no exponent, no leading
// zero (json.Valid has already refused one), of any magnitude -- the id is
// echoed as given, never decoded, so its size is not this server's concern.
var integerLiteral = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)

// validID reports whether a raw id is a JSON string or an integer literal,
// which is what an MCP request id may be.
func validID(raw json.RawMessage) bool {
	text := bytes.TrimSpace(raw)
	if len(text) == 0 {
		return false
	}
	if text[0] == '"' {
		var s string
		return json.Unmarshal(text, &s) == nil
	}
	return integerLiteral.Match(text)
}
