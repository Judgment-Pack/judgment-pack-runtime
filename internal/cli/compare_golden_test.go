package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// annotatedExpensePack is a Core 0.5.0 pack whose decline outcome carries a
// severity annotation and whose one rule declines an amount over limit. The
// two versions below differ in the version, that annotation's value and the
// limit, and in nothing else.
func annotatedExpensePack(version, severity, limit string) string {
	return `{"specVersion":"0.5.0","id":"https://example.invalid/judgment-packs/expense-approval","version":"` + version + `",` +
		`"title":"Expense approval","decision":{"intent":"Approve ordinary expenses without review.","question":"May this expense be approved?"},` +
		`"outcomes":[{"id":"approve","label":"Approve"},{"id":"decline","label":"Decline","extensions":{"org.judgmentpack.annotations":{"severity":"` + severity + `"}}}],` +
		`"rules":[{"id":"over-limit","description":"An amount over the limit is declined.","when":{"op":"fact","path":"/expense/amount","operator":"greater-than","value":"` + limit + `"},"outcome":"decline","onUnknown":"ignore"}],` +
		`"fallbackOutcome":"approve"}`
}

// annotatedExpenseMatrix holds one input on each side of both limits and none
// between them.
const annotatedExpenseMatrix = `{"matrixVersion":"1","cases":[
  {"id":"small","facts":{"expense":{"amount":"100"}},"expectedDisposition":{"kind":"outcome","outcomeId":"approve","reasons":[],"handoff":{"state":"none"}}},
  {"id":"large","facts":{"expense":{"amount":"6000"}},"expectedDisposition":{"kind":"outcome","outcomeId":"decline","reasons":[],"handoff":{"state":"none"}}}
]}`

// experimental compare without --structural writes exactly the bytes it wrote
// before the structural comparison existed, in both formats. The expected
// output is written by hand in testdata; the two packs differ only in an
// annotation, a limit no input falls between and the version, so the
// behavioural comparison sees no difference at all. The digests in the files
// are the SHA-256 of the two pack texts above, computed outside the runtime.
func TestCompareWithoutTheStructuralOptionKeepsItsBytes(t *testing.T) {
	oldPack := writeDocument(t, "old.json", annotatedExpensePack("1.0.0", "medium", "5000"))
	newPack := writeDocument(t, "new.json", annotatedExpensePack("1.1.0", "high", "4000"))
	matrix := writeDocument(t, "matrix.json", annotatedExpenseMatrix)
	for format, golden := range map[string]string{"json": "compare_annotation_only.golden.json", "human": "compare_annotation_only.golden.txt"} {
		want, err := os.ReadFile(filepath.Join("testdata", golden))
		if err != nil {
			t.Fatal(err)
		}
		want = []byte(strings.ReplaceAll(string(want), "\r\n", "\n"))
		code, stdout, stderr := runTest(t, []string{"experimental", "compare", oldPack, newPack, "--inputs", matrix, "--format", format}, "")
		got := stdout
		for path, shown := range map[string]string{oldPack: "old.json", newPack: "new.json"} {
			spelled := path
			if format == "json" {
				// The path appears JSON-escaped (backslashes doubled on Windows).
				encoded, err := json.Marshal(path)
				if err != nil {
					t.Fatal(err)
				}
				spelled = string(encoded[1 : len(encoded)-1])
			}
			got = strings.ReplaceAll(got, spelled, shown)
		}
		if code != 0 || stderr != "" || got != string(want) {
			t.Fatalf("%s: exit=%d stderr=%q\ngot:  %q\nwant: %q", format, code, stderr, got, want)
		}
	}
}
