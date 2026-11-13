package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

// packs diff reports a change to an outcome's severity alone as it reports a
// change to any annotation: by the outcome's id, the key, and the old and new
// values as written, with nothing else changed and nothing said about levels
// or the vocabulary (ADR-0088 gives the diff no case of its own). The rows are
// a level lowered, and a level turned into a value that is not one. The
// expected bytes are written by hand; the digests are the SHA-256 of the two
// pack texts, computed here.
func TestPacksDiffReportsASeverityOnlyChangeAsAnAnnotation(t *testing.T) {
	for _, row := range []struct{ name, old, new string }{{"lowered", "critical", "low"}, {"not a level", "high", "High"}} {
		t.Run(row.name, func(t *testing.T) {
			olderText, newerText := annotatedExpensePack("1.0.0", row.old, "5000"), annotatedExpensePack("1.0.0", row.new, "5000")
			older, newer := writeDocument(t, "older.json", olderText), writeDocument(t, "newer.json", newerText)
			identity := func(label, text string) string {
				sum := sha256.Sum256([]byte(text))
				return label + `: id "https://example.invalid/judgment-packs/expense-approval" version "1.0.0" specVersion "0.5.0" digest sha256:` + hex.EncodeToString(sum[:]) + "\n"
			}
			want := "STRUCTURAL COMPARISON (diff version 1): what the two documents say differently, member by member; it says nothing about how they decide any input\n" +
				identity("old", olderText) + identity("new", newerText) +
				"outcomes:\n" +
				`  "decline" changed:` + "\n" +
				`    annotation "severity" changed: "` + row.old + `" -> "` + row.new + `"` + "\n"
			code, stdout, stderr := runTest(t, []string{"packs", "diff", older, newer}, "")
			if code != 0 || stderr != "" || stdout != want {
				t.Fatalf("exit=%d stderr=%q\n got %q\nwant %q", code, stderr, stdout, want)
			}

			code, stdout, stderr = runTest(t, []string{"packs", "diff", older, newer, "--format", "json"}, "")
			if code != 0 || stderr != "" {
				t.Fatalf("exit=%d stderr=%q", code, stderr)
			}
			var diff map[string]json.RawMessage
			if err := json.Unmarshal([]byte(decodeObject(t, stdout)["diff"].(string)), &diff); err != nil {
				t.Fatal(err)
			}
			wantOutcomes := `{"changes":[{"annotations":[{"change":"changed","key":"severity","new":"` + row.new + `","old":"` + row.old + `"}],"change":"changed","id":"decline","members":[]}]}`
			if got := string(diff["outcomes"]); got != wantOutcomes {
				t.Fatalf("outcomes\n got %s\nwant %s", got, wantOutcomes)
			}
			for name, empty := range map[string]string{"rules": `{"changes":[]}`, "pack": `{"annotations":[],"members":[]}`, "differs": `true`} {
				if got := string(diff[name]); got != empty {
					t.Fatalf("%s: %s", name, got)
				}
			}
		})
	}
}
