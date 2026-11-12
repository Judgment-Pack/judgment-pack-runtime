package evaluation

import "testing"

// A corpus run is labeled as the evidence for the claim in CONFORMANCE.md
// only under the specVersion that claim attaches to, 0.2.0-draft. A run under
// 0.3.0 or 0.5.0 is labeled as outside the claim's version scope: its results
// are not evidence for the recorded claim.
func TestACorpusRunIsLabeledEvidenceOnlyUnderTheClaimsVersion(t *testing.T) {
	engine := newTestEngine(t)
	for version, want := range map[string]string{
		"0.2.0-draft": "corpus results, the required evidence for the claim in CONFORMANCE.md",
		"0.3.0":       "corpus results under a specVersion outside the version scope of the claim in CONFORMANCE.md",
		"0.5.0":       "corpus results under a specVersion outside the version scope of the claim in CONFORMANCE.md",
	} {
		run, failure := engine.RunCorpus(version, "experimental evaluate-corpus")
		if failure != nil {
			t.Fatalf("%s: %+v", version, failure)
		}
		if run.Label != want || run.ConformanceClaimReference != "CONFORMANCE.md" {
			t.Errorf("%s: label %q, reference %q", version, run.Label, run.ConformanceClaimReference)
		}
	}
}
