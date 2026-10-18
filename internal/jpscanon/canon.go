// Package jpscanon implements the existing Dialect/checkpoint-witness canon form.
// It is neither JCS nor preparation canonicalization. Values must come from carrier.Decode.
package jpscanon

import (
	"encoding/json"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"sort"
	"strconv"
	"strings"
)

const maxInteger = int64(9007199254740991)

// UTF-8 lexical order equals Unicode code-point order for the well-formed
// strings admitted by carrier.Decode. This is not disposition JCS (UTF-16
// ordering) and not preparation canonicalization (which preserves -0).
func Write(out *strings.Builder, value any, path []string) (string, bool) {
	switch v := value.(type) {
	case nil:
		out.WriteString("null")
	case bool:
		out.WriteString(strconv.FormatBool(v))
	case string:
		writeString(out, v)
	case json.Number:
		literal := v.String()
		if strings.ContainsAny(literal, ".eE") {
			return carrier.Pointer(path), false
		}
		n, err := strconv.ParseInt(literal, 10, 64)
		if err != nil || n < -maxInteger || n > maxInteger {
			return carrier.Pointer(path), false
		}
		out.WriteString(strconv.FormatInt(n, 10))
	case []any:
		out.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				out.WriteByte(',')
			}
			if p, ok := Write(out, item, child(path, strconv.Itoa(i))); !ok {
				return p, false
			}
		}
		out.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				out.WriteByte(',')
			}
			writeString(out, key)
			out.WriteByte(':')
			if p, ok := Write(out, v[key], child(path, key)); !ok {
				return p, false
			}
		}
		out.WriteByte('}')
	default:
		panic("carrier returned a value outside JSON")
	}
	return "", true
}
func child(path []string, key string) []string {
	out := make([]string, len(path)+1)
	copy(out, path)
	out[len(path)] = key
	return out
}
func writeString(out *strings.Builder, value string) {
	const hexDigits = "0123456789abcdef"
	out.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"', '\\':
			out.WriteByte('\\')
			out.WriteRune(r)
		case '\b':
			out.WriteString(`\b`)
		case '\f':
			out.WriteString(`\f`)
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		default:
			if r < 0x20 {
				out.WriteString(`\u00`)
				out.WriteByte(hexDigits[r>>4])
				out.WriteByte(hexDigits[r&15])
			} else {
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte('"')
}
