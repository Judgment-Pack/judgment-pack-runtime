package carrier

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

const (
	HardMaxBytes          int64 = 10 * 1024 * 1024
	DefaultMaxDepth             = 128
	DefaultMaxNodes             = 250_000
	DefaultMaxStringBytes       = 1024 * 1024
)

type Limits struct {
	MaxDepth       int
	MaxNodes       int
	MaxStringBytes int
}

func DefaultLimits() Limits {
	return Limits{
		MaxDepth:       DefaultMaxDepth,
		MaxNodes:       DefaultMaxNodes,
		MaxStringBytes: DefaultMaxStringBytes,
	}
}

type Failure struct {
	Resource   bool
	Diagnostic result.Diagnostic
}

func (f *Failure) Error() string {
	return f.Diagnostic.Message
}

// parser is one decode. A document that is valid JSON is read from data
// directly, at offset at; any other is read through the standard decoder's
// tokens, so that its syntax error is the decoder's own. Either way, path is
// the location of the value being read, step by step, made into a pointer
// only for a refusal that reports one.
type parser struct {
	decoder  *json.Decoder
	limits   Limits
	nodes    int
	data     []byte
	at       int
	rootScan bool
	path     []step
}

// step is one step of a location: a member's name, or an array's index when
// index is not negative.
type step struct {
	name  string
	index int
}

func (p *parser) enter(name string, index int) {
	p.path = append(p.path, step{name: name, index: index})
}

func (p *parser) leave() {
	p.path = p.path[:len(p.path)-1]
}

// pointer is the JSON Pointer of the value being read.
func (p *parser) pointer() string {
	if len(p.path) == 0 {
		return ""
	}
	parts := make([]string, len(p.path))
	for i, step := range p.path {
		parts[i] = step.name
		if step.index >= 0 {
			parts[i] = strconv.Itoa(step.index)
		}
	}
	return Pointer(parts)
}

func Decode(data []byte, limits Limits) (any, *Failure) {
	return decode(data, limits, false)
}

// ScanRootDeclarations parses the complete bounded carrier while retaining only
// the root version and profile declarations. It never accepts a JSON prefix.
func ScanRootDeclarations(data []byte, limits Limits) (map[string]any, *Failure) {
	value, failure := decode(data, limits, true)
	root, _ := value.(map[string]any)
	return root, failure
}

func decode(data []byte, limits Limits, rootScan bool) (any, *Failure) {
	if !utf8.Valid(data) {
		return nil, invalid("JPS-CARRIER-INVALID-JSON", "", "Input is not valid UTF-8 JSON.")
	}
	value, failure := decodeValid(data, limits, rootScan)
	if failure == errUnread {
		value, failure = decodeTokens(data, limits, rootScan)
	}
	if failure != nil {
		return nil, failure
	}
	// The surrogate scan runs last, over bytes now known to be well-formed JSON,
	// so its string tracking is exact rather than a guess about where a string
	// begins. What it refuses is what the decoder above silently repaired.
	if offset, found := UnpairedSurrogateEscape(data); found {
		return nil, invalid("JPS-CARRIER-INVALID-JSON", "",
			fmt.Sprintf("Input contains an unpaired surrogate escape at byte offset %d. RFC 8785 §3.2.2.2 makes such a value invalid rather than replaceable, and this decoder refuses it rather than substituting U+FFFD.", offset))
	}
	return value, nil
}

