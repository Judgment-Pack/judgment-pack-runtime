package dialect

import (
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// Validate checks the version-2 document's closed forms and internal references.
// Profile-dependent checks, preparation, source truth and pack compatibility
// require other inputs and are never implied by document-valid.
func Validate(command string, data []byte) (Report, int) {
	r, code := Encode(command, data)
	r.ValidationScope = "version-2-document"
	if code != result.ExitSuccess {
		return r, code
	}
	if r.DialectVersion != 2 {
		return refused(r, "JPS-DIALECT-VALIDATION-VERSION", "/version", "Document validation supports version 2. Version 3 encoding does not imply validation support.", result.ExitUnsupported)
	}
	// Encode has already bounded and checked the carrier. Keep json.Number here
	// too; validation must never round a retained integer through float64.
	value, failure := carrier.Decode(data, carrier.Limits{MaxDepth: MaxDepth, MaxNodes: MaxDocumentBytes, MaxStringBytes: MaxDocumentBytes})
	if failure != nil {
		return refused(r, "JPS-DIALECT-DOCUMENT", "", "The dialect could not be read.", result.ExitInvalid)
	}
	v := documentCheck{}
	v.document(value)
	if v.issue != nil {
		return refused(r, v.issue.Code, v.issue.InstancePath, v.issue.Message, result.ExitInvalid)
	}
	r.Status = "document-valid"
	return r, result.ExitSuccess
}

type documentCheck struct{ issue *result.Diagnostic }
type target struct{ name, path string }
type readTargets struct{ facts, evidence []target }

func (v *documentCheck) fail(path, message string) {
	v.refuse("JPS-DIALECT-FORM", path, message)
}
func (v *documentCheck) refuse(code, path, message string) {
	if v.issue == nil {
		d := result.ErrorDiagnostic(code, "dialect", path, message)
		v.issue = &d
	}
}
func at(path, member string) string {
	return path + "/" + strings.ReplaceAll(strings.ReplaceAll(member, "~", "~0"), "/", "~1")
}
func keys[T any](m map[string]T) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
func (v *documentCheck) object(value any, path string, required, optional []string) map[string]any {
	obj, ok := value.(map[string]any)
	if !ok {
		v.fail(path, "Expected an object.")
		return nil
	}
	allowed := map[string]bool{}
	for _, key := range required {
		allowed[key] = true
		if _, ok := obj[key]; !ok {
			v.fail(at(path, key), "Required member is missing.")
		}
	}
	for _, key := range optional {
		allowed[key] = true
	}
	for _, key := range keys(obj) {
		if !allowed[key] {
			v.fail(at(path, key), "Member is not defined for this form.")
		}
	}
	return obj
}
func (v *documentCheck) dictionary(value any, path string) map[string]any {
	obj, ok := value.(map[string]any)
	if !ok {
		v.fail(path, "Expected an object.")
	}
	return obj
}
func (v *documentCheck) list(value any, path string, min, max int) []any {
	items, ok := value.([]any)
	if !ok {
		v.fail(path, "Expected an array.")
		return nil
	}
	if len(items) < min {
		v.fail(path, "Array has fewer entries than this form requires.")
	}
	if max > 0 && len(items) > max {
		v.refuse("JPS-DIALECT-COUNT-LIMIT", path, "Array exceeds this form's entry limit.")
		return nil
	}
	return items
}
func (v *documentCheck) text(value any, path string, nonempty bool) string {
	s, ok := value.(string)
	if !ok || (nonempty && s == "") {
		v.fail(path, "Expected a string of this form.")
	}
	return s
}
func (v *documentCheck) choice(value any, path string, choices ...string) string {
	s := v.text(value, path, false)
	for _, c := range choices {
		if s == c {
			return s
		}
	}
	v.fail(path, "Value is not defined for this form.")
	return s
}
func (v *documentCheck) integer(value any, path string, min, max int64) {
	n, ok := value.(json.Number)
	i, err := n.Int64()
	if !ok || err != nil || i < min || i > max {
		v.fail(path, "Expected an integer in this form's range.")
	}
}
func pointer(s string) bool {
	if s == "" {
		return true
	}
	if s[0] != '/' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] == '~' {
			i++
			if i == len(s) || (s[i] != '0' && s[i] != '1') {
				return false
			}
		}
	}
	return true
}
func (v *documentCheck) pointer(value any, path string, target bool) string {
	s := v.text(value, path, false)
	if !pointer(s) || (target && s == "") {
		v.fail(path, "Expected a JSON pointer permitted for this target.")
	}
	return s
}

