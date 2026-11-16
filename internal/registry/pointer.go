package registry

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
)

// parsePointer parses an RFC 6901 JSON Pointer into its decoded tokens.
func parsePointer(pointer string) ([]string, error) {
	if pointer == "" {
		return []string{}, nil
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, fmt.Errorf("a pointer begins with /")
	}
	raw := strings.Split(pointer[1:], "/")
	tokens := make([]string, len(raw))
	for i, token := range raw {
		decoded, err := unescapeToken(token, false)
		if err != nil {
			return nil, err
		}
		tokens[i] = decoded
	}
	return tokens, nil
}

// pointerTokenCount is the number of reference tokens in a pointer that
// begins with /, counted without splitting it and at most limit+1: the scan
// stops once the count passes limit.
func pointerTokenCount(pointer string, limit int) int {
	count := 0
	for i := 0; i < len(pointer) && count <= limit; i++ {
		if pointer[i] == '/' {
			count++
		}
	}
	return count
}

// parsePointerWithin is parsePointer for a pointer read no deeper than limit
// tokens. One deeper is reported as over, with no tokens, once its form is
// checked by a scan of its bytes, and before any token is split out: its
// cost is its length, not its tokens. A malformed pointer gets
// parsePointer's error whatever its depth.
func parsePointerWithin(pointer string, limit int) ([]string, bool, error) {
	if !strings.HasPrefix(pointer, "/") || pointerTokenCount(pointer, limit) <= limit {
		tokens, err := parsePointer(pointer)
		return tokens, false, err
	}
	for i := 0; i < len(pointer); i++ {
		if pointer[i] == '~' && (i+1 >= len(pointer) || pointer[i+1] != '0' && pointer[i+1] != '1') {
			return nil, false, fmt.Errorf("a ~ must be followed by 0 or 1")
		}
	}
	return nil, true, nil
}

func unescapeToken(token string, allowStar bool) (string, error) {
	if !strings.Contains(token, "~") {
		return token, nil
	}
	var out strings.Builder
	for i := 0; i < len(token); i++ {
		if token[i] != '~' {
			out.WriteByte(token[i])
			continue
		}
		if i+1 >= len(token) {
			return "", fmt.Errorf("a ~ must be followed by 0 or 1")
		}
		switch token[i+1] {
		case '0':
			out.WriteByte('~')
		case '1':
			out.WriteByte('/')
		case '2':
			if !allowStar {
				return "", fmt.Errorf("~2 is defined only in pointer patterns")
			}
			out.WriteByte('*')
		default:
			return "", fmt.Errorf("a ~ must be followed by 0 or 1")
		}
		i++
	}
	return out.String(), nil
}

// patternToken is one token of a pointer pattern: a literal, or * for any
// member name or array index.
type patternToken struct {
	literal string
	any     bool
}

// pointerPattern is a parsed pointer pattern (RFC 0034 §2.3).
type pointerPattern struct {
	source string
	tokens []patternToken
}

// parsePointerPattern parses a pointer pattern. An unescaped * token is the
// wildcard and ~2 is a literal *. The empty pattern, which would name the
// whole value, is refused: no kind needs it, and a whole value that is a
// reference is not a registry entry.
func parsePointerPattern(source string) (pointerPattern, error) {
	if source == "" || !strings.HasPrefix(source, "/") {
		return pointerPattern{}, fmt.Errorf("a pointer pattern begins with /")
	}
	if len(source) > 512 {
		return pointerPattern{}, fmt.Errorf("longer than 512 bytes")
	}
	var tokens []patternToken
	for _, raw := range strings.Split(source[1:], "/") {
		if raw == "*" {
			tokens = append(tokens, patternToken{any: true})
			continue
		}
		decoded, err := unescapeToken(raw, true)
		if err != nil {
			return pointerPattern{}, err
		}
		tokens = append(tokens, patternToken{literal: decoded})
	}
	return pointerPattern{source: source, tokens: tokens}, nil
}

// matches reports whether the pattern matches the concrete path exactly.
func (p pointerPattern) matches(path []string) bool {
	if len(path) != len(p.tokens) {
		return false
	}
	for i, token := range p.tokens {
		if !token.any && token.literal != path[i] {
			return false
		}
	}
	return true
}

// overlaps reports whether some concrete path matches both patterns.
func (p pointerPattern) overlaps(other pointerPattern) bool {
	if len(p.tokens) != len(other.tokens) {
		return false
	}
	for i, token := range p.tokens {
		theirs := other.tokens[i]
		if !token.any && !theirs.any && token.literal != theirs.literal {
			return false
		}
	}
	return true
}

// position is one concrete place in a value and what it holds.
type position struct {
	path  []string
	value any
}

// positions returns every place in value that the pattern matches, in
// document order (members by sorted name).
func (p pointerPattern) positions(value any) []position {
	var out []position
	var visit func(node any, depth int, path []string)
	visit = func(node any, depth int, path []string) {
		if depth == len(p.tokens) {
			out = append(out, position{path: path, value: node})
			return
		}
		token := p.tokens[depth]
		switch typed := node.(type) {
		case map[string]any:
			if token.any {
				for _, name := range sortedKeys(typed) {
					visit(typed[name], depth+1, appendPath(path, name))
				}
			} else if child, ok := typed[token.literal]; ok {
				visit(child, depth+1, appendPath(path, token.literal))
			}
		case []any:
			if token.any {
				for index, item := range typed {
					visit(item, depth+1, appendPath(path, strconv.Itoa(index)))
				}
			} else if index, ok := arrayIndex(token.literal, len(typed)); ok {
				visit(typed[index], depth+1, appendPath(path, token.literal))
			}
		}
	}
	visit(value, 0, nil)
	return out
}

// arrayIndex reads an RFC 6901 array index: decimal digits, no leading zero.
func arrayIndex(token string, length int) (int, bool) {
	if token == "" || len(token) > 1 && token[0] == '0' || strings.TrimLeft(token, "0123456789") != "" || len(token) > 9 {
		return 0, false
	}
	index, err := strconv.Atoi(token)
	return index, err == nil && index < length
}

// lookup returns the value at path in value.
func lookup(value any, path []string) (any, bool) {
	node := value
	for _, token := range path {
		switch typed := node.(type) {
		case map[string]any:
			child, ok := typed[token]
			if !ok {
				return nil, false
			}
			node = child
		case []any:
			index, ok := arrayIndex(token, len(typed))
			if !ok {
				return nil, false
			}
			node = typed[index]
		default:
			return nil, false
		}
	}
	return node, true
}

func pointerOf(path []string) string { return carrier.Pointer(path) }

func isPrefix(prefix, path []string) bool {
	if len(prefix) > len(path) {
		return false
	}
	for i := range prefix {
		if prefix[i] != path[i] {
			return false
		}
	}
	return true
}
