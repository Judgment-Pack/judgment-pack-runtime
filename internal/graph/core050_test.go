package graph

import (
	"os"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// Graphs do not read Core 0.5.0: a node pack declaring it is refused at
// contract selection, before any node runs, with the surface refusal code and
// a message naming the surface and the version.
func TestCore050NodePackIsRefused(t *testing.T) {
	raw, err := os.ReadFile("../artifacts/jps/0.3.0/cases/valid/minimal-literal.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, failure := selectNodeContract(raw, nil); failure != nil {
		t.Fatalf("released pack refused: %+v", failure)
	}
	pack, err := os.ReadFile("../artifacts/jps/0.5.0/cases/valid/minimal-literal.json")
	if err != nil {
		t.Fatal(err)
	}
	_, failure := selectNodeContract(pack, nil)
	if failure == nil || failure.Code != "JPS-SURFACE-SPEC-VERSION-UNSUPPORTED" || failure.ExitCode != result.ExitUnsupported || failure.Message != `Graph evaluation does not read packs declaring specVersion "0.5.0"; it reads Core 0.2.0-draft and 0.3.0 packs.` {
		t.Fatalf("0.5.0 node pack: %+v", failure)
	}
}
