package conformance

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// Options are the conditions of one harness run.
type Options struct {
	// Root is the repository root, absolute and with no symbolic link in it
	// (emit.RealRoot). The emitter runs in a fresh copy of it (check 6).
	Root string
	// Timeout is the wall-clock limit on each emitter run; zero is
	// emit.DefaultTimeout.
	Timeout time.Duration
	// Environ is the host's environment; nil is os.Environ().
	Environ []string
	// Update rewrites each vector's expected/ from the emitter's run
	// (--update). An expected-failure vector is never touched.
	Update bool
	// BeginWrite, when set, is called just before each vector's expected/
	// is replaced (--update), once ctx is seen not done, as
	// emit.Options.BeginWrite is: false refuses the run as interrupted,
	// with that expected/ untouched. The caller that sets it holds an
	// interrupt back until EndWrite, so that a replacement once begun
	// finishes.
	BeginWrite func() bool
	// EndWrite, when set, is called when a replacement BeginWrite allowed
	// has ended.
	EndWrite func()
}

// The fixed reasons a row gives.
const (
	reasonRefused        = "the request is refused before the emitter runs"
	reasonRunFailed      = "the run failed"
	reasonWrongFailure   = "the run did not fail with the expected code"
	reasonSucceeded      = "the run succeeded, and the vector expects a failure"
	reasonDifferent      = "the files differ from expected/"
	reasonUpdated        = "expected/ was written from this run (--update)"
	reasonNotUpdated     = "the run failed, so expected/ was not written (--update)"
	reasonNondeterminism = "a run with another HOME, TZ, LANG and SOURCE_DATE_EPOCH answered differently"
	reasonNoResponse     = "the emitter's run failed before its response was read"
	reasonRefusedAsMeant = "the host refused the response, as the vector expects"
	reasonUnsafe         = "the host refused the response's form, a path or a bound"
	reasonNoRegistry     = "the vector gives its request whole, with no registry"
	reasonFilterRefused  = "the request with the synthetic undeclared kind is refused"
	reasonFilterHolds    = "the request holds the synthetic undeclared kind"
	reasonFilterBytes    = "with the registry digest restored, the request with the synthetic undeclared kind is not the original's bytes"
	reasonFilterResponse = "the response to the request with the synthetic undeclared kind differs"
	reasonFacts          = "a facts-contracts request holds no secret position"
	reasonGivenRequest   = "the vector gives its request whole, with no kinds to find its secret positions by"
	reasonNoFiles        = "the run failed, so there are no files"
	reasonReferences     = "a file holds a run of a secret reference that no complete secret reference in it holds"
	reasonNoCopy         = "this system cannot make the repository's symbolic links, so the emitter runs in the repository itself and stray writes are not checked"
	reasonStray          = "the emitter changed the repository copy itself"
	reasonHostileAccept  = "the host accepted a hostile response"
	reasonHostilePlace   = "the host refused the hostile response, but not at the place the hostile names"
)

// determinismRuns are runs 2 and 3 of check 2: each with a HOME of its own
// (as run 1 has), and TZ, LANG and SOURCE_DATE_EPOCH set to values run 1 does
// not have and the other run does not have either. Run 1 is the emitter's run
// as `jpack emit` makes it.
var determinismRuns = []struct {
	run             int
	tz, lang, epoch string
	home            string
}{
	{run: 2, tz: "Pacific/Kiritimati", lang: "tr_TR.UTF-8", epoch: "1", home: "h"},
	{run: 3, tz: "America/St_Johns", lang: "de_DE.UTF-8", epoch: "2000000000", home: filepath.Join("a", "longer", "home")},
}

// harness is one run of the harness.
type harness struct {
	ctx    context.Context
	m      *emit.Manifest
	opts   Options
	ws     *workspace
	report *Report
	keep   func(kind string) bool
	// ran says whether any run started the emitter's process.
	ran bool
	// interrupted says that an update's write was refused as interrupted
	// (Options.BeginWrite).
	interrupted bool
}

// vectorRun is what one vector's request and run 1 gave.
type vectorRun struct {
	v *Vector
	// registry is the vector's registry, validated; nil for a given request.
	registry *registry.Result
	requests []emit.Request
	// refused refuses the request: the emitter never runs.
	refused []emit.Diagnostic
	// results are run 1's, one per request.
	results []emit.RunResult
	// crossRun are the paths two runs both give (EMIT-RESPONSE), as the host
	// refuses them.
	crossRun []emit.Diagnostic
	// files are every run's files, by path, when every run succeeded.
	files []emit.File
}

