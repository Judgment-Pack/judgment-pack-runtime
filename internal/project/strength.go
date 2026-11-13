package project

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"math/big"
	"slices"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// DefaultMaxVariants is the ceiling a test-strength run holds each pack to
// when the caller states none: the changes one pack may derive, admitted or
// not, before the run refuses rather than running a part of them (ADR-0084).
const DefaultMaxVariants = 1000

// StrengthOptions are a test-strength run's caller-chosen options
// (ADR-0084). MaxVariants is the per-pack ceiling on derived changes, and
// FailOnSurvivor makes a pack with a surviving variant fail the run.
type StrengthOptions struct {
	MaxVariants    int
	FailOnSurvivor bool
}

// CheckStrength refuses a ceiling below one, and accepts nil, which asks for
// no test-strength run.
func CheckStrength(options *StrengthOptions) *Failure {
	if options != nil && options.MaxVariants < 1 {
		return &Failure{Code: "JPS-INVOCATION-MAX-VARIANTS", Message: "--max-variants must be a whole number of at least 1.", ExitCode: result.ExitInvocation}
	}
	return nil
}

// strengthVariant is one derived change before it runs: its report, where
// in the authored document it applies, and what it does to the node there.
type strengthVariant struct {
	report result.StrengthVariant
	path   []any
	edit   func(any) (any, bool)
}

// variantDeriver collects one pack's changes under the caller's ceiling.
// Reaching past the ceiling sets over and keeps nothing more: the run then
// refuses, so what the derivation already holds is never reported.
type variantDeriver struct {
	limit    int
	over     bool
	variants []strengthVariant
	outcomes []string
	bounds   map[string]boundaryRef
	// valueSetReaders maps a value set's id to the fact pointers whose
	// conditions reference it, in walk order, for the hint.
	valueSetReaders map[string][]string
}

// boundaryRef is what a moved bound needs from its boundary group: the
// literal the group's probe is named after, and the step at the finest
// precision the pack authored that value in at that pointer.
type boundaryRef struct {
	literal string
	digits  int
}

func (d *variantDeriver) add(variant strengthVariant) {
	if d.over {
		return
	}
	if len(d.variants) >= d.limit {
		d.over = true
		return
	}
	d.variants = append(d.variants, variant)
}

// The reversals one comparison operator derives: the strict or non-strict
// twin first, which differs only at the literal, then the complement, which
// differs everywhere the comparison is reached. Equality reverses to
// inequality and back; membership derives none, because its complement is
// not an operator the grammar has.
var operatorReversals = map[string][]string{
	"greater-than-or-equal": {"greater-than", "less-than"},
	"greater-than":          {"greater-than-or-equal", "less-than-or-equal"},
	"less-than-or-equal":    {"less-than", "greater-than"},
	"less-than":             {"less-than-or-equal", "greater-than-or-equal"},
	"equals":                {"not-equals"},
	"not-equals":            {"equals"},
}

// deriveVariants derives every change of ADR-0084 from one authored pack
// document, sorted by id, and reports whether the ceiling was passed. The
// document is only read: every variant is built later as a path copy.
func deriveVariants(document map[string]any, valueSets bool, limit int) ([]strengthVariant, bool) {
	d := &variantDeriver{limit: limit, outcomes: declaredOutcomeIDs(document), bounds: boundaryRefs(document), valueSetReaders: map[string][]string{}}
	if node, ok := document["applicability"]; ok {
		d.conditions(node, []any{"applicability"}, "applicability", "applicability")
	}
	for _, member := range []string{"rules", "exceptions"} {
		noun := "rule"
		if member == "exceptions" {
			noun = "exception"
		}
		entries, _ := document[member].([]any)
		for index, raw := range entries {
			entry, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if d.over {
				break
			}
			id, _ := entry["id"].(string)
			declaration := fmt.Sprintf("%s %q", noun, capRendered(id))
			prefix := member + "/" + idPart(id)
			at := func(name string) []any { return []any{member, index, name} }
			d.conditions(entry["when"], at("when"), prefix+"/when", declaration)
			if member == "rules" || entry["effect"] == "force-outcome" {
				d.outcome(entry["outcome"], at("outcome"), prefix+"/outcome", declaration, "outcome")
			}
			d.onUnknown(entry["onUnknown"], at("onUnknown"), prefix+"/onUnknown", declaration)
		}
	}
	if _, ok := document["fallbackOutcome"]; ok {
		d.outcome(document["fallbackOutcome"], []any{"fallbackOutcome"}, "fallbackOutcome", "fallbackOutcome", "fallbackOutcome")
	}
	if valueSets {
		d.valueSets(document)
	}
	slices.SortFunc(d.variants, func(a, b strengthVariant) int { return strings.Compare(a.report.ID, b.report.ID) })
	return d.variants, d.over
}

