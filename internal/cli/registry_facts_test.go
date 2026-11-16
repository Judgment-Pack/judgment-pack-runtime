package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// queuePack is a synthetic pack over the facts of the queue kind: a
// retention of seven days or more is approved, a shorter one needs approval.
const queuePack = `{"specVersion":"0.2.0-draft","id":"https://example.invalid/judgment-packs/queue-retention","version":"0.1.0","title":"Synthetic queue retention review","description":"Synthetic content for runtime testing; it authorizes nothing.","decision":{"intent":"Synthetic.","question":"Synthetic?"},` +
	`"applicability":{"op":"fact","path":"/entry/kind","operator":"equals","value":"queue"},` +
	`"outcomes":[{"id":"approved","label":"Approved"},{"id":"needs-approval","label":"Needs approval"}],` +
	`"rules":[{"id":"rule-long","description":"Synthetic rule.","onUnknown":"escalate","rationale":"Synthetic.","when":{"op":"fact","path":"/value/retention","operator":"greater-than-or-equal","value":"7"},"outcome":"approved"},` +
	`{"id":"rule-short","description":"Synthetic rule.","onUnknown":"escalate","rationale":"Synthetic.","when":{"op":"fact","path":"/value/retention","operator":"less-than","value":"7"},"outcome":"needs-approval"}],` +
	`"escalation":{"triggers":["unknown"],"target":{"kind":"human-role","name":"Reviewer"},"message":"Synthetic."}}`

