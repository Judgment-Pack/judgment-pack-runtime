package compare

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

func comparisonEngine(t *testing.T) *evaluation.Engine {
	t.Helper()
	validator, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	return evaluation.NewEngine(validator)
}

func comparisonPack(version, escalation string) []byte {
	return []byte(`{"specVersion":"` + version + `","id":"https://example.com/packs/compare","version":"1.0.0","title":"Comparison","decision":{"intent":"Review","question":"Review?"},"outcomes":[{"id":"yes","label":"Yes"},{"id":"no","label":"No"}],"rules":[{"id":"one","description":"One","when":{"op":"literal","value":false},"outcome":"yes","onUnknown":"ignore"}],"escalation":` + escalation + `}`)
}

const comparisonRoutes = `{"triggers":["no-match"],"routes":[{"target":{"kind":"queue","name":"Z"}},{"target":{"kind":"system","name":"A"}},{"target":{"kind":"queue","name":"Z"}}]}`
const comparisonCanonical = `{"handoff":{"routes":[{"kind":"queue","name":"Z"},{"kind":"system","name":"A"},{"kind":"queue","name":"Z"}],"state":"requested","triggeredBy":["no-match"]},"kind":"unresolved","reasons":["no-match"]}`

func TestCore030ComparisonPreservesOrderedDuplicateRoutes(t *testing.T) {
	old := comparisonPack("0.3.0", comparisonRoutes)
	next := comparisonPack("0.3.0", strings.Replace(comparisonRoutes, `{"kind":"system","name":"A"}`, `{"kind":"system","name":"B"}`, 1))
	got, err := Run(comparisonEngine(t), Side{Pack: old}, Side{Pack: next}, "candidates", []Input{{ID: "one", Facts: json.RawMessage(`{}`)}}, nil, "compare")
	if err != nil {
		t.Fatal(err)
	}
	if got.EvaluatorSpecVersion != "0.3.0" || got.Inputs.Different != 1 || len(got.Differences) != 1 || !reflect.DeepEqual(got.Differences[0].Changed, []string{"handoff"}) {
		t.Fatalf("comparison: %+v", got)
	}
	side := got.Differences[0].Old
	canonical, err := side.Disposition.Canonical()
	if err != nil || string(canonical) != comparisonCanonical || side.HandoffTarget != nil {
		t.Fatalf("old side: %s, %v, %+v", canonical, err, side)
	}
	if got.Old.Digest == "" || got.New.Digest == "" || got.Old.Digest == got.New.Digest {
		t.Fatalf("exact identities: %+v %+v", got.Old, got.New)
	}
	same, err := Run(comparisonEngine(t), Side{Pack: old}, Side{Pack: old}, "candidates", []Input{{ID: "one", Facts: json.RawMessage(`{}`)}}, nil, "compare")
	if err != nil || same.Inputs.Same != 1 || len(same.Differences) != 0 {
		t.Fatalf("same ordered routes: %+v %v", same, err)
	}
}

func TestCore030ComparisonRefusesMixedContractsBeforeRows(t *testing.T) {
	old := comparisonPack(result.EvaluatorSpecVersion, `{"triggers":["no-match"],"target":{"kind":"queue","name":"Z"}}`)
	next := comparisonPack("0.3.0", comparisonRoutes)
	got, err := Run(nil, Side{Pack: old}, Side{Pack: next}, "candidates", nil, nil, "compare")
	if !errors.Is(err, ErrMixedContracts) || got.EvaluatorSpecVersion != "" {
		t.Fatalf("mixed comparison was not refused atomically: %+v %v", got, err)
	}
}

func TestCore030ComparisonPreservesUnsupportedProfile(t *testing.T) {
	base := comparisonPack("0.3.0", comparisonRoutes)
	extended := []byte(strings.Replace(string(base), `"specVersion":"0.3.0"`, `"specVersion":"0.3.0","profile":"extended"`, 1))
	_, err := Run(nil, Side{Pack: extended}, Side{Pack: base}, "candidates", nil, nil, "compare")
	var refused *SelectionError
	if !errors.As(err, &refused) || refused.Failure.Code != "profile-unsupported" || refused.Failure.Profile != "extended" || refused.Failure.Message != "" {
		t.Fatalf("profile refusal: %#v", err)
	}
}

func TestLegacyComparisonStillUsesTargetOnlyCanonicalBytes(t *testing.T) {
	old := comparisonPack(result.EvaluatorSpecVersion, `{"triggers":["no-match"],"target":{"kind":"queue","name":"Z"}}`)
	next := []byte(strings.Replace(string(old), `"name":"Z"`, `"name":"B"`, 1))
	got, err := Run(comparisonEngine(t), Side{Pack: old}, Side{Pack: next}, "candidates", []Input{{ID: "one", Facts: json.RawMessage(`{}`)}}, nil, "compare")
	if err != nil || got.EvaluatorSpecVersion != result.EvaluatorSpecVersion || len(got.Differences) != 1 || !reflect.DeepEqual(got.Differences[0].Changed, []string{"handoffTarget"}) {
		t.Fatalf("legacy comparison: %+v %v", got, err)
	}
	canonical, err := got.Differences[0].Old.Disposition.Canonical()
	if err != nil || string(canonical) != `{"handoff":{"state":"requested","triggeredBy":["no-match"]},"kind":"unresolved","reasons":["no-match"]}` {
		t.Fatalf("legacy canonical: %s %v", canonical, err)
	}
}
