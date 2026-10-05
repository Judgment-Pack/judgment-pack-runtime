package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// witnessVectorSource is the gateway commit testdata/witness/ was copied from
// (gateway ADR-0013, PR 1 of 6). witness.lock.json names it too.
const witnessVectorSource = "c916ee933dff0b72ebf7741c1ce8022487bfb807"

// witnessVectorCount is how many vectors the gateway's corpus/README.md states
// for corpus/witness/ at that commit.
const witnessVectorCount = 54

// witnessLock is testdata/witness.lock.json: where the vectors came from, and
// each file's size and SHA-256.
type witnessLock struct {
	FormatVersion int `json:"formatVersion"`
	Source        struct {
		Repository string `json:"repository"`
		Kind       string `json:"kind"`
		BaseCommit string `json:"baseCommit"`
		Path       string `json:"path"`
	} `json:"source"`
	Files []struct {
		Path   string `json:"path"`
		Bytes  int64  `json:"bytes"`
		SHA256 string `json:"sha256"`
	} `json:"files"`
}

// The vectors are the gateway's frozen data, copied verbatim: the directory
// holds exactly the files the lock names, each of the size and digest it
// states, and the lock names the gateway commit they came from. A file added,
// removed or edited without the lock fails here.
func TestTheWitnessVectorsAreTheGatewaysCopy(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "witness.lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	var lock witnessLock
	if err := json.Unmarshal(raw, &lock); err != nil {
		t.Fatal(err)
	}
	if lock.FormatVersion != 1 || lock.Source.Repository != "https://github.com/Judgment-Pack/judgment-pack-gateway" ||
		lock.Source.Kind != "immutable-git-ref" || lock.Source.BaseCommit != witnessVectorSource || lock.Source.Path != "corpus/witness" {
		t.Fatalf("the lock names another source: %+v", lock.Source)
	}
	entries, err := os.ReadDir(filepath.Join("testdata", "witness"))
	if err != nil {
		t.Fatal(err)
	}
	held := []string{}
	for _, entry := range entries {
		held = append(held, entry.Name())
	}
	sort.Strings(held)
	if len(held) != len(lock.Files) || len(held) != witnessVectorCount {
		t.Fatalf("the directory holds %d files and the lock names %d; the gateway states %d", len(held), len(lock.Files), witnessVectorCount)
	}
	for index, file := range lock.Files {
		if held[index] != file.Path {
			t.Fatalf("file %d is %q, and the lock names %q", index, held[index], file.Path)
		}
		data, err := os.ReadFile(filepath.Join("testdata", "witness", file.Path))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		if int64(len(data)) != file.Bytes || hex.EncodeToString(sum[:]) != file.SHA256 {
			t.Fatalf("%s is %d bytes with digest %x, and the lock states %d bytes and %s", file.Path, len(data), sum, file.Bytes, file.SHA256)
		}
	}
}