// Run is `jpack emit conformance`: it reads the vectors' level and lays out
// every vector, makes a copy of the repository, and reads each vector and
// runs the emitter on its request in the copy, applying the level's checks;
// then it adds the rows of the harness's hostile responses. A vector
// directory that is not of the harness's layout, a level it does not know,
// or a bound it reaches refuses the run, which is reported as not run.
func Run(ctx context.Context, m *emit.Manifest, vectorsDir string, opts Options) *Report {
	report := NewReport()
	report.Emitter = &emit.Identity{ID: m.ID, Version: m.Version}
	if m.Deprecated {
		report.Diagnostics = append(report.Diagnostics, emit.NewDiagnostic("EMIT-DEPRECATED", emit.Location{Document: emit.DocumentManifest, Pointer: "/deprecated"}))
	}
	level, refused := readLevel(vectorsDir)
	if len(refused) > 0 {
		return report.Refuse(refused...)
	}
	report.HarnessLevel = level
	vectors, refused := readVectors(vectorsDir, opts.Update)
	if len(refused) > 0 {
		return report.Refuse(refused...)
	}
	ws, failure := newWorkspace(opts.Root)
	if failure != nil {
		return report.Refuse(*failure)
	}
	defer ws.close()
	reads := map[string]bool{}
	for _, r := range m.Reads {
		reads[r.Kind] = true
	}
	h := &harness{ctx: ctx, m: m, opts: opts, ws: ws, report: report, keep: func(kind string) bool { return reads[kind] }}
	checks := levels[level]
	for _, v := range vectors {
		// Each vector is read just before its run and dropped after it, so
		// that the harness holds one vector's files at a time.
		if refused := v.load(); len(refused) > 0 {
			return report.Refuse(refused...)
		}
		before := len(report.Rows)
		failure := h.vector(v, checks)
		v.release()
		if h.ctx.Err() != nil || h.interrupted {
			// The run was interrupted (jpack was sent an interrupt or
			// SIGTERM): this vector's rows would report failures the
			// emitter did not cause, and the vectors after it would only
			// add more. The rows already complete are kept, and so is
			// this vector's check 1 row when it says expected/ was
			// rewritten (--update), which it was.
			if len(report.Rows) > before && report.Rows[before].Reason == reasonUpdated {
				before++
			}
			report.Rows = report.Rows[:before]
			return report.Refuse(vectorDiagnostic("EMIT-INTERRUPTED", v.Name, ""))
		}
		if failure != nil {
			return report.Refuse(*failure)
		}
	}
	if !h.ran {
		// No run started the emitter, every vector refused before it or its
		// command never started: as with no vector at all, the rows would
		// pass a run that never ran the emitter.
		return report.Refuse(vectorDiagnostic("EMIT-VECTOR", "", ""))
	}
	if slices.Contains(checks, checkPathSafety) {
		report.Rows = append(report.Rows, hostileRows()...)
	}
	return report.finish()
}

// vector runs one vector and adds its rows. It returns the diagnostic that
// stops the harness, when a check could not be completed.
func (h *harness) vector(v *Vector, checks []int) *emit.Diagnostic {
	run := h.form(v)
	if run.refused == nil {
		h.runAll(run)
	}
	var failure *emit.Diagnostic
	for _, check := range checks {
		row := Row{Vector: v.Name, Check: check, Name: checkNames[check]}
		switch check {
		case checkGolden:
			row, failure = h.golden(run, row)
		case checkDeterminism:
			row, failure = h.determinism(run, row)
		case checkPathSafety:
			row = pathSafety(run, row)
		case checkKindFiltering:
			row, failure = h.kindFiltering(run, row)
		case checkReferences:
			row, failure = h.references(run, row)
		case checkStrayWrites:
			row, failure = h.strayWrites(run, row)
		}
		if failure != nil {
			// A row its check completed before the harness stopped, check
			// 6's stray writes when the copy cannot be made again, stays in
			// the report, as the earlier rows do.
			if row.Status != "" {
				h.report.Rows = append(h.report.Rows, row)
			}
			return failure
		}
		h.report.Rows = append(h.report.Rows, row)
	}
	return nil
}

