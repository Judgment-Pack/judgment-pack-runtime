package carrier

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

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
}

func Decode(data []byte, limits Limits) (any, *Failure) {
	if !utf8.Valid(data) {
		return nil, invalid("JPS-CARRIER-INVALID-JSON", "", "Input is not valid UTF-8 JSON.")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	p := parser{decoder: decoder, limits: limits}
	value, failure := p.value(nil, 0)
	if failure != nil {
		return nil, failure
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, invalid("JPS-CARRIER-INVALID-JSON", "", "Input contains trailing data after the JSON value.")
	}
	return value, nil
}

func (p *parser) value(location []string, depth int) (any, *Failure) {
	if p.nodes >= p.limits.MaxNodes {
		return nil, resource("JPS-RESOURCE-NODE-LIMIT", Pointer(location), "Input exceeds the configured JSON node limit.")
	}
	p.nodes++
	token, err := p.decoder.Token()
	if err != nil {
		return nil, invalid("JPS-CARRIER-INVALID-JSON", Pointer(location), "Input is not valid complete JSON.")
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
			return nil, invalid("JPS-CARRIER-INVALID-JSON", Pointer(location), "Input contains an unexpected JSON delimiter.")
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
		return nil, invalid("JPS-CARRIER-INVALID-JSON", Pointer(location), "Input contains a value outside the JSON data model.")
	}
}

func (p *parser) object(location []string, depth int) (map[string]any, *Failure) {
	value := map[string]any{}
	for p.decoder.More() {
		token, err := p.decoder.Token()
		if err != nil {
			return nil, invalid("JPS-CARRIER-INVALID-JSON", Pointer(location), "Input contains an incomplete JSON object.")
		}
		key, ok := token.(string)
		if !ok {
			return nil, invalid("JPS-CARRIER-INVALID-JSON", Pointer(location), "Input contains an invalid JSON object member.")
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
		return nil, invalid("JPS-CARRIER-INVALID-JSON", Pointer(location), "Input contains an incomplete JSON object.")
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
		return nil, invalid("JPS-CARRIER-INVALID-JSON", Pointer(location), "Input contains an incomplete JSON array.")
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
