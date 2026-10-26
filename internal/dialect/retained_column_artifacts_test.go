package dialect

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/retainedcolumn"
)

func TestRetainedColumnInstallationGate(t *testing.T) {
	if err := VerifyRetainedColumnArtifacts(); err != nil {
		t.Fatal(err)
	}
	files := fstest.MapFS{"retained-column/README.md": &fstest.MapFile{Data: []byte("uninstalled test state")}}
	selected := RetainedColumnDraftBase().Spec
	if err := verifyRetainedColumnArtifacts(files, "", retainedcolumn.Identity{}, selected); err != nil {
		t.Fatal(err)
	}
	files["retained-column/official/provenance.json"] = &fstest.MapFile{Data: []byte("{}")}
	if err := verifyRetainedColumnArtifacts(files, "", retainedcolumn.Identity{}, selected); err == nil {
		t.Fatal("unpinned resources accepted")
	}
	delete(files, "retained-column/official/provenance.json")
	selected.Version = "0.3.0-rc.2"
	if err := verifyRetainedColumnArtifacts(files, "", retainedcolumn.Identity{}, selected); err == nil {
		t.Fatal("uninstalled selected RC2 identity accepted")
	}
	installed := retainedcolumn.Identity{Version: selected.Version, Revision: selected.Revision, SchemaSHA256: selected.SchemaSHA256, CorpusSHA256: selected.CorpusSHA256}
	selected.Revision = strings.Repeat("2", 40)
	if err := verifyRetainedColumnArtifacts(files, "sha256:"+strings.Repeat("3", 64), installed, selected); err == nil {
		t.Fatal("installed and selected mismatch accepted")
	}
	selected.Revision = installed.Revision
	if err := verifyRetainedColumnArtifacts(files, "sha256:"+strings.Repeat("3", 64), installed, selected); err == nil {
		t.Fatal("missing pinned installation accepted")
	}
}
