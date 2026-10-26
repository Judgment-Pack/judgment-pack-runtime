package dialect

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/retainedcolumn"
)

// A reviewed import goes below retained-column/official and requires an
// independent provenance pin and exact supported tuple in this file. An empty
// pin means no RC2 installation, not a placeholder RC2 capability.
//
//go:embed retained-column
var retainedColumnArtifacts embed.FS

const retainedColumnProvenancePin = ""

var retainedColumnInstalledIdentity = retainedcolumn.Identity{}

// VerifyRetainedColumnArtifacts binds installed resources to the identity the
// selected CLI/MCP reader actually reports, independently of caller agreement.
func VerifyRetainedColumnArtifacts() error {
	return verifyRetainedColumnArtifacts(retainedColumnArtifacts, retainedColumnProvenancePin, retainedColumnInstalledIdentity, RetainedColumnDraftBase().Spec)
}
func verifyRetainedColumnArtifacts(source fs.FS, pin string, installed retainedcolumn.Identity, selected RetainedColumnDraftIdentity) error {
	if pin == "" {
		if installed != (retainedcolumn.Identity{}) || selected != (RetainedColumnDraftIdentity{"Draft RFC0029", "ade3914b0b81ef299e5b9a8d6147f37ff3f0976c", "sha256:996cec17473987eebf1aa553536df0ce5a740971532567f411282ee70036879f", "sha256:e2ac53f56cd5ab779e032d2847b49fa04438ffb9df57d456f75f2ba058dc12d2"}) {
			return fmt.Errorf("retained-column selected identity requires a reviewed immutable installation")
		}
		return fs.WalkDir(source, "retained-column", func(name string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if name != "retained-column" && name != "retained-column/README.md" {
				return fmt.Errorf("retained-column resources installed without an independent provenance pin: %s", name)
			}
			return nil
		})
	}
	actual := retainedcolumn.Identity{Version: selected.Version, Revision: selected.Revision, SchemaSHA256: selected.SchemaSHA256, CorpusSHA256: selected.CorpusSHA256}
	if actual != installed {
		return fmt.Errorf("retained-column installed and selected identities differ")
	}
	if err := fs.WalkDir(source, "retained-column", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if name == "retained-column" || name == "retained-column/README.md" || name == "retained-column/official" || strings.HasPrefix(name, "retained-column/official/") {
			return nil
		}
		return fmt.Errorf("unmanifested retained-column installation entry: %s", name)
	}); err != nil {
		return err
	}
	official, err := fs.Sub(source, "retained-column/official")
	if err != nil {
		return err
	}
	return retainedcolumn.Verify(official, pin, installed)
}
