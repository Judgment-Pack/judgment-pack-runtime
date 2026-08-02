package project

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

// The named checks packs validate applies, in the order it applies them.
const (
	CheckPathInsideRoot = "path-inside-root"
	CheckDocument       = "document-validation"
	CheckExpectedVer    = "expected-version"
	CheckFilename       = "filename"
	CheckHintKeys       = "hint-keys"
	CheckMatrix         = "matrix"
	// CheckAuditDirInsideRoot is the one check that is about the configuration
	// itself rather than about a pack: the audit directory is the only declared
	// path no pack entry owns (ADR-0018).
	CheckAuditDirInsideRoot = "audit-dir-inside-root"
)

// documentChecks are the checks that read the pack document, in order. They are
// named once so the branches that cannot run them skip exactly the same set.
var documentChecks = []string{CheckDocument, CheckExpectedVer, CheckFilename, CheckHintKeys, CheckMatrix}

// filenameConvention recognizes the optional <decision-id>-<semver>.pack.json
// filename.
//
// It is a convention and never a requirement: a file named anything else is not
// a defect and its check is skipped. What the convention buys is that when a
// filename *does* carry an id and a version, those two are cross-checked against
// the configuration key and the pack document's version member — so a file
// renamed during a version bump, or copied to a new decision and left with the
// old name, is caught instead of quietly disagreeing with the document it names.
// The filename is a third reference to the pack's one statement of identity, and
// this is what keeps it from becoming a fourth place a version is declared.
var filenameConvention = regexp.MustCompile(`^([a-z][a-z0-9]*(?:-[a-z0-9]+)*)-((?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*))\.pack\.json$`)

// Validate checks the project's packs and reports every check it applied.
//
// The configuration itself was already checked: Load holds it to the embedded
// schema and to a configVersion this runtime accepts, and a configuration that
// fails either never reaches here. What this adds is everything that depends on
// the files the configuration points at.
//
// An empty id runs every configured pack; a named one runs that pack alone and
// an unknown name is refused rather than reported as zero results, because a
// typo in a CI invocation that silently checks nothing is the failure mode this
// command exists to prevent.
func (p *Project) Validate(engine *validation.Engine, id, command string) (result.PackValidation, *Failure) {
	selected, failure := p.selection(id)
	if failure != nil {
		return result.PackValidation{}, failure
	}
	output := result.PackValidation{
		OutputVersion: result.OutputVersion,
		Tool:          result.CurrentTool(),
		Command:       command,
		Status:        "valid",
		Kind:          result.ProjectKind,
		ConfigPath:    p.ConfigPath,
		ConfigVersion: p.Config.ConfigVersion,
		Packs:         make([]result.PackValidationEntry, 0, len(selected)),
	}
	// The configuration's own declared path, checked before any pack is: an
	// audit directory that leaves the project would otherwise be a report of
	// full validity followed by an unexplainable refusal at the first decision.
	if p.Config.Audit != nil {
		check := p.checkAuditDir()
		output.Checks = append(output.Checks, check)
		if check.Status == result.PackCheckFailed {
			output.Status = "invalid"
		}
	}
	for _, packID := range selected {
		entry := p.validatePack(engine, packID, p.Config.Packs[packID])
		output.Summary.Total++
		if entry.Status == "valid" {
			output.Summary.Passed++
		} else {
			output.Summary.Failed++
			output.Status = "invalid"
		}
		output.Packs = append(output.Packs, entry)
	}
	return output, nil
}

// checkAuditDir reports whether the declared audit directory stays inside the
// project.
//
// It is decided against the project's own handle, exactly as a pack path's
// containment is, so the check and the write cannot be about two different
// directories. It uses the directory form of the question rather than the file
// form: a pack path's final component is never resolved here because the open
// refuses a final symlink whatever it points at, while an audit directory's
// final component becomes an intermediate component of the trail written under
// it — so `audit -> ../outside` is an escape the write will refuse and this
// check must refuse too, not a name the check may leave alone.
//
// A contained directory that is not there yet passes: the first record makes
// it, and reporting "missing" for a directory the runtime is about to create
// would be a named check asserting a defect that does not exist.
func (p *Project) checkAuditDir() result.PackCheck {
	check := result.PackCheck{Name: CheckAuditDirInsideRoot, Status: result.PackCheckPassed}
	switch err := p.ContainsDir(p.Config.Audit.Dir); {
	case errors.Is(err, fssecure.ErrOutsideRoot):
		check.Status = result.PackCheckFailed
		check.Detail = fmt.Sprintf("The audit directory %q resolves outside the configuration's own directory, which no configured path may.", display.Sanitize(p.Config.Audit.Dir))
	case err != nil:
		check.Status = result.PackCheckFailed
		check.Detail = fmt.Sprintf("The audit directory %q is inside the configuration's own directory but cannot be used as one: %s", display.Sanitize(p.Config.Audit.Dir), display.Sanitize(err.Error()))
	}
	return check
}

