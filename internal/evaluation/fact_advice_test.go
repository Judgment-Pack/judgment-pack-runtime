package evaluation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

func TestFactTypeAdviceUsesRuntimeTypesWithoutCoercion(t *testing.T) {
	validator, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	admitted := NewEngine(validator).AdmitPack(comparablePack(t))
	for _, row := range []struct {
		name  string
		value any
		found bool
		kind  string
	}{
		{"decimal", "0", false, ""}, {"number-zero", 0, true, "number"}, {"false", false, true, "boolean"},
		{"null", nil, true, "null"}, {"missing", absentFact, false, ""}, {"bad-string", "01", true, "string"},
	} {
		t.Run(row.name, func(t *testing.T) {
			facts := comparableFacts(t, map[string]any{"/amount/value": row.value})
			original := bytes.Clone(facts)
			findings, failure := admitted.CheckFactTypes(facts, Options{})
			if failure != nil {
				t.Fatal(failure)
			}
			if !bytes.Equal(facts, original) {
				t.Fatal("modified facts")
			}
			if row.found {
				if len(findings) != 1 || findings[0].Path != "/amount/value" || findings[0].FactType != row.kind || findings[0].ExpectedTypes[0] != "decimal-string" {
					t.Fatalf("%+v", findings)
				}
			} else if len(findings) != 0 {
				t.Fatal(findings)
			}
		})
	}
	facts := comparableFacts(t, map[string]any{"/request/exempt": true, "/amount/value": 42})
	findings, failure := admitted.CheckFactTypes(facts, Options{})
	if failure != nil || len(findings) != 1 {
		t.Fatalf("unreached rules must still be checked: %+v %v", findings, failure)
	}
	_, failure = admitted.CheckFactTypes(facts, Options{WorkBudget: 1})
	if failure == nil || failure.Code != "FACT-CHECK-LIMIT" {
		t.Fatal("work exhaustion must not look clean", failure)
	}
}

func TestFactTypeAdviceFailuresNeverReturnPartialFindings(t *testing.T) {
	engine := newTestEngine(t)
	pack := comparablePack(t)
	for name, row := range map[string]struct{ pack, facts []byte }{
		"bad-pack":        {[]byte(`{}`), []byte(`{}`)},
		"bad-facts":       {pack, []byte(`{"amount":{"value":0},"amount":{}}`)},
		"old-core":        {bytes.Replace(pack, []byte(`0.2.0-draft`), []byte(`0.1.0-draft`), 1), []byte(`{}`)},
		"unvendored-core": {bytes.Replace(pack, []byte(`0.2.0-draft`), []byte(`0.3.0`), 1), []byte(`{}`)},
	} {
		t.Run(name, func(t *testing.T) {
			findings, failure := engine.AdmitPack(row.pack).CheckFactTypes(row.facts, Options{})
			if failure == nil || findings != nil {
				t.Fatalf("failure must discard findings: %+v %+v", findings, failure)
			}
		})
	}
}

// Inputs are hand-authored constructions, not captured implementation output.
// Each bound independently refuses a complete check, including output escaping.
func TestFactTypeAdviceReportLimits(t *testing.T) {
	for _, tc := range []struct {
		name  string
		count int
		key   string
	}{
		{"count", MaxFactTypeFindings + 1, "p"},
		{"escaped-bytes", 1, strings.Repeat("<", 50000)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var pack map[string]any
			if err := json.Unmarshal(comparablePack(t), &pack); err != nil {
				t.Fatal(err)
			}
			conditions := []any{}
			facts := map[string]any{}
			for i := 0; i < tc.count; i++ {
				key := fmt.Sprintf("%s%d", tc.key, i)
				conditions = append(conditions, map[string]any{"op": "fact", "path": "/" + key, "operator": "equals", "value": true})
				facts[key] = 0
			}
			pack["rules"] = []any{map[string]any{"id": "check", "description": "Check types.", "when": map[string]any{"op": "all", "conditions": conditions}, "outcome": "violation", "onUnknown": "escalate"}}
			packBytes, _ := json.Marshal(pack)
			factBytes, _ := json.Marshal(facts)
			findings, failure := newTestEngine(t).AdmitPack(packBytes).CheckFactTypes(factBytes, Options{})
			if failure == nil || failure.Code != "FACT-CHECK-LIMIT" || findings != nil {
				t.Fatalf("%+v %+v", findings, failure)
			}
		})
	}
}

func TestFactTypeAdviceReportsRuntimeOwnedTypesInWalkOrder(t *testing.T) {
	facts := []byte(`{"amount":{"exceedsCap":"SECRET","value":9007199254740993},"request":{"exempt":null,"outsideWindow":0,"region":false,"verified":[]}}`)
	original := bytes.Clone(facts)
	findings, failure := newTestEngine(t).AdmitPack(comparablePack(t)).CheckFactTypes(facts, Options{})
	if failure != nil || len(findings) != 6 || !bytes.Equal(facts, original) {
		t.Fatalf("%+v %+v", findings, failure)
	}
	for i, want := range []struct{ path, operator, kind, expected string }{
		{"/request/exempt", "equals", "null", "boolean"},
		{"/amount/exceedsCap", "equals", "string", "boolean"},
		{"/request/outsideWindow", "equals", "number", "boolean"},
		{"/amount/value", "greater-than", "number", "decimal-string"},
		{"/request/region", "in", "boolean", "string"},
		{"/request/verified", "not-equals", "array", "boolean"},
	} {
		got := findings[i]
		if got.Code != "FACT-NOT-COMPARABLE" || got.Path != want.path || got.Operator != want.operator || got.FactType != want.kind || !reflect.DeepEqual(got.ExpectedTypes, []string{want.expected}) || got.Within != nil || got.Message == "" {
			t.Fatalf("row %d: %+v", i, got)
		}
	}
	encoded, _ := json.Marshal(findings)
	if bytes.Contains(encoded, []byte("SECRET")) || bytes.Contains(encoded, []byte("9007199254740993")) {
		t.Fatal("finding leaked a fact value")
	}
}
