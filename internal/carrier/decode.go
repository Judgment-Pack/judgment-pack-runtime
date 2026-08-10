package carrier

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

type parser struct {
	decoder *json.Decoder
	limits  Limits
	nodes   int
	data    []byte
}

func Decode(data []byte, limits Limits) (any, *Failure) {
	if !utf8.Valid(data) {
		return nil, invalid("JPS-CARRIER-INVALID-JSON", "", "Input is not valid UTF-8 JSON.")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	p := parser{decoder: decoder, limits: limits, data: data}
	value, failure := p.value(nil, 0)
	if failure != nil {
		return nil, failure
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, invalid("JPS-CARRIER-INVALID-JSON", "", "Input contains trailing data after the JSON value.")
	}
	// The surrogate scan runs last, over bytes now known to be well-formed JSON,
	// so its string tracking is exact rather than a guess about where a string
	// begins. What it refuses is what the decoder above silently repaired.
	if offset, found := unpairedSurrogateEscape(data); found {
		return nil, invalid("JPS-CARRIER-INVALID-JSON", "",
			fmt.Sprintf("Input contains an unpaired surrogate escape at byte offset %d. RFC 8785 §3.2.2.2 makes such a value invalid rather than replaceable, and this decoder refuses it rather than substituting U+FFFD.", offset))
	}
	return value, nil
}

// unpairedSurrogateEscape reports the offset of the first \uD800-\uDFFF escape
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
// It runs over JSON already parsed successfully, so a backslash appears only
// inside a string and every escape is well-formed; the walk still checks its
// own bounds rather than trusting that.
func unpairedSurrogateEscape(data []byte) (int, bool) {
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

func (p *parser) value(location []string, depth int) (any, *Failure) {
	if p.nodes >= p.limits.MaxNodes {
		return nil, resource("JPS-RESOURCE-NODE-LIMIT", Pointer(location), "Input exceeds the configured JSON node limit.")
	}
	p.nodes++
	token, err := p.decoder.Token()
	if err != nil {
		return nil, p.invalidJSON(Pointer(location), "", err)
	}
	switch typed := token.(type) {
	case json.Delim:
		if depth >= p.limits.MaxDepth {
			return nil, resource("JPS-RESOURCE-DEPTH-LIMIT", Pointer(location), "Input exceeds the configured JSON nesting limit.")
		}
		switch typed {
		case '{':
			return p.object(location, depth+1)
		case '[':
			return p.array(location, depth+1)
		default:
			return nil, p.invalidJSON(Pointer(location), "unexpected JSON delimiter", nil)
		}
	case string:
		if len(typed) > p.limits.MaxStringBytes {
			return nil, resource("JPS-RESOURCE-STRING-LIMIT", Pointer(location), "Input contains a string exceeding the configured limit.")
		}
		return typed, nil
	case json.Number:
		if len(typed.String()) > p.limits.MaxStringBytes {
			return nil, resource("JPS-RESOURCE-NUMBER-LIMIT", Pointer(location), "Input contains a number token exceeding the configured limit.")
		}
		return typed, nil
	case bool, nil:
		return typed, nil
	default:
		return nil, p.invalidJSON(Pointer(location), "value outside the JSON data model", nil)
	}
}

func (p *parser) object(location []string, depth int) (map[string]any, *Failure) {
	value := map[string]any{}
	for p.decoder.More() {
		token, err := p.decoder.Token()
		if err != nil {
			return nil, p.invalidJSON(Pointer(location), "", err)
		}
		key, ok := token.(string)
		if !ok {
			return nil, p.invalidJSON(Pointer(location), "object member name is not a string", nil)
		}
		memberLocation := appendLocation(location, key)
		if len(key) > p.limits.MaxStringBytes {
			return nil, resource("JPS-RESOURCE-STRING-LIMIT", Pointer(memberLocation), "Input contains an object member name exceeding the configured limit.")
		}
		if _, exists := value[key]; exists {
			return nil, invalid("JPS-CARRIER-DUPLICATE-MEMBER", Pointer(memberLocation), "Object member name is duplicated.")
		}
		item, failure := p.value(memberLocation, depth)
		if failure != nil {
			return nil, failure
		}
		value[key] = item
	}
	if token, err := p.decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, p.invalidJSON(Pointer(location), "expected the end of the object", err)
	}
	return value, nil
}

func (p *parser) array(location []string, depth int) ([]any, *Failure) {
	value := []any{}
	for index := 0; p.decoder.More(); index++ {
		itemLocation := appendLocation(location, fmt.Sprint(index))
		item, failure := p.value(itemLocation, depth)
		if failure != nil {
			return nil, failure
		}
		value = append(value, item)
	}
	if token, err := p.decoder.Token(); err != nil || token != json.Delim(']') {
		return nil, p.invalidJSON(Pointer(location), "expected the end of the array", err)
	}
	return value, nil
}

func Pointer(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	value := ""
	for _, part := range parts {
		value += "/"
		for _, char := range part {
			switch char {
			case '~':
				value += "~0"
			case '/':
				value += "~1"
			default:
				value += string(char)
			}
		}
	}
	return value
}

func appendLocation(location []string, part string) []string {
	copyOfLocation := append([]string(nil), location...)
	return append(copyOfLocation, part)
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
