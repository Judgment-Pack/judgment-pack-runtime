package validation

// This recommended authoring check is deliberately independent of evaluation.
// A clause describes values that make a condition true (or false), never the
// complement of its true set: ordered comparisons on other types are unknown.
import (
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

const overlapWorkLimit = 1_000_000
const overlapClauseLimit = 256
const overlapOutputLimit = 1 << 20

var overlapDecimal = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?$`)

type overlapBudget struct{ remaining int }

func (b *overlapBudget) take(n int) bool { b.remaining -= n; return b.remaining >= 0 }

type overlapTerm struct {
	node  map[string]any
	truth bool
}
type overlapClause []overlapTerm
type overlapFormula struct {
	clauses  []overlapClause
	complete bool
}

// overlapDNF distributes only within explicit work and clause budgets. An
// incomplete formula is never used to establish either exactness or disjointness.
func overlapDNF(node map[string]any, truth bool, budget *overlapBudget) overlapFormula {
	if !budget.take(1) {
		return overlapFormula{}
	}
	op := stringValue(node["op"])
	if op == "literal" {
		if node["value"] == truth {
			return overlapFormula{[]overlapClause{{}}, true}
		}
		return overlapFormula{[]overlapClause{}, true}
	}
	if op == "not" {
		return overlapDNF(node["condition"].(map[string]any), !truth, budget)
	}
	if op != "all" && op != "any" {
		return overlapFormula{[]overlapClause{{{node, truth}}}, true}
	}
	product := (op == "all") == truth
	clauses := []overlapClause{}
	if product {
		clauses = []overlapClause{{}}
	}
	for _, raw := range node["conditions"].([]any) {
		child := overlapDNF(raw.(map[string]any), truth, budget)
		if !child.complete {
			return overlapFormula{}
		}
		if !product {
			if len(clauses)+len(child.clauses) > overlapClauseLimit {
				return overlapFormula{}
			}
			clauses = append(clauses, child.clauses...)
			continue
		}
		if len(clauses)*len(child.clauses) > overlapClauseLimit {
			return overlapFormula{}
		}
		joined := []overlapClause{}
		for _, a := range clauses {
			for _, c := range child.clauses {
				if !budget.take(len(a) + len(c) + 1) {
					return overlapFormula{}
				}
				row := append(append(overlapClause{}, a...), c...)
				joined = append(joined, row)
			}
		}
		clauses = joined
	}
	return overlapFormula{clauses, true}
}

func ruleOverlapDiagnostics(root map[string]any) ([]result.Diagnostic, bool) {
	collector := newDiagnosticCollector()
	budget := &overlapBudget{overlapWorkLimit}
	remainingOutput := overlapOutputLimit
	rules := objectList(root["rules"])
	outcomes := map[string]bool{}
	for _, rule := range rules {
		outcomes[stringValue(rule["outcome"])] = true
	}
	if len(outcomes) < 2 {
		return []result.Diagnostic{}, false
	}
	formulas := make([]overlapFormula, len(rules))
	for i, rule := range rules {
		formulas[i] = overlapDNF(rule["when"].(map[string]any), true, budget)
	}
	for i, a := range rules {
		for j := i + 1; j < len(rules); j++ {
			b := rules[j]
			if a["outcome"] == b["outcome"] {
				continue
			}
			possible, exact, witness, path := overlapPair(formulas[i], formulas[j], budget)
			if !possible {
				continue
			}
			d := result.ErrorDiagnostic("JPS-RULES-OVERLAP", "advisory", fmt.Sprintf("/rules/%d/when", i), "")
			d.OverlapAdvice = &result.OverlapAdvice{}
			d.Severity = "warning"
			d.Certainty = "not-excluded"
			d.Rules = []result.OverlapRule{{ID: stringValue(a["id"]), Outcome: stringValue(a["outcome"])}, {ID: stringValue(b["id"]), Outcome: stringValue(b["outcome"])}}
			d.Effect = "Core §8 records conflict; the disposition is unresolved."
			d.SuggestedRewrite = exclusiveRewrite(a, b)
			if exact {
				d.Certainty = "exact"
				d.WitnessFacts, _ = json.Marshal(overlapFacts(path, witness))
				d.Overlap = simpleOverlap(a, b)
			}
			rewrite, _ := json.Marshal(d.SuggestedRewrite)
			d.Message = fmt.Sprintf("Rules %q (%s) and %q (%s): overlap %s. %s Suggested exclusive rewrite: %s", a["id"], a["outcome"], b["id"], b["outcome"], d.Certainty, d.Effect, rewrite)
			encoded, _ := json.Marshal(d)
			remainingOutput -= len(encoded)
			if remainingOutput < 0 {
				return collector.sorted(), true
			}
			if !collector.add(d) {
				return collector.sorted(), true
			}
		}
	}
	if len(collector.items) == 0 {
		return []result.Diagnostic{}, collector.truncated
	}
	return collector.sorted(), collector.truncated
}

func overlapPair(a, b overlapFormula, budget *overlapBudget) (possible, exact bool, witness any, path string) {
	if a.complete && len(a.clauses) == 0 || b.complete && len(b.clauses) == 0 {
		return false, false, nil, ""
	}
	if !a.complete || !b.complete {
		return true, false, nil, ""
	}
	// Across paths this is a relaxation: no relation between pointer values is
	// assumed, including ancestor pointers. Unsatisfiability still proves exclusion.
	paths := map[string]bool{}
	unsupported := false
	for _, f := range []overlapFormula{a, b} {
		for _, clause := range f.clauses {
			for _, term := range clause {
				if !budget.take(1) {
					return true, false, nil, ""
				}
				if term.node["op"] == "fact" {
					paths[stringValue(term.node["path"])] = true
				} else {
					unsupported = true
				}
			}
		}
	}
	for p := range paths {
		path = p
	}
	for _, left := range a.clauses {
		for _, right := range b.clauses {
			if !budget.take(len(left) + len(right) + 1) {
				return true, false, nil, ""
			}
			clause := append(append(overlapClause{}, left...), right...)
			groups := map[string]overlapClause{}
			symbols := map[string]bool{}
			contradiction := false
			for _, term := range clause {
				if term.node["op"] == "fact" {
					p := stringValue(term.node["path"])
					groups[p] = append(groups[p], term)
				} else {
					key := overlapKey(term.node)
					if previous, found := symbols[key]; found && previous != term.truth {
						contradiction = true
						break
					}
					symbols[key] = term.truth
				}
			}
			if contradiction {
				continue
			}
			for p, terms := range groups {
				values, complete := overlapValues(terms, budget)
				if !complete {
					return true, false, nil, ""
				}
				found := false
				for _, value := range values {
					matches := true
					for _, term := range terms {
						if !budget.take(overlapJSONCost(value) + overlapJSONCost(term.node["value"]) + 1) {
							return true, false, nil, ""
						}
						known, truth := overlapFact(term.node, value)
						if !known || truth != term.truth {
							matches = false
							break
						}
					}
					if matches {
						found = true
						if len(paths) == 1 {
							witness, path = value, p
						}
						break
					}
				}
				if !found {
					contradiction = true
					break
				}
			}
			if !contradiction {
				return true, len(paths) <= 1 && !unsupported, witness, path
			}
		}
	}
	return false, false, nil, ""
}

// The finite partition has one cell per equality value, one for all other
// nondecimal JSON, and decimal points/open intervals at every authored decimal.
// At a point an extra spelling represents decimal equality without string equality.
func overlapValues(terms overlapClause, budget *overlapBudget) ([]any, bool) {
	values := []any{}
	seen := map[string]bool{}
	decimals := map[string]*big.Rat{}
	scale := 0
	add := func(v any) {
		key := overlapKey(v)
		if !seen[key] {
			seen[key] = true
			values = append(values, v)
		}
	}
	for _, term := range terms {
		operands := []any{term.node["value"]}
		if term.node["operator"] == "in" {
			operands = term.node["value"].([]any)
		}
		if !budget.take(len(operands) + 1) {
			return nil, false
		}
		for _, v := range operands {
			encoded, _ := json.Marshal(v)
			if !budget.take(len(encoded)) {
				return nil, false
			}
			add(v)
			if r, ok := overlapNumber(v); ok {
				decimals[r.RatString()] = r
				s := v.(string)
				if dot := strings.IndexByte(s, '.'); dot >= 0 && len(s)-dot-1 > scale {
					scale = len(s) - dot - 1
				}
			}
		}
	}
	// Every equality outside the explicit values has the same false answer;
	// every ordered comparison outside decimal strings is unknown.
	other := "#overlap"
	for seen[overlapKey(other)] {
		other += "#"
	}
	add(other)
	points := make([]*big.Rat, 0, len(decimals))
	for _, r := range decimals {
		points = append(points, r)
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Cmp(points[j]) < 0 })
	freshDecimal := func(r *big.Rat, digits int) bool {
		// Precision is inherited from authored endpoints. Charge each generated
		// spelling before retaining it so one long fraction cannot amplify all
		// other interval representatives into an unbounded allocation.
		s := r.FloatString(digits)
		if !budget.take(len(s)) {
			return false
		}
		for seen[overlapKey(s)] {
			if !budget.take(len(s) + 1) {
				return false
			}
			if !strings.Contains(s, ".") {
				s += ".0"
			} else {
				s += "0"
			}
		}
		add(s)
		return true
	}
	for i, p := range points {
		if !budget.take(1) {
			return nil, false
		}
		if !freshDecimal(p, scale) {
			return nil, false
		}
		if i == 0 && !freshDecimal(new(big.Rat).Sub(p, big.NewRat(1, 1)), scale) {
			return nil, false
		}
		if i+1 < len(points) && !freshDecimal(new(big.Rat).Quo(new(big.Rat).Add(p, points[i+1]), big.NewRat(2, 1)), scale+1) {
			return nil, false
		}
		if i == len(points)-1 && !freshDecimal(new(big.Rat).Add(p, big.NewRat(1, 1)), scale) {
			return nil, false
		}
	}
	return values, true
}

func overlapNumber(v any) (*big.Rat, bool) {
	s, ok := v.(string)
	if !ok || !overlapDecimal.MatchString(s) {
		return nil, false
	}
	r, ok := new(big.Rat).SetString(s)
	return r, ok
}
func overlapFact(node map[string]any, value any) (known, truth bool) {
	operand := node["value"]
	switch node["operator"] {
	case "equals":
		return true, overlapKey(value) == overlapKey(operand)
	case "not-equals":
		return true, overlapKey(value) != overlapKey(operand)
	case "in":
		key := overlapKey(value)
		for _, item := range operand.([]any) {
			if key == overlapKey(item) {
				return true, true
			}
		}
		return true, false
	default:
		a, ok := overlapNumber(value)
		if !ok {
			return false, false
		}
		b, ok := overlapNumber(operand)
		if !ok {
			return false, false
		}
		cmp := a.Cmp(b)
		switch node["operator"] {
		case "greater-than":
			return true, cmp > 0
		case "greater-than-or-equal":
			return true, cmp >= 0
		case "less-than":
			return true, cmp < 0
		case "less-than-or-equal":
			return true, cmp <= 0
		}
	}
	return false, false
}

// Equality keys normalize number tokens by significand and arbitrary-size
// exponent, never expanding a value such as 1e999999999. Strings stay lexical.
func overlapKey(v any) string {
	switch value := v.(type) {
	case json.Number:
		token := string(value)
		negative := ""
		if strings.HasPrefix(token, "-") {
			negative = "-"
			token = token[1:]
		}
		mantissa, exponent := token, "0"
		if i := strings.IndexAny(token, "eE"); i >= 0 {
			mantissa, exponent = token[:i], token[i+1:]
		}
		e, _ := new(big.Int).SetString(exponent, 10)
		integer, fraction, _ := strings.Cut(mantissa, ".")
		digits := strings.TrimLeft(integer+fraction, "0")
		if digits == "" {
			return "n:0"
		}
		trimmed := strings.TrimRight(digits, "0")
		e.Add(e, big.NewInt(int64(len(digits)-len(trimmed)-len(fraction))))
		return "n:" + negative + trimmed + "e" + e.String()
	case []any:
		keys := make([]string, len(value))
		for i, item := range value {
			keys[i] = overlapKey(item)
		}
		b, _ := json.Marshal(keys)
		return "a:" + string(b)
	case map[string]any:
		keys := map[string]string{}
		for k, item := range value {
			keys[k] = overlapKey(item)
		}
		b, _ := json.Marshal(keys)
		return "o:" + string(b)
	default:
		b, _ := json.Marshal(v)
		return "v:" + string(b)
	}
}

func overlapFacts(path string, value any) any {
	if path == "" {
		return value
	}
	tokens := strings.Split(path[1:], "/")
	for i := len(tokens) - 1; i >= 0; i-- {
		token := strings.ReplaceAll(strings.ReplaceAll(tokens[i], "~1", "/"), "~0", "~")
		value = map[string]any{token: value}
	}
	return value
}

func exclusiveRewrite(a, b map[string]any) *result.OverlapRewrite {
	low, high := a, b
	ac, bc := a["when"].(map[string]any), b["when"].(map[string]any)
	// The common ascending-band rewrite uses two existing comparisons. Reverse
	// authored order still rewrites the lower threshold, not an arbitrary rule.
	if ascendingFact(ac) && ascendingFact(bc) && ac["path"] == bc["path"] {
		ar, _ := overlapNumber(ac["value"])
		br, _ := overlapNumber(bc["value"])
		if ar.Cmp(br) > 0 {
			low, high = b, a
			ac, bc = bc, ac
		}
		operator := "less-than"
		if bc["operator"] == "greater-than" {
			operator = "less-than-or-equal"
		}
		return &result.OverlapRewrite{RuleID: stringValue(low["id"]), When: map[string]any{"op": "all", "conditions": []any{ac, map[string]any{"op": "fact", "path": bc["path"], "operator": operator, "value": bc["value"]}}}}
	}
	return &result.OverlapRewrite{RuleID: stringValue(low["id"]), When: map[string]any{"op": "all", "conditions": []any{low["when"], map[string]any{"op": "not", "condition": high["when"]}}}}
}
func ascendingFact(c map[string]any) bool {
	return c["op"] == "fact" && (c["operator"] == "greater-than" || c["operator"] == "greater-than-or-equal")
}
func ascendingOverlap(a, b map[string]any) *result.OverlapRegion {
	ac, bc := a["when"].(map[string]any), b["when"].(map[string]any)
	if !ascendingFact(ac) || !ascendingFact(bc) || ac["path"] != bc["path"] {
		return nil
	}
	ar, _ := overlapNumber(ac["value"])
	br, _ := overlapNumber(bc["value"])
	cmp := ar.Cmp(br)
	if cmp < 0 {
		ac = bc
	}
	inclusive := ac["operator"] == "greater-than-or-equal"
	if cmp == 0 {
		inclusive = inclusive && bc["operator"] == "greater-than-or-equal"
	}
	return &result.OverlapRegion{Path: stringValue(ac["path"]), From: stringValue(ac["value"]), FromInclusive: &inclusive}
}

func overlapJSONCost(value any) int { encoded, _ := json.Marshal(value); return len(encoded) }

func simpleOverlap(a, b map[string]any) *result.OverlapRegion {
	if region := ascendingOverlap(a, b); region != nil {
		return region
	}
	ac, bc := a["when"].(map[string]any), b["when"].(map[string]any)
	if ac["op"] != "fact" || bc["op"] != "fact" || ac["path"] != bc["path"] {
		return nil
	}
	values := func(c map[string]any) ([]any, bool) {
		switch c["operator"] {
		case "equals":
			return []any{c["value"]}, true
		case "in":
			return c["value"].([]any), true
		}
		return nil, false
	}
	finite, ok := values(ac)
	if !ok {
		finite, ok = values(bc)
		ac, bc = bc, ac
	}
	if !ok {
		return nil
	}
	shared := []any{}
	seen := map[string]bool{}
	for _, v := range finite {
		if known, truth := overlapFact(bc, v); known && truth {
			key := overlapKey(v)
			if !seen[key] {
				shared = append(shared, v)
				seen[key] = true
			}
		}
	}
	if len(shared) == 0 {
		return nil
	}
	return &result.OverlapRegion{Path: stringValue(ac["path"]), SharedValues: shared}
}