// validatePack applies every check to one configured pack.
//
// A path that escapes the project root short-circuits the rest: every later
// check reads that file, and reporting five more failures about a file this
// runtime refuses to open would say the same thing five times.
//
// Containment is decided against the project's own directory handle — the same
// handle the read below is bounded by — so the named check reports the
// containment truth rather than the lexical half of it. A path that escapes only
// through a symlinked component would otherwise be reported as having passed
// containment and then failed to be read, which is a named check asserting the
// opposite of what happened.
func (p *Project) validatePack(engine *validation.Engine, id string, entry Pack) result.PackValidationEntry {
	report := result.PackValidationEntry{ID: id, Path: entry.Path, Status: "valid", Checks: []result.PackCheck{}}
	add := func(name, status, detail string) {
		report.Checks = append(report.Checks, result.PackCheck{Name: name, Status: status, Detail: detail})
		if status == result.PackCheckFailed {
			report.Status = "invalid"
		}
	}

	// Only an escape fails this check. A path inside the project whose directory
	// is missing or unreadable is contained, and the read below reports it as the
	// read failure it is.
	if err := p.Contains(entry.Path); errors.Is(err, fssecure.ErrOutsideRoot) {
		add(CheckPathInsideRoot, result.PackCheckFailed, ReadFailureMessage(entry.Path, err))
		for _, name := range documentChecks {
			add(name, result.PackCheckSkipped, "The pack path was refused, so this check had nothing to read.")
		}
		return report
	}
	add(CheckPathInsideRoot, result.PackCheckPassed, "")

	data, err := p.ReadPack(entry)
	if err != nil {
		add(CheckDocument, result.PackCheckFailed, ReadFailureMessage(entry.Path, err))
		for _, name := range []string{CheckExpectedVer, CheckFilename, CheckHintKeys} {
			add(name, result.PackCheckSkipped, "The pack document could not be read, so this check had nothing to compare against.")
		}
		p.checkMatrix(entry, add)
		return report
	}
	validated, operational := engine.Validate(data, validation.Options{Through: "semantic", Limits: carrier.DefaultLimits()})
	switch {
	case operational != nil:
		add(CheckDocument, result.PackCheckFailed, display.Sanitize(operational.Message))
	case validated.Status == "valid":
		add(CheckDocument, result.PackCheckPassed, "")
	default:
		add(CheckDocument, result.PackCheckFailed, documentDetail(validated))
	}

	// The identity comes from the bytes just validated, not from a second read: a
	// report must not pair a verdict about one revision of a file with an identity
	// read from another.
	found, identityErr := identityFrom(data)
	if identityErr != nil {
		for _, name := range []string{CheckExpectedVer, CheckFilename, CheckHintKeys} {
			add(name, result.PackCheckSkipped, "The pack document did not decode, so its own identity members could not be read.")
		}
		p.checkMatrix(entry, add)
		return report
	}

	switch expectedVersionStatus(entry.ExpectedVersion, found.Version) {
	case result.PackVersionUnset:
		add(CheckExpectedVer, result.PackCheckSkipped, "The entry pins no expectedVersion.")
	case result.PackVersionMatches:
		add(CheckExpectedVer, result.PackCheckPassed, "")
	case result.PackVersionUnknown:
		add(CheckExpectedVer, result.PackCheckFailed, fmt.Sprintf("The entry pins expectedVersion %q and the pack document declares no version member to check it against.", display.Sanitize(entry.ExpectedVersion)))
	default:
		add(CheckExpectedVer, result.PackCheckFailed, fmt.Sprintf("The entry pins expectedVersion %q and the pack document declares version %q. The document is what the pack is; update the pin, or the pack.", display.Sanitize(entry.ExpectedVersion), display.Sanitize(found.Version)))
	}

	p.checkFilename(id, entry, found, add)
	checkHintKeys(entry, found, add)
	p.checkMatrix(entry, add)
	return report
}

