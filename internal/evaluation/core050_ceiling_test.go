package evaluation

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

// ceilingPack writes a Core 0.5.0 pack of sets value sets of entries entries
// each, whose entry values are seven bytes ("s00-000"), and references rules,
// rule i holding one fact in condition on /k that names set i mod 64. nested
// puts that condition under all, any and not; inline writes the pack's
// evaluation form instead, each reference replaced by its set's values and no
// sets, which is the inline twin.
func ceilingPack(sets, entries, references int, nested, inline bool) []byte {
	values := func(set int) string {
		items := make([]string, entries)
		for entry := range entries {
			items[entry] = fmt.Sprintf(`"s%02d-%03d"`, set, entry)
		}
		return strings.Join(items, ",")
	}
	var builder strings.Builder
	builder.WriteString(`{"specVersion":"0.5.0","id":"https://example.com/packs/ceilings","version":"1.0.0","title":"Ceilings",`)
	builder.WriteString(`"decision":{"intent":"Route","question":"Route?"},`)
	if !inline {
		builder.WriteString(`"valueSets":[`)
		for set := range sets {
			if set > 0 {
				builder.WriteByte(',')
			}
			items := make([]string, entries)
			for entry := range entries {
				items[entry] = fmt.Sprintf(`{"value":"s%02d-%03d"}`, set, entry)
			}
			fmt.Fprintf(&builder, `{"id":"set-%02d","entries":[%s]}`, set, strings.Join(items, ","))
		}
		builder.WriteString(`],`)
	}
	builder.WriteString(`"outcomes":[{"id":"accept","label":"Accept"},{"id":"refer","label":"Refer"}],"rules":[`)
	for rule := range references {
		if rule > 0 {
			builder.WriteByte(',')
		}
		condition := fmt.Sprintf(`{"op":"fact","path":"/k","operator":"in","valueSet":"set-%02d"}`, rule%64)
		if inline {
			condition = fmt.Sprintf(`{"op":"fact","path":"/k","operator":"in","value":[%s]}`, values(rule%64))
		}
		if nested {
			condition = `{"op":"all","conditions":[{"op":"any","conditions":[{"op":"not","condition":` + condition + `}]}]}`
		}
		fmt.Fprintf(&builder, `{"id":"r%03d","description":"R","when":%s,"outcome":"accept","onUnknown":"ignore"}`, rule, condition)
	}
	builder.WriteString(`]}`)
	return []byte(builder.String())
}

// ceilingFacts selects a four-byte string that no entry holds.
const ceilingFacts = `{"k":"none"}`

// requireWork holds an admitted pack to exactly work units: it evaluates under
// a budget of work and is refused with the work limit one unit below.
func requireWork(t *testing.T, engine *Engine, admitted *AdmittedPack, work int) {
	t.Helper()
	if _, failure := engine.EvaluateAdmitted(admitted, []byte(ceilingFacts), nil, Options{SpecVersion: artifacts.Core050Version, WorkBudget: work}); failure != nil {
		t.Fatalf("refused under a budget of %d: %+v", work, failure)
	}
	if _, failure := engine.EvaluateAdmitted(admitted, []byte(ceilingFacts), nil, Options{SpecVersion: artifacts.Core050Version, WorkBudget: work - 1}); failure == nil || failure.Code != "JPS-RESOURCE-EVALUATION-WORK-LIMIT" {
		t.Fatalf("evaluated under a budget of %d: %+v", work-1, failure)
	}
}

