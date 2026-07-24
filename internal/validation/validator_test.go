package validation

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

type testManifest struct {
	SpecVersion string `json:"specVersion"`
	Cases       []struct {
		ID                  string   `json:"id"`
		Path                string   `json:"path"`
		ExpectedResult      string   `json:"expectedResult"`
		SupportedExtensions []string `json:"supportedExtensions"`
		ExpectedDiagnostic  *struct {
			Code string `json:"code"`
			Path string `json:"path"`
		} `json:"expectedDiagnostic"`
	} `json:"cases"`
}

func TestBundledConformanceCorpus(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	set, err := artifacts.Load(artifacts.DraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := set.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	var manifest testManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, testCase := range manifest.Cases {
		t.Run(testCase.ID, func(t *testing.T) {
			fixture, err := set.Case(testCase.Path)
			if err != nil {
				t.Fatal(err)
			}
			actual, operational := engine.Validate(fixture, Options{
				Through:             "semantic",
				ExpectedSpecVersion: manifest.SpecVersion,
				SupportedExtensions: testCase.SupportedExtensions,
				Limits:              carrier.DefaultLimits(),
			})
			if operational != nil {
				t.Fatalf("unexpected operational failure: %v", operational)
			}
			if actual.Status != testCase.ExpectedResult {
				t.Fatalf("status = %s, want %s; diagnostics: %#v", actual.Status, testCase.ExpectedResult, actual.Diagnostics)
			}
			if testCase.ExpectedDiagnostic == nil {
				if len(actual.Diagnostics) != 0 {
					t.Fatalf("unexpected diagnostics: %#v", actual.Diagnostics)
				}
				return
			}
			for _, diagnostic := range actual.Diagnostics {
				if diagnostic.Code == testCase.ExpectedDiagnostic.Code && diagnostic.InstancePath == testCase.ExpectedDiagnostic.Path {
					return
				}
			}
			t.Fatalf("missing diagnostic %s at %s; got %#v", testCase.ExpectedDiagnostic.Code, testCase.ExpectedDiagnostic.Path, actual.Diagnostics)
		})
	}
}

func TestGenericUnknownVersionIsUnsupported(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	document := `{
  "specVersion": "9.9.9",
  "id": "https://example.com/unknown",
  "version": "1.0.0"
}`
	actual, operational := engine.Validate([]byte(document), Options{Through: "semantic", Limits: carrier.DefaultLimits()})
	if operational != nil {
		t.Fatal(operational)
	}
	if actual.Status != "unsupported" || len(actual.Diagnostics) != 1 || actual.Diagnostics[0].Code != "JPS-CAPABILITY-SPEC-VERSION" {
		t.Fatalf("unexpected result: %#v", actual)
	}
}

func TestStrictCarrierRejectsNestedEscapedDuplicateAndConstants(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	for name, document := range map[string]string{
		"escaped duplicate": `{"outer":{"a":1,"\u0061":2}}`,
		"nan":               `{"value":NaN}`,
		"trailing":          `{} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			actual, operational := engine.Validate([]byte(document), Options{Through: "semantic", Limits: carrier.DefaultLimits()})
			if operational != nil {
				t.Fatal(operational)
			}
			if actual.Status != "invalid" || actual.Layers[0].Name != "carrier" || actual.Layers[0].Status != "failed" {
				t.Fatalf("unexpected result: %#v", actual)
			}
		})
	}
}

func TestResourceLimitIsOperational(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	limits := carrier.DefaultLimits()
	limits.MaxStringBytes = 4
	_, operational := engine.Validate([]byte(`{"value":"`+strings.Repeat("x", 5)+`"}`), Options{Through: "semantic", Limits: limits})
	if operational == nil || operational.ExitCode != 4 {
		t.Fatalf("expected resource failure, got %#v", operational)
	}
}

func TestStructuralKeywordViolationsCannotPass(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(map[string]any)
		code     string
		location string
	}{
		{
			name: "enum",
			mutate: func(document map[string]any) {
				document["rules"].([]any)[0].(map[string]any)["onUnknown"] = "bogus"
			},
			code: "JPS-STRUCTURE-ENUM", location: "/rules/0/onUnknown",
		},
		{
			name: "minimum length",
			mutate: func(document map[string]any) {
				document["title"] = ""
			},
			code: "JPS-STRUCTURE-MIN-LENGTH", location: "/title",
		},
		{
			name: "unique items",
			mutate: func(document map[string]any) {
				document["metadata"] = map[string]any{"authors": []any{"A", "A"}}
			},
			code: "JPS-STRUCTURE-UNIQUE-ITEMS", location: "/metadata/authors",
		},
		{
			name: "required extension name",
			mutate: func(document map[string]any) {
				document["metadata"] = map[string]any{"requiredExtensions": []any{"not-namespaced"}}
			},
			code: "JPS-STRUCTURE-EXTENSION-NAME", location: "/metadata/requiredExtensions/0",
		},
	}

	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			document := validDocument(t)
			testCase.mutate(document)
			actual := validateDocument(t, engine, document, Options{Through: "structural", Limits: carrier.DefaultLimits()})
			if actual.Status != "invalid" || !hasDiagnostic(actual.Diagnostics, testCase.code, testCase.location) {
				t.Fatalf("unexpected result: %#v", actual)
			}
		})
	}
}

func TestConditionDiagnosticsExcludeIrrelevantOneOfBranches(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	set, err := artifacts.Load(artifacts.DraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	data, err := set.Case("structural/invalid-fact-path.json")
	if err != nil {
		t.Fatal(err)
	}
	actual, operational := engine.Validate(data, Options{Through: "structural", Limits: carrier.DefaultLimits()})
	if operational != nil {
		t.Fatal(operational)
	}
	if actual.Status != "invalid" || len(actual.Diagnostics) != 1 || !hasDiagnostic(actual.Diagnostics, "JPS-STRUCTURE-FACT-PATH", "/rules/0/when/path") {
		t.Fatalf("unexpected diagnostics: %#v", actual.Diagnostics)
	}
}

func TestURIFormatUsesStrictRFC3986ASCII(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct {
		name  string
		value string
		valid bool
	}{
		{name: "space", value: "https://example.com/a b"},
		{name: "raw unicode", value: "https://例.com/path"},
		{name: "bad percent escape", value: "https://example.com/%ZZ"},
		{name: "relative", value: "/relative/path"},
		{name: "backslash", value: `https://example.com/a\b`},
		{name: "urn", value: "urn:example:animal:ferret:nose", valid: true},
		{name: "percent encoded unicode", value: "https://example.com/%E4%BE%8B", valid: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			document := validDocument(t)
			document["id"] = testCase.value
			actual := validateDocument(t, engine, document, Options{Through: "semantic", Limits: carrier.DefaultLimits()})
			if testCase.valid && actual.Status != "valid" {
				t.Fatalf("valid URI rejected: %#v", actual)
			}
			if !testCase.valid && (actual.Status != "invalid" || !hasDiagnostic(actual.Diagnostics, "JPS-STRUCTURE-FORMAT-URI", "/id")) {
				t.Fatalf("invalid URI accepted or misreported: %#v", actual)
			}
		})
	}
}

