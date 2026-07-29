// Package diagram renders a judgment pack document as a deterministic Mermaid
// flowchart: the same document yields the same bytes, members are walked in
// document order, and every member node quotes the document. Resolution-state
// nodes — not-applicable, unresolved (unknown), no rule fired — are
// synthesized and labeled as what they are. The diagram is a reading aid
// derived from the pack, never a second statement of it: it adds no member,
// decides nothing, and a document that is not valid still diagrams as
// whatever it says — validation is spec validate's answer, not this
// package's.
package diagram

import (
	"encoding/json"
	"fmt"
	"strings"
)

// labelLimit bounds every rendered label. A pack description can be a
// paragraph; a node cannot. Truncation is marked and deterministic.
const labelLimit = 88

// renderer holds one render's id allocations. Node ids derive from member
// ids, which sanitization can collide (a-b and a_b); the allocator gives the
// later claimant a deterministic ordinal suffix so distinct members never
// merge into one vertex — a merged vertex would silently redraw an invalid
// document as a different document.
type renderer struct {
	b        strings.Builder
	assigned map[string]string
	used     map[string]bool
}

// Mermaid renders one decoded pack document as a Mermaid flowchart.
func Mermaid(document map[string]any) string {
	r := &renderer{assigned: map[string]string{}, used: map[string]bool{}}

	// Allocate every declared member's node id first, in a fixed class order
	// and document order within each class, so a collision's suffix never
	// depends on which section references a member first.
	for _, requirement := range arr(document, "evidenceRequirements") {
		r.id("ev", str(requirement, "id"))
	}
	for _, outcome := range arr(document, "outcomes") {
		r.id("out", str(outcome, "id"))
	}
	for _, rule := range arr(document, "rules") {
		r.id("rule", str(rule, "id"))
	}
	for _, exception := range arr(document, "exceptions") {
		r.id("exc", str(exception, "id"))
	}

	// The frontmatter title names what is diagrammed: the document's own id
	// and version — the one statement of identity a pack carries.
	title := strings.TrimSpace(str(document, "id") + " " + str(document, "version"))
	if title != "" {
		r.b.WriteString("---\ntitle: " + yamlQuote(title) + "\n---\n")
	}
	r.b.WriteString("flowchart TD\n")

	escalationNeeded, unresolvedNeeded := diagramSinks(document)
	r.writeApplicability(document)
	r.writeEvidence(document)
	r.writeOutcomes(document)
	if unresolvedNeeded {
		// One shared sink for every escalating unknown: onUnknown "escalate"
		// retains reason "unknown" and produces unresolved (resolve.go); it
		// requests a handoff only when escalation.triggers says so, which the
		// triggers node states — so no unknown edge points at escalation.
		r.b.WriteString("  unresolved_unknown([\"unresolved (unknown)\"])\n")
	}
	r.writeRules(document)
	r.writeExceptions(document)
	r.writeFallback(document)
	if escalationNeeded {
		r.writeEscalation(document)
	}
	return r.b.String()
}

// diagramSinks reports which shared terminal nodes the document needs: the
// escalation node when it is declared or an escalate-effect exception makes a
// direct request, and the unresolved-unknown node when applicability or any
// onUnknown "escalate" can reach it.
func diagramSinks(document map[string]any) (escalation, unresolved bool) {
	if _, declared := document["escalation"]; declared {
		escalation = true
	}
	if _, present := document["applicability"]; present {
		unresolved = true
	}
	for _, rule := range arr(document, "rules") {
		if str(rule, "onUnknown") == "escalate" {
			unresolved = true
		}
	}
	for _, exception := range arr(document, "exceptions") {
		if str(exception, "onUnknown") == "escalate" {
			unresolved = true
		}
		if str(exception, "effect") == "escalate" {
			escalation = true
		}
	}
	return escalation, unresolved
}

func (r *renderer) writeApplicability(document map[string]any) {
	condition, present := document["applicability"]
	if !present {
		return
	}
	// False and unknown are different results (resolve.go step 1): false is
	// the terminal not-applicable disposition; unknown is unresolved with
	// reason "unknown". Two edges, because conflating them would state
	// something the resolution model does not.
	fmt.Fprintf(&r.b, "  applicability{\"applicability: %s\"}\n", label(conditionSummary(condition)))
	r.b.WriteString("  not_applicable([\"not-applicable\"])\n")
	r.b.WriteString("  applicability -. \"false\" .-> not_applicable\n")
	r.b.WriteString("  applicability -. \"unknown\" .-> unresolved_unknown\n")
	for _, requirement := range evidenceReads(condition) {
		fmt.Fprintf(&r.b, "  %s -. \"reads\" .-> applicability\n", r.id("ev", requirement))
	}
}

func (r *renderer) writeEvidence(document map[string]any) {
	requirements := arr(document, "evidenceRequirements")
	if len(requirements) == 0 {
		return
	}
	r.b.WriteString("  subgraph evidence[\"evidence requirements\"]\n")
	for _, requirement := range requirements {
		id := str(requirement, "id")
		need := "optional"
		if boolean(requirement, "required") {
			need = "required"
		}
		fmt.Fprintf(&r.b, "    %s[\"%s\"]\n", r.id("ev", id), label(id+" ("+need+", "+str(requirement, "kind")+")"))
	}
	r.b.WriteString("  end\n")
}

