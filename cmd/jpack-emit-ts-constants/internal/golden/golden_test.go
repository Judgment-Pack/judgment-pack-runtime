package golden

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

// generatedFiles are the vectors' expected TypeScript files.
func generatedFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "vectors", "*", "expected", "generated", "constants", "*.ts"))
	if err != nil || len(files) < 4 {
		t.Fatalf("found %d generated files, %v", len(files), err)
	}
	return files
}

var importPattern = regexp.MustCompile(`(?m)^import \{[^}]*\} from "([^"]+)";$`)

// TestConsumerImportsTheVectors: every module consumer.ts imports is a
// vector's expected file, so the type check below reads the emitter's output.
// It runs without tsc.
func TestConsumerImportsTheVectors(t *testing.T) {
	data, err := os.ReadFile("consumer.ts")
	if err != nil {
		t.Fatal(err)
	}
	imports := importPattern.FindAllStringSubmatch(string(data), -1)
	if len(imports) != 3 {
		t.Fatalf("consumer.ts has %d imports, want 3", len(imports))
	}
	for _, m := range imports {
		if _, err := os.Stat(filepath.FromSlash(m[1]) + ".ts"); err != nil {
			t.Errorf("consumer.ts imports %s, which is not a generated file", m[1])
		}
	}
}

// TestTypeScriptChecks type-checks the generated files of every vector, and
// consumer.ts, which reads three of them, with tsc --strict. It is skipped
// when tsc is not on PATH: the tests never install or fetch a toolchain.
func TestTypeScriptChecks(t *testing.T) {
	tsc, err := exec.LookPath("tsc")
	if err != nil {
		t.Skip("tsc is not on PATH, so the generated TypeScript is not type-checked")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	args := []string{"--noEmit", "--strict", "--target", "es2020", "--module", "commonjs"}
	args = append(append(args, generatedFiles(t)...), "consumer.ts")
	out, err := exec.CommandContext(ctx, tsc, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("tsc: %v\n%s", err, out)
	}
}
