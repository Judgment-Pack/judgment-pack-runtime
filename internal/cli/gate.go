package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// The outcome gate (ADR-0092) turns an evaluation, or a batch of them, into a
// check a CI step can act on: --require-outcome names the outcomes a run may
// end in, and the exit code says whether every disposition is one of them.
//
//	0  every item produced a disposition, and every disposition is an outcome
//	   in the allowed set
//	1  every item produced a disposition, and at least one is an outcome
//	   outside the set or not-applicable: a finding
//	2  the gate could not judge: an item was unresolved or refused, an input
//	   could not be read, a record or a result could not be written, the gate's
//	   own arguments were refused, or a batch held no item (unless
//	   --allow-empty)
//
// A command line that cannot be parsed exits 3, as every command does, since
// the gate was never read; a CI step treats every exit but 0 and 1 as "the
// check did not judge". Without --require-outcome nothing here runs and every
// exit code is the one ADR-0085 and the command's own text give.

const (
	gateExitPass  = 0
	gateExitFail  = 1
	gateExitError = 2

	// gateSummaryVersion is the version of the summary document's shape. The
	// gate is under the registry line's 1.0 promise: members are added, never
	// removed or changed in meaning, within one major version.
	gateSummaryVersion = "1"

	gateInvocationCode = "JPS-INVOCATION-REQUIRE-OUTCOME"
	// gateDisplayLimit bounds the failing items standard error names; the
	// summary file names every one.
	gateDisplayLimit = 20
)

// gate is one run's outcome gate: the allowed set, the inputs it covers, and
// what the run's items came to. A nil *gate is a run without one, and every
// method is a no-op on it.
type gate struct {
	command     string
	allowed     map[string]bool
	order       []string
	allowEmpty  bool
	summaryPath string

	packID      string
	packVersion string
	inputs      []gateInput
	batch       *gateHasher

	items     int
	decisions int
	failing   []gateCase
	judged    bool // some item could not be judged: unresolved or refused
}

