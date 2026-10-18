package policy

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

func TestReviewF1DiagnosticSelectionPrecedesCap(t *testing.T) {
	// Deliberately reverse causes and missing-property order. This exercises the
	// dependency's possible traversal orders without relying on random Go maps.
	tree := func(reverse bool) *jsonschema.ValidationError {
		root := &jsonschema.ValidationError{}
		for i := 0; i < 64; i++ {
			index := i
			if reverse {
				index = 63 - i
			}
			missing := []string{"title", "anchor", "id"}
			if reverse {
				missing = []string{"id", "anchor", "title"}
			}
			cause := &jsonschema.ValidationError{InstanceLocation: []string{"sections", fmt.Sprint(index)}, ErrorKind: &kind.Required{Missing: missing}}
			root.Causes = append(root.Causes, cause, cause) // Duplicate branch findings collapse.
		}
		root.Causes = append(root.Causes, &jsonschema.ValidationError{ErrorKind: &kind.AdditionalProperties{Properties: []string{strings.Repeat("private-prose", 1000), "another-unknown"}}})
		return root
	}
	forward, forwardTruncated := schemaDiagnostics(tree(false))
	reverse, reverseTruncated := schemaDiagnostics(tree(true))
	if !forwardTruncated || !reverseTruncated || len(forward) != validation.MaxDiagnostics || !reflect.DeepEqual(forward, reverse) {
		t.Fatalf("capped findings depend on traversal: forward=%+v reverse=%+v", forward, reverse)
	}
	for i := 1; i < len(forward); i++ {
		if !diagnosticLess(forward[i-1], forward[i]) {
			t.Fatal("findings are not sorted and unique")
		}
	}
	if strings.Contains(string(mustJSON(t, forward)), "private-prose") {
		t.Fatal("unknown-property content was echoed")
	}
}

func TestReviewF1RealDocumentAndTruncationFlag(t *testing.T) {
	f := loadFixture(t, "refunds")
	sections := []any{}
	all := []result.Diagnostic{}
	for i := 0; i < 64; i++ {
		sections = append(sections, map[string]any{"id": fmt.Sprintf("section-%d", i), "title": "", "anchor": map[string]any{"startByte": -1, "endByte": 1, "bytesDigest": "invalid"}})
		for _, suffix := range []string{"/title", "/anchor/startByte", "/anchor/bytesDigest"} {
			all = append(all, result.ErrorDiagnostic("JPS-POLICY-SCHEMA", "policy", fmt.Sprintf("/sections/%d%s", i, suffix), "Value does not satisfy the closed prototype schema."))
		}
	}
	sort.Slice(all, func(i, j int) bool { return diagnosticLess(all[i], all[j]) })
	f.policy["sections"] = sections
	report, code := Validate("validate", mustJSON(t, f.policy))
	requireRefusal(t, report, code, result.ExitInvalid)
	if !report.DiagnosticsTruncated || !reflect.DeepEqual(report.Diagnostics, all[:validation.MaxDiagnostics]) {
		t.Fatalf("not the stable first 100 findings: %+v", report)
	}
	if !strings.Contains(string(mustJSON(t, report)), `"diagnosticsTruncated":true`) {
		t.Fatal("missing truncation indication")
	}
	for _, count := range []int{100, 101} {
		root := &jsonschema.ValidationError{}
		for i := 0; i < count; i++ {
			root.Causes = append(root.Causes, &jsonschema.ValidationError{InstanceLocation: []string{fmt.Sprint(i)}, ErrorKind: &kind.Required{Missing: []string{"value"}}})
		}
		diagnostics, truncated := schemaDiagnostics(root)
		if len(diagnostics) != 100 || truncated != (count > 100) {
			t.Fatalf("count %d truncated=%v", count, truncated)
		}
	}
	valid := loadFixture(t, "refunds")
	report, code = Validate("validate", mustJSON(t, valid.policy))
	if code != 0 || report.DiagnosticsTruncated {
		t.Fatal("successful validation marked truncated")
	}
	valid.policy["unknown"] = true
	report, code = Validate("validate", mustJSON(t, valid.policy))
	requireRefusal(t, report, code, result.ExitInvalid)
	if report.DiagnosticsTruncated || strings.Contains(string(mustJSON(t, report)), `"diagnosticsTruncated"`) {
		t.Fatal("complete refusal marked truncated")
	}
}

