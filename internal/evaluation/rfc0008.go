package evaluation

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

// The draft RFC 0008 grammar gate. The RFC's operators belong to no published
// JPS version, so no bundled schema describes them and spec validate rejects
// every pack that uses one — which is exactly the behavior this file preserves.
// Under the opt-in, a pack is admitted in two independent steps with a division
// of labour worth stating exactly, because neither step checks the whole
// condition. The Go gate here owns what the draft grammar adds and nothing
// else: aggregate shape and the aggregate-depth bound, checked against the same
// depth-indexed construction the testdata schema artifact states declaratively
// for those branches. The Core validity of everything inside a where — its op,
// its operand shapes, its evidence references — is delegated to the untouched
// 0.1.0-draft validator over the pack's Core projection, which is why a where
// carrying an op §7 does not define reaches evaluation through neither step but
// is still refused.

// rfc0008Conformance admits a pack that may use the draft operators. The pack's
// real bytes still pass the carrier layer, the draft grammar is checked against
// the RFC's shape and depth bound, and the pack's Core projection is then put
// through the untouched validator, so everything the draft grammar does not add
// is held to full document conformance. What is not claimed, and what the
// output marker says out loud, is that the pack is valid: it is not, under any
// published JPS version, and spec validate rejects it.
func (e *Engine) rfc0008Conformance(pack []byte, options Options) (result.Validation, map[string]any, *Failure) {
	carrierOnly, operational := e.validator.Validate(pack, validation.Options{
		Through: "carrier",
		Limits:  carrier.DefaultLimits(),
	})
	if operational != nil {
		return result.Validation{}, nil, &Failure{Code: operational.Code, Message: operational.Message, ExitCode: operational.ExitCode}
	}
	if carrierOnly.Status != "valid" {
		return result.Validation{}, nil, packNotConformant(carrierOnly)
	}
	document, failure := decodeInput(pack, "pack")
	if failure != nil {
		return result.Validation{}, nil, failure
	}
	packRoot, isObject := document.(map[string]any)
	if !isObject {
		// The bundled validator already reports this precisely; borrow it rather
		// than inventing a second message for the same document.
		validated, operational := e.validator.Validate(pack, validation.Options{Through: "semantic", Limits: carrier.DefaultLimits()})
		if operational != nil {
			return result.Validation{}, nil, &Failure{Code: operational.Code, Message: operational.Message, ExitCode: operational.ExitCode}
		}
		return result.Validation{}, nil, packNotConformant(validated)
	}
	if diagnostics := rfc0008Diagnostics(packRoot); len(diagnostics) > 0 {
		return result.Validation{}, nil, draftGrammarFailure(diagnostics)
	}
	projected, err := json.Marshal(projectPack(packRoot))
	if err != nil {
		return result.Validation{}, nil, &Failure{Code: "JPS-EVALUATION-INTERNAL", Message: "The pack's Core projection could not be re-encoded for validation.", ExitCode: result.ExitInternal}
	}
	validated, operational := e.validator.Validate(projected, validation.Options{
		Through:             "semantic",
		SupportedExtensions: options.SupportedExtensions,
		Limits:              carrier.DefaultLimits(),
	})
	if operational != nil {
		return result.Validation{}, nil, &Failure{Code: operational.Code, Message: operational.Message, ExitCode: operational.ExitCode}
	}
	if validated.Status != "valid" {
		return result.Validation{}, nil, projectionNotConformant(validated)
	}
	return validated, packRoot, nil
}

