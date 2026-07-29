// Package diagram renders a judgment pack document as a deterministic Mermaid
// flowchart: the same document yields the same bytes, members are walked in
// document order, and every node and edge quotes a member the document
// carries. The diagram is a reading aid derived from the pack, never a second
// statement of it: it adds no member, decides nothing, and a document that is
// not valid still diagrams as whatever it says — validation is spec validate's
// answer, not this package's.
package diagram

import (
	"encoding/json"
	"fmt"
	"strings"
)

// labelLimit bounds every rendered label. A pack description can be a
// paragraph; a node cannot. Truncation is marked and deterministic.
const labelLimit = 88

// Mermaid renders one decoded pack document as a Mermaid flowchart.
func Mermaid(document map[string]any) string {
	var b strings.Builder

	// The frontmatter title names what is diagrammed: the document's own id
	// and version — the one statement of identity a pack carries.
	title := strings.TrimSpace(str(document, "id") + " " + str(document, "version"))
	if title != "" {
		b.WriteString("---\ntitle: " + frontmatterEscape(title) + "\n---\n")
	}
	b.WriteString("flowchart TD\n")

	escalationNeeded := writeEscalationUsers(&b, document)
	writeApplicability(&b, document)
	writeEvidence(&b, document)
	writeOutcomes(&b, document)
	writeRules(&b, document)
	writeExceptions(&b, document)
	writeFallback(&b, document)
	if escalationNeeded {
		writeEscalation(&b, document)
	}
	return b.String()
}

// writeEscalationUsers reports whether anything will point at the escalation
// node: a rule or exception with onUnknown escalate, or an escalate-effect
// exception. The node is drawn only when referenced or declared.
func writeEscalationUsers(_ *strings.Builder, document map[string]any) bool {
	if _, declared := document["escalation"]; declared {
		return true
	}
	for _, rule := range arr(document, "rules") {
		if str(rule, "onUnknown") == "escalate" {
			return true
		}
	}
	for _, exception := range arr(document, "exceptions") {
		if str(exception, "onUnknown") == "escalate" || str(exception, "effect") == "escalate" {
			return true
		}
	}
	return false
}

func writeApplicability(b *strings.Builder, document map[string]any) {
	condition, present := document["applicability"]
	if !present {
		return
	}
	fmt.Fprintf(b, "  applicability{\"applicability: %s\"}\n", label(conditionSummary(condition)))
	b.WriteString("  not_applicable([\"not-applicable\"])\n")
	b.WriteString("  applicability -. \"false / unknown\" .-> not_applicable\n")
}

func writeEvidence(b *strings.Builder, document map[string]any) {
	requirements := arr(document, "evidenceRequirements")
	if len(requirements) == 0 {
		return
	}
	b.WriteString("  subgraph evidence[\"evidence requirements\"]\n")
	for _, requirement := range requirements {
		id := str(requirement, "id")
		need := "optional"
		if boolean(requirement, "required") {
			need = "required"
		}
		fmt.Fprintf(b, "    %s[\"%s\"]\n", nodeID("ev", id), label(id+" ("+need+", "+str(requirement, "kind")+")"))
	}
	b.WriteString("  end\n")
}

func writeOutcomes(b *strings.Builder, document map[string]any) {
	for _, outcome := range arr(document, "outcomes") {
		id := str(outcome, "id")
		text := str(outcome, "label")
		if text == "" {
			text = id
		}
		fmt.Fprintf(b, "  %s([\"%s\"])\n", nodeID("out", id), label(text))
	}
}

func writeRules(b *strings.Builder, document map[string]any) {
	rules := arr(document, "rules")
	if len(rules) == 0 {
		return
	}
	b.WriteString("  subgraph rules\n")
	for _, rule := range rules {
		id := str(rule, "id")
		fmt.Fprintf(b, "    %s[\"%s\"]\n", nodeID("rule", id), label(id+"\nwhen "+conditionSummary(rule["when"])))
	}
	b.WriteString("  end\n")
	for _, rule := range rules {
		id := str(rule, "id")
		if outcome := str(rule, "outcome"); outcome != "" {
			fmt.Fprintf(b, "  %s --> %s\n", nodeID("rule", id), nodeID("out", outcome))
		}
		if str(rule, "onUnknown") == "escalate" {
			fmt.Fprintf(b, "  %s -. \"unknown\" .-> escalation\n", nodeID("rule", id))
		}
		for _, reference := range strs(rule, "evidenceRequirementRefs") {
			fmt.Fprintf(b, "  %s -. \"reads\" .-> %s\n", nodeID("ev", reference), nodeID("rule", id))
		}
	}
}

