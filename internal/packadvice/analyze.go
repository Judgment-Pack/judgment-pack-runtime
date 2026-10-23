// Package packadvice implements the optional Draft RFC 0027 finite-domain
// authoring report. It is neither a conformance check nor a deciding surface.
package packadvice

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"sort"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

const MaxBytes = 1048576
const maxNodes = 16384
const maxDepth = 32
const maxString = 65536
const maxDigits = 4096

type Options struct {
	MaxWork, MaxFindings int
	IntentionalFallback  bool
}

func DefaultOptions() Options { return Options{MaxWork: 100000, MaxFindings: 256} }

// Refusal is an analyzer refusal, not a Core evaluation error. A refused call
// carries no partial report, even if a previous input evaluated successfully.
type Refusal struct {
	Status  string `json:"status"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func Refuse(code string) *Refusal {
	return &Refusal{Status: "unavailable", Code: code, Message: "Pack design analysis is unavailable (" + code + "); no report was produced."}
}

type entity struct {
	raw         map[string]any
	source      map[string]any
	id, outcome string
}

func entities(root map[string]any, name string) ([]entity, bool) {
	items, _ := root[name].([]any)
	if name != "evidenceRequirements" && len(items) > 256 {
		return nil, false
	}
	out := make([]entity, 0, len(items))
	for i, raw := range items {
		m := raw.(map[string]any)
		id, _ := m["id"].(string)
		if len(id) == 0 || len(id) > 256 {
			return nil, false
		}
		outcome, _ := m["outcome"].(string)
		out = append(out, entity{m, map[string]any{"path": fmt.Sprintf("/%s/%d", name, i), "id": id}, id, outcome})
	}
	return out, true
}

// bounded checks the actual JSON shape and encoded size before marshaling the
// report. It short-circuits before walking another oversized subtree; witness
// inputs share the immutable admitted tree rather than copying caller values.
func bounded(v any) bool       { return within(v, MaxBytes) }
func reportBounded(v any) bool { return within(v, MaxBytes-1) }
func within(v any, byteCeiling int) bool {
	nodes, size := 0, 0
	var walk func(any, int) bool
	add := func(n int) bool { size += n; return byteCeiling == 0 || size <= byteCeiling }
	str := func(s string) bool {
		if len(s) > maxString {
			return false
		}
		b, _ := json.Marshal(s)
		return add(len(b))
	}
	walk = func(v any, d int) bool {
		nodes++
		if nodes > maxNodes || d > maxDepth {
			return false
		}
		switch x := v.(type) {
		case map[string]any:
			if !add(2) {
				return false
			}
			first := true
			for k, v := range x {
				if !first && !add(1) {
					return false
				}
				first = false
				if !str(k) || !add(1) || !walk(v, d+1) {
					return false
				}
			}
			return true
		case []any:
			if !add(2) {
				return false
			}
			for i, v := range x {
				if i > 0 && !add(1) {
					return false
				}
				if !walk(v, d+1) {
					return false
				}
			}
			return true
		case string:
			return str(x)
		case json.Number:
			digits := 0
			for _, c := range x.String() {
				if c >= '0' && c <= '9' {
					digits++
				}
			}
			return digits <= maxDigits && add(len(x.String()))
		case bool:
			if x {
				return add(4)
			}
			return add(5)
		case nil:
			return add(4)
		case int:
			return add(len(fmt.Sprint(x)))
		default:
			return false
		}
	}
	return walk(v, 0)
}

func domain(data []byte) (map[string]any, []any, *Refusal) {
	if len(data) > MaxBytes {
		return nil, nil, Refuse("domain-limit")
	}
	value, f := carrier.Decode(data, carrier.Limits{MaxDepth: maxDepth + 1, MaxNodes: maxNodes, MaxStringBytes: maxString})
	if f != nil {
		code := "domain-invalid"
		if f.Resource {
			code = "domain-limit"
		}
		return nil, nil, Refuse(code)
	}
	if !within(value, 0) {
		return nil, nil, Refuse("domain-limit")
	}
	root, ok := value.(map[string]any)
	if !ok || len(root) != 2 || root["kind"] != "finite-cases" {
		return nil, nil, Refuse("domain-invalid")
	}
	inputs, ok := root["inputs"].([]any)
	if !ok || len(inputs) == 0 {
		return nil, nil, Refuse("domain-invalid")
	}
	if len(inputs) > 64 {
		return nil, nil, Refuse("domain-limit")
	}
	for _, raw := range inputs {
		input, ok := raw.(map[string]any)
		if !ok || len(input) != 2 {
			return nil, nil, Refuse("domain-invalid")
		}
		if _, ok := input["facts"]; !ok {
			return nil, nil, Refuse("domain-invalid")
		}
		evidence, ok := input["evidence"].(map[string]any)
		if !ok {
			return nil, nil, Refuse("domain-invalid")
		}
		if len(evidence) > 256 {
			return nil, nil, Refuse("domain-limit")
		}
		for _, v := range evidence {
			if v != "present" && v != "absent" && v != "unknown" {
				return nil, nil, Refuse("domain-invalid")
			}
		}
	}
	return root, inputs, nil
}

type analysis struct {
	report                map[string]any
	options               Options
	findings, limitations []any
	work                  int
	stopped               bool
	seen                  map[string]bool
}

func (a *analysis) limit(path, reason string) {
	key := path + ":" + reason
	for _, v := range a.limitations {
		m := v.(map[string]any)
		if m["path"].(string)+":"+m["reason"].(string) == key {
			return
		}
	}
	// At most one per admitted rule, plus work/output control markers.
	if len(a.limitations) >= 253 && reason != "work-limit" && reason != "output-limit" {
		path = ""
		for _, raw := range a.limitations {
			m := raw.(map[string]any)
			if m["path"] == path && m["reason"] == reason {
				return
			}
		}
	}
	a.limitations = append(a.limitations, map[string]any{"path": path, "reason": reason})
}
func (a *analysis) charge() bool {
	if a.stopped {
		return false
	}
	if a.work == a.options.MaxWork {
		a.limit("", "work-limit")
		a.stopped = true
		return false
	}
	a.work++
	return true
}
func incomplete() map[string]any {
	return map[string]any{"code": "JPS-ANALYSIS-INCOMPLETE", "effect": "analysis-incomplete", "certainty": "incomplete", "severity": "info", "sources": []any{map[string]any{"path": ""}}, "message": "Some requested checks could not finish within the declared support or limits; absence of a finding is not proof of absence."}
}
func (a *analysis) projection(reserve bool) map[string]any {
	a.report["usage"] = map[string]any{"work": a.work, "findingsTruncated": a.seen["output-limit"]}
	a.report["complete"] = len(a.limitations) == 0 && !reserve
	limits := append([]any{}, a.limitations...)
	findings := append([]any{}, a.findings...)
	if reserve {
		limits = append(limits, map[string]any{"path": "", "reason": "output-limit"})
		findings = append(findings, incomplete())
	} else if len(limits) > 0 {
		findings = append(findings, incomplete())
	}
	a.report["limitations"], a.report["findings"] = limits, findings
	return a.report
}
func (a *analysis) add(f map[string]any) {
	if a.seen["output-limit"] {
		return
	}
	// Reserve the incomplete slot until every requested predicate has finished.
	if len(a.findings) >= a.options.MaxFindings-1 {
		a.outputLimit()
		return
	}
	a.findings = append(a.findings, f)
	if !reportBounded(a.projection(true)) {
		a.findings = a.findings[:len(a.findings)-1]
		a.outputLimit()
	}
}
func (a *analysis) outputLimit() { a.seen["output-limit"] = true; a.limit("", "output-limit") }
func sources(es ...entity) []any {
	out := make([]any, 0, len(es))
	for _, e := range es {
		out = append(out, e.source)
	}
	return out
}
func finding(code, effect, severity, message string, src []any, witness any) map[string]any {
	f := map[string]any{"code": code, "effect": effect, "certainty": "proved", "severity": severity, "sources": src, "message": message}
	method, summary := "exhaustive-domain", "Every declared finite input was checked through actual resolution gates, including unknowns; this claim applies only to that domain."
	if witness != nil {
		method = "witness"
		summary = "This declared domain input reproduces the stated resolution stage and effect."
		f["witness"] = witness
	}
	f["proof"] = map[string]any{"method": method, "summary": summary}
	return f
}
func (a *analysis) observed(key, code, effect, severity, message string, src []any, input any) {
	if !a.seen[key] {
		a.seen[key] = true
		a.add(finding(code, effect, severity, message, src, input))
	}
}
func digest(b []byte) string { return fmt.Sprintf("sha256:%x", sha256.Sum256(b)) }

// Analyze retains exact input-byte digests and evaluates only the explicit finite
// domain. Each per-input family/entity predicate and each evaluator invocation is
// charged before execution. Pair enumeration is incremental and never allocates
// a Cartesian product. Evaluator-internal work retains its separate Core limit.
func Analyze(engine *evaluation.Engine, packBytes, domainBytes []byte, options Options) (map[string]any, *Refusal) {
	if options.MaxWork < 1 || options.MaxWork > 100000 || options.MaxFindings < 1 || options.MaxFindings > 256 {
		return nil, Refuse("analysis-limit")
	}
	rootDomain, inputs, refusal := domain(domainBytes)
	if refusal != nil {
		return nil, refusal
	}
	contract, failure := evaluation.SelectContract(packBytes)
	if failure != nil || contract.Version() != "0.3.0" {
		return nil, Refuse("pack-unsupported")
	}
	admitted := engine.AdmitPack(packBytes)
	evalOptions := evaluation.Options{SpecVersion: "0.3.0", Command: "pack-design-analysis", EvidenceSupplied: true}
	if !admitted.AdmitsWith(evalOptions) {
		return nil, Refuse("pack-unsupported")
	}
	parsed, f := carrier.Decode(packBytes, carrier.DefaultLimits())
	if f != nil {
		return nil, Refuse("pack-unsupported")
	}
	root := parsed.(map[string]any)
	rules, ok := entities(root, "rules")
	if !ok {
		return nil, Refuse("pack-unsupported")
	}
	exceptions, ok := entities(root, "exceptions")
	if !ok {
		return nil, Refuse("pack-unsupported")
	}
	requirements, ok := entities(root, "evidenceRequirements")
	if !ok {
		return nil, Refuse("pack-unsupported")
	}
	declared := map[string]bool{}
	for _, r := range requirements {
		declared[r.id] = true
	}
	for _, raw := range inputs {
		for id := range raw.(map[string]any)["evidence"].(map[string]any) {
			if !declared[id] {
				return nil, Refuse("domain-invalid")
			}
		}
	}
	a := &analysis{options: options, seen: map[string]bool{}, report: map[string]any{"formatVersion": 1, "targetSpecVersion": "0.3.0", "packDigest": digest(packBytes), "domainDigest": digest(domainBytes), "domain": rootDomain, "fragment": "finite-enumeration", "intentionalFallback": options.IntentionalFallback, "limits": map[string]any{"maxWork": options.MaxWork, "maxFindings": options.MaxFindings}}}
	if !reportBounded(a.projection(true)) {
		return nil, Refuse("analysis-limit")
	}
	n := len(rules)
	contributions := make([]int, n)
	neverTrue := make([]bool, n)
	conditionKnown := make([]bool, n)
	redundant := make([]bool, n)
	cover := make([]map[int]bool, n)
	for i := range rules {
		neverTrue[i] = true
		conditionKnown[i] = true
		redundant[i] = true
		cover[i] = map[int]bool{}
	}
	allChecked := true
	for _, inputRaw := range inputs {
		if !a.charge() {
			allChecked = false
			break
		}
		input := inputRaw.(map[string]any)
		facts, _ := json.Marshal(input["facts"])
		evidence, _ := json.Marshal(input["evidence"])
		evaluated, failure := engine.EvaluateAdmitted(admitted, facts, evidence, evalOptions)
		if failure != nil {
			return nil, Refuse("analysis-limit")
		}
		ruleTrace := map[string]result.TraceEntry{}
		exceptionTrace := map[string]result.TraceEntry{}
		app := result.TraceEntry{}
		for _, entry := range evaluated.Trace {
			switch entry.Stage {
			case "rule":
				ruleTrace[entry.ID] = entry
			case "exception":
				exceptionTrace[entry.ID] = entry
			case "applicability":
				app = entry
			}
		}
		candidates := make([]bool, n)
		anyCandidate := false
		for i, r := range rules {
			if !a.charge() {
				allChecked = false
				break
			}
			t, exists := ruleTrace[r.id]
			candidates[i] = exists && t.Condition == "true" && !t.Skipped && !t.Suppressed
			anyCandidate = anyCandidate || candidates[i]
			if candidates[i] {
				contributions[i]++
			}
			// A separate family predicate observes condition truth, never treating a
			// missing/skipped trace as false or replaying an invented counterfactual.
			if !a.charge() {
				allChecked = false
				break
			}
			if !exists || t.Condition == "not-evaluated" {
				conditionKnown[i] = false
			} else if t.Condition == "true" {
				neverTrue[i] = false
			}
		}
		if !allChecked {
			break
		}
		for i, r := range rules {
			covered := false
			for j, s := range rules {
				if i == j {
					continue
				}
				if !a.charge() {
					allChecked = false
					break
				}
				if candidates[i] && candidates[j] && r.outcome == s.outcome {
					covered = true
					cover[i][j] = true
				}
			}
			if !allChecked {
				break
			}
			if candidates[i] && !covered {
				redundant[i] = false
			}
		}
		if !allChecked {
			break
		}
		for i := 0; i < n; i++ {
			for j := i + 1; j < n; j++ {
				if !a.charge() {
					allChecked = false
					break
				}
				if candidates[i] && candidates[j] && rules[i].outcome == rules[j].outcome {
					a.observed(fmt.Sprintf("compatible:%d:%d", i, j), "JPS-RULES-COMPATIBLE", "compatible-agreement", "info", "These rules contribute the same outcome, including when another candidate or unknown blocks resolution.", sources(rules[i], rules[j]), inputRaw)
				}
			}
			if !allChecked {
				break
			}
		}
		if !allChecked {
			break
		}
		// Coverage is the root-entity family predicate. A produced fallback must
		// have reached step 10, rather than share its outcome id with a force/rule.
		if !a.charge() {
			allChecked = false
			break
		}
		forced := false
		for _, t := range exceptionTrace {
			forced = forced || t.Effect == "force-outcome"
		}
		if evaluated.Disposition.Kind == "outcome" && !anyCandidate && !forced {
			severity := "warning"
			if options.IntentionalFallback {
				severity = "info"
			}
			a.observed("fallback", "JPS-COVERAGE-GAP", "fallback-reachable", severity, "Fallback is reachable within this declared domain; intentionality is the caller's declaration for this domain only.", []any{map[string]any{"path": "/fallbackOutcome"}}, inputRaw)
		}
		if slices.Contains(evaluated.Disposition.Reasons, evaluation.ReasonNoMatch) {
			a.observed("no-match", "JPS-COVERAGE-GAP", "no-match-reachable", "warning", "No true rule candidate or fallback resolves this domain input.", []any{map[string]any{"path": ""}}, inputRaw)
		}
		for _, e := range exceptions {
			if !a.charge() {
				allChecked = false
				break
			}
			t := exceptionTrace[e.id]
			if t.Condition != "true" {
				continue
			}
			switch t.Effect {
			case "escalate":
				a.observed("escalate:"+e.id, "JPS-EXCEPTION-INTERACTION", "direct-escalation", "warning", "This exception directly requests escalation and blocks resolution.", sources(e), inputRaw)
			case "force-outcome":
				// Step 6 bypass is observed only when the real trace marks skips.
				affected := sources(e)
				group := 0
				emit := func() {
					if len(affected) > 1 {
						a.observed(fmt.Sprintf("bypass:%s:%d", e.id, group), "JPS-EXCEPTION-INTERACTION", "force-bypasses-rules", "info", "Compatible forcing bypasses this source group of rules; no claim is made about their counterfactual candidates.", affected, inputRaw)
						group++
						affected = sources(e)
					}
				}
				for _, r := range rules {
					if ruleTrace[r.id].Skipped {
						if len(affected) == 256 {
							emit()
						}
						affected = append(affected, r.source)
					}
				}
				emit()
			case "suppress-rule":
				target, _ := e.raw["targetRule"].(string)
				for _, r := range rules {
					if r.id == target && ruleTrace[r.id].Suppressed {
						a.observed("suppress:"+e.id, "JPS-EXCEPTION-INTERACTION", "suppression", "info", "This exception suppresses the named rule; its skipped condition establishes no lost candidate.", sources(e, r), inputRaw)
					}
				}
			}
		}
		for i, e := range exceptions {
			for j := i + 1; j < len(exceptions); j++ {
				if !a.charge() {
					allChecked = false
					break
				}
				s := exceptions[j]
				t := exceptionTrace[e.id]
				other := exceptionTrace[s.id]
				if t.Effect == "force-outcome" && other.Effect == "force-outcome" && t.Outcome != other.Outcome {
					a.observed("force-conflict:"+e.id+":"+s.id, "JPS-EXCEPTION-INTERACTION", "forced-conflict", "warning", "True exceptions force incompatible outcomes and retain a conflict reason.", sources(e, s), inputRaw)
				}
			}
			if !allChecked {
				break
			}
		}
		if !allChecked {
			break
		}
		// Charge the unknown family for each potentially blocking entity, including
		// requirements and applicability. Ignored unknowns never establish blocking.
		unknown := slices.Contains(evaluated.Disposition.Reasons, evaluation.ReasonUnknown)
		if !a.charge() {
			allChecked = false
			break
		}
		if unknown && app.Condition == "unknown" {
			a.observed("unknown-app", "JPS-UNKNOWN-INTERACTION", "unknown-blocks-resolution", "warning", "Unknown applicability blocks resolution.", []any{map[string]any{"path": "/applicability"}}, inputRaw)
		}
		for _, group := range []struct {
			entities []entity
			trace    map[string]result.TraceEntry
		}{{rules, ruleTrace}, {exceptions, exceptionTrace}} {
			for _, e := range group.entities {
				if !a.charge() {
					allChecked = false
					break
				}
				t := group.trace[e.id]
				if unknown && t.Condition == "unknown" && t.OnUnknown == "escalate" {
					a.observed("unknown:"+e.source["path"].(string), "JPS-UNKNOWN-INTERACTION", "unknown-blocks-resolution", "warning", "This evaluated unknown escalates and blocks resolution.", sources(e), inputRaw)
				}
			}
			if !allChecked {
				break
			}
		}
		if !allChecked {
			break
		}
		evidenceAbsent := false
		for _, unmet := range evaluated.UnmetEvidence {
			evidenceAbsent = evidenceAbsent || unmet.State == "absent"
		}
		for _, e := range requirements {
			if !a.charge() {
				allChecked = false
				break
			}
			if unknown && !evidenceAbsent {
				for _, unmet := range evaluated.UnmetEvidence {
					if unmet.Requirement == e.id && unmet.State == "unknown" {
						a.observed("unknown-evidence:"+e.id, "JPS-UNKNOWN-INTERACTION", "unknown-blocks-resolution", "warning", "Unknown required evidence blocks resolution.", sources(e), inputRaw)
					}
				}
			}
		}
		if !allChecked {
			break
		}
	}
	// No universal finding survives a partially checked domain. Already witnessed
	// existential findings retain their proofs after work/output exhaustion.
	if allChecked {
		for i, r := range rules {
			if neverTrue[i] && conditionKnown[i] {
				a.add(finding("JPS-RULE-UNREACHABLE", "condition-never-true", "warning", "This rule condition is never true over every declared finite input; unknown is distinct from false.", sources(r), nil))
			} else if neverTrue[i] && !conditionKnown[i] {
				a.limit(r.source["path"].(string)+"/when", "unsupported-condition")
			}
			if contributions[i] == 0 {
				a.add(finding("JPS-RULE-UNREACHABLE", "never-contributes", "warning", "This rule never enters the candidate set through the actual gates over the declared domain; skipped conditions have no inferred truth value.", sources(r), nil))
			} else if redundant[i] {
				src := sources(r)
				indices := make([]int, 0, len(cover[i]))
				for j := range cover[i] {
					indices = append(indices, j)
				}
				sort.Ints(indices)
				for _, j := range indices {
					src = append(src, rules[j].source)
				}
				a.add(finding("JPS-RULE-REDUNDANT", "outcome-redundancy", "info", "Every true contribution has another same-outcome candidate within this domain. This is not permission to delete: unknown behavior, citations, exception references and attribution may differ.", src, nil))
			}
		}
	}
	report := a.projection(false)
	if !reportBounded(report) {
		return nil, Refuse("analysis-limit")
	}
	return report, nil
}