// projectionNotConformant refuses a pack whose Core projection did not reach
// full document conformance. It is packNotConformant's finding with two
// corrections the projection forces. The instance path names a location in the
// projection, not in the author's pack: every exists and every is replaced by
// its where and every uniform by a true literal, so a diagnostic inside a where
// is reported with the intervening /where segments elided. And spec validate
// cannot reproduce the finding for a pack using a draft operator — it rejects
// that pack at the aggregate itself — so the refusal does not send the author
// there for a report that will say something else.
func projectionNotConformant(validated result.Validation) *Failure {
	exitCode := result.ExitInvalid
	if validated.Status == "unsupported" {
		exitCode = result.ExitUnsupported
	}
	detail := ""
	if len(validated.Diagnostics) > 0 {
		first := validated.Diagnostics[0]
		location := first.InstancePath
		if location == "" {
			location = "<root>"
		}
		detail = fmt.Sprintf(" First diagnostic: %s at %s: %s", first.Code, location, first.Message)
	}
	return &Failure{
		Code: "JPS-EVALUATION-PACK-NOT-CONFORMANT",
		Message: fmt.Sprintf(
			"The pack is not evaluated because the document conformance status of its Core projection is %q; evaluation requires a fully conformant pack.%s The instance path is relative to that projection, in which each exists and every is replaced by its where and each uniform by a true literal, so a location inside a where is named with the /where segments elided.",
			validated.Status, detail),
		ExitCode: exitCode,
	}
}

// draftGrammarFailure refuses a pack whose draft-operator use is malformed or
// too deep, naming the first diagnostic so the refusal is self-sufficient. A
// depth-three aggregate is refused here, before any condition is evaluated.
func draftGrammarFailure(diagnostics []result.Diagnostic) *Failure {
	first := diagnostics[0]
	location := first.InstancePath
	if location == "" {
		location = "<root>"
	}
	return &Failure{
		Code: "JPS-EVALUATION-RFC0008-GRAMMAR",
		Message: fmt.Sprintf(
			"The pack is not evaluated because it does not satisfy the draft RFC 0008 grammar. First diagnostic: %s at %s: %s",
			first.Code, location, first.Message),
		ExitCode: result.ExitInvalid,
	}
}

// workLimitFailure is the explicit evaluation error RFC 0008 requires when the
// budget is exhausted. It is an error, never a disposition: the charge is
// complete before any element is evaluated, so an exhausted budget means no
// disposition was ever entitled to exist.
func workLimitFailure(e *evaluator) *Failure {
	return &Failure{
		Code: "JPS-RESOURCE-EVALUATION-WORK-LIMIT",
		Message: fmt.Sprintf(
			"The evaluation exceeds this runtime's draft RFC 0008 evaluation-work limit of %d units. The charge is computed before any element is evaluated and does not depend on element order, so no disposition is produced.",
			e.budget),
		ExitCode: result.ExitIO,
	}
}

// draftPrototype builds the in-band marker every draft-grammar evaluation
// carries. It states the one thing a reader must not miss: this pack is not
// valid under the published specification, and the operators it uses are a
// prototype of an open proposal that may never be accepted.
func draftPrototype(packRoot map[string]any, specVersion string) *result.DraftPrototype {
	operators := rfc0008Operators(packRoot)
	note := fmt.Sprintf(
		"Draft RFC 0008 (bounded collection quantifiers) prototype. The operators are a draft-RFC prototype, not part of JPS %s; a pack using one is NOT valid under it and spec validate rejects it. Nothing here claims conformance of any kind.",
		specVersion)
	if len(operators) == 0 {
		note = fmt.Sprintf(
			"Draft RFC 0008 (bounded collection quantifiers) prototype was enabled, but this pack uses no draft operator, so it remains a plain JPS %s pack.",
			specVersion)
	}
	return &result.DraftPrototype{
		RFC:                       "0008",
		Status:                    "draft-rfc-prototype",
		Operators:                 operators,
		PackValidUnderSpecVersion: len(operators) == 0,
		Note:                      note,
	}
}

// aggregateOps are RFC 0008's aggregates. uniform counts as one because it
// traverses a collection exactly as the quantifiers do.
var aggregateOps = map[string]bool{"exists": true, "every": true, "uniform": true}

// maxAggregateDepth is RFC 0008's bound: a top-level aggregate may contain
// aggregates in its where, and those may contain none. Depth is structural, so
// all/any/not wrappers inside a where do not launder it.
const maxAggregateDepth = 2

// pointerPattern is the schema's fact.path grammar, reused unchanged for the
// draft operators' path and at members.
var pointerPattern = regexp.MustCompile(`^(?:/(?:[^~/]|~0|~1)*)*$`)