// A pack at the base ceilings, 64 value sets of 512 entries, validates and
// evaluates, and its work is counted on its evaluation form, at each
// reference. By the charging model (quantifier.go), one flat reference costs
// its rule's iteration (1), the fact node (1), a resolution of /k (one step
// and one token byte, 2), the selected "none" once per candidate (512 x 5)
// and the candidate array (1 + 512 x 8): 6661 units. The pointer /k is
// compiled once per evaluation (1 + 2). Under all, any and not each
// reference costs three nodes more. A set named by two references is charged
// twice, as its inline twin's operands are.
//
// The document limits are measured on the pack as presented. With 512
// references the evaluation form would hold 512 arrays of 512 values, over
// the default node limit of 250,000; the pack, which holds each set once, is
// admitted and evaluated, and its inline twin, which presents that form, is
// refused at the node limit.
func TestCore050AtTheBaseCeilingsIsChargedPerReference(t *testing.T) {
	engine := newTestEngine(t)
	for _, tc := range []struct {
		name       string
		references int
		nested     bool
		work       int
	}{
		{"128 flat references", 128, false, 3 + 128*6661},
		{"128 references under all, any and not", 128, true, 3 + 128*(6661+3)},
		{"512 flat references", 512, false, 3 + 512*6661},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pack := ceilingPack(64, 512, tc.references, tc.nested, false)
			admitted := engine.AdmitPack(pack)
			if failure := admitted.PackFailure(Options{SpecVersion: artifacts.Core050Version}); failure != nil {
				t.Fatalf("not admitted: %+v", failure)
			}
			evaluated, failure := engine.EvaluateAdmitted(admitted, []byte(ceilingFacts), nil, Options{SpecVersion: artifacts.Core050Version})
			if failure != nil {
				t.Fatalf("refused under the default limit: %+v", failure)
			}
			if tc.work > DefaultCoreWorkLimit/4 {
				t.Fatalf("the expected work %d is not well within the default limit %d", tc.work, DefaultCoreWorkLimit)
			}
			// No entry holds "none": a flat rule never matches, and with no
			// fallback the pack is unresolved for no match; under not, every
			// rule matches and they agree on accept.
			if got := evaluated.Disposition; (!tc.nested && (got.Kind != "unresolved" || strings.Join(got.Reasons, ",") != "no-match")) || (tc.nested && (got.Kind != "outcome" || got.OutcomeID != "accept")) {
				t.Fatalf("disposition %+v", got)
			}
			requireWork(t, engine, admitted, tc.work)

			twin := ceilingPack(64, 512, tc.references, tc.nested, true)
			if tc.references > 128 {
				if _, failure := engine.EvaluateWith(twin, []byte(ceilingFacts), nil, Options{SpecVersion: artifacts.Core050Version}); failure == nil || failure.Code != "JPS-RESOURCE-NODE-LIMIT" || failure.Class != result.ClassPackNotConformant {
					t.Fatalf("the inline twin was not refused at the node limit: %+v", failure)
				}
				return
			}
			twinAdmitted := engine.AdmitPack(twin)
			if failure := twinAdmitted.PackFailure(Options{SpecVersion: artifacts.Core050Version}); failure != nil {
				t.Fatalf("twin not admitted: %+v", failure)
			}
			requireWork(t, engine, twinAdmitted, tc.work)
		})
	}
}

// One over each base ceiling is refused with the structural code for an array
// over its maximum, at the array, and nothing else is reported.
func TestCore050OneOverEachBaseCeilingIsRefused(t *testing.T) {
	validator, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name          string
		sets, entries int
		pointer       string
	}{
		{"65 value sets", 65, 1, "/valueSets"},
		{"513 entries", 1, 513, "/valueSets/0/entries"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output, operational := validator.Validate(ceilingPack(tc.sets, tc.entries, 1, false, false), validation.Options{Through: "semantic", Limits: carrier.DefaultLimits()})
			if operational != nil {
				t.Fatal(operational)
			}
			if output.Status != "invalid" || len(output.Diagnostics) != 1 || output.Diagnostics[0].Code != "JPS-STRUCTURE-MAXITEMS" || output.Diagnostics[0].InstancePath != tc.pointer {
				t.Fatalf("status %s, diagnostics %+v", output.Status, output.Diagnostics)
			}
		})
	}
}

