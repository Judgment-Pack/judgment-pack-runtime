package registry

import (
	"slices"
	"strconv"
	"strings"
)

// Reading a facts contract and checking a pack against it (RFC 0037 §1, §5,
// ADR-0095). The check reads the pack's evaluation form and evaluates
// nothing. Its advisories never change an evaluation.

// FactsCheckFormatVersion is the format of the check report.
const FactsCheckFormatVersion = "1.0.0"

// FactsCheck is the report of one check: the contract and the pack by digest
// only, and the contract's own diagnostics or the pack's advisories. It
// carries no value from either document.
type FactsCheck struct {
	ReportFormatVersion string       `json:"reportFormatVersion"`
	Contract            *Digested    `json:"contract,omitempty"`
	Pack                *Digested    `json:"pack,omitempty"`
	Diagnostics         []Diagnostic `json:"diagnostics"`
}

// contractFact is one admitted item of a contract's facts.
type contractFact struct {
	types  factType
	always bool
	// values is the closed list for a scalar type, or the elements' for an
	// array; nil when the item lists none.
	values []any
	items  factType
}

// contractNode is one position of a contract's pointer tree.
type contractNode struct {
	facts    []contractFact
	children map[string]*contractNode
	wildcard *contractNode
}

func (n *contractNode) child(token patternToken) *contractNode {
	if token.any {
		if n.wildcard == nil {
			n.wildcard = &contractNode{}
		}
		return n.wildcard
	}
	if n.children == nil {
		n.children = map[string]*contractNode{}
	}
	next, ok := n.children[token.literal]
	if !ok {
		next = &contractNode{}
		n.children[token.literal] = next
	}
	return next
}

// FactsContractDoc is an admitted, valid facts contract.
type FactsContractDoc struct {
	Digest string
	root   *contractNode
}

var contractMembers = map[string]bool{"factsContractVersion": true, "id": true, "version": true, "source": true, "facts": true}

var contractItemMembers = map[string]bool{"pointer": true, "type": true, "present": true, "values": true, "items": true, "secretReference": true}

// parseContractPointer reads a contract pointer: a pointer pattern of
// RFC 0034 with no length bound, since a generated contract nests as deep as
// a kind's schema and its references.
func parseContractPointer(source string) ([]patternToken, bool) {
	if !strings.HasPrefix(source, "/") {
		return nil, false
	}
	var tokens []patternToken
	for _, raw := range strings.Split(source[1:], "/") {
		if raw == "*" {
			tokens = append(tokens, patternToken{any: true})
			continue
		}
		decoded, err := unescapeToken(raw, true)
		if err != nil {
			return nil, false
		}
		tokens = append(tokens, patternToken{literal: decoded})
	}
	return tokens, true
}

// valueOfType reports whether a contract value is one a fact of type t can
// hold.
func valueOfType(value any, t factType) bool {
	switch t {
	case typeString:
		_, ok := value.(string)
		return ok
	case typeDecimal:
		text, ok := value.(string)
		return ok && decimalGrammar.MatchString(text)
	case typeBoolean:
		_, ok := value.(bool)
		return ok
	}
	return false
}

