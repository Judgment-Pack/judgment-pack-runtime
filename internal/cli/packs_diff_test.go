package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/sdk"
)

func loadForDiff(t *testing.T, text string) *sdk.Pack {
	t.Helper()
	loaded, err := sdk.Load(context.Background(), []byte(text), sdk.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

func decodeObject(t *testing.T, text string) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		t.Fatalf("%v: %q", err, text)
	}
	return value
}

// packs diff writes the bytes sdk.Diff and sdk.ThreeWayDiff return: the
// canonical JSON as a string in the envelope's diff or threeWayDiff member,
// and the text as the human output. It exits 0 however much differs.
func TestPacksDiffWritesTheGoAPIsBytes(t *testing.T) {
	baseText := annotatedExpensePack("1.0.0", "medium", "5000")
	firstText := annotatedExpensePack("1.1.0", "high", "4000")
	secondText := annotatedExpensePack("1.1.0", "low", "5000")
	base, first, second := writeDocument(t, "base.json", baseText), writeDocument(t, "first.json", firstText), writeDocument(t, "second.json", secondText)
	two, err := sdk.Diff(loadForDiff(t, baseText), loadForDiff(t, firstText))
	if err != nil {
		t.Fatal(err)
	}
	three, err := sdk.ThreeWayDiff(loadForDiff(t, baseText), loadForDiff(t, firstText), loadForDiff(t, secondText))
	if err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runTest(t, []string{"packs", "diff", base, first, "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	envelope := decodeObject(t, stdout)
	if envelope["command"] != "packs diff" || envelope["status"] != "compared" || envelope["diff"] != string(two.JSON()) || len(envelope) != 5 {
		t.Fatalf("envelope %v\nwant diff %s", envelope, two.JSON())
	}
	code, stdout, stderr = runTest(t, []string{"packs", "diff", base, first}, "")
	if code != 0 || stderr != "" || stdout != string(two.Text()) {
		t.Fatalf("exit=%d stderr=%q\n%s", code, stderr, stdout)
	}
	code, stdout, stderr = runTest(t, []string{"packs", "diff", "--base", base, first, second, "--format", "json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	envelope = decodeObject(t, stdout)
	if envelope["threeWayDiff"] != string(three.JSON()) || envelope["status"] != "compared" || len(envelope) != 5 {
		t.Fatalf("envelope %v", envelope)
	}
	code, stdout, _ = runTest(t, []string{"packs", "diff", "--base", base, first, second}, "")
	if code != 0 || stdout != string(three.Text()) || three.Conflicts != 1 {
		t.Fatalf("exit=%d conflicts=%d\n%s", code, three.Conflicts, stdout)
	}
	// The annotation and the threshold are reported, by id, old and new.
	for _, line := range []string{`  "decline" changed:`, `    annotation "severity" changed: "medium" -> "high"`, `  "over-limit" changed:`, `    /when/value changed: "5000" -> "4000"`} {
		if !strings.Contains(string(two.Text()), line+"\n") {
			t.Fatalf("missing %q:\n%s", line, two.Text())
		}
	}
}

// packs diff refuses what it cannot compare, with a code and nothing
// compared: standard input, a URL, a count of packs other than two, a pack
// the loader refuses (with the loader's code, naming which pack), and packs
// of two specVersions.
func TestPacksDiffRefusals(t *testing.T) {
	good := writeDocument(t, "good.json", annotatedExpensePack("1.0.0", "medium", "5000"))
	refused := writeDocument(t, "refused.json", strings.Replace(annotatedExpensePack("1.0.0", "medium", "5000"), `"decision":{"intent":"Approve ordinary expenses without review.","question":"May this expense be approved?"},`, "", 1))
	older := writeDocument(t, "older.json", strings.Replace(strings.Replace(annotatedExpensePack("1.0.0", "medium", "5000"), `"0.5.0"`, `"0.3.0"`, 1), `"extensions":{"org.judgmentpack.annotations":{"severity":"medium"}}`, `"extensions":{"com.example.review":{"severity":"medium"}}`, 1))
	for _, row := range []struct {
		name, code, message string
		args                []string
		exit                int
	}{
		{"stdin", "JPS-INVOCATION-STDIN", "standard input", []string{good, "-"}, 3},
		{"url", "JPS-INVOCATION-INPUT", "URL", []string{good, "https://example.invalid/pack.json"}, 3},
		{"one pack", "JPS-INVOCATION-DIFF", "Two packs are required", []string{good}, 3},
		{"three packs", "JPS-INVOCATION-DIFF", "Two packs are required", []string{good, good, good}, 3},
		{"refused new", "JPS-EVALUATION-PACK-NOT-CONFORMANT", "The new pack was refused, so nothing was compared.", []string{good, refused}, 1},
		{"refused base", "JPS-EVALUATION-PACK-NOT-CONFORMANT", "The base pack was refused, so nothing was compared.", []string{good, good, "--base", refused}, 1},
		{"spec versions", "JPS-DIFF-MIXED-SPEC-VERSION", `"0.3.0", "0.5.0"`, []string{older, good}, 2},
		{"spec versions, three-way", "JPS-DIFF-MIXED-SPEC-VERSION", `"0.5.0", "0.5.0", "0.3.0"`, []string{good, older, "--base", good}, 2},
	} {
		t.Run(row.name, func(t *testing.T) {
			code, stdout, stderr := runTest(t, append([]string{"packs", "diff", "--format", "json"}, row.args...), "")
			if code != row.exit || stderr != "" {
				t.Fatalf("exit=%d stderr=%q stdout=%s", code, stderr, stdout)
			}
			envelope := decodeObject(t, stdout)
			diagnostics, _ := envelope["diagnostics"].([]any)
			if len(diagnostics) != 1 {
				t.Fatalf("%v", envelope)
			}
			diagnostic := diagnostics[0].(map[string]any)
			if diagnostic["code"] != row.code || !strings.Contains(diagnostic["message"].(string), row.message) {
				t.Fatalf("%v", diagnostic)
			}
			if _, present := envelope["diff"]; present {
				t.Fatalf("a refusal carries a diff: %v", envelope)
			}
		})
	}
}

// experimental compare --structural carries the structural comparison beside
// the behavioural one: the payload is the one compare writes without it, and
// a structural member holding sdk.Diff's bytes; the human report is followed
// by the comparison's text. On two packs that differ only in an annotation
// and a threshold no input falls between, the behavioural part finds no
// difference and the structural part names both.
func TestCompareStructuralAddsTheDiff(t *testing.T) {
	oldText, newText := annotatedExpensePack("1.0.0", "medium", "5000"), annotatedExpensePack("1.1.0", "high", "4000")
	oldPack, newPack := writeDocument(t, "old.json", oldText), writeDocument(t, "new.json", newText)
	matrix := writeDocument(t, "matrix.json", annotatedExpenseMatrix)
	compared, err := sdk.Diff(loadForDiff(t, oldText), loadForDiff(t, newText))
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"experimental", "compare", oldPack, newPack, "--inputs", matrix}
	_, plain, _ := runTest(t, append(args, "--format", "json"), "")
	code, structural, stderr := runTest(t, append(args, "--format", "json", "--structural"), "")
	if code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	without, with := decodeObject(t, plain), decodeObject(t, structural)
	if with["structural"] != string(compared.JSON()) {
		t.Fatalf("structural member %v\nwant %s", with["structural"], compared.JSON())
	}
	if _, present := without["structural"]; present {
		t.Fatal("compare without --structural writes a structural member")
	}
	delete(with, "structural")
	if a, b := encodedText(t, without), encodedText(t, with); a != b {
		t.Fatalf("the behavioural part changed:\n%s\n%s", a, b)
	}
	if inputs := without["inputs"].(map[string]any); inputs["different"] != float64(0) {
		t.Fatalf("the premise changed: %v", inputs)
	}
	_, plainHuman, _ := runTest(t, args, "")
	code, structuralHuman, _ := runTest(t, append(args, "--structural"), "")
	if code != 0 || structuralHuman != plainHuman+string(compared.Text()) {
		t.Fatalf("exit=%d\n%s", code, structuralHuman)
	}
}

func encodedText(t *testing.T, value any) string {
	t.Helper()
	text, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(text)
}

// With --structural both packs must load: a pack the loader refuses, whose
// evaluations compare refuses input by input and still reports, makes the
// structural run refused with the loader's code rather than written without
// its structural part.
func TestCompareStructuralRefusesAPackTheLoaderRefuses(t *testing.T) {
	good := writeDocument(t, "good.json", annotatedExpensePack("1.0.0", "medium", "5000"))
	refused := writeDocument(t, "refused.json", strings.Replace(annotatedExpensePack("1.1.0", "high", "4000"), `"title":"Expense approval",`, "", 1))
	matrix := writeDocument(t, "matrix.json", annotatedExpenseMatrix)
	args := []string{"experimental", "compare", good, refused, "--inputs", matrix, "--format", "json"}
	if code, stdout, _ := runTest(t, args, ""); code != 0 || !strings.Contains(stdout, `"differences":[{`) {
		t.Fatalf("without --structural: exit=%d %s", code, stdout)
	}
	code, stdout, stderr := runTest(t, append(args, "--structural"), "")
	if code != 1 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q %s", code, stderr, stdout)
	}
	envelope := decodeObject(t, stdout)
	diagnostic := envelope["diagnostics"].([]any)[0].(map[string]any)
	if diagnostic["code"] != "JPS-EVALUATION-PACK-NOT-CONFORMANT" || !strings.Contains(diagnostic["message"].(string), "The new pack was refused, so nothing was compared.") {
		t.Fatalf("%v", diagnostic)
	}
	if _, present := envelope["inputs"]; present {
		t.Fatalf("a refused run reports the behavioural part: %v", envelope)
	}
	// A pack over the byte limit, which compare evaluates as refused, has no
	// structural comparison either.
	oversized := writeDocument(t, "oversized.json", strings.Replace(annotatedExpensePack("1.1.0", "high", "4000"), `"title":"Expense approval",`, `"title":"Expense approval","x-padding":"`+strings.Repeat("a", 11<<20)+`",`, 1))
	code, stdout, _ = runTest(t, []string{"experimental", "compare", good, oversized, "--inputs", matrix, "--format", "json", "--structural"}, "")
	if code != 4 || !strings.Contains(stdout, `"code":"JPS-RESOURCE-INPUT-BYTE-LIMIT"`) || !strings.Contains(stdout, "The new pack exceeds") {
		t.Fatalf("oversized: exit=%d %s", code, first(stdout, 400))
	}
	code, stdout, _ = runTest(t, []string{"packs", "diff", oversized, good, "--format", "json"}, "")
	if code != 4 || !strings.Contains(stdout, `"code":"JPS-RESOURCE-INPUT-BYTE-LIMIT"`) || !strings.Contains(stdout, "The old pack exceeds") {
		t.Fatalf("packs diff, oversized: exit=%d %s", code, first(stdout, 400))
	}
}

// compare's help says that annotations are outside what it compares and
// names the option that adds the structural comparison.
func TestCompareHelpSaysAnnotationsAreOutside(t *testing.T) {
	_, help, _ := runTest(t, []string{"experimental", "compare", "--help"}, "")
	text := strings.Join(strings.Fields(help), " ")
	for _, phrase := range []string{"annotations, and every other member that never reaches a disposition, are outside what it compares", "--structural adds the structural comparison packs diff writes"} {
		if !strings.Contains(text, phrase) {
			t.Fatalf("missing %q in %s", phrase, text)
		}
	}
}
