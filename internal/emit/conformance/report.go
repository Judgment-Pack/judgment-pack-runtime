// Package conformance is the emitter conformance harness of spec RFC 0036 §5,
// as the draft registry companion (spec/registry.md §11.5) restates it
// (ADR-0099): `jpack emit conformance`. It runs an emitter on its author's
// vectors through the host's own run step (emit.Run) and response check
// (emit.ParseResponse), and applies the checks of the vectors' harness level:
// golden equality, determinism, path safety and bounds (with the harness's
// hostile responses), kind filtering, references, and no stray writes.
//
// The report holds vector names, check names, codes, paths and locations.
// It never holds a file's contents, a request value, an env value, or a name
// the emitter chose for a file it wrote outside its response.
package conformance

import (
	"encoding/json"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// ReportVersion is the harness report's selector (RFC 0036 §5).
const ReportVersion = "1.0.0"

// DefaultLevel is the harness level of a vector directory without
// harness.json.
const DefaultLevel = "1.0"

// levels maps each harness level this harness applies to its checks. Level
// 1.0 is checks 1-6 (RFC 0036 §5). A later level adds checks; a check of a
// later level runs at an earlier one only as an info row.
var levels = map[string][]int{"1.0": {checkGolden, checkDeterminism, checkPathSafety, checkKindFiltering, checkReferences, checkStrayWrites}}

// The checks of level 1.0, by number.
const (
	checkGolden        = 1
	checkDeterminism   = 2
	checkPathSafety    = 3
	checkKindFiltering = 4
	checkReferences    = 5
	checkStrayWrites   = 6
)

var checkNames = map[int]string{
	checkGolden:        "golden",
	checkDeterminism:   "determinism",
	checkPathSafety:    "path-safety",
	checkKindFiltering: "kind-filtering",
	checkReferences:    "references",
	checkStrayWrites:   "stray-writes",
}

// Row statuses (RFC 0036 §5).
const (
	StatusPass = "pass"
	StatusFail = "fail"
	StatusSkip = "skip"
	StatusInfo = "info"
)

// Outcomes of a harness run, and their exit codes.
const (
	OutcomePass   = "pass"    // every row passed, was skipped or is info; exit 0
	OutcomeFail   = "fail"    // a check failed; exit 1
	OutcomeNotRun = "not-run" // the harness could not run; exit 2
)

// Exit codes of `jpack emit conformance` (RFC 0036 §5). A command line that
// cannot be used exits 3, as every jpack command does.
const (
	ExitPass   = 0
	ExitFail   = 1
	ExitNotRun = 2
)

// Documents a harness diagnostic's location can name besides the host's.
const (
	// DocumentVector is a file of the vector directory; the location's path
	// is relative to it.
	DocumentVector = "vector"
)

// Report is the harness report (harnessReportVersion 1.0.0).
type Report struct {
	HarnessReportVersion string         `json:"harnessReportVersion"`
	HarnessLevel         string         `json:"harnessLevel,omitempty"`
	Outcome              string         `json:"outcome"`
	Emitter              *emit.Identity `json:"emitter,omitempty"`
	Rows                 []Row          `json:"rows"`
	// Diagnostics are the harness's own: why it could not run, and the host's
	// warnings (EMIT-DEPRECATED, EMIT-ENV-UNSET), each once.
	Diagnostics []emit.Diagnostic `json:"diagnostics"`
}

// Row is one check of one vector, or of one of the harness's hostile
// responses.
type Row struct {
	Vector string `json:"vector"`
	Check  int    `json:"check"`
	Name   string `json:"name"`
	Status string `json:"status"`
	// Reason says why a row was skipped, failed or is info, in a fixed text.
	Reason string `json:"reason,omitempty"`
	// Environment is the request's environment, for a manifest whose
	// environments are "each".
	Environment string `json:"environment,omitempty"`
	// Expected is the failure code an expected-failure vector names.
	Expected string `json:"expected,omitempty"`
	// Runs are the determinism runs (2 and 3) whose outcome differs from run
	// 1's.
	Runs               []int                    `json:"runs,omitempty"`
	Diagnostics        []emit.Diagnostic        `json:"diagnostics,omitempty"`
	EmitterDiagnostics []emit.EmitterDiagnostic `json:"emitterDiagnostics,omitempty"`
	Differences        []emit.Difference        `json:"differences,omitempty"`
	References         []Reference              `json:"references,omitempty"`
	StrayWrites        []StrayWrite             `json:"strayWrites,omitempty"`
}

// Reference is one output file that holds a run of a secret-position string
// outside a complete occurrence of it (check 5), located by the file's path
// and the position the string is at. It never holds the string.
type Reference struct {
	File        string             `json:"file"`
	Environment string             `json:"environment,omitempty"`
	Entry       registry.EntryName `json:"entry"`
	Pointer     string             `json:"pointer"`
}

// Kinds of stray write (check 6).
const (
	StrayCreated = "created"
	StrayChanged = "changed"
	StrayRemoved = "removed"
)

// StrayWrite is one change the emitter made to the repository copy itself.
// For a changed or removed path, Path is the repository's own; for something
// created, it is the nearest directory that existed before, never the name
// the emitter chose.
type StrayWrite struct {
	Code   string `json:"code"`
	Path   string `json:"path"`
	Change string `json:"change"`
}

// Exit returns the report's exit code.
func (r *Report) Exit() int {
	switch r.Outcome {
	case OutcomePass:
		return ExitPass
	case OutcomeFail:
		return ExitFail
	}
	return ExitNotRun
}

// NewReport returns the report of a run that has not started, for a caller
// that refuses its inputs before Run.
func NewReport() *Report {
	return &Report{HarnessReportVersion: ReportVersion, Outcome: OutcomeNotRun, Rows: []Row{}, Diagnostics: []emit.Diagnostic{}}
}

// Refuse ends the report as a run that could not happen.
func (r *Report) Refuse(diagnostics ...emit.Diagnostic) *Report {
	r.Diagnostics = append(r.Diagnostics, diagnostics...)
	r.Outcome = OutcomeNotRun
	return r
}

func (r *Report) finish() *Report {
	r.Outcome = OutcomePass
	for _, row := range r.Rows {
		if row.Status == StatusFail {
			r.Outcome = OutcomeFail
		}
	}
	return r
}

// Canonical returns the report's RFC 8785 canonical bytes.
func (r *Report) Canonical() ([]byte, error) {
	data, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return jcs.EncodeDocument(value)
}

// maxListed bounds each list a row carries, as the host bounds its report's
// lists.
const maxListed = emit.MaxReportedDiagnostics

func bounded[T any](items []T) []T {
	if len(items) > maxListed {
		return items[:maxListed]
	}
	return items
}
