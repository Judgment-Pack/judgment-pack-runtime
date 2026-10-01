package evaluation

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// ComparableFactsCode is the refusal a project that requires comparable facts
// gives an evaluation in which a fact some comparison of the pack reads has a
// JSON type that comparison can never match (ADR-0046).
const ComparableFactsCode = "JPS-FACTS-COMPARABLE-REQUIRED"

// maxListedIncomparable bounds how many findings one refusal names. The rest
// are counted, so a facts document that is wrong everywhere produces a message
// of bounded length that still says how much is wrong.
const maxListedIncomparable = 10

// maxShownPointer bounds how much of one pointer a refusal shows, in
// characters. A pack may author a pointer a megabyte long, and ten of them
// would make a message no terminal or tool result should carry; the prefix
// with an ellipsis still says which comparison is meant.
const maxShownPointer = 200

// incomparable is one comparison the pack states whose fact is present and of
// a JSON type the comparison can never match. It names the pointer, the
// operator, the fact's type and what the comparison can match, and never the
// fact's value.
type incomparable struct {
	path string
	// within is the collection pointer of the innermost draft RFC 0008
	// aggregate whose elements path was resolved against, as a trace's
	// typeMismatches names it; nil for a pointer resolved against the facts
	// document itself.
	within   *string
	operator string
	factType string
	// ordered marks an ordered comparison, which can match a §2.2 decimal
	// string and nothing else.
	ordered bool
	// matches is what an equals, not-equals or in can match: the operand's
	// JSON types in first-appearance order.
	matches []string
}

func (finding incomparable) key() string {
	within := "\x00"
	if finding.within != nil {
		within = "\x01" + *finding.within
	}
	return strings.Join([]string{finding.path, within, finding.operator, finding.factType, strings.Join(finding.matches, "\x02")}, "\x03")
}

// comparability is one static check of a pack's comparisons against one facts
// document. It visits every condition the pack states -- the applicability,
// every exception's and every rule's -- and every branch of each, whether or
// not §8 would reach it, because which branches an evaluation reaches depends
// on the very facts being checked.
//
// It evaluates nothing. For each fact comparison it resolves the pointer and
// asks one question of the value it selects: is its JSON type one the
// comparison can match? An absent value is not a finding -- the comparison is
// unknown, and the pack's onUnknown governs it. Under the draft RFC 0008
// opt-in a comparison inside a quantifier's where is asked the question once
// per element the collection holds, since each element is a value it reads.
//
// The walk is charged against the evaluation's own work limit: one unit per
// condition node visited, and per pointer resolution the steps and bytes the
// evaluator's model charges for one. On the Core path that total is a constant
// factor of the pack's size; only an aggregate multiplies it, by the elements
// present. Reaching the limit stops the walk, and the check then cannot say that
// every compared fact is matchable.
type comparability struct {
	quantifiers bool
	budget      int
	charged     int
	exceeded    bool
	pointers    map[string]compiledPointer
	found       []incomparable
	seen        map[string]bool
}

func (c *comparability) charge(units int) bool {
	c.charged += units
	if c.charged > c.budget {
		c.exceeded = true
	}
	return !c.exceeded
}

// resolve resolves one authored pointer against root, charging what the
// evaluator charges: the scan once per distinct pointer, and each resolution
// its steps and token bytes.
func (c *comparability) resolve(root any, path string) (any, bool) {
	compiled, scanned := c.pointers[path]
	if !scanned {
		if !c.charge(unitPointerScan + len(path)) {
			return nil, false
		}
		compiled = compilePointer(path)
		c.pointers[path] = compiled
	}
	steps := max(len(compiled.tokens), 1)
	if !c.charge(steps*unitPointerStep + compiled.bytes) {
		return nil, false
	}
	return compiled.resolve(root)
}

// incomparableFacts runs the check over one admitted pack and one admitted
// facts document. It returns the findings in walk order -- the applicability,
// then the exceptions and the rules as the pack declares them, each condition
// depth first and an aggregate's elements in array order -- each distinct
// finding once, and whether the walk reached the work limit before it ended.
func incomparableFacts(pack map[string]any, facts any, options Options) ([]incomparable, bool) {
	c := &comparability{
		quantifiers: options.RFC0008Quantifiers,
		budget:      options.workLimit(),
		pointers:    map[string]compiledPointer{},
		seen:        map[string]bool{},
	}
	if condition, present := pack["applicability"]; present {
		c.walk(condition, facts, nil)
	}
	for _, member := range []string{"exceptions", "rules"} {
		for _, entry := range asArray(pack[member]) {
			if c.exceeded {
				break
			}
			if stated, ok := entry.(map[string]any); ok {
				c.walk(stated["when"], facts, nil)
			}
		}
	}
	return c.found, c.exceeded
}

// walk visits one condition node and everything under it.
func (c *comparability) walk(node any, root any, within *string) {
	if c.exceeded || !c.charge(unitNode) {
		return
	}
	condition, ok := node.(map[string]any)
	if !ok {
		return
	}
	switch condition["op"] {
	case "not":
		c.walk(condition["condition"], root, within)
	case "all", "any":
		for _, child := range asArray(condition["conditions"]) {
			c.walk(child, root, within)
		}
	case "fact":
		c.comparison(condition, root, within)
	case "exists", "every":
		// Without the opt-in the evaluator does not decide an aggregate at all,
		// and a pack carrying one is refused before this is reached.
		if !c.quantifiers {
			return
		}
		collection, _ := condition["path"].(string)
		selected, resolved := c.resolve(root, collection)
		elements, isArray := selected.([]any)
		if !resolved || !isArray {
			return
		}
		for _, element := range elements {
			c.walk(condition["where"], element, &collection)
		}
	}
	// evidence-present and literal compare no fact; uniform compares its
	// members with each other and states no operand, so no type is wrong for
	// it.
}