// rfc0008Diagnostics reports the draft grammar's structural errors over one
// decoded pack: an aggregate whose shape does not match the RFC, and an
// aggregate past the maximum depth. Depth is counted from each condition root,
// which is where a condition tree begins in a pack.
func rfc0008Diagnostics(root map[string]any) []result.Diagnostic {
	diagnostics := []result.Diagnostic{}
	for _, site := range conditionSites(root) {
		collectAggregateDiagnostics(site.node, site.location, maxAggregateDepth, &diagnostics)
	}
	return diagnostics
}

// conditionSite is one authored condition tree and its location in the pack.
type conditionSite struct {
	node     any
	location []string
}

// conditionSites enumerates every position §7 allows a condition: the pack's
// applicability, each rule's when, and each exception's when.
func conditionSites(root map[string]any) []conditionSite {
	sites := []conditionSite{}
	if condition, present := root["applicability"]; present {
		sites = append(sites, conditionSite{node: condition, location: []string{"applicability"}})
	}
	for _, collection := range []string{"rules", "exceptions"} {
		for index, entry := range asArray(root[collection]) {
			object, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			if condition, present := object["when"]; present {
				sites = append(sites, conditionSite{node: condition, location: []string{collection, fmt.Sprint(index), "when"}})
			}
		}
	}
	return sites
}

// collectAggregateDiagnostics walks one condition tree with the remaining
// aggregate depth in hand. all, any, and not recurse within their own tier;
// an aggregate consumes one tier and its where is checked in the next one, so
// the tier a node is checked in does not depend on how it was wrapped.
func collectAggregateDiagnostics(node any, location []string, remaining int, diagnostics *[]result.Diagnostic) {
	condition, ok := node.(map[string]any)
	if !ok {
		return
	}
	op, _ := condition["op"].(string)
	switch {
	case op == "all" || op == "any":
		for index, child := range asArray(condition["conditions"]) {
			collectAggregateDiagnostics(child, appendPath(location, "conditions", fmt.Sprint(index)), remaining, diagnostics)
		}
	case op == "not":
		collectAggregateDiagnostics(condition["condition"], appendPath(location, "condition"), remaining, diagnostics)
	case aggregateOps[op]:
		if remaining < 1 {
			*diagnostics = append(*diagnostics, result.ErrorDiagnostic(
				"JPS-EVALUATION-RFC0008-DEPTH",
				"structural",
				carrier.Pointer(location),
				fmt.Sprintf("Draft RFC 0008 allows a maximum aggregate depth of %d; this %q is deeper. Wrapping an aggregate in all, any, or not does not reduce its depth.", maxAggregateDepth, op),
			))
			return
		}
		collectShapeDiagnostics(condition, op, location, diagnostics)
		if op == "uniform" {
			return
		}
		collectAggregateDiagnostics(condition["where"], appendPath(location, "where"), remaining-1, diagnostics)
	}
}

// collectShapeDiagnostics checks one aggregate against the RFC's shape: the
// required members, no others, and RFC 6901 pointers in path (and in at, for
// uniform, whose empty value is admitted and selects the whole member).
func collectShapeDiagnostics(condition map[string]any, op string, location []string, diagnostics *[]result.Diagnostic) {
	required := []string{"op", "path", "where"}
	if op == "uniform" {
		required = []string{"op", "path", "at"}
	}
	allowed := map[string]bool{}
	for _, member := range required {
		allowed[member] = true
	}
	shape := func(parts []string, message string) {
		*diagnostics = append(*diagnostics, result.ErrorDiagnostic(
			"JPS-EVALUATION-RFC0008-SHAPE", "structural", carrier.Pointer(parts), message))
	}
	for _, member := range required {
		if _, present := condition[member]; !present {
			shape(appendPath(location, member), fmt.Sprintf("Draft RFC 0008 requires the %q member on a %q condition.", member, op))
		}
	}
	for _, member := range sortedMembers(condition) {
		if !allowed[member] {
			shape(appendPath(location, member), fmt.Sprintf("Member is not allowed on a draft RFC 0008 %q condition.", op))
		}
	}
	for _, member := range []string{"path", "at"} {
		if !allowed[member] {
			continue
		}
		value, present := condition[member]
		if !present {
			continue
		}
		text, isString := value.(string)
		if !isString || !pointerPattern.MatchString(text) {
			shape(appendPath(location, member), fmt.Sprintf("Draft RFC 0008 %q must be a JSON Pointer string.", member))
		}
	}
	if op != "uniform" {
		if where, present := condition["where"]; present {
			if _, isObject := where.(map[string]any); !isObject {
				shape(appendPath(location, "where"), "Draft RFC 0008 where must be a condition object.")
			}
		}
	}
}

