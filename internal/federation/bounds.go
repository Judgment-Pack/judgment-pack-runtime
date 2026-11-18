package federation

import (
	"encoding/json"
	"math/big"
	"slices"
	"strconv"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// The bounds of §6.1 and §6.2.
const (
	MaxBoundEntries  = 100000
	MaxBoundEnum     = 1024
	MaxBoundPatterns = 16
)

// rowMembers are the members of a bound (§6.2): a registry §3.2 keyword each,
// and patterns.
var rowMembers = map[string]bool{
	"type": true, "enum": true, "const": true, "minimum": true, "exclusiveMinimum": true, "maximum": true,
	"exclusiveMaximum": true, "minLength": true, "maxLength": true, "minItems": true, "maxItems": true,
	"minProperties": true, "maxProperties": true, "uniqueItems": true, "required": true, "patterns": true,
}

// schemaKeywords are registry §3.2's keywords. One of them in a bound that
// §6.2 does not list is FED-BOUND-NOT-COMPARABLE (§6.3); any other unknown
// member is FED-BOUND-FORM.
var schemaKeywords = map[string]bool{
	"type": true, "enum": true, "const": true, "properties": true, "required": true, "additionalProperties": true,
	"minProperties": true, "maxProperties": true, "propertyNames": true, "items": true, "minItems": true,
	"maxItems": true, "uniqueItems": true, "minLength": true, "maxLength": true, "pattern": true, "minimum": true,
	"maximum": true, "exclusiveMinimum": true, "exclusiveMaximum": true, "anyOf": true, "allOf": true, "not": true,
	"$defs": true, "$ref": true, "title": true, "description": true, "default": true, "deprecated": true, "examples": true,
}

var typeNames = map[string]bool{
	"null": true, "boolean": true, "object": true, "array": true, "number": true, "integer": true, "string": true,
}

// constraints are §6.2's rows that a per-environment bound replaces as one
// (§6.4): stating any member of a group replaces the whole group. Length,
// item and member counts are a constraint per member, so that the effective
// bound admits exactly what both admit.
var constraints = [][]string{
	{"type"}, {"enum", "const"}, {"minimum", "exclusiveMinimum"}, {"maximum", "exclusiveMaximum"},
	{"minLength"}, {"maxLength"}, {"minItems"}, {"maxItems"}, {"minProperties"}, {"maxProperties"}, {"uniqueItems"},
}

// added are the rows whose values a per-environment bound adds to the
// field's rather than replaces (§6.4).
var added = []string{"required", "patterns"}

var boundsMembers = map[string]bool{"boundsFormatVersion": true, "kinds": true}
var boundsKindMembers = map[string]bool{"kind": true, "maxEntries": true, "maxAssemblyEntries": true, "fields": true}
var fieldMembers = map[string]bool{"pointer": true, "bound": true, "environments": true, "description": true}

// bound is the usable members of one bound: those of their form.
type bound map[string]any

// field is one item of a bounds kind's fields.
type field struct {
	at           string
	pointer      string
	pattern      registry.PointerPattern
	whole        bool
	bound        bound
	environments map[string]bound
	schemas      map[string]*boundSchema
}

// boundSchema is a bound compiled as a schema: nil schema when it does not
// compile, which admits nothing.
type boundSchema struct {
	schema *registry.Schema
}

// boundsKind is one item of a bounds document's kinds.
type boundsKind struct {
	at          string
	kind        string
	maxEntries  int
	maxAssembly int
	fields      []*field
}

// boundsDocument is one admitted bounds document, read for its form (§6.1).
type boundsDocument struct {
	source *source
	kinds  []*boundsKind
}

// kind returns the document's first item for a kind name, or nil.
func (b *boundsDocument) kind(name string) *boundsKind {
	for _, k := range b.kinds {
		if k.kind == name {
			return k
		}
	}
	return nil
}

// readBounds reads a bounds document's form (§§6.1, 6.3, 6.4). A member not
// of its form is reported and left out, and the rest is read.
func readBounds(root map[string]any, s *source, environments []string, collected *collector) *boundsDocument {
	bad := func(code, pointer string) {
		collected.add(code, Location{Document: DocumentBounds, Source: s.name, Pointer: pointer})
	}
	doc := &boundsDocument{source: s}
	for name := range root {
		if !boundsMembers[name] {
			bad("FED-BOUND-FORM", carrier.Pointer([]string{name}))
		}
	}
	items, isList := root["kinds"].([]any)
	if !isList {
		bad("FED-BOUND-FORM", "/kinds")
		return doc
	}
	var seen []string
	for i, value := range items {
		at := "/kinds/" + strconv.Itoa(i)
		item, isObject := value.(map[string]any)
		name, _ := item["kind"].(string)
		if !isObject || !registry.ValidKindName(name) {
			bad("FED-BOUND-FORM", at)
			continue
		}
		if containsString(seen, name) || (len(seen) > 0 && name < seen[len(seen)-1]) {
			bad("FED-BOUND-FORM", at)
		}
		seen = append(seen, name)
		k := &boundsKind{at: at, kind: name, maxEntries: -1, maxAssembly: -1}
		for member := range item {
			if !boundsKindMembers[member] {
				bad("FED-BOUND-FORM", carrier.Pointer([]string{"kinds", strconv.Itoa(i), member}))
			}
		}
		for _, member := range []string{"maxEntries", "maxAssemblyEntries"} {
			value, present := item[member]
			if !present {
				continue
			}
			count, ok := integerWithin(value, MaxBoundEntries)
			switch {
			case !ok:
				bad("FED-BOUND-FORM", at+"/"+member)
			case member == "maxAssemblyEntries" && !s.standard():
				// A limit only the standard owner speaks for.
				bad("FED-BOUND-FORM", at+"/"+member)
			case member == "maxEntries":
				k.maxEntries = count
			default:
				k.maxAssembly = count
			}
		}
		fields := []any{}
		if value, present := item["fields"]; present {
			list, isList := value.([]any)
			if !isList {
				bad("FED-BOUND-FORM", at+"/fields")
			} else {
				fields = list
			}
		}
		var pointers []string
		for j, value := range fields {
			fat := at + "/fields/" + strconv.Itoa(j)
			object, isObject := value.(map[string]any)
			_, hasPointer := object["pointer"]
			_, hasBound := object["bound"]
			if !isObject || !hasPointer || !hasBound {
				bad("FED-BOUND-FORM", fat)
				continue
			}
			for member := range object {
				if !fieldMembers[member] {
					bad("FED-BOUND-FORM", fat+"/"+escapeToken(member))
				}
			}
			f := &field{at: fat}
			text, isString := object["pointer"].(string)
			if !isString {
				bad("FED-BOUND-FORM", fat+"/pointer")
				continue
			}
			if text == "" {
				f.whole = true
			} else {
				pattern, err := registry.ParsePointerPattern(text)
				if err != nil {
					bad("FED-BOUND-FORM", fat+"/pointer")
					continue
				}
				f.pattern = pattern
			}
			f.pointer = text
			if containsString(pointers, text) || (len(pointers) > 0 && text < pointers[len(pointers)-1]) {
				bad("FED-BOUND-FORM", fat)
			}
			pointers = append(pointers, text)
			if description, present := object["description"]; present {
				if _, isString := description.(string); !isString {
					bad("FED-BOUND-FORM", fat+"/description")
				}
			}
			f.bound = readBound(object["bound"], fat+"/bound", bad)
			f.environments = map[string]bound{}
			if value, present := object["environments"]; present {
				envs, isObject := value.(map[string]any)
				if !isObject {
					bad("FED-BOUND-FORM", fat+"/environments")
				} else {
					for _, env := range sortedKeys(envs) {
						eat := fat + "/environments/" + escapeToken(env)
						if !containsString(environments, env) || (!s.standard() && !containsString(s.serves, env)) {
							// It would bound nothing.
							bad("FED-BOUND-ENVIRONMENT", eat)
							continue
						}
						f.environments[env] = readBound(envs[env], eat, bad)
					}
				}
			}
			k.fields = append(k.fields, f)
		}
		doc.kinds = append(doc.kinds, k)
	}
	return doc
}

// readBound reads one bound's members (§§6.2, 6.3) and returns those of
// their form.
func readBound(value any, at string, bad func(code, pointer string)) bound {
	object, isObject := value.(map[string]any)
	if !isObject {
		bad("FED-BOUND-FORM", at)
		return bound{}
	}
	usable := bound{}
	for _, name := range sortedKeys(object) {
		member := object[name]
		here := at + "/" + escapeToken(name)
		if !rowMembers[name] {
			if schemaKeywords[name] {
				bad("FED-BOUND-NOT-COMPARABLE", here)
			} else {
				bad("FED-BOUND-FORM", here)
			}
			continue
		}
		if !memberForm(name, member) {
			bad("FED-BOUND-FORM", here)
			continue
		}
		usable[name] = member
	}
	return usable
}

// memberForm reports whether a bound member has the form registry §3.2
// gives the keyword of its name, and patterns §6.2's.
func memberForm(name string, value any) bool {
	switch name {
	case "type":
		if text, isString := value.(string); isString {
			return typeNames[text]
		}
		items, isList := value.([]any)
		if !isList || len(items) == 0 {
			return false
		}
		var seen []string
		for _, item := range items {
			text, isString := item.(string)
			if !isString || !typeNames[text] || containsString(seen, text) {
				return false
			}
			seen = append(seen, text)
		}
		return true
	case "enum":
		items, isList := value.([]any)
		return isList && len(items) > 0 && len(items) <= MaxBoundEnum
	case "const":
		return true
	case "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum":
		_, ok := ratOf(value)
		return ok
	case "minLength", "maxLength", "minItems", "maxItems", "minProperties", "maxProperties":
		_, ok := countOf(value)
		return ok
	case "uniqueItems":
		_, ok := value.(bool)
		return ok
	case "required":
		items, isList := value.([]any)
		return isList && distinctStrings(items)
	case "patterns":
		items, isList := value.([]any)
		if !isList || len(items) == 0 || len(items) > MaxBoundPatterns || !distinctStrings(items) {
			return false
		}
		for _, item := range items {
			if _, err := registry.CompilePattern(item.(string)); err != nil {
				return false
			}
		}
		return true
	}
	return false
}

func distinctStrings(items []any) bool {
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		text, isString := item.(string)
		if !isString || seen[text] {
			return false
		}
		seen[text] = true
	}
	return true
}