// conditions walks one condition tree through exactly the three doors the
// coverage walk opens -- all, any and not -- carrying the location as an
// authored-document path and as the readable pointer an id is built from.
func (d *variantDeriver) conditions(node any, path []any, pointer, declaration string) {
	condition, ok := node.(map[string]any)
	if !ok || d.over {
		return
	}
	switch op, _ := condition["op"].(string); op {
	case "all", "any":
		children, _ := condition["conditions"].([]any)
		encoded := make([]string, len(children))
		for index, child := range children {
			encoded[index] = canonicalText(child)
		}
		repeats := counted(encoded)
		for index, child := range children {
			if d.over {
				return
			}
			childPointer := fmt.Sprintf("%s/conditions/%d", pointer, index)
			// Removing a member another member repeats byte for byte changes
			// nothing the evaluator can see, so it is not derived (ADR-0084).
			if repeats[encoded[index]] == 1 {
				removed := index
				d.add(strengthVariant{
					report: result.StrengthVariant{
						ID:          childPointer + ":" + result.VariantConditionDropped,
						Kind:        result.VariantConditionDropped,
						Declaration: declaration,
						Change:      fmt.Sprintf("removes %s from the %s at %s", describeCondition(child), op, pointer),
						Hint:        droppedHint(op),
					},
					path: appendPath(path, "conditions"),
					edit: func(node any) (any, bool) {
						list, ok := node.([]any)
						if !ok || removed >= len(list) {
							return nil, false
						}
						return slices.Delete(slices.Clone(list), removed, removed+1), true
					},
				})
			}
			d.conditions(child, appendPath(path, "conditions", index), childPointer, declaration)
		}
	case "not":
		d.conditions(condition["condition"], appendPath(path, "condition"), pointer+"/condition", declaration)
	case "fact":
		d.fact(condition, path, pointer, declaration)
	}
}

func droppedHint(op string) string {
	if op == "all" {
		return "a row on which this member alone is false while the rest of the all holds may notice it"
	}
	return "a row on which this member alone holds may notice it"
}

