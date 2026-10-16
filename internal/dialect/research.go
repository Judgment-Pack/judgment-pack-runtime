package dialect

// Offline research declaration checks from RFC 0018 §§4.3–4.4 and 6.4.
// No model, acquisition, binding or answer is evaluated by these checks.
import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

func (v *documentCheck) boundedText(value any, path string, max int) string {
	s := v.text(value, path, true)
	if len(s) > max {
		v.refuse("JPS-DIALECT-COUNT-LIMIT", path, "String exceeds this form's UTF-8 byte limit.")
	}
	return s
}
func (v *documentCheck) research(s map[string]any, path string, scope map[string]string) map[string]bool {
	required := []string{"name", "kind", "read", "maxAge", "question", "constraints", "model", "evidence", "fields", "capabilities"}
	bound := false
	for _, key := range []string{"profile", "profileDigest", "webProfiles"} {
		if _, ok := s[key]; ok {
			bound = true
		}
	}
	if bound {
		required = append(required, "profile", "profileDigest", "webProfiles")
	}
	v.object(s, path, required, []string{"parameters"})
	v.integer(s["maxAge"], at(path, "maxAge"), 1, 86400)
	if _, ok := s["question"].(string); !ok {
		q := v.dictionary(s["question"], at(path, "question"))
		if len(q) != 1 {
			v.fail(at(path, "question"), "Question requires a string template.")
		}
		if p, ok := q["$param"]; ok {
			name := v.text(p, at(path, "question"), true)
			if scope[name] != "string" && scope[name] != "timestamp" {
				v.fail(at(path, "question"), "Question parameter must be a string.")
			}
		} else if _, ok := q["$text"]; !ok {
			v.fail(at(path, "question"), "Question requires a string template.")
		}
		v.template(s["question"], at(path, "question"), scope)
	}
	v.dictionary(s["constraints"], at(path, "constraints"))
	v.boundedText(s["model"], at(path, "model"), 256)
	for i, e := range v.list(s["evidence"], at(path, "evidence"), 0, 0) {
		v.boundedText(e, at(at(path, "evidence"), strconv.Itoa(i)), 4096)
	}
	fields := map[string]bool{}
	for i, item := range v.list(s["fields"], at(path, "fields"), 1, 64) {
		p := at(at(path, "fields"), strconv.Itoa(i))
		f := v.object(item, p, []string{"name", "type", "required", "description"}, nil)
		name := v.boundedText(f["name"], at(p, "name"), 64)
		if name == "case" || fields[name] {
			v.fail(at(p, "name"), "Field name is reserved or repeated.")
		}
		fields[name] = true
		v.choice(f["type"], at(p, "type"), "integer", "decimal", "boolean", "text", "text-list", "range")
		if _, ok := f["required"].(bool); !ok {
			v.fail(at(p, "required"), "Required must be a boolean.")
		}
		v.boundedText(f["description"], at(p, "description"), 2048)
	}
	capabilities := v.list(s["capabilities"], at(path, "capabilities"), 1, 3)
	last := -1
	web := []string{}
	for i, c := range capabilities {
		name := v.choice(c, at(at(path, "capabilities"), strconv.Itoa(i)), "answer", "search", "page")
		index := map[string]int{"answer": 0, "search": 1, "page": 2}[name]
		if index <= last || i == 0 && name != "answer" {
			v.fail(at(path, "capabilities"), "Capabilities require answer, then distinct search and page in that order.")
		}
		last = index
		if name != "answer" {
			web = append(web, name)
		}
	}
	if bound {
		v.researchProfile(s, path)
		assignments := v.object(s["webProfiles"], at(path, "webProfiles"), web, nil)
		for _, key := range keys(assignments) {
			p := at(at(path, "webProfiles"), key)
			assignment := v.object(assignments[key], p, []string{"profile", "profileDigest"}, nil)
			v.researchProfile(assignment, p)
		}
	}
	return fields
}
func (v *documentCheck) researchProfile(s map[string]any, path string) {
	v.text(s["profile"], at(path, "profile"), true)
	if !digestPattern.MatchString(v.text(s["profileDigest"], at(path, "profileDigest"), true)) {
		v.fail(at(path, "profileDigest"), "Expected a lowercase SHA-256 digest.")
	}
}
func pointerParts(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(strings.TrimPrefix(s, "/"), "/")
	for i, p := range parts {
		parts[i] = strings.ReplaceAll(strings.ReplaceAll(p, "~1", "/"), "~0", "~")
	}
	return parts
}

var researchIndex = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)

func researchPointer(s string, fields map[string]bool, taken bool) bool {
	parts := pointerParts(s)
	if len(parts) == 1 && parts[0] == "fields" {
		return true
	}
	if len(parts) < 2 || parts[0] != "fields" || !fields[parts[1]] {
		return false
	}
	if len(parts) == 2 {
		return true
	}
	switch parts[2] {
	case "value":
		return true
	case "status", "cites":
		return len(parts) == 3
	case "values":
		return !taken && len(parts) == 5 && researchIndex.MatchString(parts[3]) && parts[4] == "value"
	}
	return false
}