// UnpairedSurrogateEscape reports the offset of the first \uD800-\uDFFF escape
// that is not one half of a well-formed pair, and whether one was found.
//
// Go's decoder replaces such an escape with U+FFFD without complaint, which is
// silent lossy repair of a value RFC 8785 §3.2.2.2 says a canonicalizer must
// refuse: an authored "\ud800" would otherwise canonicalize to the same bytes
// as a literal U+FFFD, so two different documents would compare equal and a
// byte comparison §8.3 requires to be exact would not be. Refusing at the
// carrier fixes it once for every document that reaches this runtime — pack,
// matrix, facts, evidence, configuration, and graph — rather than once per
// reader.
//
// It is exported for the one caller outside this package that faces the same
// defect one level up: the MCP stdio transport, where the escape is written in
// the JSON-RPC message rather than in a document it carries, and no carrier is
// reached because the tool argument has already been repaired into a Go string
// (ADR-0038). Every other reader gets this through Decode.
//
// Callers must hand it bytes that already parsed as JSON, so a backslash
// appears only inside a string and every escape is well-formed; the walk still
// checks its own bounds rather than trusting that. Over bytes that are not
// JSON its string tracking is a guess — an unterminated string makes the rest
// of the input look like string content — so a caller settles JSON validity
// first and reports that defect instead.
func UnpairedSurrogateEscape(data []byte) (int, bool) {
	inString := false
	for index := 0; index < len(data); index++ {
		if !inString {
			if data[index] == '"' {
				inString = true
			}
			continue
		}
		switch data[index] {
		case '"':
			inString = false
		case '\\':
			if index+1 >= len(data) {
				return 0, false
			}
			if data[index+1] != 'u' {
				index++
				continue
			}
			value, ok := hexQuad(data, index+2)
			if !ok {
				return 0, false
			}
			index += 5
			if value < 0xD800 || value > 0xDFFF {
				continue
			}
			// A low surrogate can never open a pair, and a high one must be
			// followed immediately by another \u escape carrying a low one.
			if value > 0xDBFF {
				return index - 5, true
			}
			low, ok := hexQuad(data, index+3)
			if !ok || index+1 >= len(data) || data[index+1] != '\\' || data[index+2] != 'u' || low < 0xDC00 || low > 0xDFFF {
				return index - 5, true
			}
			index += 6
		}
	}
	return 0, false
}

// hexQuad reads the four hex digits of one \u escape at start.
func hexQuad(data []byte, start int) (int, bool) {
	if start+4 > len(data) {
		return 0, false
	}
	value := 0
	for _, char := range data[start : start+4] {
		digit := 0
		switch {
		case char >= '0' && char <= '9':
			digit = int(char - '0')
		case char >= 'a' && char <= 'f':
			digit = int(char-'a') + 10
		case char >= 'A' && char <= 'F':
			digit = int(char-'A') + 10
		default:
			return 0, false
		}
		value = value*16 + digit
	}
	return value, true
}

// errUnread is decodeValid's: the document is not one it reads, and the
// standard decoder's tokens read it instead.
var errUnread = &Failure{}

// decodeValid reads a document that is valid JSON straight from its bytes:
// the standard decoder allocates a syntax error for every value it ends at
// a delimiter, which a valid document never reports. It refuses what the
// token reading refuses, at the same location, and gives the same value: the
// same nodes counted in the same order, the same strings and number literals.
// A document that is not valid JSON, or that this reading does not finish, is
// errUnread.
func decodeValid(data []byte, limits Limits, rootScan bool) (any, *Failure) {
	if !json.Valid(data) {
		return nil, errUnread
	}
	p := parser{limits: limits, data: data, rootScan: rootScan}
	value, failure := p.read(0)
	if failure != nil {
		return nil, failure
	}
	if p.space(); p.at != len(data) {
		return nil, errUnread
	}
	return value, nil
}

// decodeTokens reads a document through the standard decoder's tokens: for a
// document that is not valid JSON, the decoder names the defect and where.
func decodeTokens(data []byte, limits Limits, rootScan bool) (any, *Failure) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	p := parser{decoder: decoder, limits: limits, data: data, rootScan: rootScan}
	value, failure := p.value(0)
	if failure != nil {
		return nil, failure
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, invalid("JPS-CARRIER-INVALID-JSON", "", "Input contains trailing data after the JSON value.")
	}
	return value, nil
}