// factsInputs writes the registry beside a kinds directory that holds the
// queue kind and a file that is not a kind declaration.
func factsInputs(t *testing.T, retention string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	kinds := filepath.Join(dir, "kinds")
	if err := os.Mkdir(kinds, 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		filepath.Join(dir, "registry.json"): cliRegistry(t, retention),
		filepath.Join(kinds, "queue.json"):  cliKind,
		filepath.Join(kinds, "README.txt"):  "not a kind",
	}
	for path, text := range files {
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(dir, "registry.json"), kinds
}

func TestRegistryFactsWritesABatch(t *testing.T) {
	registryPath, kinds := factsInputs(t, "14")
	args := []string{"registry", "facts", "--registry", registryPath, "--kinds", kinds, "--kind", "queue"}
	code, out, stderr := runTest(t, append(args, "--environment", "prod"), "")
	if code != 0 || stderr != "" || strings.Count(out, "\n") != 1 ||
		!strings.HasPrefix(out, `{"facts":{"entry":{"id":"orders","kind":"queue","owner":"platform"},"environment":"prod","references":{},"registry":{"digest":"sha256:`) ||
		!strings.HasSuffix(out, `"value":{"retention":"14"}},"id":"queue/orders"}`+"\n") {
		t.Fatalf("prod: %d %q %q", code, out, stderr)
	}
	contractPath := filepath.Join(t.TempDir(), "queue.contract.json")
	code, out, stderr = runTest(t, append(args, "--all-environments", "--contract", contractPath), "")
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if code != 0 || stderr != "" || len(lines) != 2 || !strings.HasSuffix(lines[0], `"value":{"retention":"3"}},"id":"dev:queue/orders"}`) || !strings.HasSuffix(lines[1], `"value":{"retention":"14"}},"id":"prod:queue/orders"}`) {
		t.Fatalf("all environments: %d %q %q", code, out, stderr)
	}
	contract, err := os.ReadFile(contractPath)
	if err != nil || !strings.HasSuffix(string(contract), "\n") || !strings.Contains(string(contract), `{"pointer":"/value/retention","present":"always","type":"decimal"}`) {
		t.Fatalf("contract: %v %s", err, contract)
	}
	if read, diagnostics := registry.ReadFactsContract(registry.Document{Data: contract}); read == nil {
		t.Fatalf("the written contract reads back: %+v", diagnostics)
	}
}

func TestRegistryFactsWritesNothingItCannotStandBehind(t *testing.T) {
	registryPath, kinds := factsInputs(t, "99")
	contractPath := filepath.Join(t.TempDir(), "queue.contract.json")
	base := []string{"registry", "facts", "--registry", registryPath, "--kinds", kinds, "--kind", "queue"}
	// An invalid registry: no line, the report on standard error, exit 2.
	code, out, stderr := runTest(t, append(base, "--environment", "dev", "--contract", contractPath), "")
	if code != 2 || out != "" || !strings.Contains(stderr, `"valid":false`) || !strings.Contains(stderr, "REG-VALUE-RANGE") {
		t.Fatalf("invalid registry: %d %q %q", code, out, stderr)
	}
	if _, err := os.Stat(contractPath); !os.IsNotExist(err) {
		t.Fatal("an invalid registry wrote a contract")
	}

	registryPath, kinds = factsInputs(t, "14")
	valid := []string{"registry", "facts", "--registry", registryPath, "--kinds", kinds}
	for name, test := range map[string]struct {
		args []string
		want int
	}{
		"a kind the registry does not pin":   {append(valid, "--kind", "cache", "--environment", "dev"), 3},
		"an environment it does not declare": {append(valid, "--kind", "queue", "--environment", "qa"), 3},
		"both environment options":           {append(valid, "--kind", "queue", "--environment", "dev", "--all-environments"), 3},
		"neither environment option":         {append(valid, "--kind", "queue"), 3},
		"no kind":                            {append(valid, "--environment", "dev"), 3},
		"a contract on standard output":      {append(valid, "--kind", "queue", "--environment", "dev", "--contract", "-"), 3},
		"a contract that cannot be written":  {append(valid, "--kind", "queue", "--environment", "dev", "--contract", filepath.Join(t.TempDir(), "missing", "c.json")), 4},
		"a kinds path that does not exist":   {[]string{"registry", "facts", "--registry", registryPath, "--kinds", filepath.Join(kinds, "nope.json"), "--kind", "queue", "--environment", "dev"}, 4},
		"a kinds file that is not a kind":    {[]string{"registry", "facts", "--registry", registryPath, "--kinds", filepath.Join(kinds, "README.txt"), "--kind", "queue", "--environment", "dev"}, 2},
	} {
		code, out, stderr := runTest(t, test.args, "")
		if code != test.want || out != "" || stderr == "" {
			t.Errorf("%s: %d %q %q", name, code, out, stderr)
		}
	}
}

// The batch registry facts writes is a file evaluate reads unchanged, and the
// outcome gate judges it.
func TestRegistryFactsFeedsTheOutcomeGate(t *testing.T) {
	registryPath, kinds := factsInputs(t, "14")
	pack := writeDocument(t, "pack.json", queuePack)
	base := []string{"registry", "facts", "--registry", registryPath, "--kinds", kinds, "--kind", "queue"}
	for name, test := range map[string]struct {
		environment []string
		gate        int
		items       int
	}{
		"prod, retention 14, passes":                      {[]string{"--environment", "prod"}, 0, 1},
		"every environment, dev's retention 3, a finding": {[]string{"--all-environments"}, 1, 2},
	} {
		code, out, stderr := runTest(t, append(base, test.environment...), "")
		if code != 0 {
			t.Fatalf("%s: facts: %d %q", name, code, stderr)
		}
		batch := writeDocument(t, "batch.jsonl", out)
		code, out, stderr = runTest(t, []string{"experimental", "evaluate", pack, "--batch", batch, "--format", "json", "--require-outcome", "approved"}, "")
		if code != test.gate || strings.Count(out, "\n") != test.items || strings.Contains(out, `"error"`) {
			t.Errorf("%s: gate %d, want %d:\n%s\n%s", name, code, test.gate, out, stderr)
		}
	}
}

func TestRegistryCheckPack(t *testing.T) {
	registryPath, kinds := factsInputs(t, "14")
	contractPath := filepath.Join(t.TempDir(), "queue.contract.json")
	if code, _, stderr := runTest(t, []string{"registry", "facts", "--registry", registryPath, "--kinds", kinds, "--kind", "queue", "--environment", "dev", "--contract", contractPath}, ""); code != 0 {
		t.Fatalf("facts: %d %q", code, stderr)
	}
	pack := writeDocument(t, "pack.json", queuePack)
	code, out, stderr := runTest(t, []string{"registry", "check-pack", pack, "--contract", contractPath}, "")
	if code != 0 || stderr != "" || !strings.HasPrefix(out, `{"factsCheckFormatVersion":"1.0.0","contract":{"digest":"sha256:`) || !strings.Contains(out, `"pack":{"digest":"sha256:`) || !strings.HasSuffix(out, `"diagnostics":[]}`+"\n") {
		t.Fatalf("a pack that reads what the contract lists: %d %q %q", code, out, stderr)
	}
	drifted := writeDocument(t, "drifted.json", strings.Replace(queuePack, `"/value/retention","operator":"less-than"`, `"/value/retentionDays","operator":"less-than"`, 1))
	code, out, _ = runTest(t, []string{"registry", "check-pack", drifted, "--contract", contractPath}, "")
	if code != 1 || !strings.Contains(out, `{"code":"FACTS-POINTER-UNKNOWN","severity":"warning","location":{"document":"pack","pointer":"/rules/1/when"}`) || strings.Contains(out, "retentionDays") {
		t.Fatalf("a pack that reads a pointer the contract does not list: %d %q", code, out)
	}
	broken := writeDocument(t, "broken.json", `{"factsContractVersion":"1.0.0","id":"https://example.invalid/facts/x","version":"1.0.0","source":{"authored":true},"facts":[{"pointer":"/a","type":"integer","present":"always"}]}`)
	code, out, _ = runTest(t, []string{"registry", "check-pack", pack, "--contract", broken}, "")
	if code != 1 || !strings.Contains(out, `"code":"FACTS-CONTRACT-FORM"`) || strings.Contains(out, `"pack":`) {
		t.Fatalf("a refused contract: %d %q", code, out)
	}
	code, _, _ = runTest(t, []string{"registry", "check-pack", pack}, "")
	if code != 3 {
		t.Fatalf("no contract: %d", code)
	}
	notAPack := writeDocument(t, "not-a-pack.json", `{"specVersion":"0.2.0-draft"}`)
	if code, _, _ = runTest(t, []string{"registry", "check-pack", notAPack, "--contract", contractPath}, ""); code == 0 {
		t.Fatal("a pack the evaluator refuses is not checked")
	}
}
