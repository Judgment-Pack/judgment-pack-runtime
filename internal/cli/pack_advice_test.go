package cli

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/packadvice"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

func TestPackAdviceCLIParityAndRefusal(t *testing.T) {
	pack, err := os.ReadFile("../validation/testdata/rules-overlap-pack.json")
	if err != nil {
		t.Fatal(err)
	}
	domain := []byte(`{"kind":"finite-cases","inputs":[{"facts":{"score":"80"},"evidence":{}}]}`)
	dir := t.TempDir()
	packPath := filepath.Join(dir, "pack.json")
	domainPath := filepath.Join(dir, "domain.json")
	if err := os.WriteFile(packPath, pack, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(domainPath, domain, 0600); err != nil {
		t.Fatal(err)
	}
	v, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	want, f := packadvice.Analyze(evaluation.NewEngine(v), pack, domain, packadvice.DefaultOptions())
	if f != nil {
		t.Fatal(f)
	}
	wantBytes, _ := json.Marshal(want)
	for _, args := range [][]string{{"experimental", "analyze-pack", packPath, "--domain", domainPath}, {"--pretty", "experimental", "analyze-pack", packPath, "--domain", domainPath}, {"experimental", "analyze-pack", "-", "--domain", domainPath}} {
		code, out, stderr := runTest(t, args, string(pack))
		if code != 0 || stderr != "" || out != string(wantBytes)+"\n" {
			t.Fatal(code, out, stderr)
		}
	}
	for _, row := range []struct {
		args   []string
		domain string
		code   string
	}{
		{[]string{"--max-work", "0"}, string(domain), "analysis-limit"},
		{nil, `{}`, "domain-invalid"},
		{nil, string(make([]byte, packadvice.MaxBytes+1)), "domain-limit"},
	} {
		if err := os.WriteFile(domainPath, []byte(row.domain), 0600); err != nil {
			t.Fatal(err)
		}
		args := append([]string{"experimental", "analyze-pack", packPath, "--domain", domainPath}, row.args...)
		code, out, _ := runTest(t, args, "")
		var f packadvice.Refusal
		if json.Unmarshal([]byte(out), &f) != nil || code != 1 || f.Code != row.code || f.Status != "unavailable" {
			t.Fatal(code, out)
		}
	}
}

// The 0.3.0 golden is the stdout of `experimental analyze-pack` from a build of
// main at 621976e, the commit before Core 0.5.0 packs were analysed, over the
// hand-written twin pack and domain in ../packadvice/testdata/core050; it pins
// that a Core 0.3.0 report is byte-identical to the one before the change. The
// 0.5.0 pack's report is the same bytes with the two members that name the pack
// -- its own selector and the digest of its exact bytes -- and nothing else.
func TestPackAdviceCLICore030GoldenAndCore050EvaluationForm(t *testing.T) {
	dir := "../packadvice/testdata/core050/"
	golden, err := os.ReadFile("testdata/analyze_pack_core030.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	p030, err := os.ReadFile(dir + "pack-0.3.0.json")
	if err != nil {
		t.Fatal(err)
	}
	p050, err := os.ReadFile(dir + "pack-0.5.0.json")
	if err != nil {
		t.Fatal(err)
	}
	code, out, stderr := runTest(t, []string{"experimental", "analyze-pack", dir + "pack-0.3.0.json", "--domain", dir + "domain.json"}, "")
	if code != 0 || stderr != "" || out != string(golden) {
		t.Fatalf("Core 0.3.0 report changed: %d %q\n got %s\nwant %s", code, stderr, out, golden)
	}
	digestOf := func(b []byte) string { return fmt.Sprintf("sha256:%x", sha256.Sum256(b)) }
	want := strings.Replace(string(golden), `"packDigest":"`+digestOf(p030)+`"`, `"packDigest":"`+digestOf(p050)+`"`, 1)
	want = strings.Replace(want, `"targetSpecVersion":"0.3.0"`, `"targetSpecVersion":"0.5.0"`, 1)
	if want == string(golden) || strings.Count(want, digestOf(p050)) != 1 {
		t.Fatal("the golden does not name the twin's digest and selector once each")
	}
	code, out, stderr = runTest(t, []string{"experimental", "analyze-pack", "-", "--domain", dir + "domain.json"}, string(p050))
	if code != 0 || stderr != "" || out != want {
		t.Fatalf("Core 0.5.0 report: %d %q\n got %s\nwant %s", code, stderr, out, want)
	}
	unresolved := strings.Replace(string(p050), `"valueSet": "regions"`, `"valueSet": "missing"`, 1)
	code, out, _ = runTest(t, []string{"experimental", "analyze-pack", "-", "--domain", dir + "domain.json"}, unresolved)
	var refusal packadvice.Refusal
	if json.Unmarshal([]byte(out), &refusal) != nil || code != 1 || refusal.Code != "pack-unsupported" || !strings.Contains(refusal.Message, "first diagnostic JPS-SEMANTIC-UNRESOLVED-VALUE-SET") || strings.Contains(out, "findings") {
		t.Fatal(code, out)
	}
}
