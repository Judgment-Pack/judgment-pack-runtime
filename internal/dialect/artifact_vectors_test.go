package dialect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNamedArtifactSpecVectors(t *testing.T) {
	var manifest struct {
		Cases []struct{ Name, DialectDigest string }
	}
	raw, e := os.ReadFile("testdata/artifact-identity/manifest.json")
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(raw, &manifest); e != nil {
		t.Fatal(e)
	}
	for _, entry := range manifest.Cases {
		t.Run(entry.Name, func(t *testing.T) {
			raw, e := os.ReadFile(filepath.Join("testdata/artifact-identity", entry.Name, "dialect.json"))
			if e != nil {
				t.Fatal(e)
			}
			expected, e := os.ReadFile(filepath.Join("testdata/artifact-identity", entry.Name, "dialect.canonical.json"))
			if e != nil {
				t.Fatal(e)
			}
			r, code := Validate("test", raw)
			if code != 0 || r.Document != string(expected) || r.Digest != entry.DialectDigest {
				t.Fatalf("%d %+v", code, r)
			}
		})
	}
}