// checkHintKeys holds a configuration's hint keys to the pack document.
//
// A hint is guidance this runtime never acts on, which is exactly why its key
// has to be checked: nothing else ever resolves it, so a misspelled key is
// handed to an agent as an authoritative instruction to gather a fact no
// condition reads or evidence no requirement declares. The schema says an
// evidence hint is keyed by a declared evidence-requirement id and a facts hint
// by the pointer a condition reads; that sentence is a cross-reference, and the
// one-statement discipline makes every cross-reference a check rather than a
// second statement.
//
// An evidence key is exact: it names a declared requirement or it does not. A
// facts key is satisfied by a pointer some condition reads, or by being an
// ancestor of one — a project that hints at a subtree an agent must gather has
// said something true about the pack, and only a key matching nothing is wrong.
func checkHintKeys(entry Pack, found identity, add func(name, status, detail string)) {
	if len(entry.Facts) == 0 && len(entry.Evidence) == 0 {
		add(CheckHintKeys, result.PackCheckSkipped, "The entry declares no hints.")
		return
	}
	problems := []string{}
	for _, key := range sortedKeys(entry.Evidence) {
		if !slices.Contains(found.EvidenceRequirements, key) {
			problems = append(problems, fmt.Sprintf("the evidence hint %q names no evidence requirement the pack declares", display.Sanitize(key)))
		}
	}
	for _, key := range sortedKeys(entry.Facts) {
		if !readsPointer(found.FactPaths, key) {
			problems = append(problems, fmt.Sprintf("the fact hint %q names a pointer no condition in the pack reads", display.Sanitize(key)))
		}
	}
	if len(problems) == 0 {
		add(CheckHintKeys, result.PackCheckPassed, "")
		return
	}
	detail := "A hint the runtime never resolves is instructions an agent follows, so its key is held to the document: " +
		strings.Join(problems, "; ") + ". Fix the key, or the pack."
	add(CheckHintKeys, result.PackCheckFailed, detail)
}

// readsPointer reports whether some condition reads this pointer, or reads a
// pointer beneath it.
func readsPointer(paths []string, key string) bool {
	for _, path := range paths {
		if path == key || strings.HasPrefix(path, key+"/") {
			return true
		}
	}
	return false
}

// sortedKeys orders one hint map, so two runs over the same configuration report
// the same problems in the same order rather than Go's map order.
func sortedKeys(declared map[string]Hint) []string {
	keys := make([]string, 0, len(declared))
	for key := range declared {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// checkFilename applies the optional filename convention.
func (p *Project) checkFilename(id string, entry Pack, found identity, add func(name, status, detail string)) {
	base := filepath.Base(filepath.FromSlash(entry.Path))
	match := filenameConvention.FindStringSubmatch(base)
	if match == nil {
		add(CheckFilename, result.PackCheckSkipped, fmt.Sprintf("The filename %q is outside the optional <decision-id>-<semver>.pack.json convention, which is never required.", display.Sanitize(base)))
		return
	}
	problems := []string{}
	if match[1] != id {
		problems = append(problems, fmt.Sprintf("names decision id %q where the configuration key is %q", display.Sanitize(match[1]), display.Sanitize(id)))
	}
	if match[2] != found.Version {
		problems = append(problems, fmt.Sprintf("names version %q where the pack document declares %q", display.Sanitize(match[2]), display.Sanitize(found.Version)))
	}
	if len(problems) == 0 {
		add(CheckFilename, result.PackCheckPassed, "")
		return
	}
	detail := "The filename follows the convention, so it is held to it: it " + problems[0]
	if len(problems) > 1 {
		detail += ", and it " + problems[1]
	}
	add(CheckFilename, result.PackCheckFailed, detail+". Rename the file, or stop following the convention.")
}

// checkMatrix checks a declared matrix for well-formedness. It does not run it:
// running is packs test, and a matrix that will not parse has to be reported by
// the command a project runs before it.
func (p *Project) checkMatrix(entry Pack, add func(name, status, detail string)) {
	if entry.Matrix == "" {
		add(CheckMatrix, result.PackCheckSkipped, "The entry declares no matrix.")
		return
	}
	matrix, err := p.LoadMatrix(entry)
	if err != nil {
		add(CheckMatrix, result.PackCheckFailed, display.Sanitize(err.Error()))
		return
	}
	add(CheckMatrix, result.PackCheckPassed, fmt.Sprintf("%d rows.", len(matrix.Cases)))
}

// documentDetail states one non-valid validation verdict in a sentence, naming
// the first diagnostic so the failure is self-sufficient — the same discipline
// the evaluator's pack-not-conformant refusal follows.
//
// An "unsupported" verdict is a failure here as much as an "invalid" one, and
// deliberately so: this command is the check a project puts in CI, and it
// answers whether every configured pack validates with this runtime as it
// stands. A pack requiring an extension this build does not support does not,
// and the detail says which verdict it was so the reason is never mistaken for a
// defect in the document.
func documentDetail(validated result.Validation) string {
	detail := fmt.Sprintf("spec validate reports %q for this document.", display.Sanitize(validated.Status))
	if len(validated.Diagnostics) > 0 {
		first := validated.Diagnostics[0]
		location := first.InstancePath
		if location == "" {
			location = "<root>"
		}
		detail += fmt.Sprintf(" First diagnostic: %s at %s: %s", display.Sanitize(first.Code), display.Sanitize(location), display.Sanitize(first.Message))
	}
	return detail
}