// fact derives a fact condition's moved bounds and reversed operators.
func (d *variantDeriver) fact(condition map[string]any, path []any, pointer, declaration string) {
	operator, _ := condition["operator"].(string)
	factPath, _ := condition["path"].(string)
	if valueSet, ok := condition["valueSet"].(string); ok {
		d.valueSetReaders[valueSet] = append(d.valueSetReaders[valueSet], factPath)
	}
	reversals := operatorReversals[operator]
	if len(reversals) == 0 {
		return
	}
	value, hasValue := condition["value"]
	if !hasValue {
		return
	}
	operand := renderOperand(value)
	subject := fmt.Sprintf("%s %s %s", capRendered(factPath), operator, operand)
	setMember := func(member string, to any) func(any) (any, bool) {
		return func(node any) (any, bool) {
			object, ok := node.(map[string]any)
			if !ok {
				return nil, false
			}
			changed := maps.Clone(object)
			changed[member] = to
			return changed, true
		}
	}
	if evaluation.OrderedOperator(operator) {
		literal, _ := value.(string)
		if number, decimal := evaluation.DecimalValue(literal); decimal {
			bound := d.bounds[factPath+"\x00"+number.RatString()]
			step := new(big.Rat).SetFrac(big.NewInt(1), new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(bound.digits)), nil))
			for _, delta := range []*big.Rat{new(big.Rat).Neg(step), step} {
				moved := new(big.Rat).Add(number, delta)
				text, renderable := evaluation.DecimalString(moved)
				if !renderable {
					continue
				}
				d.add(strengthVariant{
					report: result.StrengthVariant{
						ID:          pointer + ":" + result.VariantBoundMoved + ":" + idPart(text),
						Kind:        result.VariantBoundMoved,
						Declaration: declaration,
						Change:      fmt.Sprintf("%s → %s %s", subject, operator, renderOperand(text)),
						Hint:        boundHint(factPath, operator, number, moved, bound.literal),
					},
					path: path,
					edit: setMember("value", text),
				})
			}
		}
	}
	for index, reversed := range reversals {
		hint := "any row that reaches this comparison may notice it"
		if index == 0 && evaluation.OrderedOperator(operator) {
			literal, _ := value.(string)
			hint = fmt.Sprintf("a row that places %s at %s and reaches this comparison may notice it", capRendered(factPath), capRendered(literal))
			if number, decimal := evaluation.DecimalValue(literal); decimal {
				if bound, ok := d.bounds[factPath+"\x00"+number.RatString()]; ok {
					hint += " (the boundary:" + capRendered(factPath) + ":" + capRendered(bound.literal) + " probe)"
				}
			}
		}
		d.add(strengthVariant{
			report: result.StrengthVariant{
				ID:          pointer + ":" + result.VariantOperatorReversed + ":" + reversed,
				Kind:        result.VariantOperatorReversed,
				Declaration: declaration,
				Change:      fmt.Sprintf("%s → %s %s", subject, reversed, operand),
				Hint:        hint,
			},
			path: path,
			edit: setMember("operator", reversed),
		})
	}
}

// boundHint names the one value at which a moved bound and the pack's own
// bound disagree: of the old literal and the new one, the value the two
// comparisons answer differently.
func boundHint(factPath, operator string, old, moved *big.Rat, probeLiteral string) string {
	at := moved
	if holds(operator, old, old) != holds(operator, old, moved) {
		at = old
	}
	text, _ := evaluation.DecimalString(at)
	hint := fmt.Sprintf("a row that places %s at %s and reaches this comparison may notice it", capRendered(factPath), capRendered(text))
	if at == old && probeLiteral != "" {
		hint += " (the boundary:" + capRendered(factPath) + ":" + capRendered(probeLiteral) + " probe)"
	}
	return hint
}

// holds is an ordered comparison of a fact value against a bound.
func holds(operator string, fact, bound *big.Rat) bool {
	compared := fact.Cmp(bound)
	switch operator {
	case "greater-than":
		return compared > 0
	case "greater-than-or-equal":
		return compared >= 0
	case "less-than":
		return compared < 0
	default:
		return compared <= 0
	}
}

// outcome derives one swap of an outcome reference to each other declared
// outcome, in declaration order.
func (d *variantDeriver) outcome(node any, path []any, pointer, declaration, member string) {
	current, ok := node.(string)
	if !ok {
		return
	}
	hint := "a row whose expected outcome comes from this " + strings.SplitN(declaration, " ", 2)[0] + " may notice it"
	if member == "fallbackOutcome" {
		hint = "a row that reaches the fallback outcome may notice it"
	}
	for _, other := range d.outcomes {
		if other == current {
			continue
		}
		to := other
		d.add(strengthVariant{
			report: result.StrengthVariant{
				ID:          pointer + ":" + result.VariantOutcomeSwapped + ":" + idPart(other),
				Kind:        result.VariantOutcomeSwapped,
				Declaration: declaration,
				Change:      fmt.Sprintf("%s %q → %q", member, capRendered(current), capRendered(other)),
				Hint:        hint,
			},
			path: path,
			edit: func(any) (any, bool) { return to, true },
		})
	}
}

