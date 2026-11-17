// Command emit-conformance runs `jpack emit conformance` in-process over the
// vectors of each reference emitter in this repository (ADR-0099), as CI's
// emitter-conformance check. Run it from the repository root. It exits 1 when
// a check fails, the harness could not run, or an emitter that reads the
// resolved form has no vector on which checks 4 (kind filtering) and 5
// (references) both pass, so that those checks cannot fall silent by every
// vector giving its request whole. Each manifest's command is `go run
// ./cmd/<emitter>`; the tool builds those packages once, with `go build`, and
// runs each harness with an in-memory copy of the manifest whose command is
// the built program, looked up on PATH as a manifest's program name is. Every
// emitter run gets a fresh, empty HOME, so a `go run` there would start from a
// cold build cache each time. The manifests on disk are not changed. The
// harness is given a copy of the files git tracks, as the working tree holds
// them, so an ignored directory such as an in-tree Python venv, whose
// interpreter links out of the tree, is never seen.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit/conformance"
)

// reference is one reference emitter: its manifest, its vectors, and whether
// checks 4 and 5 must pass on at least one of them.
type reference struct {
	manifest, vectors string
	registryChecks    bool
}

var references = []reference{
	{"cmd/jpack-emit-go-facts/emitter.json", "cmd/jpack-emit-go-facts/vectors", false},
	{"cmd/jpack-emit-go-loader/emitter.json", "cmd/jpack-emit-go-loader/golden/vectors", true},
	{"cmd/jpack-emit-ts-constants/emitter.json", "cmd/jpack-emit-ts-constants/vectors", true},
}

func main() {
	timeout := flag.Duration("timeout", emit.DefaultTimeout, "the wall-clock limit on each emitter run")
	flag.Parse()
	os.Exit(run(*timeout))
}

func run(timeout time.Duration) int {
	root, err := emit.RealRoot(".")
	if err != nil {
		fmt.Fprintln(os.Stderr, "emit-conformance: the repository root:", err)
		return 1
	}
	root, cleanup, err := copyTracked(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "emit-conformance: copying the tracked files:", err)
		return 1
	}
	defer cleanup()
	manifests := make([]*emit.Manifest, len(references))
	packages := make([]string, len(references))
	for i, ref := range references {
		m, pkg, err := readManifest(root, ref.manifest)
		if err != nil {
			fmt.Fprintln(os.Stderr, "emit-conformance:", err)
			return 1
		}
		manifests[i], packages[i] = m, pkg
	}
	bin, removeBin, err := buildEmitters(root, packages)
	if err != nil {
		fmt.Fprintln(os.Stderr, "emit-conformance: building the emitters:", err)
		return 1
	}
	defer removeBin()
	// The built programs are found first on PATH; the last PATH setting is
	// the one a lookup sees.
	environ := append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	failed := false
	for i, ref := range references {
		if !check(root, ref, manifests[i], environ, timeout) {
			failed = true
		}
	}
	if failed {
		return 1
	}
	return 0
}

// readManifest reads one reference emitter's manifest and returns it with
// its command replaced by the program `go build` makes of the package the
// command runs. A command that is not `go run ./<package> [args...]` is
// refused, so the tool never checks something other than what the manifest
// says.
func readManifest(root, path string) (*emit.Manifest, string, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return nil, "", fmt.Errorf("%s: %v", path, err)
	}
	m, diagnostics := emit.ReadManifest(data)
	if m == nil {
		return nil, "", fmt.Errorf("%s is refused: %v", path, diagnostics)
	}
	command := m.Command
	if len(command) < 3 || command[0] != "go" || command[1] != "run" || !strings.HasPrefix(command[2], "./") || strings.HasPrefix(command[2], "-") {
		return nil, "", fmt.Errorf("%s: the command is not `go run ./<package>`: %q", path, command)
	}
	pkg := command[2]
	built := *m
	built.Command = append([]string{programName(pkg)}, command[3:]...)
	return &built, pkg, nil
}

// programName is the file name `go build -o DIR/` gives the program built
// from pkg: the last element of its path, with .exe on Windows.
func programName(pkg string) string {
	name := pkg[strings.LastIndex(pkg, "/")+1:]
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// buildEmitters builds the packages, from root, into a fresh temporary
// directory, and returns that directory and a function that removes it.
// Each program must be there afterwards, under the name the manifest copy
// gives it.
func buildEmitters(root string, packages []string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "emit-conformance-bin-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	cmd := exec.Command("go", append([]string{"build", "-o", dir + string(filepath.Separator)}, packages...)...)
	cmd.Dir = root
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("go build %s: %v", strings.Join(packages, " "), err)
	}
	for _, pkg := range packages {
		if info, err := os.Stat(filepath.Join(dir, programName(pkg))); err != nil || !info.Mode().IsRegular() {
			cleanup()
			return "", nil, fmt.Errorf("go build %s left no program %s", pkg, programName(pkg))
		}
	}
	return dir, cleanup, nil
}

// check runs the harness over one emitter's vectors and reports it on one
// line, with a line for each failed row.
func check(root string, ref reference, m *emit.Manifest, environ []string, timeout time.Duration) bool {
	vectors := filepath.Join(root, filepath.FromSlash(ref.vectors))
	report := conformance.Run(context.Background(), m, vectors, conformance.Options{Root: root, Timeout: timeout, Environ: environ})
	counts := map[string]int{}
	passed := map[int]map[string]bool{4: {}, 5: {}}
	for _, row := range report.Rows {
		counts[row.Status]++
		if row.Status == conformance.StatusPass && passed[row.Check] != nil {
			passed[row.Check][row.Vector] = true
		}
	}
	fmt.Printf("%s: %s, %d pass, %d fail, %d skip\n", ref.vectors, report.Outcome, counts[conformance.StatusPass], counts[conformance.StatusFail], counts[conformance.StatusSkip])
	for _, row := range report.Rows {
		if row.Status == conformance.StatusFail {
			fmt.Printf("  fail: %s check %d (%s): %s\n", row.Vector, row.Check, row.Name, row.Reason)
		}
	}
	for _, d := range report.Diagnostics {
		where := d.Location.Path
		if where == "" {
			where = d.Location.Document
		}
		fmt.Printf("  diagnostic: %s at %s\n", d.Code, where)
	}
	ok := report.Exit() == 0
	if ref.registryChecks {
		both := false
		for vector := range passed[4] {
			both = both || passed[5][vector]
		}
		if !both {
			fmt.Printf("  fail: no vector passes both check 4 and check 5; add a vector formed from a registry\n")
			ok = false
		}
	}
	return ok
}
