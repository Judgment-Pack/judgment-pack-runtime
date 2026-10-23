package validation

import (
	"encoding/json"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

const overlapRewriteUnavailable = "The exclusive replacement exceeds the configured carrier limits or advisory generation budget."

// overlapShape checks the complete generated shape before marshaling or making
// pointer-derived containers. Its depth/node rules mirror carrier.Decode; every
// traversed node and string/number byte is charged to the shared work budget.
type overlapShape struct {
	limits carrier.Limits
	budget *overlapBudget
	nodes  int
	bytes  int64
}

func (s *overlapShape) size(n int) bool {
	s.bytes += int64(n)
	return s.bytes <= carrier.HardMaxBytes
}

// string accounts for exactly encoding/json's spelling without allocating that
// spelling. Source strings are admitted UTF-8, as are decoded pointer tokens.
func (s *overlapShape) string(value string) bool {
	if len(value) > s.limits.MaxStringBytes || !s.budget.take(len(value)+1) {
		return false
	}
	if !s.size(2) {
		return false
	}
	for i := 0; i < len(value); i++ {
		n := 1
		switch value[i] {
		case '"', '\\', '\b', '\f', '\n', '\r', '\t':
			n = 2
		case '<', '>', '&':
			n = 6
		default:
			if value[i] < 0x20 {
				n = 6
			}
			if value[i] == 0xe2 && i+2 < len(value) && value[i+1] == 0x80 && (value[i+2] == 0xa8 || value[i+2] == 0xa9) {
				n = 6
				i += 2
			}
		}
		if !s.size(n) {
			return false
		}
	}
	return true
}

func (s *overlapShape) value(value any, depth int) bool {
	if s.nodes >= s.limits.MaxNodes || !s.budget.take(1) {
		return false
	}
	s.nodes++
	switch v := value.(type) {
	case map[string]any:
		if depth >= s.limits.MaxDepth || !s.size(2) {
			return false
		}
		for key, item := range v {
			if !s.string(key) || !s.size(1) || !s.value(item, depth+1) {
				return false
			}
		}
		return len(v) == 0 || s.size(len(v)-1)
	case []any:
		if depth >= s.limits.MaxDepth || !s.size(2) {
			return false
		}
		for _, item := range v {
			if !s.value(item, depth+1) {
				return false
			}
		}
		return len(v) == 0 || s.size(len(v)-1)
	case string:
		return s.string(v)
	case json.Number:
		return len(v) <= s.limits.MaxStringBytes && s.budget.take(len(v)+1) && s.size(len(v))
	case bool:
		if v {
			return s.size(4)
		}
		return s.size(5)
	case nil:
		return s.size(4)
	default:
		return false
	}
}

func boundedExclusiveRewrite(root map[string]any, candidate *result.OverlapRewrite, limits carrier.Limits, budget *overlapBudget) *result.OverlapRewrite {
	rules := root["rules"].([]any)
	index := -1
	for i, raw := range rules {
		if !budget.take(1) {
			return nil
		}
		if raw.(map[string]any)["id"] == candidate.RuleID {
			index = i
			break
		}
	}
	if index < 0 {
		return nil
	}
	rule := rules[index].(map[string]any)
	// Charge the shallow replacement view before allocating it. Unchanged trees
	// remain shared; no deep copy or marshaling precedes complete shape preflight.
	if !budget.take(len(root) + len(rules) + len(rule) + 1) {
		return nil
	}
	replacement := make(map[string]any, len(rule))
	for key, value := range rule {
		replacement[key] = value
	}
	replacement["when"] = candidate.When
	replacementRules := append([]any(nil), rules...)
	replacementRules[index] = replacement
	pack := make(map[string]any, len(root))
	for key, value := range root {
		pack[key] = value
	}
	pack["rules"] = replacementRules
	shape := overlapShape{limits: limits, budget: budget}
	if !shape.value(pack, 0) {
		return nil
	}
	encoded, err := json.Marshal(pack)
	if err != nil || int64(len(encoded)) > carrier.HardMaxBytes {
		return nil
	}
	if _, failure := carrier.Decode(encoded, limits); failure != nil {
		return nil
	}
	return candidate
}

func boundedOverlapFacts(path string, value any, limits carrier.Limits, budget *overlapBudget) (json.RawMessage, bool) {
	// Charge the source pointer before scanning or tokenizing. In particular an
	// admitted megabyte pointer must not allocate hundreds of thousands of maps.
	if !budget.take(len(path) + 1) {
		return nil, false
	}
	count := strings.Count(path, "/")
	if count > limits.MaxDepth || count >= limits.MaxNodes || !budget.take(count) {
		return nil, false
	}
	tokens := []string{}
	if path != "" {
		tokens = strings.Split(path[1:], "/")
	}
	shape := overlapShape{limits: limits, budget: budget, nodes: count}
	for i, token := range tokens {
		token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		tokens[i] = token
		if !shape.string(token) || !shape.size(3) {
			return nil, false
		}
	}
	if !shape.value(value, count) {
		return nil, false
	}
	// The complete node/depth/string/byte/work preflight succeeded. Encode the
	// bounded pointer shape directly, without ever constructing nested maps.
	leaf, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	var buffer strings.Builder
	buffer.Grow(int(shape.bytes))
	for _, token := range tokens {
		key, _ := json.Marshal(token)
		buffer.WriteByte('{')
		buffer.Write(key)
		buffer.WriteByte(':')
	}
	buffer.Write(leaf)
	for range tokens {
		buffer.WriteByte('}')
	}
	encoded := json.RawMessage(buffer.String())
	if _, failure := carrier.Decode(encoded, limits); failure != nil {
		return nil, false
	}
	return encoded, true
}
