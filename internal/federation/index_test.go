package federation

import (
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// An index is admitted at 8 MiB and refused one byte over, by the check.
func TestIndexDocumentBound(t *testing.T) {
	in := twoSources(t)
	index := testdataFile(t, "assembled/two-sources/index.json")
	report, err := CheckIndex(in, registry.Document{Data: padded(t, index, MaxIndexBytes)})
	if err != nil || !report.Valid {
		t.Fatalf("an index at its bound: %v %q", err, found(report.Diagnostics))
	}
	for _, over := range []registry.Document{{Data: padded(t, index, MaxIndexBytes+1)}, {Data: index[:1], Oversize: true}} {
		report, err := CheckIndex(in, over)
		if err != nil {
			t.Fatal(err)
		}
		expectFound(t, found(report.Diagnostics), "REG-DOC-SIZE index")
		if report.Valid {
			t.Error("an index over its bound was checked")
		}
	}
}