func (v *documentCheck) document(value any) {
	top := v.object(value, "", []string{"version", "case"}, []string{"sources", "admits", "unmapped", "unmappedEvidence", "provider", "facts", "evidence"})
	if value, ok := top["provider"]; ok {
		v.choice(value, "/provider", "")
	}
	for _, key := range []string{"facts", "evidence"} {
		if value, ok := top[key]; ok && value != nil {
			v.fail(at("", key), "Legacy member must be null when present.")
		}
	}
	for _, key := range []string{"unmapped", "unmappedEvidence"} {
		if value, ok := top[key]; ok {
			seen := map[string]bool{}
			for i, item := range v.list(value, at("", key), 0, 0) {
				p := at(at("", key), strconv.Itoa(i))
				var s string
				if key == "unmapped" {
					s = v.pointer(item, p, false)
				} else {
					s = v.text(item, p, true)
				}
				if seen[s] {
					v.fail(p, "Entry is repeated.")
				}
				seen[s] = true
			}
		}
	}
	c := v.object(top["case"], "/case", []string{"facts", "evidence"}, []string{"parameters"})
	caseScope := map[string]string{"runAt": "timestamp"}
	if declarations, ok := c["parameters"]; ok {
		v.parameters(declarations, "/case/parameters", caseScope, nil)
	}
	caseTargets := v.copy(map[string]any{"facts": c["facts"], "evidence": c["evidence"]}, "/case")
	all := []readTargets{caseTargets}
	previous := map[string]readTargets{}
	if raw, ok := top["sources"]; ok {
		for i, rawSource := range v.list(raw, "/sources", 0, 16) {
			p := at("/sources", strconv.Itoa(i))
			source := v.dictionary(rawSource, p)
			name := v.text(source["name"], at(p, "name"), true)
			if _, ok := previous[name]; ok || name == "case" {
				v.fail(at(p, "name"), "Source name is reserved or repeated.")
			}
			scope := map[string]string{}
			for n, t := range caseScope {
				scope[n] = t
			}
			if declarations, ok := source["parameters"]; ok {
				v.parameters(declarations, at(p, "parameters"), scope, previous)
			}
			v.source(source, p, scope)
			targets := v.read(source["read"], at(p, "read"), scope)
			all = append(all, targets)
			previous[name] = targets
			if v.issue != nil {
				return
			}
		}
	}
	facts := []target{}
	evidence := map[string]bool{}
	written := map[string]bool{}
	for _, r := range all {
		for _, t := range r.facts {
			if len(facts) >= 256 {
				v.refuse("JPS-DIALECT-COUNT-LIMIT", "", "A dialect admits at most 256 fact targets.")
				return
			}
			for _, other := range facts {
				// Valid JSON pointer spellings are unique by decoded token. A slash
				// in a token is escaped, so this tests whole-token prefixes only.
				if t.name == other.name || strings.HasPrefix(t.name, other.name+"/") || strings.HasPrefix(other.name, t.name+"/") {
					v.fail(t.path, "Fact targets must not be equal or overlap by whole tokens.")
				}
			}
			facts = append(facts, t)
			written[t.name] = true
		}
		for _, t := range r.evidence {
			if evidence[t.name] {
				v.fail(t.path, "An evidence requirement has more than one source.")
			}
			evidence[t.name] = true
		}
	}
	if len(facts) > 256 || len(evidence) > 128 {
		v.refuse("JPS-DIALECT-COUNT-LIMIT", "", "A dialect admits at most 256 fact targets and 128 evidence targets.")
	}
	if raw, ok := top["admits"]; ok {
		admits := v.object(raw, "/admits", nil, []string{"facts", "evidence"})
		for _, member := range []string{"facts", "evidence"} {
			if raw, ok := admits[member]; ok {
				p := at("/admits", member)
				entries := v.dictionary(raw, p)
				for _, name := range keys(entries) {
					path := at(p, name)
					if (member == "facts" && !written[name]) || (member == "evidence" && !evidence[name]) {
						v.fail(path, "Admission names a target that no source writes.")
					}
					for i, c := range v.list(entries[name], path, 0, 0) {
						v.choice(c, at(path, strconv.Itoa(i)), "asserted", "record", "generated")
					}
				}
			}
		}
	}
}