// ReadFactsContract admits and checks a facts contract. It returns the
// contract, or nil and the diagnostics that refuse it. Locations name
// positions in the contract by fixed member names and indexes only. A
// document its reader stopped reading at MaxContractBytes is marked
// Oversize, and refused as REG-DOC-SIZE.
func ReadFactsContract(document Document) (*FactsContractDoc, []Diagnostic) {
	collected := &collector{}
	if document.Oversize {
		collected.add("REG-DOC-SIZE", Location{Document: DocumentContract})
		return nil, collected.sorted()
	}
	root := admit(document.Data, MaxContractBytes, "factsContractVersion", FactsContractVersion, Location{Document: DocumentContract}, collected)
	if root == nil {
		return nil, collected.sorted()
	}
	var diagnostics []Diagnostic
	fail := func(code string, path ...string) {
		diagnostics = append(diagnostics, factsDiagnostic(code, Location{Document: DocumentContract, Pointer: pointerOf(path)}))
	}
	for name := range root {
		if !contractMembers[name] {
			fail("FACTS-CONTRACT-FORM")
			break
		}
	}
	if id, ok := root["id"].(string); !ok || !isAbsoluteURI(id) {
		fail("FACTS-CONTRACT-FORM", "id")
	}
	if version, ok := root["version"].(string); !ok || !semverPattern.MatchString(version) {
		fail("FACTS-CONTRACT-FORM", "version")
	}
	if !validContractSource(root["source"]) {
		fail("FACTS-CONTRACT-FORM", "source")
	}
	items, ok := root["facts"].([]any)
	if !ok {
		fail("FACTS-CONTRACT-FORM", "facts")
		items = nil
	}
	if len(items) > MaxContractFacts {
		fail("FACTS-CONTRACT-LIMIT", "facts")
		items = nil
	}
	tree := &contractNode{}
	previousPointer, previousType, havePrevious := "", "", false
	for i, raw := range items {
		at := strconv.Itoa(i)
		item, isObject := raw.(map[string]any)
		if !isObject {
			fail("FACTS-CONTRACT-FORM", "facts", at)
			continue
		}
		bad := false
		formAt := func(member ...string) {
			fail("FACTS-CONTRACT-FORM", append([]string{"facts", at}, member...)...)
			bad = true
		}
		for name := range item {
			if !contractItemMembers[name] {
				formAt()
				break
			}
		}
		pointer, _ := item["pointer"].(string)
		tokens, pointerOK := parseContractPointer(pointer)
		if !pointerOK {
			formAt("pointer")
		}
		typeName, _ := item["type"].(string)
		t := factTypeNamed(typeName)
		if t == 0 {
			formAt("type")
		}
		present, _ := item["present"].(string)
		if present != "always" && present != "optional" {
			formAt("present")
		}
		fact := contractFact{types: t, always: present == "always"}
		if raw, has := item["items"]; has {
			name, _ := raw.(string)
			fact.items = factTypeNamed(name)
			if t != typeArray || fact.items == 0 {
				formAt("items")
			}
		}
		if raw, has := item["secretReference"]; has && (raw != true || t != typeObject) {
			formAt("secretReference")
		}
		if raw, has := item["values"]; has {
			list, isArray := raw.([]any)
			valueType := t
			if t == typeArray {
				valueType = fact.items
			}
			valid := isArray && len(list) > 0
			for j, value := range list {
				if !valid {
					break
				}
				valid = valueOfType(value, valueType) && !slices.ContainsFunc(list[:j], func(earlier any) bool { return jsonEqual(earlier, value) })
			}
			if !valid {
				formAt("values")
			}
			fact.values = list
		}
		if bad {
			continue
		}
		if havePrevious && (pointer < previousPointer || pointer == previousPointer && typeName <= previousType) {
			fail("FACTS-CONTRACT-ORDER", "facts", at)
			continue
		}
		previousPointer, previousType, havePrevious = pointer, typeName, true
		node := tree
		for _, token := range tokens {
			node = node.child(token)
		}
		node.facts = append(node.facts, fact)
	}
	if len(diagnostics) > 0 {
		return nil, diagnostics
	}
	digest, err := Digest(root)
	if err != nil {
		return nil, []Diagnostic{factsDiagnostic("FACTS-CONTRACT-FORM", Location{Document: DocumentContract})}
	}
	return &FactsContractDoc{Digest: digest, root: tree}, nil
}

func validContractSource(value any) bool {
	source, ok := value.(map[string]any)
	if !ok || len(source) != 1 {
		return false
	}
	if authored, has := source["authored"]; has {
		return authored == true
	}
	kind, ok := source["kind"].(map[string]any)
	if !ok || len(kind) != 3 {
		return false
	}
	id, idOK := kind["id"].(string)
	version, versionOK := kind["version"].(string)
	digest, digestOK := kind["digest"].(string)
	return idOK && isAbsoluteURI(id) && versionOK && semverPattern.MatchString(version) && digestOK && digestPattern.MatchString(digest)
}

// view is the set of contract positions a concrete pointer matches: each
// step follows the literal child and the wildcard.
type view []*contractNode

func (v view) step(token string) view {
	var out view
	for _, node := range v {
		for _, next := range []*contractNode{node.children[token], node.wildcard} {
			if next != nil && !slices.Contains(out, next) {
				out = append(out, next)
			}
		}
	}
	return out
}

func (v view) facts() []contractFact {
	var out []contractFact
	for _, node := range v {
		out = append(out, node.facts...)
	}
	return out
}

func (v view) hasChildren() bool {
	return slices.ContainsFunc(v, func(node *contractNode) bool { return len(node.children) > 0 || node.wildcard != nil })
}

// at is the view of a fact pointer, and whether the contract lists it. The
// facts document's root is an object the contract need not list.
func (d *FactsContractDoc) at(path []string) (view, []contractFact) {
	current := view{d.root}
	for _, token := range path {
		current = current.step(token)
		if len(current) == 0 {
			return nil, nil
		}
	}
	if len(path) == 0 {
		return current, []contractFact{{types: typeObject}}
	}
	return current, current.facts()
}

