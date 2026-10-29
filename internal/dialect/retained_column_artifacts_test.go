package dialect

import (
	"io/fs"
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
	selected := RetainedColumnDraftIdentity{"Draft RFC0029", "ade3914b0b81ef299e5b9a8d6147f37ff3f0976c", "sha256:996cec17473987eebf1aa553536df0ce5a740971532567f411282ee70036879f", "sha256:e2ac53f56cd5ab779e032d2847b49fa04438ffb9df57d456f75f2ba058dc12d2"}
	if err := verifyRetainedColumnArtifacts(files, "", retainedcolumn.Identity{}, selected); err != nil {
		t.Fatal(err)
	}
	files["retained-column/official/provenance.json"] = &fstest.MapFile{Data: []byte("{}")}
	if err := verifyRetainedColumnArtifacts(files, "", retainedcolumn.Identity{}, selected); err == nil {
		t.Fatal("unpinned resources accepted")
	}
	delete(files, "retained-column/official/provenance.json")
	selected.Version = "0.3.0"
	if err := verifyRetainedColumnArtifacts(files, "", retainedcolumn.Identity{}, selected); err == nil {
		t.Fatal("uninstalled selected release identity accepted")
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

func TestRetainedColumnAdmissionRequiresInstalledIdentity(t *testing.T) {
	original := retainedColumnInstalledIdentity
	retainedColumnInstalledIdentity.Revision = strings.Repeat("0", 40)
	defer func() { retainedColumnInstalledIdentity = original }()
	for _, raw := range [][]byte{nil, []byte("{"), []byte(`{"contractVersion":"1","origin":"asserted","values":{},"research":null}`)} {
		r, code := ValidateRetainedColumnDraft("test", raw, RetainedColumnDraftBase())
		if code == 0 || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "capability-unsupported" || r.Document != "" {
			t.Fatal("document admitted without installed identity", r, code)
		}
		frame, code := ValidateRetainedColumnDraftFrame("test", raw, RetainedColumnDraftBase())
		if code == 0 || len(frame.Diagnostics) != 1 || frame.Diagnostics[0].Code != "capability-unsupported" || frame.Frame != nil {
			t.Fatal("frame admitted without installed identity", frame, code)
		}
		encoded, code := EncodeRetainedColumnDraft("test", raw, RetainedColumnDraftBase())
		if code == 0 || len(encoded.Diagnostics) != 1 || encoded.Diagnostics[0].Code != "capability-unsupported" {
			t.Fatal("encoding admitted without installed identity", encoded, code)
		}
	}
	r, _, code := SelectRetainedColumnDraft("test", RetainedColumnDraftSelector, "base")
	if code == 0 || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "capability-unsupported" {
		t.Fatal("selector admitted without installed identity", r, code)
	}
}

func TestRetainedColumnReleaseInstalledInventoryRefusals(t *testing.T) {
	files := fstest.MapFS{}
	if err := fs.WalkDir(retainedColumnArtifacts, "retained-column", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		raw, err := fs.ReadFile(retainedColumnArtifacts, path)
		if err != nil {
			return err
		}
		files[path] = &fstest.MapFile{Data: raw}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"valid", "missing", "changed", "extra", "outside inventory", "pin", "unsupported", "candidate"} {
		t.Run(name, func(t *testing.T) {
			copy := fstest.MapFS{}
			for path, file := range files {
				copy[path] = &fstest.MapFile{Data: append([]byte(nil), file.Data...)}
			}
			pin := retainedColumnProvenancePin
			installed := retainedColumnInstalledIdentity
			selected := RetainedColumnDraftBase().Spec
			switch name {
			case "missing":
				delete(copy, "retained-column/official/"+retainedcolumn.SchemaPath)
			case "changed":
				copy["retained-column/official/"+retainedcolumn.CorpusPath].Data = append(copy["retained-column/official/"+retainedcolumn.CorpusPath].Data, ' ')
			case "extra":
				copy["retained-column/official/extra"] = &fstest.MapFile{Data: []byte("extra")}
			case "outside inventory":
				copy["retained-column/extra"] = &fstest.MapFile{Data: []byte("extra")}
			case "pin":
				pin = "sha256:" + strings.Repeat("0", 64)
			case "unsupported":
				installed.Version = "0.3.0-rc.1"
				selected.Version = installed.Version
			case "candidate":
				// The v0.3.0-rc.2 tuple, selected and installed alike, is refused
				// against the final release's installed bytes.
				installed.Version, installed.Revision = "0.3.0-rc.2", "bcac15b7537ce21879201b5e5dfe795496225121"
				selected.Version, selected.Revision = installed.Version, installed.Revision
			}
			err := verifyRetainedColumnArtifacts(copy, pin, installed, selected)
			if (err == nil) != (name == "valid") {
				t.Fatal("installed inventory result", name, err)
			}
		})
	}
}