func TestReviewF2LengthNumbersArePreflighted(t *testing.T) {
	for _, tc := range []struct {
		name, literal, code string
		exit                int
	}{
		{"oversized-integer", strings.Repeat("9", 128*1024), "JPS-POLICY-BYTE-LENGTH-BOUND", result.ExitUnsupported},
		{"over-original-bound", "4194305", "JPS-POLICY-BYTE-LENGTH-BOUND", result.ExitUnsupported},
		{"fraction", "1.0", "JPS-POLICY-BYTE-LENGTH", result.ExitInvalid},
		{"long-fraction", "1." + strings.Repeat("1", 64*1024), "JPS-POLICY-BYTE-LENGTH", result.ExitInvalid},
		{"exponent", "1e0", "JPS-POLICY-BYTE-LENGTH", result.ExitInvalid},
		{"huge-exponent", "1e" + strings.Repeat("9", 64*1024), "JPS-POLICY-BYTE-LENGTH", result.ExitInvalid},
		{"negative", "-1", "JPS-POLICY-BYTE-LENGTH", result.ExitInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire := []byte(`{"materials":[{"content":{"bytesDigest":"` + digest(nil) + `","byteLength":` + tc.literal + `,"mediaType":"application/octet-stream"},"dataBase64":""}]}`)
			_, failure, code := decodeMaterials(wire)
			if failure == nil || code != tc.exit || failure.code != tc.code || failure.path != "/materials/0/content/byteLength" {
				t.Fatalf("pre-schema refusal = %+v exit %d", failure, code)
			}
		})
	}
	for _, literal := range []string{"0", "-0"} {
		wire := []byte(`{"materials":[{"content":{"bytesDigest":"` + digest(nil) + `","byteLength":` + literal + `,"mediaType":"application/octet-stream"},"dataBase64":""}]}`)
		material, failure, code := decodeMaterials(wire)
		if failure != nil || code != 0 || len(material) != 1 {
			t.Fatalf("supported zero literal: %+v exit %d", failure, code)
		}
	}
	// The largest token admitted by the carrier is rejected lexically; this
	// checks the preflight itself and makes no timing or memory measurement claim.
	huge := json.Number(strings.Repeat("9", 5592408))
	if failure, code := preflightMaterialNumbers(huge, nil); failure == nil || code != result.ExitUnsupported {
		t.Fatal("maximum carrier number token escaped preflight")
	}
	if failure, code := preflightMaterialNumbers(json.Number("4194304"), nil); failure != nil || code != 0 {
		t.Fatal("exact original byte bound refused")
	}
}

func TestReviewF2UnknownAndWrongTypedNumbersAreGuarded(t *testing.T) {
	huge := strings.Repeat("9", 64*1024)
	secret := strings.Repeat("private-number-prose-", 200)
	key := string(mustJSON(t, secret))
	for _, tc := range []struct{ name, wire string }{
		{"root-unknown", `{"materials":[],` + key + `:` + huge + `}`},
		{"content-unknown", `{"materials":[{"content":{` + key + `:` + huge + `}}]}`},
		{"wrong-typed-length", `{"materials":[{"content":{"byteLength":{` + key + `:` + huge + `}}}]}`},
		{"wrong-typed-base64", `{"materials":[{"dataBase64":` + huge + `}]}`},
		{"wrong-typed-materials", `{"materials":{` + key + `:{"content":{"byteLength":` + huge + `}}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, failure, code := decodeMaterials([]byte(tc.wire))
			if failure == nil || code != result.ExitUnsupported || failure.code != "JPS-POLICY-MATERIAL-NUMBER-BOUND" || failure.path != "/materials" {
				t.Fatalf("unknown numeric token reached schema: %+v exit %d", failure, code)
			}
			if strings.Contains(failure.path, secret) || strings.Contains(failure.message, secret) {
				t.Fatal("untrusted numeric member appeared in refusal")
			}
		})
	}
	for _, wire := range []string{`{"materials":[],"unknown":1}`, `{"materials":[{"content":{"byteLength":"1"}}]}`} {
		_, failure, code := decodeMaterials([]byte(wire))
		if failure == nil || code != result.ExitInvalid || failure.code != "JPS-POLICY-MATERIALS-SCHEMA" {
			t.Fatalf("closed grammar was weakened: %+v exit %d", failure, code)
		}
	}
}
