package cli

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

func TestRulesOverlapCLIValidationAndStrictConformance(t *testing.T) {
	data, err := os.ReadFile("../validation/testdata/rules-overlap-pack.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"human", "json"} {
		code, stdout, stderr := runTest(t, []string{"spec", "validate", "-", "--format", format}, string(data))
		if code != 0 || stderr != "" || !strings.Contains(stdout, "JPS-RULES-OVERLAP") || !strings.Contains(stdout, "less-than") {
			t.Fatalf("%s: %d %s %s", format, code, stdout, stderr)
		}
		if format == "json" {
			var report result.Validation
			if err := json.Unmarshal([]byte(stdout), &report); err != nil || report.Status != "valid" || report.Diagnostics[0].Severity != "warning" {
				t.Fatalf("validation: %+v %v", report, err)
			}
		}
	}
	for _, strict := range []bool{false, true} {
		args := []string{"spec", "test-conformance", "--spec-version", "0.3.0", "--format", "json"}
		if strict {
			args = append(args, "--strict")
		}
		code, stdout, stderr := runTest(t, args, "")
		want := 0
		if strict {
			want = 1
		}
		var suite result.Suite
		if err := json.Unmarshal([]byte(stdout), &suite); err != nil || code != want || stderr != "" || suite.Advisories == nil || suite.Advisories.Passed != 5 {
			t.Fatalf("strict=%v: %d %s %s %v", strict, code, stdout, stderr, err)
		}
	}
	code, stdout, stderr := runTest(t, []string{"spec", "test-conformance", "--spec-version", "0.3.0", "--strict"}, "")
	if code != 1 || stderr != "" || !strings.Contains(stdout, "strict mode rejects exact rule overlap (document remains valid)") || !strings.Contains(stdout, "optional advisory assertions: 5/5 matched") {
		t.Fatalf("strict human: %d %s %s", code, stdout, stderr)
	}
}