// effective is §6.4's effective bound of a field in an environment: its
// bound, with each row the environment states in that row's place, and the
// environment's required names and patterns added to the field's. fromEnv
// says which members come from the environment.
func (f *field) effective(env string) (out bound, fromEnv map[string]bool) {
	over := f.environments[env]
	out, fromEnv = bound{}, map[string]bool{}
	for _, group := range constraints {
		stated := false
		for _, member := range group {
			if _, ok := over[member]; ok {
				stated = true
				break
			}
		}
		from := f.bound
		if stated {
			from = over
		}
		for _, member := range group {
			if value, ok := from[member]; ok {
				out[member] = value
				fromEnv[member] = stated
			}
		}
	}
	for _, member := range added {
		var values []any
		if list, ok := f.bound[member].([]any); ok {
			values = append(values, list...)
		}
		if list, ok := over[member].([]any); ok {
			for _, value := range list {
				if !slices.ContainsFunc(values, func(v any) bool { return registry.JSONEqual(v, value) }) {
					values = append(values, value)
				}
			}
		}
		if len(values) > 0 {
			out[member] = values
		}
	}
	return out, fromEnv
}

// admits reports whether a value meets a field's effective bound in an
// environment: the bound as a schema of registry §3.2, its patterns an allOf
// of pattern keywords, by registry §7.2's meanings.
func (f *field) admits(env string, value any) bool {
	if f.schemas == nil {
		f.schemas = map[string]*boundSchema{}
	}
	compiled, ok := f.schemas[env]
	if !ok {
		b, _ := f.effective(env)
		compiled = compileBound(b)
		f.schemas[env] = compiled
	}
	return compiled.admits(value)
}