func (r *renderer) writeOutcomes(document map[string]any) {
	for _, outcome := range arr(document, "outcomes") {
		id := str(outcome, "id")
		text := str(outcome, "label")
		if text == "" {
			text = id
		}
		fmt.Fprintf(&r.b, "  %s([\"%s\"])\n", r.id("out", id), label(text))
	}
}

func (r *renderer) writeRules(document map[string]any) {
	rules := arr(document, "rules")
	if len(rules) == 0 {
		return
	}
	r.b.WriteString("  subgraph rules\n")
	for _, rule := range rules {
		id := str(rule, "id")
		fmt.Fprintf(&r.b, "    %s[\"%s\"]\n", r.id("rule", id), label(id+"\nwhen "+conditionSummary(rule["when"])))
	}
	r.b.WriteString("  end\n")
	for _, rule := range rules {
		node := r.id("rule", str(rule, "id"))
		if outcome := str(rule, "outcome"); outcome != "" {
			fmt.Fprintf(&r.b, "  %s --> %s\n", node, r.id("out", outcome))
		}
		if str(rule, "onUnknown") == "escalate" {
			fmt.Fprintf(&r.b, "  %s -. \"unknown\" .-> unresolved_unknown\n", node)
		}
		// "reads" is what the condition actually tests (evidence-present);
		// evidenceRequirementRefs is the document's citation, which the
		// resolver never reads — two different relationships, two labels.
		for _, requirement := range evidenceReads(rule["when"]) {
			fmt.Fprintf(&r.b, "  %s -. \"reads\" .-> %s\n", r.id("ev", requirement), node)
		}
		for _, reference := range strs(rule, "evidenceRequirementRefs") {
			fmt.Fprintf(&r.b, "  %s -. \"cites\" .-> %s\n", node, r.id("ev", reference))
		}
	}
}

func (r *renderer) writeExceptions(document map[string]any) {
	exceptions := arr(document, "exceptions")
	if len(exceptions) == 0 {
		return
	}
	r.b.WriteString("  subgraph exceptions\n")
	for _, exception := range exceptions {
		id := str(exception, "id")
		fmt.Fprintf(&r.b, "    %s{{\"%s\"}}\n", r.id("exc", id), label(id+"\nwhen "+conditionSummary(exception["when"])))
	}
	r.b.WriteString("  end\n")
	for _, exception := range exceptions {
		node := r.id("exc", str(exception, "id"))
		switch str(exception, "effect") {
		case "force-outcome":
			fmt.Fprintf(&r.b, "  %s == \"force-outcome\" ==> %s\n", node, r.id("out", str(exception, "outcome")))
		case "suppress-rule":
			fmt.Fprintf(&r.b, "  %s -. \"suppress\" .-> %s\n", node, r.id("rule", str(exception, "targetRule")))
		case "escalate":
			fmt.Fprintf(&r.b, "  %s == \"escalate\" ==> escalation\n", node)
		}
		if str(exception, "onUnknown") == "escalate" {
			fmt.Fprintf(&r.b, "  %s -. \"unknown\" .-> unresolved_unknown\n", node)
		}
		for _, requirement := range evidenceReads(exception["when"]) {
			fmt.Fprintf(&r.b, "  %s -. \"reads\" .-> %s\n", r.id("ev", requirement), node)
		}
	}
}

func (r *renderer) writeFallback(document map[string]any) {
	fallback := str(document, "fallbackOutcome")
	if fallback == "" {
		return
	}
	r.b.WriteString("  no_rule_fired[\"no rule fired\"]\n")
	fmt.Fprintf(&r.b, "  no_rule_fired -. \"fallbackOutcome\" .-> %s\n", r.id("out", fallback))
}

func (r *renderer) writeEscalation(document map[string]any) {
	escalation, _ := document["escalation"].(map[string]any)
	text := "escalation"
	if target, ok := escalation["target"].(map[string]any); ok {
		text = "escalation → " + strings.TrimSpace(str(target, "kind")+": "+str(target, "name"))
	} else if escalation == nil {
		// Referenced by an escalate effect, declared nowhere: exactly what
		// JPS permits, and the diagram says so rather than inventing a
		// destination.
		text = "escalation (no target declared)"
	}
	fmt.Fprintf(&r.b, "  escalation[/\"%s\"/]\n", label(text))
	if triggers := strs(escalation, "triggers"); len(triggers) > 0 {
		fmt.Fprintf(&r.b, "  escalation_triggers[\"triggers: %s\"]\n", label(strings.Join(triggers, ", ")))
		r.b.WriteString("  escalation_triggers -.-> escalation\n")
	}
}