// form forms a vector's requests: a given request held to the host's
// envelope, or the requests `jpack emit` forms from the vector's registry,
// kinds and options.
func (h *harness) form(v *Vector) *vectorRun {
	run := &vectorRun{v: v}
	if len(v.Refused) > 0 {
		run.refused = v.Refused
		return run
	}
	if !v.hasRegistry() {
		request, refused := givenRequest(v.Request, h.m)
		if len(refused) > 0 {
			run.refused = refused
			return run
		}
		run.requests = []emit.Request{request}
		return run
	}
	run.registry = registry.ValidateDocuments(v.Registry, v.Kinds)
	requests, refused := emit.FormRequests(h.m, emit.Inputs{Registry: run.registry, Options: v.Options})
	if len(refused) > 0 {
		run.refused = refused
		return run
	}
	run.requests = requests
	return run
}

// execute runs the emitter on one request in the repository copy, under
// overrides of its environment (check 2's), and keeps the host's
// EMIT-ENV-UNSET warnings for the report, once each.
func (h *harness) execute(request emit.Request, override map[string]string) emit.RunResult {
	result := emit.Run(h.ctx, h.m, request.Version, request.Bytes, emit.RunOptions{
		Root: h.ws.root, Timeout: h.opts.Timeout, Environ: h.opts.Environ, Override: override,
	})
	// Only a run that started the emitter's process counts: one that could
	// not (command[0] not found) is EMIT-PROCESS without the emitter.
	if result.Started {
		h.ran = true
	}
	for _, d := range result.Diagnostics {
		if d.Code == "EMIT-ENV-UNSET" && !slices.Contains(h.report.Diagnostics, d) {
			h.report.Diagnostics = append(h.report.Diagnostics, d)
		}
	}
	return result
}

// runAll is run 1: the emitter on each request, as `jpack emit` runs it,
// with the files of every run gathered under the host's rule that two runs
// may not give one path.
func (h *harness) runAll(run *vectorRun) {
	paths := emit.NewPathSet()
	failed := false
	for _, request := range run.requests {
		result := h.execute(request, nil)
		run.results = append(run.results, result)
		if result.Response == nil {
			failed = true
			continue
		}
		for i, file := range result.Response.Files {
			if paths.Add(file.Path) {
				run.crossRun = append(run.crossRun, emit.NewDiagnostic("EMIT-RESPONSE", emit.Location{
					Document: emit.DocumentResponse, Environment: request.Environment, Pointer: "/files/" + strconv.Itoa(i) + "/path",
				}))
				continue
			}
			run.files = append(run.files, file)
		}
	}
	if failed || len(run.crossRun) > 0 {
		run.files = nil
		return
	}
	slices.SortFunc(run.files, func(a, b emit.File) int { return strings.Compare(a.Path, b.Path) })
}

// succeeded says whether every run gave a response and no two gave one path.
func (run *vectorRun) succeeded() bool {
	if run.refused != nil || len(run.crossRun) > 0 {
		return false
	}
	for _, result := range run.results {
		if result.Response == nil {
			return false
		}
	}
	return true
}

// errors are the vector's error diagnostics, the host's and the emitter's,
// each located in its environment.
func (run *vectorRun) errors() ([]emit.Diagnostic, []emit.EmitterDiagnostic) {
	var host []emit.Diagnostic
	var emitter []emit.EmitterDiagnostic
	for _, d := range run.refused {
		if d.Severity == emit.SeverityError {
			host = append(host, d)
		}
	}
	for i, result := range run.results {
		environment := run.requests[i].Environment
		for _, d := range result.Diagnostics {
			if d.Severity == emit.SeverityError {
				d.Location.Environment = environment
				host = append(host, d)
			}
		}
		for _, d := range result.EmitterDiagnostics {
			if d.Severity == emit.SeverityError {
				d.Environment = environment
				emitter = append(emitter, d)
			}
		}
	}
	host = append(host, run.crossRun...)
	return host, emitter
}