func compileBound(b bound) *boundSchema {
	raw := map[string]any{}
	for name, value := range b {
		if name != "patterns" {
			raw[name] = value
		}
	}
	if patterns, ok := b["patterns"].([]any); ok && len(patterns) > 0 {
		all := make([]any, 0, len(patterns))
		for _, pattern := range patterns {
			all = append(all, map[string]any{"pattern": pattern})
		}
		raw["allOf"] = all
	}
	schema, findings := registry.CompileSchema(raw)
	if len(findings) > 0 {
		// A bound of its form compiles; one that does not admits nothing,
		// so that a defect here never lets a value past a bound.
		return &boundSchema{}
	}
	return &boundSchema{schema: schema}
}

func (b *boundSchema) admits(value any) bool {
	return b.schema != nil && len(b.schema.Validate(value)) == 0
}

// widenings returns the members of R whose row is looser than S's, or that
// name a value S's whole bound refuses (§6.2). Each row is compared on its
// own, never across rows; a row only one side states is inherited or added,
// and never a widening.
func widenings(r, s bound) []string {
	var out []string
	if rTypes, sTypes := typeList(r["type"]), typeList(s["type"]); rTypes != nil && sTypes != nil {
		for _, t := range rTypes {
			if !containsString(sTypes, t) && !(t == "integer" && containsString(sTypes, "number")) {
				out = append(out, "type")
				break
			}
		}
	}
	whole := compileBound(s)
	sSet, hasSet := valueSet(s)
	for _, member := range []string{"enum", "const"} {
		value, present := r[member]
		if !present {
			continue
		}
		values := []any{value}
		if member == "enum" {
			values = value.([]any)
		}
		for _, v := range values {
			inSet := !hasSet || slices.ContainsFunc(sSet, func(w any) bool { return registry.JSONEqual(v, w) })
			if !inSet || !whole.admits(v) {
				out = append(out, member)
				break
			}
		}
	}
	for _, upper := range []bool{false, true} {
		rb, sb := limitOf(r, upper), limitOf(s, upper)
		if rb == nil || sb == nil {
			continue
		}
		order := rb.value.Cmp(sb.value)
		tighter := order > 0
		if upper {
			tighter = order < 0
		}
		if !tighter && !(order == 0 && (rb.exclusive || !sb.exclusive)) {
			out = append(out, rb.member)
		}
	}
	for _, pair := range [][2]string{{"minLength", "maxLength"}, {"minItems", "maxItems"}, {"minProperties", "maxProperties"}} {
		if rv, sv, ok := countPair(r, s, pair[0]); ok && rv.Cmp(sv) < 0 {
			out = append(out, pair[0])
		}
		if rv, sv, ok := countPair(r, s, pair[1]); ok && rv.Cmp(sv) > 0 {
			out = append(out, pair[1])
		}
	}
	if rUnique, present := r["uniqueItems"]; present && s["uniqueItems"] == true && rUnique != true {
		out = append(out, "uniqueItems")
	}
	return out
}