func TestSchemaCompilerRejectsExternalResourcesOffline(t *testing.T) {
	_, err := CompileSchema([]byte(`{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$ref": "https://example.com/remote-schema.json"
}`), "urn:judgmentpack:test:offline")
	if err == nil || !strings.Contains(err.Error(), "external schema resources are disabled") {
		t.Fatalf("expected offline loader failure, got %v", err)
	}
}

func TestDiagnosticLimitsApplyToStructuralAndCapabilityResults(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}

	structuralDocument := validDocument(t)
	for index := 0; index < MaxDiagnostics+50; index++ {
		structuralDocument[fmt.Sprintf("unknown%03d", index)] = true
	}
	structural := validateDocument(t, engine, structuralDocument, Options{Through: "structural", Limits: carrier.DefaultLimits()})
	if structural.Status != "invalid" || len(structural.Diagnostics) != MaxDiagnostics || !structural.DiagnosticsTruncated {
		t.Fatalf("unexpected structural limit result: diagnostics=%d truncated=%v status=%s", len(structural.Diagnostics), structural.DiagnosticsTruncated, structural.Status)
	}

	capabilityDocument := validDocument(t)
	required := make([]any, 0, MaxDiagnostics+50)
	extensions := map[string]any{}
	for index := 0; index < MaxDiagnostics+50; index++ {
		name := fmt.Sprintf("com.example.extension-%03d", index)
		required = append(required, name)
		extensions[name] = true
	}
	capabilityDocument["metadata"] = map[string]any{"requiredExtensions": required}
	capabilityDocument["extensions"] = extensions
	capability := validateDocument(t, engine, capabilityDocument, Options{Through: "semantic", Limits: carrier.DefaultLimits()})
	if capability.Status != "unsupported" || len(capability.Diagnostics) != MaxDiagnostics || !capability.DiagnosticsTruncated {
		t.Fatalf("unexpected capability limit result: diagnostics=%d truncated=%v status=%s", len(capability.Diagnostics), capability.DiagnosticsTruncated, capability.Status)
	}
}

