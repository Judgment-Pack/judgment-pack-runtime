package project

import (
	"errors"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

// Independently authored Core §§6.7, 8.1 and 8.3 public integration vectors.
const core030Pack = `{"specVersion":"0.3.0","id":"https://example.com/packs/routes","version":"1.0.0","title":"Routes","decision":{"intent":"Review","question":"Review?"},"outcomes":[{"id":"yes","label":"Yes"},{"id":"no","label":"No"}],"rules":[{"id":"one","description":"One","when":{"op":"literal","value":false},"outcome":"yes","onUnknown":"ignore"}],"escalation":{"triggers":["no-match"],"routes":[{"target":{"kind":"queue","name":"Z"}},{"target":{"kind":"system","name":"A"}},{"target":{"kind":"queue","name":"Z"}}]}}`
const core030Expected = `{"handoff":{"routes":[{"kind":"queue","name":"Z"},{"kind":"system","name":"A"},{"kind":"queue","name":"Z"}],"state":"requested","triggeredBy":["no-match"]},"kind":"unresolved","reasons":["no-match"]}`
const core030Matrix = `{"matrixVersion":"3","cases":[{"id":"routes","origin":"history","facts":{},"expectedDisposition":` + core030Expected + `}]}`

func core030Engine(t *testing.T) *evaluation.Engine {
	t.Helper()
	engine, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	return evaluation.NewEngine(engine)
}

func TestCore030MatrixUsesExplicitContract(t *testing.T) {
	if ValidateMatrix([]byte(core030Matrix)).Status != "invalid" {
		t.Fatal("legacy admission inferred routes")
	}
	if got := ValidateMatrixWith([]byte(core030Matrix), result.Core030EvaluatorContract()); got.Status != "valid" {
		t.Fatalf("explicit admission: %+v", got)
	}
	if _, ok := DecodeWitness("row", []byte(core030Expected)); ok {
		t.Fatal("legacy witness inferred routes")
	}
	if _, ok := DecodeWitnessWith("row", []byte(core030Expected), result.Core030EvaluatorContract()); !ok {
		t.Fatal("explicit routed witness lost")
	}
	matrix, err := DecodeMatrix([]byte(core030Matrix))
	if err != nil {
		t.Fatal(err)
	}
	matrix.DispositionContract = result.Core030EvaluatorContract()
	if len(matrixWitnesses(matrix)) != 1 {
		t.Fatal("coverage lost routed witness")
	}
}

func TestCore030SnapshotRoutesAndSingularAssertion(t *testing.T) {
	output, err := TestSnapshot(core030Engine(t), []byte(core030Pack), []byte(core030Matrix), "test")
	if err != nil {
		t.Fatal(err)
	}
	if output.Status != "passed" || output.EvaluatorSpecVersion != "0.3.0" || output.Packs[0].Rows[0].Actual != core030Expected {
		t.Fatalf("snapshot: %+v", output)
	}
	if output.Packs[0].Profile == nil || len(output.Packs[0].Profile.Coverage) != 1 || output.Packs[0].Profile.Coverage[0].Covered == 0 {
		t.Fatal("history lost routed witness")
	}
	singular := strings.Replace(core030Matrix, `"facts":{}`, `"facts":{},"expectedHandoffTarget":null`, 1)
	output, err = TestSnapshot(core030Engine(t), []byte(core030Pack), []byte(singular), "test")
	if err != nil || output.Status != "mismatch" || !strings.Contains(output.Packs[0].Rows[0].Detail, "incompatible with a routed") {
		t.Fatalf("singular target: %+v, %v", output, err)
	}
}

func TestCore030ProjectRefusesMixedContractsAndBoundsBeforeRetention(t *testing.T) {
	legacy := strings.Replace(core030Pack, `"specVersion":"0.3.0"`, `"specVersion":"0.2.0-draft"`, 1)
	path := writeProject(t, `{"configVersion":"1","packs":{"a":{"path":"a.json","matrix":"rows.json"},"b":{"path":"b.json","matrix":"rows.json"}}}`, map[string]string{"a.json": core030Pack, "b.json": legacy, "rows.json": core030Matrix})
	project, failure := Load(path)
	if failure != nil {
		t.Fatal(failure)
	}
	defer project.Close()
	output, failure := project.Test(core030Engine(t), "", "test")
	if failure == nil || failure.Code != "JPS-MATRIX-MIXED-CONTRACT" || len(output.Packs) != 0 {
		t.Fatalf("mixed: %+v, %+v", output, failure)
	}
	project.dispositionReportBudget = 1
	output, failure = project.Test(core030Engine(t), "a", "test")
	if failure == nil || failure.Code != "JPS-RESOURCE-MATRIX-DISPOSITIONS" || len(output.Packs) != 0 {
		t.Fatalf("budget: %+v, %+v", output, failure)
	}
}

func TestCore030SnapshotUnsupportedProfileExact(t *testing.T) {
	pack := strings.Replace(core030Pack, `"specVersion":"0.3.0"`, `"specVersion":"0.3.0","profile":"extended","invalid":true`, 1)
	_, err := TestSnapshot(core030Engine(t), []byte(pack), []byte(core030Matrix), "test")
	var failure *Failure
	if !errors.As(err, &failure) || failure.Code != "profile-unsupported" || failure.Profile != "extended" || failure.Message != "" {
		t.Fatalf("profile: %+v", err)
	}
}