// golden is check 1. A vector of expected/ passes when every run succeeded
// and its files equal expected/; an expected-failure vector passes when the
// run failed with the code it names, the host's or the emitter's. Under
// update, expected/ is first written from the run.
func (h *harness) golden(run *vectorRun, row Row) (Row, *emit.Diagnostic) {
	host, emitter := run.errors()
	if run.v.ExpectedFailure != "" {
		row.Expected = run.v.ExpectedFailure
		found := slices.ContainsFunc(host, func(d emit.Diagnostic) bool { return d.Code == run.v.ExpectedFailure }) ||
			slices.ContainsFunc(emitter, func(d emit.EmitterDiagnostic) bool { return d.Code == run.v.ExpectedFailure })
		switch {
		case found:
			row.Status = StatusPass
		case run.succeeded():
			row.Status, row.Reason = StatusFail, reasonSucceeded
		default:
			row.Status, row.Reason = StatusFail, reasonWrongFailure
			row.Diagnostics, row.EmitterDiagnostics = bounded(host), bounded(emitter)
		}
		return row, nil
	}
	if !run.succeeded() {
		row.Status, row.Reason = StatusFail, reasonRunFailed
		if h.opts.Update {
			row.Reason = reasonNotUpdated
		}
		row.Diagnostics, row.EmitterDiagnostics = bounded(host), bounded(emitter)
		return row, nil
	}
	if h.opts.Update {
		// An interrupt before this point refuses the run with expected/
		// untouched; a replacement once begun is not cut short by one
		// (BeginWrite), so expected/ is never left missing.
		if !h.beginWrite() {
			h.interrupted = true
			d := vectorDiagnostic("EMIT-INTERRUPTED", run.v.Name, "")
			return row, &d
		}
		err := writeExpected(run.v.Dir, run.files)
		if h.opts.EndWrite != nil {
			h.opts.EndWrite()
		}
		if err != nil {
			d := vectorDiagnostic("EMIT-WRITE", run.v.Name+"/"+dirExpected, "")
			return row, &d
		}
		run.v.Expected = map[string]expectedFile{}
		for _, file := range run.files {
			run.v.Expected[file.Path] = expectedFile{data: file.Data, mode: file.Mode}
		}
		row.Status, row.Reason = StatusPass, reasonUpdated
		return row, nil
	}
	if differences := compareExpected(run.files, run.v.Expected); len(differences) > 0 {
		row.Status, row.Reason, row.Differences = StatusFail, reasonDifferent, bounded(differences)
		return row, nil
	}
	row.Status = StatusPass
	return row, nil
}

// beginWrite reports whether an update's write of expected/ may start: only
// while the run is not interrupted, and then as Options.BeginWrite says.
func (h *harness) beginWrite() bool {
	if h.ctx.Err() != nil {
		return false
	}
	return h.opts.BeginWrite == nil || h.opts.BeginWrite()
}

// determinism is check 2: runs 2 and 3 of every request, each with a HOME of
// its own and TZ, LANG and SOURCE_DATE_EPOCH changed, the manifest's env
// names held as the host gives them, must answer as run 1 did: the same
// response bytes, or the same refusal.
func (h *harness) determinism(run *vectorRun, row Row) (Row, *emit.Diagnostic) {
	if run.refused != nil {
		row.Status, row.Reason = StatusSkip, reasonRefused
		return row, nil
	}
	// A HOME the harness cannot make is the harness's fault, not the
	// emitter's: it stops the run, and never reads as a difference.
	fault := func() (Row, *emit.Diagnostic) {
		d := vectorDiagnostic("EMIT-HARNESS-BOUND", run.v.Name, "")
		return row, &d
	}
	differing := map[int]bool{}
	for _, d := range determinismRuns {
		base, err := makeTemp("", "jpack-harness-home-")
		if err != nil {
			return fault()
		}
		home := filepath.Join(base, d.home)
		if err := os.MkdirAll(home, 0o700); err != nil {
			removeTree(base)
			return fault()
		}
		override := determinismOverride(runtime.GOOS, h.m.Env, home, d.tz, d.lang, d.epoch)
		for i, request := range run.requests {
			if !sameOutcome(run.results[i], h.execute(request, override)) {
				differing[d.run] = true
			}
		}
		removeTree(base)
	}
	if len(differing) == 0 {
		row.Status = StatusPass
		return row, nil
	}
	row.Status, row.Reason = StatusFail, reasonNondeterminism
	for _, d := range determinismRuns {
		if differing[d.run] {
			row.Runs = append(row.Runs, d.run)
		}
	}
	return row, nil
}

// windowsHomes are the variables a program on Windows finds its home and
// its application data by, where HOME is not read: Go's os.UserHomeDir reads
// USERPROFILE, os.UserConfigDir APPDATA and os.UserCacheDir LOCALAPPDATA.
var windowsHomes = []string{"USERPROFILE", "APPDATA", "LOCALAPPDATA"}

// determinismOverride is the environment a determinism run changes: HOME,
// TZ, LANG and SOURCE_DATE_EPOCH, and on Windows each of windowsHomes moved
// to the run's HOME too, unless the manifest's env names it, which is held
// as the host gives it. Run 1 sets none of windowsHomes, as it sets no TZ.
func determinismOverride(goos string, env []string, home, tz, lang, epoch string) map[string]string {
	override := map[string]string{"HOME": home, "TZ": tz, "LANG": lang, "SOURCE_DATE_EPOCH": epoch}
	if goos != "windows" {
		return override
	}
	for _, name := range windowsHomes {
		if !slices.ContainsFunc(env, func(listed string) bool { return strings.EqualFold(listed, name) }) {
			override[name] = home
		}
	}
	return override
}