func validDocument(t *testing.T) map[string]any {
	t.Helper()
	set, err := artifacts.Load(artifacts.DraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	data, err := set.Case("valid/minimal-literal.json")
	if err != nil {
		t.Fatal(err)
	}
	value, failure := carrier.Decode(data, carrier.DefaultLimits())
	if failure != nil {
		t.Fatal(failure)
	}
	return value.(map[string]any)
}

func validateDocument(t *testing.T, engine *Engine, document map[string]any, options Options) result.Validation {
	t.Helper()
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	actual, operational := engine.Validate(data, options)
	if operational != nil {
		t.Fatal(operational)
	}
	return actual
}

func hasDiagnostic(diagnostics []result.Diagnostic, code, location string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code && diagnostic.InstancePath == location {
			return true
		}
	}
	return false
}

func diagnosticMessage(diagnostics []result.Diagnostic, code, location string) (string, bool) {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code && diagnostic.InstancePath == location {
			return diagnostic.Message, true
		}
	}
	return "", false
}

func TestTypeDiagnosticNamesExpectedType(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	document := validDocument(t)
	document["title"] = float64(7)
	actual := validateDocument(t, engine, document, Options{Through: "structural", Limits: carrier.DefaultLimits()})
	message, ok := diagnosticMessage(actual.Diagnostics, "JPS-STRUCTURE-TYPE", "/title")
	if actual.Status != "invalid" || !ok {
		t.Fatalf("expected a type diagnostic at /title: %#v", actual.Diagnostics)
	}
	if !strings.Contains(message, "string") {
		t.Fatalf("type diagnostic should name the expected type: %q", message)
	}
}

func TestNumericOrderedOperandReportsDecimalDiagnostic(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	document := validDocument(t)
	rule := document["rules"].([]any)[0].(map[string]any)
	rule["when"] = map[string]any{
		"op": "fact", "path": "/amount", "operator": "greater-than", "value": float64(5000),
	}
	actual := validateDocument(t, engine, document, Options{Through: "structural", Limits: carrier.DefaultLimits()})
	message, ok := diagnosticMessage(actual.Diagnostics, "JPS-STRUCTURE-DECIMAL-OPERAND", "/rules/0/when/value")
	if actual.Status != "invalid" || !ok {
		t.Fatalf("a numeric ordered operand should report the decimal-operand diagnostic, not a generic type error: %#v", actual.Diagnostics)
	}
	if !strings.Contains(message, "decimal string") {
		t.Fatalf("decimal-operand message should teach the requirement: %q", message)
	}
}

