// Package emit is the registry emitter host of spec RFC 0036 §1–§4, as the
// draft registry companion (spec/registry.md §11) restates it (ADR-0098): an
// emitter manifest, the request a host forms from a registry, the response an
// emitter answers with, and the host that runs an emitter under bounds and
// either writes the files it answers with into the paths it owns or compares
// them with the files on disk.
//
// An emitter is a process. It reads one request on standard input and writes
// one response on standard output; it never touches the repository itself.
// The host refuses an unsafe or unowned path, a symbolic link at or above an
// owned path, and a response over its bounds, before it changes anything. It
// is not a sandbox: an emitter is code the repository chose to run.
package emit

import (
	"slices"
	"strconv"
	"time"
)

// Selectors this host reads and writes, each exact (companion §2.1).
const (
	ManifestVersion = "1.0.0"
	// ReportVersion is the selector of the host's own report. The RFC defines
	// no host report, so it is this runtime's, provisional (VERSIONING.md).
	ReportVersion = "1.0.0"
)

// requestVersions maps each request selector this host writes to the
// resolved format and the facts contract format it carries (RFC 0036 §2).
var requestVersions = map[string]struct{ resolved, contract string }{
	"1.0.0": {resolved: "1.0.0", contract: "1.0.0"},
}

// responseVersions are the response selectors this host accepts. A response
// is accepted when it is one of them and its major and minor do not exceed
// the request's.
var responseVersions = []string{"1.0.0"}

// The response bounds of RFC 0036 §3, and the host's own.
const (
	// MaxResponseBytes bounds what the host reads of standard output.
	MaxResponseBytes = 64 << 20
	// MaxRequestBytes bounds one request's RFC 8785 bytes, as the response
	// is bounded: the host refuses to form a larger one (EMIT-REQUEST), and
	// the harness admits no larger given request.
	MaxRequestBytes = 64 << 20
	// MaxFiles bounds a response's files.
	MaxFiles = 10000
	// MaxFileBytes bounds one file's bytes, after base64 decoding.
	MaxFileBytes = 16 << 20
	// MaxPathBytes bounds one path, in bytes.
	MaxPathBytes = 512
	// MaxStderrBytes is the most of standard error the host keeps; the rest
	// is counted and discarded.
	MaxStderrBytes = 64 << 10
	// DefaultTimeout is the wall-clock limit on one emitter run.
	DefaultTimeout = 120 * time.Second

	// MaxManifestBytes bounds an emitter manifest, as a kind declaration is
	// bounded (RFC 0034 §1). The RFC names no bound; this is the host's.
	MaxManifestBytes = 256 << 10
	// MaxOptionsBytes bounds an options document. The host's.
	MaxOptionsBytes = 1 << 20
	// MaxReportedDiagnostics bounds the emitter diagnostics the report lists,
	// as the validation report is bounded (ADR-0093). The host's.
	MaxReportedDiagnostics = 1000
	// MaxListedPaths bounds the paths a warning that lists paths names, one
	// diagnostic each (ListPaths). The host's.
	MaxListedPaths = 100
)

// The bounds as the code applies them; a test lowers them.
var (
	maxResponseBytes = MaxResponseBytes
	maxFiles         = MaxFiles
	maxFileBytes     = MaxFileBytes
	maxStderrBytes   = MaxStderrBytes
	maxReported      = MaxReportedDiagnostics
)

// Severity of a diagnostic.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

// Documents a host diagnostic's location can name.
const (
	DocumentManifest   = "manifest"
	DocumentOptions    = "options"
	DocumentRegistry   = "registry"
	DocumentKind       = "kind"
	DocumentContract   = "contract"
	DocumentRequest    = "request"
	DocumentResponse   = "response"
	DocumentRepository = "repository"
)

type codeInfo struct {
	severity string
	message  string
}