func (v *documentCheck) parameters(value any, path string, scope map[string]string, previous map[string]readTargets) {
	declarations := v.dictionary(value, path)
	for _, name := range keys(declarations) {
		p := at(path, name)
		if name == "" || name == "runAt" {
			v.fail(p, "Parameter name is empty or reserved.")
		}
		if _, ok := scope[name]; ok {
			v.fail(p, "Source parameter shadows a case parameter.")
		}
		required := []string{"pointer", "type"}
		if previous != nil {
			required = append(required, "from")
		}
		d := v.object(declarations[name], p, required, nil)
		ptr := v.pointer(d["pointer"], at(p, "pointer"), false)
		typ := v.choice(d["type"], at(p, "type"), "string", "integer", "timestamp")
		if previous != nil {
			from := v.text(d["from"], at(p, "from"), true)
			r, ok := previous[from]
			if !ok {
				v.fail(at(p, "from"), "Source parameter must name an earlier source.")
			}
			found := false
			for _, t := range r.facts {
				if t.name == ptr {
					found = true
				}
			}
			if !found {
				v.fail(at(p, "pointer"), "Source parameter must name a fact target of its earlier source.")
			}
		}
		scope[name] = typ
	}
	if len(scope) > 64 {
		v.refuse("JPS-DIALECT-COUNT-LIMIT", path, "A scope admits at most 64 parameters, including runAt.")
	}
}

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func (v *documentCheck) source(s map[string]any, path string, scope map[string]string) {
	kind := v.choice(s["kind"], at(path, "kind"), "operation", "selected-file")
	required := []string{"name", "kind", "read"}
	optional := []string{"parameters"}
	acquired := kind == "operation"
	if kind == "selected-file" {
		required = append(required, "provider")
		provider := v.choice(s["provider"], at(path, "provider"), "local-file", "google-drive")
		acquired = provider == "google-drive"
	}
	if acquired {
		required = append(required, "profile", "profileDigest", "maxAge", "arguments")
	}
	if kind == "operation" {
		optional = append(optional, "calculation")
	}
	v.object(s, path, required, optional)
	if acquired {
		v.text(s["profile"], at(path, "profile"), true)
		d := v.text(s["profileDigest"], at(path, "profileDigest"), true)
		if !digestPattern.MatchString(d) {
			v.fail(at(path, "profileDigest"), "Expected a lowercase SHA-256 digest.")
		}
		v.integer(s["maxAge"], at(path, "maxAge"), 1, 86400)
		request := v.dictionary(s["arguments"], at(path, "arguments"))
		for _, k := range keys(request) {
			if strings.HasPrefix(k, "$") {
				v.fail(at(path, "arguments"), "A request is an object, not a template form.")
			}
		}
		v.template(request, at(path, "arguments"), scope)
	}
	if raw, ok := s["calculation"]; ok {
		p := at(path, "calculation")
		c := v.object(raw, p, []string{"inputs", "tables"}, nil)
		inputs := v.dictionary(c["inputs"], at(p, "inputs"))
		for _, name := range keys(inputs) {
			where := at(at(p, "inputs"), name)
			param := v.text(inputs[name], where, true)
			if _, ok := scope[param]; !ok || param == "runAt" {
				v.fail(where, "Calculation input must name a parameter in scope other than runAt.")
			}
		}
		tables := v.dictionary(c["tables"], at(p, "tables"))
		for _, name := range keys(tables) {
			v.integer(tables[name], at(at(p, "tables"), name), 1, 315360000)
		}
	}
}
func (v *documentCheck) template(value any, path string, scope map[string]string) {
	if v.issue != nil {
		return
	}
	switch value := value.(type) {
	case map[string]any:
		for _, name := range keys(value) {
			if !strings.HasPrefix(name, "$") {
				continue
			}
			if len(value) != 1 {
				v.fail(path, "A template form must contain exactly one member.")
				return
			}
			argument := v.text(value[name], at(path, name), false)
			switch name {
			case "$param":
				if _, ok := scope[argument]; !ok || argument == "runAt" {
					v.fail(path, "Template parameter must be in scope and cannot be runAt.")
				}
			case "$text":
				for {
					start := strings.Index(argument, "{{")
					if start < 0 {
						break
					}
					argument = argument[start+2:]
					end := strings.Index(argument, "}}")
					if end < 0 {
						v.fail(path, "Text template has an unclosed placeholder.")
						break
					}
					if scope[argument[:end]] != "integer" {
						v.fail(path, "Text placeholder must name an integer parameter in scope.")
					}
					argument = argument[end+2:]
				}
			default:
				v.fail(path, "Unknown template form.")
			}
			return
		}
		for _, name := range keys(value) {
			v.template(value[name], at(path, name), scope)
		}
	case []any:
		for i, item := range value {
			v.template(item, at(path, strconv.Itoa(i)), scope)
		}
	}
}

