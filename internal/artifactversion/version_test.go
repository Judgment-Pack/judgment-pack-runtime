package artifactversion

import (
	"strings"
	"testing"
)

func TestExactVersionOptions(t *testing.T) {
	for _, tc := range []struct {
		base string
		want Choices
	}{
		{"0.0.0", Choices{"0.0.1", "0.1.0", "1.0.0"}},
		{"1.2.3", Choices{"1.2.4", "1.3.0", "2.0.0"}},
		{"9007199254740993.99999999999999999999.99", Choices{"9007199254740993.99999999999999999999.100", "9007199254740993.100000000000000000000.0", "9007199254740994.0.0"}},
	} {
		t.Run(tc.base, func(t *testing.T) {
			r, code := Options("test", tc.base, nil)
			if code != 0 || r.Choices == nil || *r.Choices != tc.want || r.CompatibilityChecked || r.Scope != "version-arithmetic" || r.Comparison != "" {
				t.Fatal(code, r)
			}
		})
	}
}
func TestCandidateOrderingDoesNotClaimAdmission(t *testing.T) {
	for _, tc := range []struct{ candidate, want string }{
		{"1.2.3", "equal"}, {"1.2.4", "greater"}, {"1.10.0", "greater"}, {"1.1.99999999999999999999", "lower"}, {"0.99999999999999999999.99999999", "lower"}, {"999999999999999999999999999.0.0", "greater"},
	} {
		r, code := Options("test", "1.2.3", &tc.candidate)
		if code != 0 || r.Comparison != tc.want || r.CompatibilityChecked {
			t.Fatal(tc, r)
		}
	}
}
func TestVersionRefusalsAreWholeReports(t *testing.T) {
	for _, bad := range []string{"", "1", "1.2", "1.2.3.4", "01.2.3", "1.02.3", "1.2.03", "v1.2.3", "1.2.3-dev", "1.2.3+build", "1.2.3\n", " 1.2.3", "+1.2.3", "-1.2.3", "1e2.0.0", "١.2.3", strings.Repeat("9", MaxVersionBytes) + ".0.0"} {
		for _, candidate := range []bool{false, true} {
			base := "1.2.3"
			var next *string
			if candidate {
				next = &bad
			} else {
				base = bad
			}
			r, code := Options("test", base, next)
			if code == 0 || r.Status != "refused" || r.Choices != nil || r.Base != "" || r.Comparison != "" || r.Candidate != "" || len(r.Diagnostics) != 1 {
				t.Fatal(bad, candidate, r)
			}
		}
	}
	// All suggested versions remain in the accepted input domain, including carry.
	base := "0.0." + strings.Repeat("9", MaxVersionBytes-4)
	if r, code := Options("test", base, nil); code == 0 || r.Choices != nil {
		t.Fatal("unusable next version", r)
	}
	base = "0.0." + strings.Repeat("9", MaxVersionBytes-5)
	r, code := Options("test", base, nil)
	if code != 0 || len(r.Choices.Patch) != MaxVersionBytes {
		t.Fatal(r)
	}
}
