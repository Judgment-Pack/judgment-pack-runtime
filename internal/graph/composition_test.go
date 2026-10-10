package graph

import (
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"path/filepath"
	"testing"
)

func TestCompositionExamples(t *testing.T) {
	loaded, failure := project.Load(filepath.Join("testdata", "composition", "jpack.json"))
	if failure != nil {
		t.Fatal(failure.Message)
	}
	defer loaded.Close()
	output, runFailure := TestProject(loaded, newEngine(t), "", Options{Command: "test"})
	if runFailure != nil {
		t.Fatal(runFailure.Message)
	}
	if output.Status != "passed" {
		t.Fatalf("composition cases failed: %s", rawJSON(t, output))
	}
}