// makeTemp makes the base directory of a determinism run's HOME; a test
// makes it fail.
var makeTemp = os.MkdirTemp

// sameOutcome says whether two runs of one request answered alike: both with
// the same response bytes, or both refused with the same diagnostics.
func sameOutcome(a, b emit.RunResult) bool {
	if (a.Response == nil) != (b.Response == nil) {
		return false
	}
	if a.Response != nil {
		return bytes.Equal(a.ResponseBytes, b.ResponseBytes)
	}
	errorsOf := func(diagnostics []emit.Diagnostic) []emit.Diagnostic {
		var out []emit.Diagnostic
		for _, d := range diagnostics {
			if d.Severity == emit.SeverityError {
				out = append(out, d)
			}
		}
		return out
	}
	emitterOf := func(diagnostics []emit.EmitterDiagnostic) []emit.EmitterDiagnostic {
		if len(diagnostics) == 0 {
			return nil
		}
		return diagnostics
	}
	return slices.Equal(errorsOf(a.Diagnostics), errorsOf(b.Diagnostics)) &&
		reflect.DeepEqual(emitterOf(a.EmitterDiagnostics), emitterOf(b.EmitterDiagnostics))
}

// pathSafety is check 3 for a vector: every response passed the host's §3
// rules, which emit.Run applies to it whole. A response that failed only by
// the error diagnostics the emitter reported is of its form. A vector that
// expects EMIT-RESPONSE passes when the host refused its response.
func pathSafety(run *vectorRun, row Row) Row {
	if run.refused != nil {
		row.Status, row.Reason = StatusSkip, reasonRefused
		return row
	}
	var unsafe []emit.Diagnostic
	read := false
	for i, result := range run.results {
		if result.Response != nil {
			read = true
			continue
		}
		reported := 0
		for _, d := range result.EmitterDiagnostics {
			if d.Severity == emit.SeverityError {
				reported++
			}
		}
		var findings []emit.Diagnostic
		atDiagnostic := 0
		process := false
		for _, d := range result.Diagnostics {
			switch {
			case d.Code == "EMIT-PROCESS":
				process = true
			case d.Code == "EMIT-RESPONSE":
				if isDiagnosticPointer(d.Location.Pointer) {
					atDiagnostic++
				}
				d.Location.Environment = run.requests[i].Environment
				findings = append(findings, d)
			}
		}
		if process {
			continue
		}
		read = true
		// An error diagnostic fails the run at its own pointer; a response
		// whose every finding is one of those is of its form.
		if len(findings) == atDiagnostic && atDiagnostic == reported {
			continue
		}
		unsafe = append(unsafe, findings...)
	}
	unsafe = append(unsafe, run.crossRun...)
	switch {
	case len(unsafe) > 0 && run.v.ExpectedFailure == "EMIT-RESPONSE":
		row.Status, row.Reason = StatusPass, reasonRefusedAsMeant
	case len(unsafe) > 0:
		row.Status, row.Reason, row.Diagnostics = StatusFail, reasonUnsafe, bounded(unsafe)
	case !read:
		row.Status, row.Reason = StatusSkip, reasonNoResponse
	default:
		row.Status = StatusPass
	}
	return row
}

// isDiagnosticPointer says whether a pointer is /diagnostics/N, where the
// host fails a response for an error diagnostic it reports.
func isDiagnosticPointer(pointer string) bool {
	rest, ok := strings.CutPrefix(pointer, "/diagnostics/")
	if !ok || rest == "" {
		return false
	}
	for _, c := range rest {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// strayWrites is check 6: what the emitter's runs of this vector changed in
// the repository copy, which is then made afresh.
func (h *harness) strayWrites(run *vectorRun, row Row) (Row, *emit.Diagnostic) {
	switch {
	case run.refused != nil:
		row.Status, row.Reason = StatusSkip, reasonRefused
		return row, nil
	case h.ws.fallback:
		row.Status, row.Reason = StatusSkip, reasonNoCopy
		return row, nil
	}
	changes, failure := h.ws.strayWrites()
	switch {
	case len(changes) > 0:
		row.Status, row.Reason, row.StrayWrites = StatusFail, reasonStray, changes
	case failure == nil:
		row.Status = StatusPass
	}
	return row, failure
}
