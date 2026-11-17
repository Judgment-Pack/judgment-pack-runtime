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

// tscEnv names the variable that opts into the type check: the absolute
// path of a tsc to run, for example
//
//	JPACK_TEST_TSC=/opt/node/bin/tsc go test ./cmd/jpack-emit-ts-constants/...
//
// The tests never look a program up on PATH, install one or fetch one.
const tscEnv = "JPACK_TEST_TSC"

// TestTypeScriptChecks type-checks the generated files of every vector, and
// consumer.ts, which reads three of them, with tsc --strict. It runs only
// when JPACK_TEST_TSC holds tsc's absolute path, and is skipped when the
// variable is unset or empty. A path that is not absolute fails the test, so
// that a check asked for is never skipped.
func TestTypeScriptChecks(t *testing.T) {
	tsc := os.Getenv(tscEnv)
	if tsc == "" {
		t.Skip(tscEnv + " is not set, so the generated TypeScript is not type-checked")
	}
	if !filepath.IsAbs(tsc) {
		t.Fatalf("%s is %q, not an absolute path", tscEnv, tsc)
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