// read is value over valid JSON's bytes.
func (p *parser) read(depth int) (any, *Failure) {
	if p.nodes >= p.limits.MaxNodes {
		return nil, resource("JPS-RESOURCE-NODE-LIMIT", p.pointer(), "Input exceeds the configured JSON node limit.")
	}
	p.nodes++
	if p.space(); p.at >= len(p.data) {
		return nil, errUnread
	}
	switch c := p.data[p.at]; c {
	case '{', '[':
		if depth >= p.limits.MaxDepth {
			return nil, resource("JPS-RESOURCE-DEPTH-LIMIT", p.pointer(), "Input exceeds the configured JSON nesting limit.")
		}
		p.at++
		if c == '{' {
			return p.readObject(depth + 1)
		}
		return p.readArray(depth + 1)
	case '"':
		text, ok := p.readString()
		if !ok {
			return nil, errUnread
		}
		if len(text) > p.limits.MaxStringBytes {
			return nil, resource("JPS-RESOURCE-STRING-LIMIT", p.pointer(), "Input contains a string exceeding the configured limit.")
		}
		return text, nil
	case 't':
		return p.literal("true", true)
	case 'f':
		return p.literal("false", false)
	case 'n':
		return p.literal("null", nil)
	default:
		start := p.at
		for p.at < len(p.data) && strings.IndexByte("+-.0123456789Ee", p.data[p.at]) >= 0 {
			p.at++
		}
		if p.at == start {
			return nil, errUnread
		}
		if p.at-start > p.limits.MaxStringBytes {
			return nil, resource("JPS-RESOURCE-NUMBER-LIMIT", p.pointer(), "Input contains a number token exceeding the configured limit.")
		}
		return json.Number(p.data[start:p.at]), nil
	}
}

func (p *parser) literal(text string, value any) (any, *Failure) {
	if len(p.data)-p.at < len(text) || string(p.data[p.at:p.at+len(text)]) != text {
		return nil, errUnread
	}
	p.at += len(text)
	return value, nil
}

func (p *parser) readObject(depth int) (map[string]any, *Failure) {
	value := map[string]any{}
	var seen map[string]bool
	if p.rootScan {
		seen = map[string]bool{}
	}
	if p.space(); p.next('}') {
		return value, nil
	}
	for {
		if p.space(); p.at >= len(p.data) || p.data[p.at] != '"' {
			return nil, errUnread
		}
		key, ok := p.readString()
		if !ok {
			return nil, errUnread
		}
		p.enter(key, -1)
		if len(key) > p.limits.MaxStringBytes {
			return nil, resource("JPS-RESOURCE-STRING-LIMIT", p.pointer(), "Input contains an object member name exceeding the configured limit.")
		}
		_, exists := value[key]
		if p.rootScan {
			exists = seen[key]
		}
		if exists {
			return nil, invalid("JPS-CARRIER-DUPLICATE-MEMBER", p.pointer(), "Object member name is duplicated.")
		}
		if p.rootScan {
			seen[key] = true
		}
		if p.space(); !p.next(':') {
			return nil, errUnread
		}
		item, failure := p.read(depth)
		if failure != nil {
			return nil, failure
		}
		p.leave()
		if !p.rootScan || depth == 1 && (key == "specVersion" || key == "profile") {
			value[key] = item
		}
		switch p.space(); {
		case p.next(','):
		case p.next('}'):
			return value, nil
		default:
			return nil, errUnread
		}
	}
}

func (p *parser) readArray(depth int) ([]any, *Failure) {
	value := []any{}
	if p.space(); p.next(']') {
		return value, nil
	}
	for index := 0; ; index++ {
		p.enter("", index)
		item, failure := p.read(depth)
		if failure != nil {
			return nil, failure
		}
		p.leave()
		if !p.rootScan {
			value = append(value, item)
		}
		switch p.space(); {
		case p.next(','):
		case p.next(']'):
			return value, nil
		default:
			return nil, errUnread
		}
	}
}