// reachable reports whether a fact at v, with the contract's facts there,
// can equal value under Core's type-preserving equality (§7.4).
func reachable(v view, facts []contractFact, value any) bool {
	for _, fact := range facts {
		if fact.admits(v, value) {
			return true
		}
	}
	return false
}

func (fact contractFact) admits(v view, value any) bool {
	listed := func() bool {
		return fact.values == nil || slices.ContainsFunc(fact.values, func(w any) bool { return jsonEqual(w, value) })
	}
	switch typed := value.(type) {
	case nil:
		return fact.types == typeNull
	case bool:
		return fact.types == typeBoolean && listed()
	case string:
		switch fact.types {
		case typeString:
			return listed()
		case typeDecimal:
			return decimalGrammar.MatchString(typed) && listed()
		}
		return false
	case []any:
		if fact.types != typeArray {
			return false
		}
		for i, item := range typed {
			if fact.items != 0 {
				element := contractFact{types: fact.items, values: fact.values}
				if !element.admits(nil, item) {
					return false
				}
			}
			next := v.step(strconv.Itoa(i))
			if listedFacts := next.facts(); len(listedFacts) > 0 && !reachable(next, listedFacts, item) {
				return false
			}
		}
		return true
	case map[string]any:
		if fact.types != typeObject {
			return false
		}
		if !v.hasChildren() {
			// An object the contract lists nothing under may hold
			// anything.
			return true
		}
		for _, name := range sortedKeys(typed) {
			next := v.step(name)
			listedFacts := next.facts()
			if len(listedFacts) == 0 || !reachable(next, listedFacts, typed[name]) {
				return false
			}
		}
		for _, node := range v {
			for name, child := range node.children {
				if _, has := typed[name]; !has && len(child.facts) > 0 && !slices.ContainsFunc(child.facts, func(f contractFact) bool { return !f.always }) {
					return false
				}
			}
		}
		return true
	}
	// A JSON number: Core compares it with no fact value, since facts hold
	// decimals as strings.
	return false
}

var orderedFactOperators = map[string]bool{"greater-than": true, "greater-than-or-equal": true, "less-than": true, "less-than-or-equal": true}

// CheckPack checks a pack's evaluation form against the contract (RFC 0037
// §5): every fact condition in applicability, exceptions[].when and
// rules[].when, through not, all and any. Each advisory is located at its
// condition in the pack, which is the same position in the pack as written,
// and carries no value.
func (d *FactsContractDoc) CheckPack(form map[string]any) []Diagnostic {
	diagnostics := []Diagnostic{}
	var walk func(node any, path []string)
	walk = func(node any, path []string) {
		condition, ok := node.(map[string]any)
		if !ok {
			return
		}
		switch condition["op"] {
		case "not":
			walk(condition["condition"], appendPath(path, "condition"))
		case "all", "any":
			children, _ := condition["conditions"].([]any)
			for i, child := range children {
				walk(child, appendPath(appendPath(path, "conditions"), strconv.Itoa(i)))
			}
		case "fact":
			if code := d.checkCondition(condition); code != "" {
				diagnostics = append(diagnostics, factsDiagnostic(code, Location{Document: DocumentPack, Pointer: pointerOf(path)}))
			}
		}
	}
	if applicability, ok := form["applicability"]; ok {
		walk(applicability, []string{"applicability"})
	}
	for _, member := range []string{"exceptions", "rules"} {
		entries, _ := form[member].([]any)
		for i, entry := range entries {
			if stated, ok := entry.(map[string]any); ok {
				walk(stated["when"], []string{member, strconv.Itoa(i), "when"})
			}
		}
	}
	return diagnostics
}

func (d *FactsContractDoc) checkCondition(condition map[string]any) string {
	pointer, _ := condition["path"].(string)
	path, err := parsePointer(pointer)
	if err != nil {
		return "FACTS-POINTER-UNKNOWN"
	}
	v, facts := d.at(path)
	if len(facts) == 0 {
		return "FACTS-POINTER-UNKNOWN"
	}
	operator, _ := condition["operator"].(string)
	switch {
	case orderedFactOperators[operator]:
		if !slices.ContainsFunc(facts, func(f contractFact) bool { return f.types == typeDecimal }) {
			return "FACTS-TYPE-ORDERED"
		}
	case operator == "equals" || operator == "not-equals":
		if !reachable(v, facts, condition["value"]) {
			return "FACTS-VALUE-UNREACHABLE"
		}
	case operator == "in":
		items, _ := condition["value"].([]any)
		for _, item := range items {
			if !reachable(v, facts, item) {
				return "FACTS-VALUE-UNREACHABLE"
			}
		}
	}
	return ""
}