// Malformed value sets, references and annotations reach the 0.5.0 code paths
// of validation and admission, and each is refused with a diagnostic at the
// member at fault; none panics, and none is evaluated.
func TestCore050MalformedProbesAreRefused(t *testing.T) {
	validator, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	engine := NewEngine(validator)
	const rule = `{"id":"a","description":"A","when":%s,"outcome":"accept","onUnknown":"ignore"}`
	pack := func(valueSets, condition, annotations string) []byte {
		return []byte(`{"specVersion":"0.5.0","id":"https://example.com/packs/probe","version":"1.0.0","title":"Probe",` +
			`"decision":{"intent":"Route","question":"Route?"},` +
			`"extensions":{"org.judgmentpack.annotations":` + annotations + `},` +
			`"valueSets":` + valueSets + `,` +
			`"outcomes":[{"id":"accept","label":"Accept"},{"id":"refer","label":"Refer"}],` +
			`"rules":[` + fmt.Sprintf(rule, condition) + `]}`)
	}
	const sets = `[{"id":"kinds","entries":[{"value":"intake"}]}]`
	const reference = `{"op":"fact","path":"/k","operator":"in","valueSet":"kinds"}`
	const annotations = `{"team":"intake"}`
	deep := func(depth int) string {
		return strings.Repeat(`{"a":`, depth) + `"x"` + strings.Repeat(`}`, depth)
	}
	for _, tc := range []struct {
		name, valueSets, condition, annotations string
		code, pointer                           string
	}{
		{"value sets as a string", `"kinds"`, reference, annotations, "JPS-STRUCTURE-TYPE", "/valueSets"},
		{"value sets as an object", `{"kinds":[]}`, reference, annotations, "JPS-STRUCTURE-TYPE", "/valueSets"},
		{"a value set as a number", `[1]`, reference, annotations, "JPS-STRUCTURE-TYPE", "/valueSets/0"},
		{"a value set as null", `[null]`, reference, annotations, "JPS-STRUCTURE-TYPE", "/valueSets/0"},
		{"entries as a string", `[{"id":"kinds","entries":"intake"}]`, reference, annotations, "JPS-STRUCTURE-TYPE", "/valueSets/0/entries"},
		{"an entry as a number", `[{"id":"kinds","entries":[1]}]`, reference, annotations, "JPS-STRUCTURE-TYPE", "/valueSets/0/entries/0"},
		{"a set id as a number", `[{"id":7,"entries":[{"value":"intake"}]}]`, reference, annotations, "JPS-STRUCTURE-TYPE", "/valueSets/0/id"},
		{"a reference as a number", sets, `{"op":"fact","path":"/k","operator":"in","valueSet":1}`, annotations, "JPS-STRUCTURE-TYPE", "/rules/0/when/valueSet"},
		{"a reference as null", sets, `{"op":"fact","path":"/k","operator":"in","valueSet":null}`, annotations, "JPS-STRUCTURE-TYPE", "/rules/0/when/valueSet"},
		{"a reference as an array", sets, `{"op":"fact","path":"/k","operator":"in","valueSet":["kinds"]}`, annotations, "JPS-STRUCTURE-TYPE", "/rules/0/when/valueSet"},
		{"a reference as an object", sets, `{"op":"fact","path":"/k","operator":"in","valueSet":{"id":"kinds"}}`, annotations, "JPS-STRUCTURE-TYPE", "/rules/0/when/valueSet"},
		{"annotations as a string", sets, reference, `"intake"`, "JPS-STRUCTURE-TYPE", "/extensions/org.judgmentpack.annotations"},
		{"an annotation nested 100 objects deep", sets, reference, `{"team":` + deep(100) + `}`, "JPS-STRUCTURE-TYPE", "/extensions/org.judgmentpack.annotations/team"},
		{"an annotation list nested in lists", sets, reference, `{"team":[[["intake"]]]}`, "JPS-STRUCTURE-TYPE", "/extensions/org.judgmentpack.annotations/team/0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("panic: %v", recovered)
				}
			}()
			document := pack(tc.valueSets, tc.condition, tc.annotations)
			output, operational := validator.Validate(document, validation.Options{Through: "semantic", Limits: carrier.DefaultLimits()})
			if operational != nil {
				t.Fatal(operational)
			}
			found := false
			for _, diagnostic := range output.Diagnostics {
				found = found || (diagnostic.Code == tc.code && diagnostic.InstancePath == tc.pointer)
			}
			if output.Status != "invalid" || !found {
				t.Fatalf("status %s, diagnostics %+v", output.Status, output.Diagnostics)
			}
			if _, failure := engine.EvaluateWith(document, []byte(`{"k":"intake"}`), nil, Options{SpecVersion: artifacts.Core050Version}); failure == nil || failure.Code != "JPS-EVALUATION-PACK-NOT-CONFORMANT" {
				t.Fatalf("evaluated: %+v", failure)
			}
		})
	}

	// An annotations object nested past the default nesting limit is refused
	// while it is read, before any rule of the version applies.
	t.Run("an annotation nested past the nesting limit", func(t *testing.T) {
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Fatalf("panic: %v", recovered)
			}
		}()
		document := pack(sets, reference, `{"team":`+deep(carrier.DefaultMaxDepth)+`}`)
		if _, operational := validator.Validate(document, validation.Options{Through: "semantic", Limits: carrier.DefaultLimits()}); operational == nil || operational.Code != "JPS-RESOURCE-DEPTH-LIMIT" {
			t.Fatalf("operational %+v", operational)
		}
		if _, failure := engine.EvaluateWith(document, []byte(`{"k":"intake"}`), nil, Options{SpecVersion: artifacts.Core050Version}); failure == nil || failure.Code != "JPS-RESOURCE-DEPTH-LIMIT" || failure.Class != result.ClassPackNotConformant {
			t.Fatalf("evaluated: %+v", failure)
		}
	})
}