// space skips JSON whitespace.
func (p *parser) space() {
	for p.at < len(p.data) {
		switch p.data[p.at] {
		case ' ', '\t', '\n', '\r':
			p.at++
		default:
			return
		}
	}
}

// next consumes c if it is the next byte.
func (p *parser) next(c byte) bool {
	if p.at < len(p.data) && p.data[p.at] == c {
		p.at++
		return true
	}
	return false
}

// readString reads the string at p.at, its quotes included, as the standard
// decoder unquotes one: each escape replaced, a surrogate pair joined into
// its code point, and any other surrogate escape replaced by U+FFFD (the
// surrogate scan refuses the document afterwards). A string with no escape
// is its bytes.
func (p *parser) readString() (string, bool) {
	start := p.at + 1
	end := start
	escaped := false
	for {
		if end >= len(p.data) {
			return "", false
		}
		switch p.data[end] {
		case '"':
			p.at = end + 1
			if !escaped {
				return string(p.data[start:end]), true
			}
			return unquote(p.data[start:end])
		case '\\':
			escaped = true
			end += 2
			continue
		}
		end++
	}
}

// unquote replaces a string's escapes, as the standard decoder does.
func unquote(raw []byte) (string, bool) {
	var out strings.Builder
	out.Grow(len(raw))
	for i := 0; i < len(raw); {
		c := raw[i]
		if c != '\\' {
			out.WriteByte(c)
			i++
			continue
		}
		if i+1 >= len(raw) {
			return "", false
		}
		switch raw[i+1] {
		case '"', '\\', '/':
			out.WriteByte(raw[i+1])
		case 'b':
			out.WriteByte('\b')
		case 'f':
			out.WriteByte('\f')
		case 'n':
			out.WriteByte('\n')
		case 'r':
			out.WriteByte('\r')
		case 't':
			out.WriteByte('\t')
		case 'u':
			code, ok := hexQuad(raw, i+2)
			if !ok {
				return "", false
			}
			i += 6
			r := rune(code)
			if utf16.IsSurrogate(r) {
				if low, ok := hexQuad(raw, i+2); ok && raw[i] == '\\' && raw[i+1] == 'u' {
					if pair := utf16.DecodeRune(r, rune(low)); pair != unicode.ReplacementChar {
						out.WriteRune(pair)
						i += 6
						continue
					}
				}
				r = unicode.ReplacementChar
			}
			out.WriteRune(r)
			continue
		default:
			return "", false
		}
		i += 2
	}
	return out.String(), true
}

func (p *parser) value(depth int) (any, *Failure) {
	if p.nodes >= p.limits.MaxNodes {
		return nil, resource("JPS-RESOURCE-NODE-LIMIT", p.pointer(), "Input exceeds the configured JSON node limit.")
	}
	p.nodes++
	token, err := p.decoder.Token()
	if err != nil {
		return nil, p.invalidJSON(p.pointer(), "", err)
	}
	switch typed := token.(type) {
	case json.Delim:
		if depth >= p.limits.MaxDepth {
			return nil, resource("JPS-RESOURCE-DEPTH-LIMIT", p.pointer(), "Input exceeds the configured JSON nesting limit.")
		}
		switch typed {
		case '{':
			return p.object(depth + 1)
		case '[':
			return p.array(depth + 1)
		default:
			return nil, p.invalidJSON(p.pointer(), "unexpected JSON delimiter", nil)
		}
	case string:
		if len(typed) > p.limits.MaxStringBytes {
			return nil, resource("JPS-RESOURCE-STRING-LIMIT", p.pointer(), "Input contains a string exceeding the configured limit.")
		}
		return typed, nil
	case json.Number:
		if len(typed.String()) > p.limits.MaxStringBytes {
			return nil, resource("JPS-RESOURCE-NUMBER-LIMIT", p.pointer(), "Input contains a number token exceeding the configured limit.")
		}
		return typed, nil
	case bool, nil:
		return typed, nil
	default:
		return nil, p.invalidJSON(p.pointer(), "value outside the JSON data model", nil)
	}
}

