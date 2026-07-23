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