// rfc0008Operators lists, sorted, the draft operators a pack actually uses. An
// empty result means the pack is expressible in Core and the opt-in changed
// nothing about it.
func rfc0008Operators(root map[string]any) []string {
	used := map[string]bool{}
	for _, site := range conditionSites(root) {
		collectOperators(site.node, used)
	}
	operators := make([]string, 0, len(used))
	for op := range used {
		operators = append(operators, op)
	}
	sort.Strings(operators)
	return operators
}

func collectOperators(node any, used map[string]bool) {
	condition, ok := node.(map[string]any)
	if !ok {
		return
	}
	op, _ := condition["op"].(string)
	switch {
	case op == "all" || op == "any":
		for _, child := range asArray(condition["conditions"]) {
			collectOperators(child, used)
		}
	case op == "not":
		collectOperators(condition["condition"], used)
	case aggregateOps[op]:
		used[op] = true
		collectOperators(condition["where"], used)
	}
}

// projectPack produces the pack's Core projection: the same document with every
// aggregate replaced by a condition 0.1.0-draft defines. A quantifier is
// replaced by its own where, so the inner condition is still checked — its
// operand shapes structurally and its evidence-present references semantically,
// which is RFC 0008's §3.3 amendment obtained for free rather than
// reimplemented. uniform has no where and is replaced by a true literal.
//
// The projection is a validation instrument only: it does not mean the same
// thing as the pack, and it is never evaluated. Evaluation always reads the
// original document, which is left untouched.
func projectPack(root map[string]any) map[string]any {
	projected := copyObject(root)
	if condition, present := root["applicability"]; present {
		projected["applicability"] = projectCondition(condition)
	}
	for _, collection := range []string{"rules", "exceptions"} {
		entries, present := root[collection].([]any)
		if !present {
			continue
		}
		copied := make([]any, 0, len(entries))
		for _, entry := range entries {
			object, ok := entry.(map[string]any)
			if !ok {
				copied = append(copied, entry)
				continue
			}
			item := copyObject(object)
			if condition, present := object["when"]; present {
				item["when"] = projectCondition(condition)
			}
			copied = append(copied, item)
		}
		projected[collection] = copied
	}
	return projected
}

// projectCondition rewrites one condition tree into Core.
func projectCondition(node any) any {
	condition, ok := node.(map[string]any)
	if !ok {
		return node
	}
	op, _ := condition["op"].(string)
	switch {
	case op == "all" || op == "any":
		children, present := condition["conditions"].([]any)
		if !present {
			return condition
		}
		copied := make([]any, 0, len(children))
		for _, child := range children {
			copied = append(copied, projectCondition(child))
		}
		projected := copyObject(condition)
		projected["conditions"] = copied
		return projected
	case op == "not":
		projected := copyObject(condition)
		projected["condition"] = projectCondition(condition["condition"])
		return projected
	case op == "exists" || op == "every":
		return projectCondition(condition["where"])
	case op == "uniform":
		return map[string]any{"op": "literal", "value": true}
	default:
		return condition
	}
}

func copyObject(object map[string]any) map[string]any {
	copied := make(map[string]any, len(object))
	for key, value := range object {
		copied[key] = value
	}
	return copied
}

func sortedMembers(object map[string]any) []string {
	members := make([]string, 0, len(object))
	for member := range object {
		members = append(members, member)
	}
	sort.Strings(members)
	return members
}

func appendPath(location []string, parts ...string) []string {
	output := append([]string(nil), location...)
	return append(output, parts...)
}