// onUnknown derives the other of the two onUnknown values.
func (d *variantDeriver) onUnknown(node any, path []any, pointer, declaration string) {
	current, ok := node.(string)
	if !ok {
		return
	}
	other := map[string]string{"ignore": "escalate", "escalate": "ignore"}[current]
	if other == "" {
		return
	}
	d.add(strengthVariant{
		report: result.StrengthVariant{
			ID:          pointer + ":" + result.VariantOnUnknownChanged + ":" + other,
			Kind:        result.VariantOnUnknownChanged,
			Declaration: declaration,
			Change:      fmt.Sprintf("onUnknown %q → %q", current, other),
			Hint:        "a row that leaves a fact this " + strings.SplitN(declaration, " ", 2)[0] + " reads unknown may notice it",
		},
		path: path,
		edit: func(any) (any, bool) { return other, true },
	})
}

// valueSets derives one removal per entry of each value set some condition
// references. An entry whose value another entry of the same set repeats is
// not derived: removing it changes nothing the evaluator can see.
func (d *variantDeriver) valueSets(document map[string]any) {
	sets, _ := document["valueSets"].([]any)
	for index, raw := range sets {
		set, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := set["id"].(string)
		readers := d.valueSetReaders[id]
		if len(readers) == 0 {
			continue
		}
		entries, _ := set["entries"].([]any)
		values := make([]string, len(entries))
		for at, entry := range entries {
			object, _ := entry.(map[string]any)
			values[at] = canonicalText(object["value"])
		}
		repeats := counted(values)
		rendered := make([]string, len(readers))
		for at, reader := range readers {
			rendered[at] = capRendered(reader)
		}
		for at := range entries {
			if d.over {
				return
			}
			if repeats[values[at]] > 1 {
				continue
			}
			removed := at
			value := capRendered(values[at])
			d.add(strengthVariant{
				report: result.StrengthVariant{
					ID:          "valueSets/" + idPart(id) + "/entries:" + result.VariantValueSetEntryRemoved + ":" + idPart(values[at]),
					Kind:        result.VariantValueSetEntryRemoved,
					Declaration: fmt.Sprintf("value set %q", capRendered(id)),
					Change:      fmt.Sprintf("removes entry %s from value set %q", value, capRendered(id)),
					Hint:        fmt.Sprintf("a row that places %s at %s and reaches a condition reading this set may notice it", joinCapped(slices.Compact(rendered), boundarySiteCap), value),
				},
				path: []any{"valueSets", index, "entries"},
				edit: func(node any) (any, bool) {
					list, ok := node.([]any)
					if !ok || removed >= len(list) {
						return nil, false
					}
					return slices.Delete(slices.Clone(list), removed, removed+1), true
				},
			})
		}
	}
}

// declaredOutcomeIDs lists the declared outcome ids in declaration order.
func declaredOutcomeIDs(document map[string]any) []string {
	var ids []string
	for _, outcome := range asObjects(document["outcomes"]) {
		if id, ok := outcome["id"].(string); ok {
			ids = append(ids, id)
		}
	}
	return ids
}

// boundaryRefs reads each boundary group of the pack once: the literal its
// probe is named after and the finest precision it is authored at, the step
// packs suggest takes one unit of (ADR-0024).
func boundaryRefs(document map[string]any) map[string]boundaryRef {
	refs := map[string]boundaryRef{}
	for _, group := range boundaryGroups(comparisonSites(document)) {
		digits, _ := groupStepPrecision(group)
		refs[group.path+"\x00"+group.number.RatString()] = boundaryRef{literal: group.literal, digits: digits}
	}
	return refs
}

