package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
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