// comparison asks the one question of one fact comparison.
func (c *comparability) comparison(condition map[string]any, root any, within *string) {
	path, ok := condition["path"].(string)
	if !ok {
		return
	}
	operator, _ := condition["operator"].(string)
	value, resolved := c.resolve(root, path)
	if !resolved {
		return
	}
	finding := incomparable{path: path, within: within, operator: operator, factType: jsonType(value)}
	switch {
	case orderedOperators[operator]:
		// The evaluator's own requirement: a §2.2 decimal string and nothing
		// else, which its grammar decides. A JSON number is not one.
		if text, isString := value.(string); isString && decimalPattern.MatchString(text) {
			return
		}
		finding.ordered = true
	case operator == "equals" || operator == "not-equals":
		finding.matches = []string{jsonType(condition["value"])}
		if finding.matches[0] == finding.factType {
			return
		}
	case operator == "in":
		items, isArray := condition["value"].([]any)
		// An empty in states no type, as the trace reads it; a conformant
		// pack cannot state one.
		if !isArray || len(items) == 0 || !c.charge(len(items)) {
			return
		}
		for _, item := range items {
			if kind := jsonType(item); !slices.Contains(finding.matches, kind) {
				finding.matches = append(finding.matches, kind)
			}
		}
		if slices.Contains(finding.matches, finding.factType) {
			return
		}
	default:
		return
	}
	if key := finding.key(); !c.seen[key] {
		c.seen[key] = true
		c.found = append(c.found, finding)
	}
}

// requireComparable is the refusal a deciding surface asks for when its
// project sets requireComparableFacts (ADR-0046): nil when every fact a
// comparison of the pack reads is absent or of a type that comparison can
// match, and otherwise the refusal naming them. It carries no §8.4 class,
// because it is not an evaluation error: the inputs were admitted, and it is
// the project that declines to have them evaluated.
func requireComparable(pack map[string]any, facts any, options Options) *Failure {
	found, exceeded := incomparableFacts(pack, facts, options)
	const lead = "This evaluation was refused because the project's configuration sets requireComparableFacts"
	if exceeded {
		return &Failure{
			Code:     ComparableFactsCode,
			Message:  fmt.Sprintf("%s, and checking every comparison the pack states against these facts exceeds this evaluation's work limit of %d units, so this runtime cannot say that every fact a comparison reads has a type it can match.", lead, options.workLimit()),
			ExitCode: result.ExitInvalid,
		}
	}
	if len(found) == 0 {
		return nil
	}
	listed := make([]string, 0, min(len(found), maxListedIncomparable))
	for _, finding := range found[:min(len(found), maxListedIncomparable)] {
		listed = append(listed, finding.describe())
	}
	more := ""
	if len(found) > maxListedIncomparable {
		more = fmt.Sprintf("; and %d more", len(found)-maxListedIncomparable)
	}
	return &Failure{
		Code: ComparableFactsCode,
		Message: fmt.Sprintf("%s, and these facts have a JSON type the comparison that reads them can never match: %s%s. There is no coercion between JSON types: across types, equals and in are false and not-equals is true whatever the value, and an ordered comparison is unknown. Send each fact in a type its comparison can match, or leave it out, which the pack reads as unknown.",
			lead, strings.Join(listed, "; "), more),
		ExitCode: result.ExitInvalid,
	}
}

// describe says one finding in words: where the fact is, what type it has, and
// what its comparison can match. It quotes the pointer, which is the pack's,
// and never the value, which is the caller's.
func (finding incomparable) describe() string {
	where := shownPointer(finding.path)
	if finding.within != nil {
		where += " in an element of " + shownPointer(*finding.within)
	}
	if finding.ordered {
		has := withArticle(finding.factType)
		if finding.factType == "string" {
			has = "a string that is not a decimal string"
		}
		return fmt.Sprintf("%s is %s, and %s can match only a decimal string", where, has, finding.operator)
	}
	matches := make([]string, 0, len(finding.matches))
	for _, kind := range finding.matches {
		matches = append(matches, withArticle(kind))
	}
	return fmt.Sprintf("%s is %s, and %s can match only %s", where, withArticle(finding.factType), finding.operator, strings.Join(matches, " or "))
}

// shownPointer quotes one authored pointer for a message: sanitized, and cut to
// its first maxShownPointer characters with an ellipsis when it is longer.
func shownPointer(pointer string) string {
	shown := display.Sanitize(pointer)
	if characters := []rune(shown); len(characters) > maxShownPointer {
		shown = string(characters[:maxShownPointer]) + "…"
	}
	return fmt.Sprintf("%q", shown)
}

// withArticle names a JSON type as a noun phrase: null is a value, the rest
// are kinds of value.
func withArticle(kind string) string {
	switch kind {
	case "null":
		return "null"
	case "object", "array":
		return "an " + kind
	default:
		return "a " + kind
	}
}