// describeCondition names one condition in the pack's own terms, briefly.
func describeCondition(node any) string {
	condition, _ := node.(map[string]any)
	op, _ := condition["op"].(string)
	switch op {
	case "fact":
		path, _ := condition["path"].(string)
		operator, _ := condition["operator"].(string)
		if valueSet, ok := condition["valueSet"].(string); ok {
			return fmt.Sprintf("%s %s value set %q", capRendered(path), operator, capRendered(valueSet))
		}
		return fmt.Sprintf("%s %s %s", capRendered(path), operator, renderOperand(condition["value"]))
	case "all", "any":
		children, _ := condition["conditions"].([]any)
		return fmt.Sprintf("an %s of %d %s", op, len(children), plural(len(children), "condition", "conditions"))
	case "not":
		return "a not"
	case "evidence-present":
		requirement, _ := condition["evidenceRequirement"].(string)
		return fmt.Sprintf("evidence-present %q", capRendered(requirement))
	case "literal":
		return "literal " + renderOperand(condition["value"])
	}
	return "a condition"
}

// renderOperand is an authored value as its JSON text, under the report's
// text budget.
func renderOperand(value any) string {
	return capRendered(canonicalText(value))
}

// canonicalText is one decoded value's JSON text with object members in
// sorted order, which is what makes two equal values one text.
func canonicalText(value any) string {
	var rendered bytes.Buffer
	encoder := json.NewEncoder(&rendered)
	encoder.SetEscapeHTML(false)
	if encoder.Encode(value) != nil {
		return ""
	}
	return strings.TrimSuffix(rendered.String(), "\n")
}

// counted is how many times each text occurs, so a repeat is found in one
// pass over an array however wide it is.
func counted(values []string) map[string]int {
	counts := make(map[string]int, len(values))
	for _, value := range values {
		counts[value]++
	}
	return counts
}

// idPart renders one authored text for a variant id. The display sanitizer
// maps every character it replaces to one character, so two texts that
// differ only there would render alike; whenever it changed the text, the
// rendering carries a short digest of the exact text, after a "#" no
// canonical JSON text or schema-pinned id ends in. A text the sanitizer
// leaves alone renders exactly as capRendered renders it, and a long one
// already ends in that function's digest of the exact text.
func idPart(text string) string {
	rendered := capRendered(text)
	if display.Sanitize(text) == text {
		return rendered
	}
	digest := sha256.Sum256([]byte(text))
	return rendered + "#" + hex.EncodeToString(digest[:])[:boundaryDigestHex]
}

func appendPath(path []any, steps ...any) []any {
	return append(slices.Clone(path), steps...)
}

// rewriteAt returns a copy of node in which the value at path is replaced by
// edit's result. Only the maps and arrays along the path are copied; nothing
// the caller holds is changed.
func rewriteAt(node any, path []any, edit func(any) (any, bool)) (any, bool) {
	if len(path) == 0 {
		return edit(node)
	}
	switch typed := node.(type) {
	case map[string]any:
		key, ok := path[0].(string)
		if !ok {
			return nil, false
		}
		child, present := typed[key]
		if !present {
			return nil, false
		}
		replaced, ok := rewriteAt(child, path[1:], edit)
		if !ok {
			return nil, false
		}
		changed := maps.Clone(typed)
		changed[key] = replaced
		return changed, true
	case []any:
		index, ok := path[0].(int)
		if !ok || index < 0 || index >= len(typed) {
			return nil, false
		}
		replaced, ok := rewriteAt(typed[index], path[1:], edit)
		if !ok {
			return nil, false
		}
		changed := slices.Clone(typed)
		changed[index] = replaced
		return changed, true
	}
	return nil, false
}

// encodeVariant renders a variant document as the bytes the evaluator
// admits: compact, sorted members, HTML escaping off.
func encodeVariant(document any) ([]byte, error) {
	var rendered bytes.Buffer
	encoder := json.NewEncoder(&rendered)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(document); err != nil {
		return nil, err
	}
	return rendered.Bytes(), nil
}

// strengthTarget is what one pack's test-strength run reads from the run
// that already judged its rows.
type strengthTarget struct {
	id       string
	pack     []byte
	matrix   Matrix
	contract result.EvaluatorContract
	command  string
	// rows and mismatched count the pack's own rows and those that did not
	// match their expectation, and admitted is whether the evaluator admits
	// the pack for some capability set a row declares.
	rows, mismatched int
	admitted         bool
}