func TestSourceLocatorValueStaysGenericType(t *testing.T) {
	// A member named "value" that is not a comparison operand (here a source
	// locator value) must keep the generic type diagnostic, not be misreported
	// as an operand-specific one.
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	document := validDocument(t)
	document["sources"] = []any{
		map[string]any{
			"id":      "s1",
			"title":   "Example source",
			"locator": map[string]any{"kind": "uri", "value": float64(3)},
		},
	}
	actual := validateDocument(t, engine, document, Options{Through: "structural", Limits: carrier.DefaultLimits()})
	if actual.Status != "invalid" || !hasDiagnostic(actual.Diagnostics, "JPS-STRUCTURE-TYPE", "/sources/0/locator/value") {
		t.Fatalf("a locator value type error should stay a generic type diagnostic: %#v", actual.Diagnostics)
	}
}

func TestUnresolvedReferenceNamesOffendingIDAndDeclaredSet(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	document := validDocument(t)
	document["rules"].([]any)[0].(map[string]any)["outcome"] = "does-not-exist"
	actual := validateDocument(t, engine, document, Options{Through: "semantic", Limits: carrier.DefaultLimits()})
	message, ok := diagnosticMessage(actual.Diagnostics, "JPS-SEMANTIC-UNRESOLVED-OUTCOME", "/rules/0/outcome")
	if actual.Status != "invalid" || !ok {
		t.Fatalf("expected an unresolved-outcome diagnostic: %#v", actual.Diagnostics)
	}
	if !strings.Contains(message, `"does-not-exist"`) || !strings.Contains(message, "Declared outcome ids:") {
		t.Fatalf("unresolved message should name the offending id and the declared set: %q", message)
	}
}

func TestNestedOperandErrorsAreNotMaskedByCompositeShape(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	document := validDocument(t)
	rule := document["rules"].([]any)[0].(map[string]any)
	rule["when"] = map[string]any{
		"op": "all",
		"conditions": []any{
			map[string]any{"op": "fact", "path": "/a", "operator": "greater-than", "value": float64(5)},
			map[string]any{"op": "fact", "path": "/b", "operator": "less-than", "value": float64(9)},
		},
	}
	actual := validateDocument(t, engine, document, Options{Through: "structural", Limits: carrier.DefaultLimits()})
	if actual.Status != "invalid" {
		t.Fatalf("expected invalid: %#v", actual)
	}
	// Both nested operands must report at their own paths, not collapse to a
	// single generic shape error at the composite condition.
	if !hasDiagnostic(actual.Diagnostics, "JPS-STRUCTURE-DECIMAL-OPERAND", "/rules/0/when/conditions/0/value") ||
		!hasDiagnostic(actual.Diagnostics, "JPS-STRUCTURE-DECIMAL-OPERAND", "/rules/0/when/conditions/1/value") {
		t.Fatalf("both nested operand errors should surface at their own paths: %#v", actual.Diagnostics)
	}
	if hasDiagnostic(actual.Diagnostics, "JPS-STRUCTURE-CONDITION-SHAPE", "/rules/0/when") {
		t.Fatalf("composite should not report a generic shape error when its children have specific errors: %#v", actual.Diagnostics)
	}
}

func TestEnumDiagnosticListsAllowedValues(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	document := validDocument(t)
	document["rules"].([]any)[0].(map[string]any)["onUnknown"] = "bogus-mode"
	actual := validateDocument(t, engine, document, Options{Through: "structural", Limits: carrier.DefaultLimits()})
	message, ok := diagnosticMessage(actual.Diagnostics, "JPS-STRUCTURE-ENUM", "/rules/0/onUnknown")
	if actual.Status != "invalid" || !ok {
		t.Fatalf("expected an enum diagnostic at /rules/0/onUnknown: %#v", actual.Diagnostics)
	}
	if !strings.Contains(message, "escalate") {
		t.Fatalf("enum message should list the allowed values: %q", message)
	}
}