// evidenceReads walks one condition tree and collects, in document order and
// deduplicated, the evidence-requirement ids its evidence-present conditions
// test. These are the reads the resolver actually performs.
func evidenceReads(condition any) []string {
	var out []string
	seen := map[string]bool{}
	var walk func(any)
	walk = func(node any) {
		object, ok := node.(map[string]any)
		if !ok {
			return
		}
		switch str(object, "op") {
		case "evidence-present":
			if id := str(object, "evidenceRequirement"); id != "" && !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		case "all", "any":
			for _, child := range arrAny(object["conditions"]) {
				walk(child)
			}
		case "not":
			walk(object["condition"])
		}
	}
	walk(condition)
	return out
}

// conditionSummary compacts a condition tree to one line: a fact leaf shows
// its pointer, operator, and value; a composite shows its op and arity. Depth
// is not walked — the pack document holds the full tree, and the diagram is a
// reading aid pointing back at it.
func conditionSummary(condition any) string {
	node, ok := condition.(map[string]any)
	if !ok {
		return "?"
	}
	switch op := str(node, "op"); op {
	case "fact":
		return strings.TrimSpace(str(node, "path") + " " + str(node, "operator") + " " + compactValue(node["value"]))
	case "evidence-present":
		return "evidence-present " + str(node, "evidenceRequirement")
	case "all", "any":
		return fmt.Sprintf("%s(%d)", op, len(arrAny(node["conditions"])))
	case "not":
		return "not(" + conditionSummary(node["condition"]) + ")"
	case "":
		return "?"
	default:
		return op
	}
}

// compactValue renders a condition's comparison value small and
// deterministically: JSON, truncated with a marked ellipsis.
func compactValue(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "?"
	}
	text := string(encoded)
	if runes := []rune(text); len(runes) > 32 {
		text = string(runes[:32]) + "…"
	}
	return text
}

// id returns the node id allocated for one member, allocating on first use.
// Sanitization is many-to-one, so a raw id whose sanitized form is already
// claimed by a different raw id gets a deterministic ordinal suffix.
func (r *renderer) id(prefix, raw string) string {
	key := prefix + "\x00" + raw
	if allocated, ok := r.assigned[key]; ok {
		return allocated
	}
	base := sanitizeID(prefix, raw)
	candidate := base
	for n := 2; r.used[candidate]; n++ {
		candidate = fmt.Sprintf("%s_%d", base, n)
	}
	r.assigned[key] = candidate
	r.used[candidate] = true
	return candidate
}

// sanitizeID derives a Mermaid-safe identifier from a member id. Distinct
// member classes carry distinct prefixes, so a rule and an outcome sharing an
// id cannot collide across classes.
func sanitizeID(prefix, raw string) string {
	var b strings.Builder
	b.WriteString(prefix)
	b.WriteByte('_')
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// label makes text safe inside a quoted Mermaid label. Quotes, angle
// brackets, backticks, percent signs, and hashes become entity codes — a
// leading backtick after the opening quote would switch Mermaid's lexer into
// its markdown-string state, %% opens a directive scanned outside any string
// state, and a raw # would make Mermaid decode authored text like #quot; as
// an entity — newlines become the <br/> Mermaid renders, and length is
// bounded, counting runes so truncation never splits one. Empty text gets a
// placeholder: Mermaid's grammar derives no empty quoted label.
func label(text string) string {
	if text == "" {
		return "(unnamed)"
	}
	if runes := []rune(text); len(runes) > labelLimit {
		text = string(runes[:labelLimit]) + "…"
	}
	replacer := strings.NewReplacer(
		"#", "#35;",
		"\"", "#quot;",
		"<", "#lt;",
		">", "#gt;",
		"`", "#96;",
		"%", "#37;",
		"\r\n", "<br/>",
		"\n", "<br/>",
		"\r", "<br/>",
	)
	return replacer.Replace(text)
}

// yamlQuote renders text as one YAML double-quoted scalar. Mermaid parses the
// frontmatter with a YAML loader that has no error handling — an id that
// happens to contain ": ", "#", a flow character, or a carriage return would
// abort or truncate the whole render as a plain scalar. Inside double quotes
// every such byte is inert, and control characters become YAML escapes so the
// scalar stays one line.
func yamlQuote(text string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range text {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '"':
			b.WriteString(`\"`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func str(node any, key string) string {
	object, ok := node.(map[string]any)
	if !ok {
		return ""
	}
	text, _ := object[key].(string)
	return text
}

func boolean(node any, key string) bool {
	object, ok := node.(map[string]any)
	if !ok {
		return false
	}
	value, _ := object[key].(bool)
	return value
}

func arr(node any, key string) []map[string]any {
	object, ok := node.(map[string]any)
	if !ok {
		return nil
	}
	out := []map[string]any{}
	for _, entry := range arrAny(object[key]) {
		if member, ok := entry.(map[string]any); ok {
			out = append(out, member)
		}
	}
	return out
}

func arrAny(value any) []any {
	entries, _ := value.([]any)
	return entries
}

func strs(node any, key string) []string {
	object, ok := node.(map[string]any)
	if !ok {
		return nil
	}
	out := []string{}
	for _, entry := range arrAny(object[key]) {
		if text, ok := entry.(string); ok {
			out = append(out, text)
		}
	}
	return out
}