func writeExceptions(b *strings.Builder, document map[string]any) {
	exceptions := arr(document, "exceptions")
	if len(exceptions) == 0 {
		return
	}
	b.WriteString("  subgraph exceptions\n")
	for _, exception := range exceptions {
		id := str(exception, "id")
		fmt.Fprintf(b, "    %s{{\"%s\"}}\n", nodeID("exc", id), label(id+"\nwhen "+conditionSummary(exception["when"])))
	}
	b.WriteString("  end\n")
	for _, exception := range exceptions {
		id := str(exception, "id")
		switch str(exception, "effect") {
		case "force-outcome":
			fmt.Fprintf(b, "  %s == \"force-outcome\" ==> %s\n", nodeID("exc", id), nodeID("out", str(exception, "outcome")))
		case "suppress-rule":
			fmt.Fprintf(b, "  %s -. \"suppress\" .-> %s\n", nodeID("exc", id), nodeID("rule", str(exception, "targetRule")))
		case "escalate":
			fmt.Fprintf(b, "  %s == \"escalate\" ==> escalation\n", nodeID("exc", id))
		}
		if str(exception, "onUnknown") == "escalate" {
			fmt.Fprintf(b, "  %s -. \"unknown\" .-> escalation\n", nodeID("exc", id))
		}
	}
}

func writeFallback(b *strings.Builder, document map[string]any) {
	fallback := str(document, "fallbackOutcome")
	if fallback == "" {
		return
	}
	b.WriteString("  no_rule_fired[\"no rule fired\"]\n")
	fmt.Fprintf(b, "  no_rule_fired -. \"fallbackOutcome\" .-> %s\n", nodeID("out", fallback))
}

func writeEscalation(b *strings.Builder, document map[string]any) {
	escalation, _ := document["escalation"].(map[string]any)
	text := "escalation"
	if target, ok := escalation["target"].(map[string]any); ok {
		text = "escalation → " + strings.TrimSpace(str(target, "kind")+": "+str(target, "name"))
	} else if escalation == nil {
		// Referenced by an escalate effect or onUnknown, declared nowhere:
		// exactly what JPS permits, and the diagram says so rather than
		// inventing a destination.
		text = "escalation (no target declared)"
	}
	fmt.Fprintf(b, "  escalation[/\"%s\"/]\n", label(text))
	if triggers := strs(escalation, "triggers"); len(triggers) > 0 {
		fmt.Fprintf(b, "  escalation_triggers[\"triggers: %s\"]\n", label(strings.Join(triggers, ", ")))
		b.WriteString("  escalation_triggers -.-> escalation\n")
	}
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

// nodeID derives a Mermaid-safe identifier from a member id. Distinct member
// classes carry distinct prefixes, so a rule and an outcome sharing an id
// cannot collide.
func nodeID(prefix, raw string) string {
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

// label makes text safe inside a quoted Mermaid label: quotes and angle
// brackets become entities, newlines become the <br/> Mermaid renders, and
// length is bounded. Truncation counts runes, never splitting one.
func label(text string) string {
	if runes := []rune(text); len(runes) > labelLimit {
		text = string(runes[:labelLimit]) + "…"
	}
	replacer := strings.NewReplacer(
		"\"", "#quot;",
		"<", "#lt;",
		">", "#gt;",
		"\n", "<br/>",
	)
	return replacer.Replace(text)
}

// frontmatterEscape keeps the YAML title one plain line.
func frontmatterEscape(text string) string {
	return strings.NewReplacer("\n", " ", "\"", "'").Replace(text)
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