func typeList(value any) []string {
	switch typed := value.(type) {
	case string:
		return []string{typed}
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	}
	return nil
}

// valueSet is a bound's value set: its enum, its const as a one-value enum,
// or the enum's values equal to the const when it states both.
func valueSet(b bound) ([]any, bool) {
	enum, hasEnum := b["enum"].([]any)
	constant, hasConst := b["const"]
	switch {
	case hasEnum && hasConst:
		var out []any
		for _, v := range enum {
			if registry.JSONEqual(v, constant) {
				out = append(out, v)
			}
		}
		return out, true
	case hasEnum:
		return enum, true
	case hasConst:
		return []any{constant}, true
	}
	return nil, false
}

// limit is a bound's lower or upper bound: the tighter of its inclusive and
// exclusive members, the member that gives it, and whether it is exclusive.
type limit struct {
	value     *big.Rat
	exclusive bool
	member    string
}

func limitOf(b bound, upper bool) *limit {
	inclusive, exclusive := "minimum", "exclusiveMinimum"
	if upper {
		inclusive, exclusive = "maximum", "exclusiveMaximum"
	}
	var best *limit
	for _, candidate := range []struct {
		member    string
		exclusive bool
	}{{inclusive, false}, {exclusive, true}} {
		value, ok := ratOf(b[candidate.member])
		if !ok {
			continue
		}
		if best == nil {
			best = &limit{value: value, exclusive: candidate.exclusive, member: candidate.member}
			continue
		}
		order := value.Cmp(best.value)
		tighter := order > 0
		if upper {
			tighter = order < 0
		}
		if tighter || (order == 0 && candidate.exclusive) {
			best = &limit{value: value, exclusive: candidate.exclusive, member: candidate.member}
		}
	}
	return best
}

func countPair(r, s bound, member string) (*big.Rat, *big.Rat, bool) {
	rv, rok := ratOf(r[member])
	sv, sok := ratOf(s[member])
	return rv, sv, rok && sok
}

// ratOf is a JSON number's exact value (registry §7.2).
func ratOf(value any) (*big.Rat, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return nil, false
	}
	return new(big.Rat).SetString(number.String())
}

// maxCount is registry §3.2's largest count, 2^53 − 1.
var maxCount = new(big.Rat).SetInt64(1<<53 - 1)

// countOf reads a count of registry §3.2: a number with a non-negative
// integer value of at most 2^53 − 1 (2.0 is a count).
func countOf(value any) (*big.Rat, bool) {
	rat, ok := ratOf(value)
	if !ok || !rat.IsInt() || rat.Sign() < 0 || rat.Cmp(maxCount) > 0 {
		return nil, false
	}
	return rat, true
}

// integerWithin reads a number with an integer value from 0 to most.
func integerWithin(value any, most int) (int, bool) {
	rat, ok := ratOf(value)
	if !ok || !rat.IsInt() || rat.Sign() < 0 || rat.Cmp(new(big.Rat).SetInt64(int64(most))) > 0 {
		return 0, false
	}
	return int(rat.Num().Int64()), true
}

// overlaps is §6.3's overlap: one field's pointer can match a position the
// other's matches.
func (f *field) overlaps(other *field) bool {
	if f.whole || other.whole {
		return f.whole && other.whole
	}
	return f.pattern.Overlaps(other.pattern)
}

// positions are the places in a value the field's pointer matches: the
// value itself for "", and otherwise each position the pattern matches. A
// matched position absent from the value is no position.
func (f *field) positions(value any) []registry.Position {
	if f.whole {
		return []registry.Position{{Pointer: "", Value: value}}
	}
	return f.pattern.Positions(value)
}

func escapeToken(token string) string {
	return carrier.Pointer([]string{token})[1:]
}

func sortedKeys[V any](object map[string]V) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