// countVariants holds one pack to the ceiling. TestWith calls it for every
// selected pack with a matrix before any pack's rows run, so a run that one
// pack would take past the ceiling is refused before any variant of any pack
// runs (ADR-0084). The derivation stops one past the ceiling and keeps
// nothing. A pack that does not decode is left to its own run to report.
func countVariants(id string, pack []byte, contract result.EvaluatorContract, options StrengthOptions) *Failure {
	decoded, failure := carrier.Decode(pack, carrier.DefaultLimits())
	document, ok := decoded.(map[string]any)
	if failure != nil || !ok {
		return nil
	}
	if _, over := deriveVariants(document, contract.Core050Rules(), options.MaxVariants); over {
		return &Failure{
			Code: "JPS-RESOURCE-STRENGTH-VARIANTS",
			Message: fmt.Sprintf("Pack %q derives more than %d variants, the --max-variants ceiling of this run; raise --max-variants or select one pack with --id. Every selected pack is counted before any variant runs, so no variant ran, and no partial report is returned, because a report cut short looks exactly like a complete one.",
				display.Sanitize(id), options.MaxVariants),
			ExitCode: result.ExitInvocation,
		}
	}
	return nil
}

func skippedStrength(detail string) *result.PackStrength {
	return &result.PackStrength{Status: result.StrengthSkipped, Variants: []result.StrengthVariant{}, Detail: detail}
}

// strengthOf runs one pack's test-strength variants (ADR-0084). Each variant
// is built in memory from the authored document, admitted as the pack it is,
// and run against the pack's own matrix in row order until a row's result
// differs from its expectation. Nothing is written anywhere.
func strengthOf(evaluator *evaluation.Engine, target strengthTarget, options StrengthOptions) (*result.PackStrength, *Failure) {
	switch {
	case target.rows == 0:
		return skippedStrength("The matrix has no rows, so no variant could be noticed and none ran."), nil
	case target.mismatched > 0:
		return skippedStrength("Some of the pack's own rows did not match their expectation, so every variant would read as noticed; no variant ran."), nil
	case !target.admitted:
		return skippedStrength("The evaluator admits this pack for no capability set its rows declare, so no row reaches §8 and no variant ran."), nil
	}
	decoded, decodeFailure := carrier.Decode(target.pack, carrier.DefaultLimits())
	document, ok := decoded.(map[string]any)
	if decodeFailure != nil || !ok {
		// The rows above ran against this pack, so its bytes decoded once
		// already; a second decode that fails is a defect here.
		return nil, &Failure{Code: "JPS-INTERNAL-STRENGTH-DECODE", Message: "The pack decoded for its rows and not for its variants.", ExitCode: result.ExitInternal}
	}
	variants, over := deriveVariants(document, target.contract.Core050Rules(), options.MaxVariants)
	if over {
		// countVariants held this pack to the ceiling before any variant of
		// the run ran. The pack is read again for its rows, so passing the
		// ceiling here means its bytes changed during the run: still the
		// ceiling's refusal, and the message does not claim no variant ran.
		return nil, &Failure{
			Code: "JPS-RESOURCE-STRENGTH-VARIANTS",
			Message: fmt.Sprintf("Pack %q derives more than %d variants, the --max-variants ceiling of this run, although it was counted under the ceiling before the run, so the pack changed while the run read it. No partial report is returned.",
				display.Sanitize(target.id), options.MaxVariants),
			ExitCode: result.ExitInvocation,
		}
	}
	report := &result.PackStrength{Status: result.StrengthRan, Variants: make([]result.StrengthVariant, 0, len(variants))}
	escalation, _ := PackRoot(target.pack)["escalation"].(map[string]any)
	_, routed := escalation["routes"]
	for index, variant := range variants {
		if index > 0 && variants[index-1].report.ID == variant.report.ID {
			return nil, &Failure{Code: "JPS-INTERNAL-STRENGTH-ID", Message: fmt.Sprintf("Two variants share the id %q.", display.Sanitize(variant.report.ID)), ExitCode: result.ExitInternal}
		}
		outcome := variant.report
		changed, ok := rewriteAt(document, variant.path, variant.edit)
		if !ok {
			return nil, &Failure{Code: "JPS-INTERNAL-STRENGTH-VARIANT", Message: fmt.Sprintf("Variant %q does not apply to the document it was derived from.", display.Sanitize(variant.report.ID)), ExitCode: result.ExitInternal}
		}
		encoded, err := encodeVariant(changed)
		if err != nil {
			return nil, &Failure{Code: "JPS-INTERNAL-STRENGTH-VARIANT", Message: fmt.Sprintf("Variant %q could not be encoded.", display.Sanitize(variant.report.ID)), ExitCode: result.ExitInternal}
		}
		admitted := evaluator.AdmitPack(encoded)
		if !admitsForSomeRowWith(admitted, target.matrix, target.contract) {
			outcome.Status = result.VariantNotDerivable
			outcome.Hint = ""
			outcome.Detail = notDerivableDetail(admitted.PackFailure(evaluation.Options{SpecVersion: target.contract.Version()}))
			report.Counts.NotDerivable++
			report.Variants = append(report.Variants, outcome)
			continue
		}
		var declaredTarget evaluation.HandoffTargetRendering
		if assertsHandoffTarget(target.matrix) && !routed {
			declaredTarget = evaluator.PackHandoffTarget(encoded)
		}
		outcome.Status = result.VariantSurvived
		for _, row := range target.matrix.Cases {
			if judgeRow(evaluator, admitted, row, declaredTarget, target.command, target.contract, routed).Status != "passed" {
				outcome.Status = result.VariantNoticed
				outcome.NoticedBy = row.ID
				outcome.Hint = ""
				break
			}
		}
		report.Counts.Derived++
		if outcome.Status == result.VariantNoticed {
			report.Counts.Noticed++
		} else {
			report.Counts.Survived++
		}
		report.Variants = append(report.Variants, outcome)
	}
	return report, nil
}