func (p *parser) object(depth int) (map[string]any, *Failure) {
	value := map[string]any{}
	var seen map[string]bool
	if p.rootScan {
		seen = map[string]bool{}
	}
	for p.decoder.More() {
		token, err := p.decoder.Token()
		if err != nil {
			return nil, p.invalidJSON(p.pointer(), "", err)
		}
		key, ok := token.(string)
		if !ok {
			return nil, p.invalidJSON(p.pointer(), "object member name is not a string", nil)
		}
		p.enter(key, -1)
		if len(key) > p.limits.MaxStringBytes {
			return nil, resource("JPS-RESOURCE-STRING-LIMIT", p.pointer(), "Input contains an object member name exceeding the configured limit.")
		}
		_, exists := value[key]
		if p.rootScan {
			exists = seen[key]
		}
		if exists {
			return nil, invalid("JPS-CARRIER-DUPLICATE-MEMBER", p.pointer(), "Object member name is duplicated.")
		}
		if p.rootScan {
			seen[key] = true
		}
		item, failure := p.value(depth)
		if failure != nil {
			return nil, failure
		}
		p.leave()
		if !p.rootScan || depth == 1 && (key == "specVersion" || key == "profile") {
			value[key] = item
		}
	}
	if token, err := p.decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, p.invalidJSON(p.pointer(), "expected the end of the object", err)
	}
	return value, nil
}

func (p *parser) array(depth int) ([]any, *Failure) {
	value := []any{}
	for index := 0; p.decoder.More(); index++ {
		p.enter("", index)
		item, failure := p.value(depth)
		if failure != nil {
			return nil, failure
		}
		p.leave()
		if !p.rootScan {
			value = append(value, item)
		}
	}
	if token, err := p.decoder.Token(); err != nil || token != json.Delim(']') {
		return nil, p.invalidJSON(p.pointer(), "expected the end of the array", err)
	}
	return value, nil
}

func Pointer(parts []string) string {
	l := len(parts)
	if l == 0 {
		return ""
	}
	var value strings.Builder
	for i := 0; i < l; i++ {
		value.WriteByte('/')
		for _, c := range parts[i] {
			switch c {
			case '~':
				value.WriteString("~0")
			case '/':
				value.WriteString("~1")
			default:
				value.WriteRune(c)
			}
		}
	}
	return value.String()
}

func invalid(code, location, message string) *Failure {
	return &Failure{Diagnostic: result.ErrorDiagnostic(code, "carrier", location, message)}
}

func resource(code, location, message string) *Failure {
	return &Failure{Resource: true, Diagnostic: result.ErrorDiagnostic(code, "operation", location, message)}
}

// invalidJSON reports a carrier parse failure with its line, column, and byte
// offset, preserving (and sanitizing) the decoder's own reason when one is
// available so an author can find and fix the exact spot.
func (p *parser) invalidJSON(location, detail string, err error) *Failure {
	offset := p.decoder.InputOffset()
	line, column := lineColumn(p.data, offset)
	message := fmt.Sprintf("Input is not valid JSON at line %d, column %d (byte offset %d)", line, column, offset)
	if err != nil {
		detail = display.Sanitize(err.Error())
	}
	if detail != "" {
		message += ": " + detail
	}
	return invalid("JPS-CARRIER-INVALID-JSON", location, message+".")
}

func lineColumn(data []byte, offset int64) (int, int) {
	if offset > int64(len(data)) {
		offset = int64(len(data))
	}
	line, column := 1, 1
	for index := int64(0); index < offset; index++ {
		if data[index] == '\n' {
			line++
			column = 1
		} else {
			column++
		}
	}
	return line, column
}