func (v *documentCheck) read(value any, path string, scope map[string]string) readTargets {
	r := v.object(value, path, nil, []string{"copy", "rule", "unwrap"})
	_, copyOK := r["copy"]
	_, ruleOK := r["rule"]
	if copyOK == ruleOK {
		v.fail(path, "A read requires exactly one of copy and rule.")
	}
	if raw, ok := r["unwrap"]; ok {
		p := at(path, "unwrap")
		for i, ptr := range v.list(raw, p, 1, 4) {
			v.pointer(ptr, at(p, strconv.Itoa(i)), false)
		}
	}
	if copyOK {
		return v.copy(r["copy"], at(path, "copy"))
	}
	return v.rule(r["rule"], at(path, "rule"), scope)
}
func (v *documentCheck) copy(value any, path string) readTargets {
	c := v.object(value, path, nil, []string{"facts", "evidence"})
	r := readTargets{}
	if raw, ok := c["facts"]; ok {
		p := at(path, "facts")
		for i, item := range v.list(raw, p, 0, 256) {
			q := at(p, strconv.Itoa(i))
			f := v.object(item, q, []string{"target", "source"}, nil)
			t := v.pointer(f["target"], at(q, "target"), true)
			v.pointer(f["source"], at(q, "source"), false)
			r.facts = append(r.facts, target{t, at(q, "target")})
		}
	}
	if raw, ok := c["evidence"]; ok {
		p := at(path, "evidence")
		seen := map[string]bool{}
		for i, item := range v.list(raw, p, 0, 128) {
			q := at(p, strconv.Itoa(i))
			e := v.object(item, q, []string{"requirement", "source"}, nil)
			name := v.text(e["requirement"], at(q, "requirement"), true)
			v.pointer(e["source"], at(q, "source"), false)
			if seen[name] {
				v.fail(at(q, "requirement"), "Copy names an evidence requirement more than once.")
			}
			seen[name] = true
			r.evidence = append(r.evidence, target{name, at(q, "requirement")})
		}
	}
	return r
}
func (v *documentCheck) rule(value any, path string, scope map[string]string) readTargets {
	r := v.object(value, path, []string{"ruleVersion", "parameters", "clauses"}, nil)
	v.choice(r["ruleVersion"], at(path, "ruleVersion"), "1")
	parameters := v.dictionary(r["parameters"], at(path, "parameters"))
	declared := map[string]string{}
	for _, name := range keys(parameters) {
		p := at(at(path, "parameters"), name)
		typ := v.choice(parameters[name], p, "string", "integer", "timestamp")
		if scope[name] != typ {
			v.fail(p, "Rule parameter must be in scope with its declared type.")
		}
		declared[name] = typ
	}
	clauses := v.list(r["clauses"], at(path, "clauses"), 1, 64)
	targets := readTargets{}
	facts, evidence := map[string]bool{}, map[string]bool{}
	for i, raw := range clauses {
		p := at(at(path, "clauses"), strconv.Itoa(i))
		c := v.object(raw, p, []string{"when", "claim", "reason"}, nil)
		v.condition(c["when"], at(p, "when"), declared)
		v.text(c["reason"], at(p, "reason"), false)
		if i == len(clauses)-1 {
			last, ok := c["when"].(map[string]any)
			if !ok || last["op"] != "always" || len(last) != 1 {
				v.fail(at(p, "when"), "The last clause must have the always condition.")
			}
		}
		q := at(p, "claim")
		claim := v.object(c["claim"], q, []string{"facts", "evidence", "acquisitionStatus"}, nil)
		v.choice(claim["acquisitionStatus"], at(q, "acquisitionStatus"), "resolved", "absent", "unknown")
		for j, item := range v.list(claim["facts"], at(q, "facts"), 0, 0) {
			where := at(at(q, "facts"), strconv.Itoa(j))
			f := v.object(item, where, []string{"pointer", "from"}, nil)
			t := v.pointer(f["pointer"], at(where, "pointer"), true)
			v.pointer(f["from"], at(where, "from"), false)
			if !facts[t] {
				targets.facts = append(targets.facts, target{t, at(where, "pointer")})
				facts[t] = true
			}
		}
		e := v.dictionary(claim["evidence"], at(q, "evidence"))
		for _, name := range keys(e) {
			where := at(at(q, "evidence"), name)
			if name == "" {
				v.fail(where, "Evidence requirement id must be non-empty.")
			}
			v.choice(e[name], where, "present", "absent", "unknown")
			if !evidence[name] {
				targets.evidence = append(targets.evidence, target{name, where})
				evidence[name] = true
			}
		}
		if v.issue != nil {
			return targets
		}
	}
	return targets
}
func (v *documentCheck) condition(value any, path string, declared map[string]string) {
	if v.issue != nil {
		return
	}
	c := v.dictionary(value, path)
	op := v.text(c["op"], at(path, "op"), false)
	members := map[string][]string{
		"always": {}, "exists": {"field"}, "equals": {"field", "to"}, "equalsParam": {"field", "param"},
		"isTrue": {"field"}, "isDecimalString": {"field"}, "freshWithin": {"field", "asOf", "maxAge"},
		"all": {"of"}, "any": {"of"}, "not": {"of"},
	}
	shape, ok := members[op]
	if !ok {
		v.fail(at(path, "op"), "Unknown condition operation.")
		return
	}
	v.object(c, path, append([]string{"op"}, shape...), nil)
	if raw, ok := c["field"]; ok {
		v.pointer(raw, at(path, "field"), false)
	}
	for _, name := range []string{"param", "asOf", "maxAge"} {
		if raw, ok := c[name]; ok {
			param := v.text(raw, at(path, name), true)
			if _, ok := declared[param]; !ok {
				v.fail(at(path, name), "Condition parameter must be declared by its rule.")
			}
		}
	}
	if op == "all" || op == "any" {
		for i, item := range v.list(c["of"], at(path, "of"), 0, 64) {
			v.condition(item, at(at(path, "of"), strconv.Itoa(i)), declared)
		}
	} else if op == "not" {
		v.condition(c["of"], at(path, "of"), declared)
	}
}