// Codes is the closed list of the host's codes, each with its severity and
// fixed message (the last diagnostic of a capped listing adds its count to
// it, ListPaths). The companion (§12) names EMIT-REQUEST-VERSION,
// EMIT-KIND-RANGE, EMIT-REQUEST, EMIT-PROCESS, EMIT-RESPONSE, EMIT-DEPRECATED
// and the harness's EMIT-STRAY-WRITE. The others are this runtime's, for the host
// refusals the companion leaves without a code (§17), provisional until the
// companion's host rows name them (VERSIONING.md).
var Codes = map[string]codeInfo{
	// The companion's.
	"EMIT-REQUEST-VERSION": {SeverityError, "The host writes no request version the manifest lists, or none that carries the registry's resolved format."},
	"EMIT-KIND-RANGE":      {SeverityError, "A pinned kind the manifest reads has a version outside the manifest's range."},
	"EMIT-REQUEST":         {SeverityError, "The request could not be formed: the registry is not valid, its pins and declarations do not match, a facts contract is refused or exceeds its bounds, two contracts share an id and version, or the request exceeds its size bound."},
	"EMIT-PROCESS":         {SeverityError, "The emitter could not be started, exited non-zero, or did not finish within the time limit, or the run was interrupted before it wrote anything."},
	"EMIT-RESPONSE":        {SeverityError, "The emitter's output is not one valid response: its form, a path, a bound, or an error diagnostic it reported."},
	"EMIT-DEPRECATED":      {SeverityWarning, "The emitter's manifest is deprecated."},
	// The conformance harness's (ADR-0099).
	"EMIT-STRAY-WRITE": {SeverityError, "The emitter created, changed or removed a file of the repository copy itself, outside the response."},

	// This runtime's, provisional.
	"EMIT-MANIFEST":         {SeverityError, "The emitter manifest is not admissible JSON, or a member is missing, not defined, or not of its form."},
	"EMIT-OWNS-OVERLAP":     {SeverityError, "An owns glob of the manifest can match a path an owns glob of another manifest given can match."},
	"EMIT-KIND-ID":          {SeverityError, "The registry pins a kind of a name the manifest reads with another kind id."},
	"EMIT-OPTIONS":          {SeverityError, "The options document is not admissible JSON, or does not satisfy the manifest's options schema."},
	"EMIT-ENV-UNSET":        {SeverityWarning, "A variable the manifest's env lists is not set in the host's environment, so the emitter does not receive it."},
	"EMIT-LINK":             {SeverityError, "A symbolic link, or another reparse point such as a Windows junction, is at or above an owned path; the host does not follow it."},
	"EMIT-PATH-UNREADABLE":  {SeverityError, "A path could not be read: an input, or an owned path that is not a regular file or directory or could not be listed or read."},
	"EMIT-WRITE":            {SeverityError, "A file could not be written or deleted, or a path the response names is held by a directory or by a file the manifest does not own."},
	"EMIT-REPORT-TRUNCATED": {SeverityWarning, "The report lists the first 1000 diagnostics of a list, the host's or the emitter's, and there were more."},
	"EMIT-STAGING-LEFTOVER": {SeverityWarning, "A staging file (.jpack-emit-*) that an earlier run left in a directory the host writes to is not owned, so the host does not delete it; remove it by hand."},

	// The conformance harness's, provisional (ADR-0099).
	"EMIT-VECTOR":        {SeverityError, "The vectors are not of the harness's layout: harness.json or its level, a vector's files, no vectors at all, or a vector past a bound."},
	"EMIT-HARNESS-BOUND": {SeverityError, "A check could not be completed: a bound of the harness (the repository copy, the registry with the synthetic kind, or the references scan), a fresh HOME for a determinism run that could not be made, or the run was interrupted."},
	"EMIT-LINK-OMITTED":  {SeverityWarning, "A symbolic link or junction that leads out of the repository was left out of the copy the emitter runs in: git does not track it, and no owned path and no path to the vectors could name it."},
}

// Location says where a host diagnostic applies. Pointer is a JSON Pointer
// into Document; Path is a repository path the host read or would write;
// Variable is the name of an env variable. None carries a value: a response
// path that the path rules refuse is located by its pointer, never written.
type Location struct {
	Document    string `json:"document"`
	Manifest    int    `json:"manifest,omitempty"`
	Environment string `json:"environment,omitempty"`
	Pointer     string `json:"pointer"`
	Path        string `json:"path,omitempty"`
	Variable    string `json:"variable,omitempty"`
}

// Diagnostic is one host finding, with a fixed message.
type Diagnostic struct {
	Code     string   `json:"code"`
	Severity string   `json:"severity"`
	Location Location `json:"location"`
	Message  string   `json:"message"`
}

// NewDiagnostic returns the diagnostic of a listed code at a location, for a
// caller that refuses an input before the host runs.
func NewDiagnostic(code string, location Location) Diagnostic { return diagnostic(code, location) }

func diagnostic(code string, location Location) Diagnostic {
	info, ok := Codes[code]
	if !ok {
		panic("emit: unlisted code " + code)
	}
	return Diagnostic{Code: code, Severity: info.severity, Location: location, Message: info.message}
}

// ListPaths returns the diagnostics of a warning that lists paths of one
// document (EMIT-STAGING-LEFTOVER, EMIT-LINK-OMITTED): one at each of the
// first MaxListedPaths paths in sorted order, each with the code's fixed
// message, and, when there are more, one more of the code at the document
// alone. That one gives the count of the paths not listed, the one number a
// message carries: the report has no member for counts, and none is added.
func ListPaths(code, document string, paths []string) []Diagnostic {
	sorted := slices.Clone(paths)
	slices.Sort(sorted)
	listed := min(len(sorted), MaxListedPaths)
	out := make([]Diagnostic, 0, listed+1)
	for _, path := range sorted[:listed] {
		out = append(out, diagnostic(code, Location{Document: document, Path: path}))
	}
	if more := len(sorted) - listed; more > 0 {
		d := diagnostic(code, Location{Document: document})
		d.Message += " The report lists the first " + strconv.Itoa(MaxListedPaths) + " by path; " + strconv.Itoa(more) + " more are not listed."
		out = append(out, d)
	}
	return out
}

// hasError says whether any diagnostic is an error.
func hasError(diagnostics []Diagnostic) bool {
	for _, d := range diagnostics {
		if d.Severity == SeverityError {
			return true
		}
	}
	return false
}
