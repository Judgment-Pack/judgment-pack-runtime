package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestReadManifestReplacesOnlyTheCommand: the copy runs the program built
// from the package `go run` names, with the command's further arguments, and
// keeps every other field; the manifest on disk is unchanged. A command that
// is not `go run ./<package>` is refused, not run some other way.
func TestReadManifestReplacesOnlyTheCommand(t *testing.T) {
	original, err := os.ReadFile(filepath.Join("..", "..", "cmd", "jpack-emit-go-facts", "emitter.json"))
	if err != nil {
		t.Fatal(err)
	}
	write := func(command []string) string {
		t.Helper()
		var fields map[string]any
		if err := json.Unmarshal(original, &fields); err != nil {
			t.Fatal(err)
		}
		fields["command"] = command
		data, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "emitter.json"), data, 0o644); err != nil {
			t.Fatal(err)
		}
		return root
	}

	root := write([]string{"go", "run", "./cmd/jpack-emit-go-facts", "extra"})
	before, _ := os.ReadFile(filepath.Join(root, "emitter.json"))
	m, pkg, err := readManifest(root, "emitter.json")
	if err != nil {
		t.Fatal(err)
	}
	if pkg != "./cmd/jpack-emit-go-facts" {
		t.Errorf("package = %q", pkg)
	}
	if want := []string{programName(pkg), "extra"}; !slices.Equal(m.Command, want) {
		t.Errorf("command = %q, want %q", m.Command, want)
	}
	if strings.Contains(m.Command[0], "/") {
		t.Errorf("command[0] = %q is not a program name looked up on PATH", m.Command[0])
	}
	if !slices.Equal(m.Env, []string{"GOCACHE", "GOMODCACHE", "GOPATH"}) || len(m.Owns) != 1 || m.Input != "factsContracts" {
		t.Errorf("fields other than command changed: env %q, owns %v, input %q", m.Env, m.Owns, m.Input)
	}
	if after, _ := os.ReadFile(filepath.Join(root, "emitter.json")); string(after) != string(before) {
		t.Error("the manifest on disk changed")
	}

	for name, command := range map[string][]string{
		"a program":       {"bin/emitter"},
		"go build":        {"go", "build", "./cmd/jpack-emit-go-facts"},
		"go run, a flag":  {"go", "run", "-race", "./cmd/jpack-emit-go-facts"},
		"go run, no ./":   {"go", "run", "cmd/jpack-emit-go-facts"},
		"go run, nothing": {"go", "run"},
	} {
		if _, _, err := readManifest(write(command), "emitter.json"); err == nil {
			t.Errorf("%s: %q is not refused", name, command)
		}
	}
}