// notDerivableDetail says why a change is not a variant: the admission
// refusal's code and its first diagnostic, under the report's text budget.
func notDerivableDetail(failure *evaluation.Failure) string {
	detail := "The change does not leave a pack this runtime admits for any capability set the matrix's rows declare, so it is not a variant and nothing ran"
	if failure == nil {
		return detail + "."
	}
	detail += ": " + display.Sanitize(failure.Code)
	const marker = "First diagnostic: "
	if at := strings.Index(failure.Message, marker); at >= 0 {
		detail += " (" + capRendered(strings.TrimSuffix(strings.TrimSpace(failure.Message[at+len(marker):]), ".")) + ")"
	}
	return detail + "."
}

// holdToSurvivors fails one entry whose matrix ran when the caller asked
// that no variant survive and one did, or none ran (ADR-0084). A
// requirement that could not be measured is not met, as ADR-0080 holds.
func holdToSurvivors(report *result.PackTestEntry, options StrengthOptions) {
	if !options.FailOnSurvivor || report.Strength == nil {
		return
	}
	var detail string
	switch strength := report.Strength; {
	case strength.Status != result.StrengthRan:
		detail = "This run fails a pack with a surviving variant, and no variant ran for this pack, so the requirement is not met."
	case strength.Counts.Survived > 0:
		detail = fmt.Sprintf("This run fails a pack with a surviving variant, and %d of %d %s.", strength.Counts.Survived, strength.Counts.Derived, plural(strength.Counts.Derived, "variant survived", "variants survived"))
	default:
		return
	}
	report.Status = "mismatch"
	report.UnmetRequirements = append(report.UnmetRequirements, result.RequirementNoSurvivor)
	if report.Detail != "" {
		detail = report.Detail + " " + detail
	}
	report.Detail = detail
}
