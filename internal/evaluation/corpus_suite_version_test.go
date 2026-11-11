package evaluation

import (
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
)

// CorpusSuiteVersion is the suiteVersion the corpus runner reports, and refuses
// what the runner refuses: a surface that describes the corpus behind its
// evaluator must name the corpus that runs, not a second reading of it.
func TestCorpusSuiteVersionIsTheVersionRunCorpusReports(t *testing.T) {
	engine := newTestEngine(t)
	for _, row := range []struct{ specVersion, suite string }{
		{"", "0.2.0-draft"},
		{artifacts.EvaluatorDraftVersion, "0.2.0-draft"},
		{artifacts.CoreVersion, "0.3.0"},
	} {
		suite, failure := CorpusSuiteVersion(row.specVersion)
		if failure != nil {
			t.Fatalf("%q: %+v", row.specVersion, failure)
		}
		if suite != row.suite {
			t.Fatalf("%q: suite version %q, want %q", row.specVersion, suite, row.suite)
		}
		run, failure := engine.RunCorpus(row.specVersion, "test")
		if failure != nil {
			t.Fatalf("%q: %+v", row.specVersion, failure)
		}
		if run.SuiteVersion != suite {
			t.Fatalf("%q: RunCorpus reports %q, CorpusSuiteVersion %q", row.specVersion, run.SuiteVersion, suite)
		}
	}
	for _, row := range []struct{ specVersion, code string }{
		{artifacts.DraftVersion, "JPS-CAPABILITY-EVALUATION-CORPUS"},
		{"9.9.9-draft", "JPS-CAPABILITY-SPEC-VERSION"},
	} {
		suite, failure := CorpusSuiteVersion(row.specVersion)
		if failure == nil || failure.Code != row.code || suite != "" {
			t.Fatalf("%q: suite %q, failure %+v, want code %s", row.specVersion, suite, failure, row.code)
		}
		if _, runFailure := engine.RunCorpus(row.specVersion, "test"); runFailure == nil || *runFailure != *failure {
			t.Fatalf("%q: RunCorpus refuses %+v, CorpusSuiteVersion %+v", row.specVersion, runFailure, failure)
		}
	}
}
