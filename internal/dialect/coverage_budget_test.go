package dialect

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCoverageBudgetBoundsEncodedReport(t *testing.T) {
	for _, status := range []string{"complete", "review-required", "incomplete", "not-applicable"} {
		c := &Compatibility{Scope: "declared-inventory-coverage", Status: status, Pack: PackIdentity{ID: "id<>&", Version: "1.0.0", SpecVersion: "0.2.0-draft", SHA256: strings.Repeat("a", 64)}, InventoryOverApproximates: true, Facts: []CoverageEntry{}, Evidence: []CoverageEntry{}, Notes: []CoverageNote{}}
		b := newCoverageBudget(c, MaxCoverageReportBytes)
		for _, target := range []string{"/a", "/quote\"slash\\line\n", "/<>&\u2028\u2029", "/世界😀"} {
			e := CoverageEntry{Target: target, Status: "acknowledged", Producers: []string{target}, AcknowledgedBy: []string{"/", target}}
			n := CoverageNote{Code: "acknowledged-omission", Kind: "fact", Target: target, Severity: "warning"}
			if !b.entry(e) || !b.note(n) {
				t.Fatal("small report exceeded budget")
			}
			c.Facts = append(c.Facts, e)
			c.Notes = append(c.Notes, n)
		}
		data, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		if b.used < len(data) || b.used > len(data)+20 {
			t.Fatalf("budget=%d encoded=%d status=%s", b.used, len(data), status)
		}
	}
}

func TestCoverageBudgetStopsAmplificationWithoutEncodingIt(t *testing.T) {
	c := &Compatibility{Pack: PackIdentity{ID: "pack", Version: "1.0.0"}}
	base := "/" + strings.Repeat("a", 500)
	omissions := make([]string, 100)
	for i := range omissions {
		omissions[i] = base + strings.Repeat("/x", i)
	}
	e := CoverageEntry{Target: omissions[99] + "/p", Status: "acknowledged", Producers: []string{}, AcknowledgedBy: omissions}
	// A full 2,000-entry report is about 120 MB. Refuse after only the entries
	// that fit the budget; accounting must never allocate that encoded payload.
	allocs := testing.AllocsPerRun(3, func() {
		b := newCoverageBudget(c, MaxCoverageReportBytes)
		accepted := 0
		for accepted < 2000 && b.entry(e) {
			accepted++
		}
		if accepted == 0 || accepted > 40 || !b.exceeded() {
			t.Fatalf("failed to stop amplified report: accepted=%d used=%d", accepted, b.used)
		}
	})
	if allocs > 1 {
		t.Fatalf("budget accounting allocated %.0f objects", allocs)
	}
}

func FuzzCoverageStringBudget(f *testing.F) {
	for _, s := range []string{"", "plain", "\"\\\b\t\n\f\r", "<>&\u2028\u2029", "世界😀�", string([]byte{0xff, 0xfe}), strings.Repeat("x", 100)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		encoded, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		n := len(encoded) - 2
		b := &coverageBudget{limit: 6 * len(s)}
		// Carrier validation excludes malformed UTF-8. Its replacement spelling
		// differs across Go encoders (escaped or literal U+FFFD), so require a
		// conservative bound there and an exact count for admitted strings.
		if !b.stringContent(s) || b.used < n || (utf8.ValidString(s) && b.used != n) {
			t.Fatalf("count=%d encoded=%d validUTF8=%t", b.used, n, utf8.ValidString(s))
		}
		if b.used > 0 {
			b = &coverageBudget{limit: b.used - 1}
			if b.stringContent(s) || !b.exceeded() {
				t.Fatal("accepted over-limit string")
			}
		}
	})
}
