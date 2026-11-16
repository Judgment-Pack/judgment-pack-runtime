package registry

import (
	"slices"
	"strconv"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
)

// Reading a facts contract and checking a pack against it (RFC 0037 §1, §5,
// ADR-0095). The check reads the pack's evaluation form and evaluates
// nothing. Its advisories never change an evaluation.

// FactsCheckFormatVersion is the format of the check report, under its own
// selector, factsCheckFormatVersion: the validation report's
// reportFormatVersion is RFC 0034's, and this report is not that one.
const FactsCheckFormatVersion = "1.0.0"

// FactsCheck is the report of one check: the contract and the pack by digest
// only, and the contract's own diagnostics or the pack's advisories. It
// carries no value from either document.
type FactsCheck struct {
	FactsCheckFormatVersion string       `json:"factsCheckFormatVersion"`
	Contract                *Digested    `json:"contract,omitempty"`
	Pack                    *Digested    `json:"pack,omitempty"`
	Diagnostics             []Diagnostic `json:"diagnostics"`
}

// contractFact is one admitted item of a contract's facts.
type contractFact struct {
	types  factType
	always bool
	// values is the closed list for a scalar type, or the elements' for an
	// array; nil when the item lists none.
	values []any
	items  factType
	secret bool
}

// contractNode is one position of a contract's pointer tree.
type contractNode struct {
	facts    []contractFact
	children map[string]*contractNode
	wildcard *contractNode
	// open is a wildcard position listed once for every type, with no closed
	// list and nothing under it: an open position of the facts, under which
	// every pointer is listed (ADR-0095).
	open bool
	// required names the children listed only as always present.
	required []string
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
// RFC 0034 with no 512-byte bound, since a generated contract nests as deep
// as a kind's schema and its references. It returns the tokens, or the code
// that refuses the pointer: FACTS-CONTRACT-FORM for one that is not a
// pattern, and FACTS-CONTRACT-LIMIT for one of more than maxFactsDepth
// tokens, which no facts document can hold. The tokens are counted by a scan
// that stops past that bound, before any is split out, so a pointer of
// millions of tokens costs no more than its first maxFactsDepth+1. Its bytes
// are bounded by the contract's, MaxContractBytes, which admission applies.
func parseContractPointer(source string) ([]patternToken, string) {
	if !strings.HasPrefix(source, "/") {
		return nil, "FACTS-CONTRACT-FORM"
	}
	if pointerTokenCount(source, maxFactsDepth) > maxFactsDepth {
		return nil, "FACTS-CONTRACT-LIMIT"
	}
	var tokens []patternToken
	for _, raw := range strings.Split(source[1:], "/") {
		if raw == "*" {
			tokens = append(tokens, patternToken{any: true})
			continue
		}
		decoded, err := unescapeToken(raw, true)
		if err != nil {
			return nil, "FACTS-CONTRACT-FORM"
		}
		tokens = append(tokens, patternToken{literal: decoded})
	}
	return tokens, ""
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
		tokens, pointerCode := parseContractPointer(pointer)
		if pointerCode != "" {
			fail(pointerCode, "facts", at, "pointer")
			bad = true
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
		fact.secret = item["secretReference"] == true
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
	markTree(tree)
	return &FactsContractDoc{Digest: digest, root: tree}, nil
}

// markTree marks each node's open and required, without recursion.
func markTree(root *contractNode) {
	pending := []*contractNode{root}
	for len(pending) > 0 {
		node := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		for _, name := range sortedKeys(node.children) {
			child := node.children[name]
			if len(child.facts) > 0 && !slices.ContainsFunc(child.facts, func(f contractFact) bool { return !f.always }) {
				node.required = append(node.required, name)
			}
			pending = append(pending, child)
		}
		if node.wildcard != nil {
			node.wildcard.open = openFacts(node.wildcard)
			pending = append(pending, node.wildcard)
		}
	}
}

// openFacts reports whether a node lists every type, each with no closed
// list, no element type and no secret, and nothing under it.
func openFacts(node *contractNode) bool {
	if len(node.children) > 0 || node.wildcard != nil {
		return false
	}
	var types factType
	for _, fact := range node.facts {
		if fact.values != nil || fact.items != 0 || fact.secret {
			return false
		}
		types |= fact.types
	}
	return types == allTypes
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

// Check bounds (ADR-0095). One check visits at most maxCheckWork contract
// positions and facts in all, which also bounds the lookups it keeps, and
// reads no fact pointer, operand included, deeper than a facts document can
// nest (carrier.DefaultMaxDepth). A check over either stops and reports
// FACTS-CHECK-LIMIT.
const (
	maxCheckWork  = 1 << 22
	maxCheckDepth = carrier.DefaultMaxDepth
)

// view is the set of contract positions a concrete pointer matches: each
// step follows the literal child and the wildcard, and an open position
// matches every pointer below it.
type view []*contractNode

// pointerLookup is one concrete pointer a check has read, with its view, so that
// each pointer is looked up once per check.
type pointerLookup struct {
	view  view
	depth int
	next  map[string]*pointerLookup
}

// checker is the state of one check: its pointer lookups and its work.
type checker struct {
	root     *pointerLookup
	work     int
	exceeded bool
}

func (d *FactsContractDoc) checker() *checker {
	// The facts document's root is an object the contract need not list.
	root := *d.root
	root.facts = []contractFact{{types: typeObject}}
	return &checker{root: &pointerLookup{view: view{&root}}}
}

func (c *checker) charge(units int) bool {
	c.work += units
	if c.work > maxCheckWork {
		c.exceeded = true
	}
	return !c.exceeded
}

// step is the lookup of one token below at, made once.
func (c *checker) step(at *pointerLookup, token string) *pointerLookup {
	if next, ok := at.next[token]; ok {
		return next
	}
	if at.depth+1 > maxCheckDepth {
		c.exceeded = true
	}
	if c.exceeded || !c.charge(1+len(at.view)) {
		return &pointerLookup{depth: at.depth + 1}
	}
	var seen map[*contractNode]bool
	if len(at.view) > 1 {
		seen = make(map[*contractNode]bool, 2*len(at.view))
	}
	var out view
	add := func(node *contractNode) {
		if node == nil || seen[node] {
			return
		}
		if seen != nil {
			seen[node] = true
		}
		out = append(out, node)
	}
	for _, node := range at.view {
		if node.open {
			add(node)
			continue
		}
		add(node.children[token])
		add(node.wildcard)
	}
	next := &pointerLookup{view: out, depth: at.depth + 1}
	if at.next == nil {
		at.next = map[string]*pointerLookup{}
	}
	at.next[token] = next
	return next
}

// anyFact reports whether a fact the contract lists at a lookup satisfies
// ok, charging each fact it visits to the check.
func (c *checker) anyFact(at *pointerLookup, ok func(contractFact) bool) bool {
	for _, node := range at.view {
		for _, fact := range node.facts {
			if !c.charge(1) {
				return false
			}
			if ok(fact) {
				return true
			}
		}
	}
	return false
}

// listed reports whether the contract lists a fact at a lookup.
func (c *checker) listed(at *pointerLookup) bool {
	return c.anyFact(at, func(contractFact) bool { return true })
}

func (v view) hasChildren() bool {
	return slices.ContainsFunc(v, func(node *contractNode) bool { return len(node.children) > 0 || node.wildcard != nil })
}

// at is the lookup of a fact pointer, or nil when the contract does not list
// it.
func (c *checker) at(path []string) *pointerLookup {
	if len(path) > maxCheckDepth {
		c.exceeded = true
		return nil
	}
	current := c.root
	for _, token := range path {
		current = c.step(current, token)
		if len(current.view) == 0 {
			return nil
		}
	}
	if !c.listed(current) {
		return nil
	}
	return current
}

// reachable reports whether a fact at at can equal value under Core's
// type-preserving equality (§7.4).
func (c *checker) reachable(at *pointerLookup, value any) bool {
	return c.anyFact(at, func(fact contractFact) bool { return c.admits(fact, at, value) })
}

func (c *checker) admits(fact contractFact, at *pointerLookup, value any) bool {
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
				if !c.admits(element, nil, item) {
					return false
				}
			}
			if at == nil {
				continue
			}
			if next := c.step(at, strconv.Itoa(i)); c.listed(next) && !c.reachable(next, item) {
				return false
			}
		}
		return !c.exceeded
	case map[string]any:
		if fact.types != typeObject {
			return false
		}
		if at == nil || !at.view.hasChildren() {
			// An object the contract lists nothing under may hold
			// anything.
			return true
		}
		for _, name := range sortedKeys(typed) {
			if next := c.step(at, name); !c.listed(next) || !c.reachable(next, typed[name]) {
				return false
			}
		}
		for _, node := range at.view {
			if !c.charge(len(node.required)) {
				return false
			}
			for _, name := range node.required {
				if _, has := typed[name]; !has {
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
//
// One check has one work bound. A check that passes it reports
// FACTS-CHECK-LIMIT at the condition it stopped at, and checks no further
// condition.
func (d *FactsContractDoc) CheckPack(form map[string]any) []Diagnostic {
	diagnostics := []Diagnostic{}
	c := d.checker()
	var walk func(node any, path []string)
	walk = func(node any, path []string) {
		condition, ok := node.(map[string]any)
		if !ok || c.exceeded {
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
			code := c.checkCondition(condition)
			if c.exceeded {
				code = "FACTS-CHECK-LIMIT"
			}
			if code != "" {
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

func (c *checker) checkCondition(condition map[string]any) string {
	pointer, _ := condition["path"].(string)
	path, over, err := parsePointerWithin(pointer, maxCheckDepth)
	if err != nil {
		return "FACTS-POINTER-UNKNOWN"
	}
	if over {
		c.exceeded = true
		return "FACTS-POINTER-UNKNOWN"
	}
	at := c.at(path)
	if at == nil {
		return "FACTS-POINTER-UNKNOWN"
	}
	operator, _ := condition["operator"].(string)
	switch {
	case orderedFactOperators[operator]:
		if !c.anyFact(at, func(f contractFact) bool { return f.types == typeDecimal }) {
			return "FACTS-TYPE-ORDERED"
		}
	case operator == "equals" || operator == "not-equals":
		if !c.reachable(at, condition["value"]) {
			return "FACTS-VALUE-UNREACHABLE"
		}
	case operator == "in":
		items, _ := condition["value"].([]any)
		for _, item := range items {
			if !c.reachable(at, item) {
				return "FACTS-VALUE-UNREACHABLE"
			}
		}
	}
	return ""
}
