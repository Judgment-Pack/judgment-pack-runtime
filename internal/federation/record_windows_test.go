//go:build windows

package federation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/junctiontest"
)

// A record directory that is a junction is refused as one that is a link
// is, when read and when written, and nothing is written through it.
func TestRecordRefusesADirectoryThroughAJunction(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(dir, "records")
	junctiontest.Make(t, junction, real)
	store := NewRecordStore(junction)
	if _, problem := store.read(recordID); problem == nil || !strings.Contains(problem.Reason, "link") {
		t.Fatalf("%v", problem)
	}
	if _, problem := store.record(recordID, nil, 1, digestA, one()); problem == nil || !strings.Contains(problem.Reason, "link") {
		t.Fatalf("written through a junction: %v", problem)
	}
	if entries, _ := os.ReadDir(real); len(entries) != 0 {
		t.Fatalf("%v", entries)
	}
}