// Read only pointer-bearing syntax. Constants may contain arbitrary member names.
func readPointers(read map[string]any, visit func(string, bool)) {
	if unwrap, ok := read["unwrap"].([]any); ok {
		for _, p := range unwrap {
			if s, ok := p.(string); ok {
				visit(s, true)
			}
		}
	}
	if copy, ok := read["copy"].(map[string]any); ok {
		for _, k := range []string{"facts", "evidence"} {
			items, _ := copy[k].([]any)
			for _, item := range items {
				f, _ := item.(map[string]any)
				if s, ok := f["source"].(string); ok {
					visit(s, true)
				}
			}
		}
	}
	if rule, ok := read["rule"].(map[string]any); ok {
		clauses, _ := rule["clauses"].([]any)
		for _, item := range clauses {
			clause, _ := item.(map[string]any)
			walkConditions(clause["when"], func(c map[string]any) {
				if s, ok := c["field"].(string); ok {
					visit(s, false)
				}
			})
			claim, _ := clause["claim"].(map[string]any)
			facts, _ := claim["facts"].([]any)
			for _, f := range facts {
				entry, _ := f.(map[string]any)
				if s, ok := entry["from"].(string); ok {
					visit(s, true)
				}
			}
		}
	}
}
func walkConditions(value any, visit func(map[string]any)) {
	c, ok := value.(map[string]any)
	if !ok {
		return
	}
	visit(c)
	if c["op"] == "all" || c["op"] == "any" {
		items, _ := c["of"].([]any)
		for _, item := range items {
			walkConditions(item, visit)
		}
	} else if c["op"] == "not" {
		walkConditions(c["of"], visit)
	}
}
func (v *documentCheck) researchRead(value any, path string, scope map[string]string, fields map[string]bool, earlier map[string]map[string]bool) readTargets {
	read := v.dictionary(value, path)
	if _, ok := read["sources"]; !ok {
		targets := v.read(value, path, scope)
		if v.issue == nil {
			readPointers(read, func(ptr string, taken bool) {
				if !researchPointer(ptr, fields, taken) {
					v.fail(path, "Research read names a pointer outside declared field values or selects a conflicting observation.")
				}
			})
		}
		return targets
	}
	v.object(value, path, []string{"sources", "rule"}, nil)
	entries := map[string]bool{}
	for name := range fields {
		entries["/current/fields/"+escape(name)] = true
	}
	seen := map[string]bool{}
	for i, raw := range v.list(read["sources"], at(path, "sources"), 1, 15) {
		name := v.text(raw, at(at(path, "sources"), strconv.Itoa(i)), true)
		prior, ok := earlier[name]
		if !ok || seen[name] {
			v.fail(path, "Reconciliation must name distinct earlier research sources.")
		}
		seen[name] = true
		for field := range prior {
			entries["/sources/"+escape(name)+"/fields/"+escape(field)] = true
		}
	}
	targets := v.rule(read["rule"], at(path, "rule"), scope)
	if v.issue != nil {
		return targets
	}
	rule := read["rule"].(map[string]any)
	clauses := rule["clauses"].([]any)
	every := map[string]bool{}
	for _, raw := range clauses {
		c := raw.(map[string]any)
		walkConditions(c["when"], func(condition map[string]any) { every[jsonKey(condition)] = true })
	}
	readPointers(read, func(ptr string, taken bool) {
		if taken {
			return
		}
		for entry := range entries {
			if ptr == entry || ptr == entry+"/status" || ptr == entry+"/value" || strings.HasPrefix(ptr, entry+"/value/") || ptr == entry+"/values" || strings.HasPrefix(ptr, entry+"/values/") {
				return
			}
		}
		v.fail(path, "Reconciliation condition must read a declared field of a named source.")
	})
	for i, raw := range clauses {
		c := raw.(map[string]any)
		claim := c["claim"].(map[string]any)
		facts := claim["facts"].([]any)
		if len(claim["evidence"].(map[string]any)) != 0 {
			v.fail(path, "Reconciliation may not write evidence.")
		}
		if i == len(clauses)-1 && (len(facts) != 0 || claim["acquisitionStatus"] != "unknown" && claim["acquisitionStatus"] != "absent") {
			v.fail(path, "Final reconciliation clause must leave facts absent and acquisition unknown or absent.")
		}
		for _, rawFact := range facts {
			fact := rawFact.(map[string]any)
			ptr := fact["from"].(string)
			entry := ""
			for candidate := range entries {
				if ptr == candidate+"/value" || strings.HasPrefix(ptr, candidate+"/value/") {
					entry = candidate
					break
				}
			}
			if entry == "" {
				v.fail(path, "Reconciliation facts must select a declared field value.")
				continue
			}
			guard := func(status string) map[string]any {
				return map[string]any{"op": "equals", "field": entry + "/status", "to": status}
			}
			found := jsonKey(guard("found"))
			when := c["when"].(map[string]any)
			gated := jsonKey(when) == found
			if when["op"] == "all" {
				for _, condition := range when["of"].([]any) {
					gated = gated || jsonKey(condition) == found
				}
			}
			if !gated {
				v.fail(path, "A selected field must be gated by its found status.")
			}
			for _, status := range []string{"not-found", "stale", "conflicting"} {
				if !every[jsonKey(guard(status))] {
					v.fail(path, "Reconciliation must explicitly handle every unresolved field state.")
				}
			}
			if !every[jsonKey(map[string]any{"op": "not", "of": map[string]any{"op": "exists", "field": entry}})] {
				v.fail(path, "Reconciliation must explicitly handle a missing field.")
			}
		}
	}
	return targets
}
func escape(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1") }
func jsonKey(v any) string   { b, _ := json.Marshal(v); return string(b) }
