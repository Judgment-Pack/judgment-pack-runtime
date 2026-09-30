package graph

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// A node evaluation carries what a standalone evaluation carries (ADR-0027
// clause 0), and since ADR-0040 that includes the required evidence §8 step 2
// found wanting. The fixture's screening node requires screening-record; a run
// that marks it absent names it on that node, and a run that supplies it names
// nothing there.
func TestANodeEvaluationNamesItsUnmetEvidence(t *testing.T) {
	loaded := fixtureProject(t)
	graphBytes, err := os.ReadFile(filepath.Join("testdata", "project", "onboarding.graph.json"))
	if err != nil {
		t.Fatal(err)
	}
	document, loadFailure := Load(graphBytes, "onboarding.graph.json")
	if loadFailure != nil {
		t.Fatal(loadFailure.Message)
	}
	for _, tc := range []struct {
		name   string
		inputs string
		want   []result.UnmetEvidence
	}{
		{"absent", `{"screening":{"facts":{"screening":{"matches":"0"}},"evidence":{"screening-record":"absent"}}}`,
			[]result.UnmetEvidence{{Requirement: "screening-record", State: "absent"}}},
		{"present", `{"screening":{"facts":{"screening":{"matches":"0"}},"evidence":{"screening-record":"present"}}}`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			evaluated, failure := Evaluate(loaded, newEngine(t), document, "onboarding.graph.json", []byte(tc.inputs), true, Options{Command: "evaluate"})
			if failure != nil {
				t.Fatal(failure.Message)
			}
			found := false
			for _, node := range evaluated.Nodes {
				if node.Node == "screening" {
					found = true
					if !reflect.DeepEqual(node.UnmetEvidence, tc.want) {
						t.Fatalf("screening node unmetEvidence = %+v, want %+v", node.UnmetEvidence, tc.want)
					}
				}
			}
			if !found {
				t.Fatal("the screening node was not reported")
			}
		})
	}
}
