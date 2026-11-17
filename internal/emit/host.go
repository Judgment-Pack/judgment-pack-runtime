package emit

import (
	"context"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// Outcomes of a host run, and their exit codes (RFC 0036 §4).
const (
	OutcomeWritten    = "written"     // write mode, exit 0
	OutcomeNotWritten = "not-written" // write mode, exit 2
	OutcomeIdentical  = "identical"   // check mode, exit 0
	OutcomeDifferent  = "different"   // check mode, exit 1
	OutcomeNotChecked = "not-checked" // check mode, exit 2
)

// Exit codes of `jpack emit`. A command line that cannot be used exits 3, as
// every jpack command does.
const (
	ExitCurrent   = 0
	ExitDifferent = 1
	ExitNotDone   = 2
)

// Options are a host run's conditions.
type Options struct {
	// Root is the repository root, as RealRoot returns it.
	Root string
	// Check compares instead of writing.
	Check bool
	// Timeout is each emitter run's limit; zero is DefaultTimeout.
	Timeout time.Duration
	// Environ is the host's environment; nil is os.Environ().
	Environ []string
	// Stderr receives the emitter's kept standard error when set.
	Stderr io.Writer
	// BeginWrite, when set, is called just before the write phase, once ctx
	// is seen not done. It reports whether the write may start; false
	// refuses the run as interrupted, writing nothing. The caller that sets
	// it holds an interrupt back until the write has ended, so that a
	// write once begun finishes.
	BeginWrite func() bool
}

// Identity names an emitter by its manifest's id and version.
type Identity struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

// RunRecord describes one emitter run by digests and counts.
type RunRecord struct {
	Environment    string `json:"environment,omitempty"`
	RequestVersion string `json:"requestVersion"`
	RequestDigest  string `json:"requestDigest"`
	ResponseDigest string `json:"responseDigest,omitempty"`
	Files          int    `json:"files"`
	StderrBytes    int64  `json:"stderrBytes"`
}

// Report is the host's report (emitReportVersion 1.0.0, this runtime's). It
// carries paths, codes, digests and counts; never a file's contents, an env
// value, or a value from the registry.
type Report struct {
	EmitReportVersion  string              `json:"emitReportVersion"`
	Mode               string              `json:"mode"`
	Outcome            string              `json:"outcome"`
	Emitter            *Identity           `json:"emitter,omitempty"`
	Runs               []RunRecord         `json:"runs"`
	Written            []string            `json:"written"`
	Deleted            []string            `json:"deleted"`
	Differences        []Difference        `json:"differences"`
	Diagnostics        []Diagnostic        `json:"diagnostics"`
	EmitterDiagnostics []EmitterDiagnostic `json:"emitterDiagnostics"`
	// RegistryReport is the registry's validation report, when the registry
	// given is not valid.
	RegistryReport *registry.Report `json:"registryReport,omitempty"`
}

// Exit returns the report's exit code.
func (r *Report) Exit() int {
	switch r.Outcome {
	case OutcomeWritten, OutcomeIdentical:
		return ExitCurrent
	case OutcomeDifferent:
		return ExitDifferent
	}
	return ExitNotDone
}

// NewReport returns the report of a run that has not started, for a caller
// that refuses its inputs before Emit.
func NewReport(check bool) *Report {
	r := &Report{
		EmitReportVersion: ReportVersion, Mode: "write", Outcome: OutcomeNotWritten,
		Runs: []RunRecord{}, Written: []string{}, Deleted: []string{}, Differences: []Difference{},
		Diagnostics: []Diagnostic{}, EmitterDiagnostics: []EmitterDiagnostic{},
	}
	if check {
		r.Mode, r.Outcome = "check", OutcomeNotChecked
	}
	return r
}

// Refuse ends the report with diagnostics, as a run that did not happen.
func (r *Report) Refuse(diagnostics ...Diagnostic) *Report {
	r.Diagnostics = append(r.Diagnostics, diagnostics...)
	r.finish(false)
	return r
}

// Emit is `jpack emit`: it forms the manifest's requests, runs the emitter on
// each, validates every response, and then, only if nothing refused the run,
// writes the files into the owned paths or compares them with the disk.
// Others are the repository's other emitter manifests given, whose owns
// globs must not overlap the manifest's.
func Emit(ctx context.Context, m *Manifest, others []*Manifest, in Inputs, opts Options) *Report {
	report := NewReport(opts.Check)
	report.Emitter = &Identity{ID: m.ID, Version: m.Version}
	if m.Deprecated {
		report.Diagnostics = append(report.Diagnostics, diagnostic("EMIT-DEPRECATED", Location{Document: DocumentManifest, Pointer: "/deprecated"}))
	}
	if overlaps := ownsOverlaps(m, others); len(overlaps) > 0 {
		return report.Refuse(overlaps...)
	}
	if in.Registry != nil && !in.Registry.Report.Valid {
		validation := in.Registry.Report
		report.RegistryReport = &validation
	}
	requests, refused := FormRequests(m, in)
	if len(refused) > 0 {
		return report.Refuse(refused...)
	}

	// Run every request and validate every response before anything is
	// compared or written.
	var files []File
	seen := map[string]bool{}
	paths := newPathSet()
	failed := false
	for _, request := range requests {
		result := Run(ctx, m, request.Version, request.Bytes, RunOptions{Root: opts.Root, Timeout: opts.Timeout, Environ: opts.Environ, Stderr: opts.Stderr})
		record := RunRecord{Environment: request.Environment, RequestVersion: request.Version, RequestDigest: request.Digest(), StderrBytes: result.StderrBytes}
		for _, d := range result.EmitterDiagnostics {
			d.Environment = request.Environment
			report.EmitterDiagnostics = append(report.EmitterDiagnostics, d)
		}
		for _, d := range result.Diagnostics {
			if d.Code == "EMIT-ENV-UNSET" {
				// The same for every run; reported once, by name only.
				if !slices.Contains(report.Diagnostics, d) {
					report.Diagnostics = append(report.Diagnostics, d)
				}
				continue
			}
			d.Location.Environment = request.Environment
			report.Diagnostics = append(report.Diagnostics, d)
		}
		if result.Response == nil {
			failed = true
			report.Runs = append(report.Runs, record)
			continue
		}
		record.ResponseDigest = digestOf(result.ResponseBytes)
		record.Files = len(result.Response.Files)
		report.Runs = append(report.Runs, record)
		// One request per environment answers into one set of files: a path
		// two runs both give, or one that is a directory of another run's, is
		// refused, as it is within one run.
		for i, file := range result.Response.Files {
			if paths.add(file.Path) {
				report.Diagnostics = append(report.Diagnostics, diagnostic("EMIT-RESPONSE", Location{Document: DocumentResponse, Environment: request.Environment, Pointer: "/files/" + strconv.Itoa(i) + "/path"}))
				failed = true
				continue
			}
			seen[file.Path] = true
			files = append(files, file)
		}
	}
	if failed || hasError(report.Diagnostics) {
		report.finish(false)
		return report
	}
	slices.SortFunc(files, func(a, b File) int { return strings.Compare(a.Path, b.Path) })

	disk, scanned := scanOwned(opts.Root, m.Owns)
	states, parents, examined := preflight(opts.Root, files)
	// Every response path is owned, so the walk and the preflight can both
	// find one link or one unreadable path; each is reported once.
	unusable := scanned
	reported := make(map[Diagnostic]bool, len(scanned))
	for _, d := range scanned {
		reported[d] = true
	}
	for _, d := range examined {
		if !reported[d] {
			reported[d] = true
			unusable = append(unusable, d)
		}
	}
	if len(unusable) > 0 {
		return report.Refuse(unusable...)
	}
	// A staging file a stopped run left, which no owns glob matches, is
	// reported in both modes and never deleted.
	report.Diagnostics = append(report.Diagnostics, ListPaths("EMIT-STAGING-LEFTOVER", DocumentRepository, stagingLeftovers(opts.Root, m.Owns, files, parents))...)
	if opts.Check {
		differences, unreadable := compare(opts.Root, files, disk, states)
		if len(unreadable) > 0 {
			return report.Refuse(unreadable...)
		}
		report.Differences = differences
		report.finish(true)
		return report
	}
	var stale []string
	for path := range disk {
		if !seen[path] {
			stale = append(stale, path)
		}
	}
	slices.Sort(stale)
	// An interrupt before this point refuses the run with nothing written
	// (EMIT-INTERRUPTED: every emitter may have exited 0, so EMIT-PROCESS
	// would blame it); a write once begun is not cut short by one
	// (BeginWrite).
	if ctx.Err() != nil || opts.BeginWrite != nil && !opts.BeginWrite() {
		return report.Refuse(diagnostic("EMIT-INTERRUPTED", Location{Document: DocumentManifest, Pointer: "/command"}))
	}
	written, deleted, failures := write(opts.Root, files, stale, states, parents)
	report.Written, report.Deleted = written, deleted
	if len(failures) > 0 {
		return report.Refuse(failures...)
	}
	report.finish(true)
	return report
}

// finish sets the outcome, sorts the diagnostics and holds the emitter
// diagnostics to their bound.
func (r *Report) finish(done bool) {
	switch {
	case !done && r.Mode == "check":
		r.Outcome = OutcomeNotChecked
	case !done:
		r.Outcome = OutcomeNotWritten
	case r.Mode == "check" && len(r.Differences) > 0:
		r.Outcome = OutcomeDifferent
	case r.Mode == "check":
		r.Outcome = OutcomeIdentical
	default:
		r.Outcome = OutcomeWritten
	}
	if len(r.EmitterDiagnostics) > maxReported {
		r.EmitterDiagnostics = r.EmitterDiagnostics[:maxReported]
		r.Diagnostics = append(r.Diagnostics, diagnostic("EMIT-REPORT-TRUNCATED", Location{Document: DocumentResponse, Pointer: "/diagnostics"}))
	}
	if len(r.Diagnostics) > maxReported {
		r.Diagnostics = append(r.Diagnostics[:maxReported], diagnostic("EMIT-REPORT-TRUNCATED", Location{Document: DocumentResponse}))
	}
}

// ownsOverlaps reports each pair of an owns glob of m and one of another
// manifest that can match one path (RFC 0036 §1), at both.
func ownsOverlaps(m *Manifest, others []*Manifest) []Diagnostic {
	var out []Diagnostic
	for k, other := range others {
		for i, glob := range m.Owns {
			for j, theirs := range other.Owns {
				if glob.Overlaps(theirs) {
					out = append(out,
						diagnostic("EMIT-OWNS-OVERLAP", Location{Document: DocumentManifest, Pointer: "/owns/" + strconv.Itoa(i)}),
						diagnostic("EMIT-OWNS-OVERLAP", Location{Document: DocumentManifest, Manifest: k + 1, Pointer: "/owns/" + strconv.Itoa(j)}))
				}
			}
		}
	}
	return out
}
