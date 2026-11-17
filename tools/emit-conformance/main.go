// Command emit-conformance runs `jpack emit conformance` in-process over the
// vectors of each reference emitter in this repository (ADR-0099), as CI's
// emitter-conformance check. Run it from the repository root. It exits 1 when
// a check fails, the harness could not run, or an emitter that reads the
// resolved form has no vector on which checks 4 (kind filtering) and 5
// (references) both pass, so that those checks cannot fall silent by every
// vector giving its request whole. Each emitter runs as its manifest says
// (`go run ./cmd/<emitter>`), in a fresh copy of the repository.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
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
	failed := false
	for _, ref := range references {
		if !check(root, ref, timeout) {
			failed = true
		}
	}
	if failed {
		return 1
	}
	return 0
}

// check runs the harness over one emitter's vectors and reports it on one
// line, with a line for each failed row.
func check(root string, ref reference, timeout time.Duration) bool {
	data, err := os.ReadFile(filepath.FromSlash(ref.manifest))
	if err != nil {
		fmt.Fprintf(os.Stderr, "emit-conformance: %s: %v\n", ref.manifest, err)
		return false
	}
	m, diagnostics := emit.ReadManifest(data)
	if m == nil {
		fmt.Fprintf(os.Stderr, "emit-conformance: %s is refused: %v\n", ref.manifest, diagnostics)
		return false
	}
	vectors := filepath.Join(root, filepath.FromSlash(ref.vectors))
	report := conformance.Run(context.Background(), m, vectors, conformance.Options{Root: root, Timeout: timeout})
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
		fmt.Printf("  diagnostic: %s\n", d.Code)
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