// gateInput names one input the summary's digests cover.
type gateInput struct {
	Covers    string `json:"covers"`
	Path      string `json:"path,omitempty"`
	PackIDRef string `json:"packIdRef,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
	ReadToEnd *bool  `json:"readToEnd,omitempty"`
}

// gateCase is one item that did not pass, named by its line and id and by
// pack-declared outcome ids only: never by a fact or evidence value.
type gateCase struct {
	Line      int64   `json:"line,omitempty"`
	ID        *string `json:"id,omitempty"`
	Kind      string  `json:"kind"`
	OutcomeID string  `json:"outcomeId,omitempty"`
	ExitCode  int     `json:"exitCode,omitempty"`
}

// gateSummary is the document --gate-summary writes.
type gateSummary struct {
	Version         string      `json:"gateSummaryVersion"`
	Command         string      `json:"command"`
	Verdict         string      `json:"verdict"`
	ExitCode        int         `json:"exitCode"`
	RunExitCode     int         `json:"runExitCode"`
	AllowedOutcomes []string    `json:"allowedOutcomes"`
	PackID          string      `json:"packId,omitempty"`
	PackVersion     string      `json:"packVersion,omitempty"`
	Inputs          []gateInput `json:"inputs"`
	Items           int         `json:"items"`
	Decisions       int         `json:"decisions"`
	Failing         []gateCase  `json:"failing"`
}

// gateFlags are the gate's command-line values, as given.
type gateFlags struct {
	requireOutcome string
	allowEmpty     bool
	summaryPath    string
}

// newGate holds the gate's own arguments to their shape, before the
// filesystem is consulted. It returns a nil gate when --require-outcome was
// not given, and the sentence that refuses the invocation when the arguments
// are not ones the gate takes.
func newGate(command string, given gateFlags, requireChanged, allowEmptyChanged, summaryChanged, batch bool) (*gate, string) {
	if !requireChanged {
		switch {
		case allowEmptyChanged:
			return nil, "--allow-empty is an outcome-gate flag and is taken only with --require-outcome."
		case summaryChanged:
			return nil, "--gate-summary is an outcome-gate flag and is taken only with --require-outcome."
		}
		return nil, ""
	}
	if allowEmptyChanged && !batch {
		return nil, "--allow-empty lets a batch with no item pass the gate and is taken only with --batch."
	}
	if summaryChanged {
		if problem := summaryPathProblem(given.summaryPath); problem != "" {
			return nil, problem
		}
	}
	g := &gate{command: command, allowed: map[string]bool{}, allowEmpty: given.allowEmpty, summaryPath: given.summaryPath}
	for _, id := range strings.Split(given.requireOutcome, ",") {
		if id == "" {
			return nil, "--require-outcome takes a comma-separated list of outcome ids, none of them empty."
		}
		if g.allowed[id] {
			return nil, fmt.Sprintf("--require-outcome names the outcome %q twice.", id)
		}
		g.allowed[id] = true
		g.order = append(g.order, id)
	}
	return g, ""
}

// summaryPathProblem is the sentence refusing a --gate-summary path, or "".
func summaryPathProblem(path string) string {
	switch {
	case path == "":
		return "--gate-summary needs a file path."
	case path == "-":
		return "--gate-summary cannot be standard output, which carries the run's results; pass a file path."
	case strings.Contains(path, "://") || fssecure.IsRemotePath(path):
		return "URL and remote filesystem paths are not supported for --gate-summary; use a local file."
	}
	return ""
}

// checkPack refuses an allowed outcome the pack does not declare, before any
// facts are read or anything is evaluated or recorded: a misspelt id would
// otherwise fail every run, or pass none, for a reason the run never names.
// A pack whose outcomes cannot be read is left to the engine, which refuses
// it; the gate then exits 2, and had the engine admitted it, every disposition
// would still be judged against the set, so the check fails closed.
func (g *gate) checkPack(pack []byte, oversized bool, packArgument, packID string) string {
	if g == nil {
		return ""
	}
	input := gateInput{Covers: "pack"}
	if packID != "" {
		input.PackIDRef = packID
	} else {
		input.Path = packArgument
	}
	if !oversized && pack != nil {
		input.SHA256 = sha256Hex(pack)
	}
	g.inputs = append(g.inputs, input)
	if oversized || pack == nil {
		return ""
	}
	// Members are matched by their exact names, as the engine reads them;
	// encoding/json's struct decoding would also match "Outcomes" or "ID".
	var document map[string]json.RawMessage
	var outcomes []map[string]json.RawMessage
	if json.Unmarshal(pack, &document) != nil || json.Unmarshal(document["outcomes"], &outcomes) != nil {
		return ""
	}
	known := map[string]bool{}
	for _, outcome := range outcomes {
		var id string
		if json.Unmarshal(outcome["id"], &id) == nil {
			known[id] = true
		}
	}
	unknown := []string{}
	for _, id := range g.order {
		if !known[id] {
			unknown = append(unknown, fmt.Sprintf("%q", display.Sanitize(id)))
		}
	}
	if len(unknown) == 0 {
		return ""
	}
	message := fmt.Sprintf("--require-outcome names %s, which the pack does not declare as an outcome; the gate is refused before anything is evaluated.", strings.Join(unknown, ", "))
	for _, id := range g.order {
		if !known[id] && (id == "not-applicable" || id == "unresolved") {
			message += " not-applicable and unresolved are kinds of disposition, not outcome ids: not-applicable is always a finding (exit 1) and unresolved never passes (exit 2)."
			break
		}
	}
	return message
}

// document notes one whole input document the summary's digests cover.
func (g *gate) document(covers, path string, data []byte) {
	if g == nil || path == "" {
		return
	}
	input := gateInput{Covers: covers, Path: path}
	if data != nil {
		input.SHA256 = sha256Hex(data)
	}
	g.inputs = append(g.inputs, input)
}

// batchInput hashes the batch input as the batch reads it, so the digest is
// of the bytes evaluated, never of a second read of the path.
func (g *gate) batchInput(path string, input io.Reader) io.Reader {
	if g == nil {
		return input
	}
	g.batch = &gateHasher{path: path, reader: input, sum: sha256.New()}
	return g.batch
}

// observe judges one item: a disposition against the set, or the exit code
// of an item that produced none.
func (g *gate) observe(line int64, id *string, evaluated *result.Evaluation, exitCode int) {
	if g == nil {
		return
	}
	g.items++
	if evaluated == nil {
		g.judged = true
		g.failing = append(g.failing, gateCase{Line: line, ID: id, Kind: "error", ExitCode: exitCode})
		return
	}
	g.decisions++
	if g.packID == "" {
		g.packID, g.packVersion = evaluated.PackID, evaluated.PackVersion
	}
	disposition := evaluated.Disposition
	switch disposition.Kind {
	case "outcome":
		if g.allowed[disposition.OutcomeID] {
			return
		}
		g.failing = append(g.failing, gateCase{Line: line, ID: id, Kind: "outcome", OutcomeID: disposition.OutcomeID})
	case "not-applicable":
		g.failing = append(g.failing, gateCase{Line: line, ID: id, Kind: "not-applicable"})
	default:
		// unresolved, and any kind a later contract adds: the gate does not
		// pass what it was not built to judge.
		g.judged = true
		g.failing = append(g.failing, gateCase{Line: line, ID: id, Kind: disposition.Kind})
	}
}

// finish turns the run's own outcome into the gate's exit code, writes the
// verdict to standard error and, with --gate-summary, the summary file.
func (a *App) finishGate(g *gate, err error) error {
	runCode := result.ExitSuccess
	if err != nil {
		var handled *handledExit
		if !errors.As(err, &handled) {
			// Not an exit the command chose: say what it was, as Run would,
			// and keep the gate's promise of 0, 1 or 2.
			fmt.Fprintln(a.errOut, "error:", display.Sanitize(err.Error()))
			runCode = result.ExitInvocation
		} else {
			runCode = handled.code
		}
	}
	verdict, code := "pass", gateExitPass
	switch {
	case runCode != result.ExitSuccess || g.judged:
		verdict, code = "error", gateExitError
	case g.decisions == 0 && !g.allowEmpty:
		verdict, code = "error", gateExitError
	case len(g.failing) > 0:
		verdict, code = "fail", gateExitFail
	}
	if g.batch != nil {
		readToEnd := g.batch.eof
		input := gateInput{Covers: "batch", Path: g.batch.path, ReadToEnd: &readToEnd}
		if readToEnd {
			input.SHA256 = hex.EncodeToString(g.batch.sum.Sum(nil))
		}
		g.inputs = append(g.inputs, input)
	}
	// The summary is written before the verdict is said, so a summary that
	// cannot be written never follows a "pass" on standard error.
	if !a.writeGateSummary(g, verdict, code, runCode) {
		return &handledExit{code: gateExitError}
	}
	a.reportGate(g, verdict, runCode)
	if code == gateExitPass {
		return nil
	}
	return &handledExit{code: code}
}

// writeGateSummary writes --gate-summary, when given, and says whether the
// file now holds this run's verdict. It replaces the file by a rename from a
// temporary file beside it, so a reader never sees half a summary, and never
// writes through a symbolic link.
func (a *App) writeGateSummary(g *gate, verdict string, code, runCode int) bool {
	if g.summaryPath == "" {
		return true
	}
	failing, inputs, allowed := g.failing, g.inputs, g.order
	if failing == nil {
		failing = []gateCase{}
	}
	if inputs == nil {
		inputs = []gateInput{}
	}
	if allowed == nil {
		allowed = []string{}
	}
	summary := gateSummary{
		Version: gateSummaryVersion, Command: g.command, Verdict: verdict, ExitCode: code, RunExitCode: runCode,
		AllowedOutcomes: allowed, PackID: g.packID, PackVersion: g.packVersion, Inputs: inputs,
		Items: g.items, Decisions: g.decisions, Failing: failing,
	}
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(summary); err != nil || replaceFile(g.summaryPath, encoded.Bytes()) != nil {
		fmt.Fprintln(a.errOut, "error: the gate summary could not be written to", display.Sanitize(g.summaryPath)+"; the gate did not judge.")
		return false
	}
	return true
}

// replaceFile replaces path with data through a temporary file in the same
// directory, refusing a path that is a symbolic link.
func replaceFile(path string, data []byte) error {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("the summary path is a symbolic link")
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".gate-summary-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// refusedGateSummary writes the summary of a gate whose own arguments were
// refused, so a summary file left by an earlier run never stands for this
// one. Nothing was judged: the allowed list is empty and no input is named.
func (a *App) refusedGateSummary(command, summaryPath string) {
	if summaryPathProblem(summaryPath) != "" {
		return
	}
	_ = a.writeGateSummary(&gate{command: command, summaryPath: summaryPath}, "error", gateExitError, result.ExitInvocation)
}

// reportGate writes the verdict to standard error: the run's results keep
// standard output to themselves. Items are named by line, id and outcome id
// only.
func (a *App) reportGate(g *gate, verdict string, runCode int) {
	allowed := strings.Join(sanitizedAll(g.order), ", ")
	switch {
	case runCode != result.ExitSuccess:
		fmt.Fprintf(a.errOut, "gate: error: the run exited %d, so the gate did not judge it (allowed: %s)\n", runCode, allowed)
	case verdict == "error" && g.decisions == 0 && !g.judged:
		fmt.Fprintf(a.errOut, "gate: error: no decision was made, so the gate did not judge (pass --allow-empty to let an empty batch pass)\n")
	case verdict == "error":
		fmt.Fprintf(a.errOut, "gate: error: %d of %d items could not be judged (allowed: %s)\n", g.unjudged(), g.items, allowed)
	case verdict == "fail":
		fmt.Fprintf(a.errOut, "gate: fail: %d of %d decisions are outside the allowed outcomes (%s)\n", len(g.failing), g.decisions, allowed)
	default:
		fmt.Fprintf(a.errOut, "gate: pass: %d of %d decisions are in the allowed outcomes (%s)\n", g.decisions, g.decisions, allowed)
	}
	for index, failing := range g.failing {
		if index == gateDisplayLimit {
			fmt.Fprintf(a.errOut, "gate: and %d more\n", len(g.failing)-gateDisplayLimit)
			break
		}
		fmt.Fprintf(a.errOut, "gate: %s: %s\n", failing.where(), failing.what())
	}
}

func (g *gate) unjudged() int {
	count := 0
	for _, failing := range g.failing {
		if failing.Kind != "outcome" && failing.Kind != "not-applicable" {
			count++
		}
	}
	return count
}

func (c gateCase) where() string {
	if c.Line == 0 {
		return "the evaluation"
	}
	if c.ID != nil {
		return fmt.Sprintf("line %d (id %q)", c.Line, display.Sanitize(*c.ID))
	}
	return fmt.Sprintf("line %d", c.Line)
}

func (c gateCase) what() string {
	switch c.Kind {
	case "outcome":
		return fmt.Sprintf("outcome %q", display.Sanitize(c.OutcomeID))
	case "error":
		return fmt.Sprintf("no disposition (exit %d)", c.ExitCode)
	}
	return display.Sanitize(c.Kind)
}

func sanitizedAll(values []string) []string {
	out := make([]string, len(values))
	for index, value := range values {
		out[index] = display.Sanitize(value)
	}
	return out
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// gateHasher digests a reader's bytes as they are read and notes whether the
// reader was read to its end.
type gateHasher struct {
	path   string
	reader io.Reader
	sum    hash.Hash
	eof    bool
}

func (h *gateHasher) Read(p []byte) (int, error) {
	n, err := h.reader.Read(p)
	h.sum.Write(p[:n])
	if errors.Is(err, io.EOF) {
		h.eof = true
	}
	return n, err
}
