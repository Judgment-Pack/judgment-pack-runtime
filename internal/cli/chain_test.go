package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// trailBytes reads a project's trail as the lines it holds, each exactly,
// without its newline.
func trailBytes(t *testing.T, configPath string) [][]byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(filepath.Dir(configPath), "audit", audit.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(data, []byte("\n")) {
		t.Fatalf("every record ends its own line: %q", data)
	}
	return bytes.Split(bytes.TrimSuffix(data, []byte("\n")), []byte("\n"))
}

// checkChained holds a trail written from empty to the chain: one identity,
// each line's sequence its line number, and each previous the digest of the
// line before it as it is in the file.
func checkChained(t *testing.T, lines [][]byte) {
	t.Helper()
	trail := ""
	for index, line := range lines {
		var link struct {
			Trail    string `json:"trail"`
			Sequence int64  `json:"sequence"`
			Previous string `json:"previous"`
		}
		if err := json.Unmarshal(line, &link); err != nil {
			t.Fatal(err)
		}
		want := audit.Digest(nil)
		if index > 0 {
			want = audit.Digest(lines[index-1])
		} else {
			trail = link.Trail
		}
		if len(link.Trail) != 32 || link.Trail != trail || link.Sequence != int64(index+1) || link.Previous != want {
			t.Fatalf("line %d is not chained to the line before it: %s", index+1, line)
		}
	}
}

// A project that keeps a trail has it chained, under the configVersion it
// already declares; a project under "6" that says chain false has its records
// written as they always were.
func TestEvaluateChainsTheTrailUnlessTheProjectSaysNot(t *testing.T) {
	configPath := auditProject(t)
	facts := writeDocument(t, "facts.json", hardFailFacts)
	for range 2 {
		if code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake",
			"--config", configPath, "--facts", facts}, ""); code != 0 || stderr != "" {
			t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
		}
	}
	lines := trailBytes(t, configPath)
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	checkChained(t, lines)

	unchained := writeProjectFixture(t, `{"configVersion":"6","audit":{"dir":"audit","chain":false},"packs":{"intake":{
	  "path":"packs/intake-0.1.0.pack.json"
	}}}`, map[string]string{"packs/intake-0.1.0.pack.json": evaluatorPack(t)})
	if code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake",
		"--config", unchained, "--facts", facts}, ""); code != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
	for _, line := range trailBytes(t, unchained) {
		for _, member := range []string{`"trail"`, `"sequence"`, `"previous"`} {
			if bytes.Contains(line, []byte(member)) {
				t.Fatalf("chain false writes no %s: %s", member, line)
			}
		}
	}
}

// A trail whose last line is incomplete refuses the run with the input/output
// exit, says why, reports no disposition, and is left as it was.
func TestAnIncompleteLastLineRefusesTheRunAndSaysWhy(t *testing.T) {
	configPath := auditProject(t)
	trail := filepath.Join(filepath.Dir(configPath), "audit", audit.FileName)
	if err := os.MkdirAll(filepath.Dir(trail), 0o700); err != nil {
		t.Fatal(err)
	}
	torn := []byte("{\"recordVersion\":\"1\"}\n{\"recordVersion\":\"1\",\"run\":\"")
	if err := os.WriteFile(trail, torn, 0o600); err != nil {
		t.Fatal(err)
	}
	facts := writeDocument(t, "facts.json", hardFailFacts)
	code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake",
		"--config", configPath, "--facts", facts}, "")
	if code != result.ExitIO || !strings.Contains(stderr, audit.FailureMessageFor(audit.ErrIncompleteLastLine)) {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if strings.Contains(stdout, "disposition") {
		t.Fatalf("no disposition is reported when its record could not be written: %q", stdout)
	}
	if data, err := os.ReadFile(trail); err != nil || !bytes.Equal(data, torn) {
		t.Fatalf("the trail is left as it was: %q %v", data, err)
	}
}

// The graph surface refuses the same way, and says the same thing.
func TestGraphEvaluateRefusesAfterAnIncompleteLastLine(t *testing.T) {
	config := strings.Replace(graphFixture(t, "jpack.json"),
		`"configVersion": "2",`, `"configVersion": "3",`+"\n"+`  "audit": {"dir": "audit"},`, 1)
	configPath := writeProjectFixture(t, config, map[string]string{
		"sanctions-screening-0.1.0.pack.json": graphFixture(t, "sanctions-screening-0.1.0.pack.json"),
		"vendor-onboarding-0.1.0.pack.json":   graphFixture(t, "vendor-onboarding-0.1.0.pack.json"),
		"onboarding.graph.json":               graphFixture(t, "onboarding.graph.json"),
	})
	trail := filepath.Join(filepath.Dir(configPath), "audit", audit.FileName)
	if err := os.MkdirAll(filepath.Dir(trail), 0o700); err != nil {
		t.Fatal(err)
	}
	torn := []byte(`{"recordVersion":"1","run":"`)
	if err := os.WriteFile(trail, torn, 0o600); err != nil {
		t.Fatal(err)
	}
	graphPath := filepath.Join(filepath.Dir(configPath), "onboarding.graph.json")
	code, _, stderr := runTest(t, []string{"experimental", "graph", "evaluate", graphPath,
		"--config", configPath, "--inputs", writeGraphInputs(t, graphHappyInputs)}, "")
	if code != result.ExitIO || !strings.Contains(stderr, audit.FailureMessageFor(audit.ErrIncompleteLastLine)) {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if data, err := os.ReadFile(trail); err != nil || !bytes.Equal(data, torn) {
		t.Fatalf("the trail is left as it was: %q %v", data, err)
	}
}
