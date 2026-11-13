package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
)

// benchProject lays out the intake project keeping a chained trail, for a
// benchmark, and returns its configuration's path and the pack's.
func benchProject(b *testing.B) string {
	b.Helper()
	pack, err := os.ReadFile(filepath.Join("..", "evaluation", "testdata", "data-request-intake-triage.json"))
	if err != nil {
		b.Fatal(err)
	}
	root := b.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "packs"), 0o755); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "packs", "intake.json"), pack, 0o600); err != nil {
		b.Fatal(err)
	}
	configPath := filepath.Join(root, project.DefaultConfigName)
	if err := os.WriteFile(configPath, []byte(`{"configVersion":"3","audit":{"dir":"audit"},"packs":{"intake":{"path":"packs/intake.json"}}}`), 0o600); err != nil {
		b.Fatal(err)
	}
	return configPath
}

// BenchmarkEvaluateDecisions is the cost of one recorded decision on a
// chained trail, in this process: one experimental evaluate run per decision,
// and one batch of b.N decisions (ADR-0085). Each op is one decision.
//
//	go test ./internal/cli -run '^$' -bench BenchmarkEvaluateDecisions -benchtime 500x
func BenchmarkEvaluateDecisions(b *testing.B) {
	b.Run("one run per decision", func(b *testing.B) {
		configPath := benchProject(b)
		facts := filepath.Join(b.TempDir(), "facts.json")
		if err := os.WriteFile(facts, []byte(hardFailFacts), 0o600); err != nil {
			b.Fatal(err)
		}
		args := []string{"experimental", "evaluate", "--pack-id", "intake", "--config", configPath, "--format", "json", "--facts", facts}
		b.ResetTimer()
		for range b.N {
			var stdout, stderr bytes.Buffer
			if code := Run(args, strings.NewReader(""), &stdout, &stderr); code != 0 {
				b.Fatalf("exit=%d %s", code, stderr.String())
			}
		}
	})
	b.Run("one batch", func(b *testing.B) {
		configPath := benchProject(b)
		line := testBatchItem{facts: hardFailFacts}.line() + "\n"
		input := strings.Repeat(line, b.N)
		args := []string{"experimental", "evaluate", "--pack-id", "intake", "--config", configPath, "--format", "json", "--batch", "-", "--max-items", "100000000"}
		b.ResetTimer()
		var stdout, stderr bytes.Buffer
		if code := Run(args, strings.NewReader(input), &stdout, &stderr); code != 0 {
			b.Fatalf("exit=%d %s", code, stderr.String())
		}
	})
}