func TestLocalIDDiagnosticNamesValueAndExample(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	document := validDocument(t)
	document["outcomes"].([]any)[0].(map[string]any)["id"] = "Bad Id"
	actual := validateDocument(t, engine, document, Options{Through: "structural", Limits: carrier.DefaultLimits()})
	message, ok := diagnosticMessage(actual.Diagnostics, "JPS-STRUCTURE-LOCAL-ID", "/outcomes/0/id")
	if actual.Status != "invalid" || !ok {
		t.Fatalf("expected a local-id diagnostic at /outcomes/0/id: %#v", actual.Diagnostics)
	}
	if !strings.Contains(message, "Bad Id") || !strings.Contains(message, "needs-review") {
		t.Fatalf("local-id message should name the offending value and give an example: %q", message)
	}
}

func TestArityDiagnosticStatesMinimumAndActual(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	document := validDocument(t)
	document["outcomes"] = document["outcomes"].([]any)[:1] // one outcome; the minimum is two
	actual := validateDocument(t, engine, document, Options{Through: "structural", Limits: carrier.DefaultLimits()})
	message, ok := diagnosticMessage(actual.Diagnostics, "JPS-STRUCTURE-COLLECTION-ARITY", "/outcomes")
	if actual.Status != "invalid" || !ok {
		t.Fatalf("expected a collection-arity diagnostic at /outcomes: %#v", actual.Diagnostics)
	}
	if !strings.Contains(message, "at least 2") || !strings.Contains(message, "has 1") {
		t.Fatalf("arity message should state the required minimum and the actual count: %q", message)
	}
}

func TestBadConditionOpIsConsistentAndNamesValidOps(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	// The same mistake -- an op that selects no condition branch -- must report
	// the same code and location regardless of sibling members, list the valid
	// ops, and never spuriously flag the op member itself.
	for _, when := range []map[string]any{
		{"op": "maybe"},
		{"op": "maybe", "value": true},
		{"op": "maybe", "conditions": []any{map[string]any{"op": "literal", "value": true}}},
	} {
		document := validDocument(t)
		document["rules"].([]any)[0].(map[string]any)["when"] = when
		actual := validateDocument(t, engine, document, Options{Through: "structural", Limits: carrier.DefaultLimits()})
		message, ok := diagnosticMessage(actual.Diagnostics, "JPS-STRUCTURE-CONDITION-SHAPE", "/rules/0/when")
		if actual.Status != "invalid" || !ok {
			t.Fatalf("bad op %v should report CONDITION-SHAPE at /rules/0/when: %#v", when, actual.Diagnostics)
		}
		if !strings.Contains(message, "fact") || !strings.Contains(message, "evidence-present") {
			t.Fatalf("condition-shape message should list the valid op values: %q", message)
		}
		if hasDiagnostic(actual.Diagnostics, "JPS-STRUCTURE-SCHEMA", "/rules/0/when/op") {
			t.Fatalf("a bad op must not spuriously flag the op member: %#v", actual.Diagnostics)
		}
	}
}

func TestValidConditionOpIsNotSpuriouslyFlagged(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	// op "all" is valid, but the object is shaped like a "not" (has "condition").
	// The losing branch's op-const failure must not leak onto the valid op; the
	// object's real errors must still surface.
	document := validDocument(t)
	document["rules"].([]any)[0].(map[string]any)["when"] = map[string]any{
		"op":        "all",
		"condition": map[string]any{"op": "literal", "value": true},
	}
	actual := validateDocument(t, engine, document, Options{Through: "structural", Limits: carrier.DefaultLimits()})
	if actual.Status != "invalid" {
		t.Fatalf("expected invalid: %#v", actual)
	}
	if hasDiagnostic(actual.Diagnostics, "JPS-STRUCTURE-SCHEMA", "/rules/0/when/op") {
		t.Fatalf("a valid op must not be flagged: %#v", actual.Diagnostics)
	}
	if !hasDiagnostic(actual.Diagnostics, "JPS-STRUCTURE-REQUIRED-MEMBER", "/rules/0/when/conditions") {
		t.Fatalf("the matched branch's real errors should surface: %#v", actual.Diagnostics)
	}
}
