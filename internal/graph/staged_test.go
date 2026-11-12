package graph

import (
	"bytes"
	"os"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// Graphs do not read the staged draft selector (EXPERIMENTAL): a node pack
// declaring it is refused at contract selection, before any node runs.
func TestStagedDraftNodePackIsRefused(t *testing.T) {
	raw, err := os.ReadFile("../artifacts/jps/0.3.0/cases/valid/minimal-literal.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, failure := selectNodeContract(raw, nil); failure != nil {
		t.Fatalf("released pack refused: %+v", failure)
	}
	pack := bytes.Replace(raw, []byte(`"specVersion": "0.3.0"`), []byte(`"specVersion": "0.4.0-draft"`), 1)
	_, failure := selectNodeContract(pack, nil)
	if failure == nil || failure.Code != result.StagedDraftUnsupportedCode || failure.ExitCode != result.ExitUnsupported {
		t.Fatalf("staged node pack: %+v", failure)
	}
}
