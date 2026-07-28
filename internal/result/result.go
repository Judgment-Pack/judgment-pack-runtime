package result

const (
	OutputVersion = "1"
	CLIName       = "judgment-pack"
)

// CLIVersion may be replaced at build time with -ldflags once releases are approved.
var CLIVersion = "0.0.0-dev"

const (
	ExitSuccess     = 0
	ExitInvalid     = 1
	ExitUnsupported = 2
	ExitInvocation  = 3
	ExitIO          = 4
	ExitInternal    = 5
)

type Tool struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func CurrentTool() Tool {
	return Tool{Name: CLIName, Version: CLIVersion}
}

type Diagnostic struct {
	Code          string `json:"code"`
	CodeStability string `json:"codeStability"`
	Layer         string `json:"layer"`
	Severity      string `json:"severity"`
	InstancePath  string `json:"instancePath"`
	Message       string `json:"message"`
}

func ErrorDiagnostic(code, layer, instancePath, message string) Diagnostic {
	return Diagnostic{
		Code:          code,
		CodeStability: "provisional",
		Layer:         layer,
		Severity:      "error",
		InstancePath:  instancePath,
		Message:       message,
	}
}

type Layer struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type ValidationScope struct {
	RequestedThrough        string `json:"requestedThrough"`
	FullDocumentConformance bool   `json:"fullDocumentConformance"`
}

type Extensions struct {
	Required    []string `json:"required"`
	Supported   []string `json:"supported"`
	Unsupported []string `json:"unsupported"`
}

type Artifact struct {
	SpecVersion  string `json:"specVersion"`
	BundleDigest string `json:"bundleDigest"`
	Provenance   string `json:"provenance"`
}

type Validation struct {
	OutputVersion        string          `json:"outputVersion"`
	Tool                 Tool            `json:"tool"`
	Command              string          `json:"command"`
	Status               string          `json:"status"`
	SpecVersion          string          `json:"specVersion,omitempty"`
	ValidationScope      ValidationScope `json:"validationScope"`
	Layers               []Layer         `json:"layers"`
	Extensions           Extensions      `json:"extensions"`
	Diagnostics          []Diagnostic    `json:"diagnostics"`
	DiagnosticsTruncated bool            `json:"diagnosticsTruncated"`
	Artifact             *Artifact       `json:"artifact,omitempty"`
}

func NewValidation(through string) Validation {
	return Validation{
		OutputVersion: OutputVersion,
		Tool:          CurrentTool(),
		Command:       "spec validate",
		ValidationScope: ValidationScope{
			RequestedThrough:        through,
			FullDocumentConformance: through == "semantic",
		},
		Layers:      []Layer{},
		Diagnostics: []Diagnostic{},
		Extensions: Extensions{
			Required:    []string{},
			Supported:   []string{},
			Unsupported: []string{},
		},
	}
}

type ExpectedDiagnostic struct {
	Code string `json:"code"`
	Path string `json:"path"`
}

type Case struct {
	ID                   string              `json:"id"`
	ExpectedStatus       string              `json:"expectedStatus"`
	ActualStatus         string              `json:"actualStatus"`
	Status               string              `json:"status"`
	ExpectedDiagnostic   *ExpectedDiagnostic `json:"expectedDiagnostic"`
	ActualDiagnostics    []Diagnostic        `json:"actualDiagnostics"`
	DiagnosticsTruncated bool                `json:"diagnosticsTruncated"`
}

type SuiteSummary struct {
	Total      int `json:"total"`
	Passed     int `json:"passed"`
	Mismatched int `json:"mismatched"`
}

type Suite struct {
	OutputVersion         string       `json:"outputVersion"`
	Tool                  Tool         `json:"tool"`
	Command               string       `json:"command"`
	Status                string       `json:"status"`
	SpecVersion           string       `json:"specVersion"`
	SuiteVersion          string       `json:"suiteVersion"`
	CorpusDigest          string       `json:"corpusDigest"`
	CorpusDigestAlgorithm string       `json:"corpusDigestAlgorithm"`
	Provenance            string       `json:"provenance"`
	Summary               SuiteSummary `json:"summary"`
	Cases                 []Case       `json:"cases"`
	Diagnostics           []Diagnostic `json:"diagnostics"`
	DiagnosticsTruncated  bool         `json:"diagnosticsTruncated"`
}

type Schema struct {
	OutputVersion string `json:"outputVersion"`
	Tool          Tool   `json:"tool"`
	Command       string `json:"command"`
	Status        string `json:"status"`
	SpecVersion   string `json:"specVersion"`
	SchemaID      string `json:"schemaId"`
	Bytes         int    `json:"bytes"`
	SHA256        string `json:"sha256"`
	Provenance    string `json:"provenance"`
	WrittenTo     string `json:"writtenTo,omitempty"`
}

type Version struct {
	OutputVersion      string   `json:"outputVersion"`
	Tool               Tool     `json:"tool"`
	Command            string   `json:"command"`
	Status             string   `json:"status"`
	SupportedSpecs     []string `json:"supportedSpecVersions"`
	ArtifactProvenance string   `json:"artifactProvenance"`
}

// ExampleKind labels the example payloads: the bundled examples are the
// runtime's digest-locked conformance fixtures surfaced read-only, not authored
// templates. It appears in-band so a consumer never mistakes one for a template.
const ExampleKind = "version-pinned-conformance-fixture"

type ExampleSummary struct {
	Name        string `json:"name"`
	Focus       string `json:"focus"`
	SpecSection string `json:"specSection"`
}

type Examples struct {
	OutputVersion string           `json:"outputVersion"`
	Tool          Tool             `json:"tool"`
	Command       string           `json:"command"`
	Status        string           `json:"status"`
	SpecVersion   string           `json:"specVersion"`
	Provenance    string           `json:"provenance"`
	Kind          string           `json:"kind"`
	Examples      []ExampleSummary `json:"examples"`
}

type Example struct {
	OutputVersion string `json:"outputVersion"`
	Tool          Tool   `json:"tool"`
	Command       string `json:"command"`
	Status        string `json:"status"`
	SpecVersion   string `json:"specVersion"`
	Name          string `json:"name"`
	Focus         string `json:"focus"`
	SpecSection   string `json:"specSection"`
	Bytes         int    `json:"bytes"`
	SHA256        string `json:"sha256"`
	Provenance    string `json:"provenance"`
	Kind          string `json:"kind"`
	WrittenTo     string `json:"writtenTo,omitempty"`
}

// --- experimental evaluation (ADR-0007; spec RFC 0006) ---

// EvaluationClaim is carried by every experimental-evaluation payload so the
// output can never be mistaken for a conformance claim: JPS 0.1.0-draft §3.4
// forbids evaluator-conformance claims outright.
const EvaluationClaim = "none"

// HandoffTarget echoes the pack's declared escalation target when a handoff is
// requested.
type HandoffTarget struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// Handoff reports whether the evaluation requests a human handoff. A direct
// exception escalation on a pack with no escalation object is a requested
// handoff with no Core-defined destination: state "requested", target absent.
type Handoff struct {
	State  string         `json:"state"`
	Target *HandoffTarget `json:"target,omitempty"`
}

// Disposition is the portable evaluation result proposed by spec RFC 0006:
// kind ("outcome", "not-applicable", or "unresolved"), the outcome id exactly
// when kind is "outcome", a deduplicated sorted reason set (empty exactly when
// kind is "outcome"), and the handoff axis.
type Disposition struct {
	Kind      string   `json:"kind"`
	OutcomeID string   `json:"outcomeId,omitempty"`
	Reasons   []string `json:"reasons"`
	Handoff   Handoff  `json:"handoff"`
}

// TraceEntry records one exception or rule evaluation. The trace is
// informative: §8 requires an unknown that resolution ignored to remain
// visible, and permits recording contributing ids.
type TraceEntry struct {
	Stage      string `json:"stage"`
	ID         string `json:"id"`
	Condition  string `json:"condition"`
	Effect     string `json:"effect,omitempty"`
	Outcome    string `json:"outcome,omitempty"`
	Suppressed bool   `json:"suppressed,omitempty"`
	OnUnknown  string `json:"onUnknown,omitempty"`
	Skipped    bool   `json:"skipped,omitempty"`
}

// DraftPrototype marks an evaluation that ran under a draft-RFC grammar
// extension rather than a published JPS version. It is present exactly when the
// caller opted into such a grammar, and it says in band what the operators are
// and that the pack carrying them is not valid under the specification the rest
// of the payload names. A consumer that ignores it is reading a disposition
// produced by operators no JPS version defines.
type DraftPrototype struct {
	RFC                       string   `json:"rfc"`
	Status                    string   `json:"status"`
	Operators                 []string `json:"operators"`
	PackValidUnderSpecVersion bool     `json:"packValidUnderSpecVersion"`
	Note                      string   `json:"note"`
}

// Evaluation is the experimental-evaluation envelope. Experimental and
// ConformanceClaim are always set so no consumer can read the payload as a
// standard: this surface may change or be removed without compatibility
// promise (ADR-0007).
type Evaluation struct {
	OutputVersion    string          `json:"outputVersion"`
	Tool             Tool            `json:"tool"`
	Command          string          `json:"command"`
	Status           string          `json:"status"`
	Experimental     bool            `json:"experimental"`
	ConformanceClaim string          `json:"conformanceClaim"`
	SpecVersion      string          `json:"specVersion"`
	DraftPrototype   *DraftPrototype `json:"draftPrototype,omitempty"`
	Disposition      Disposition     `json:"disposition"`
	Trace            []TraceEntry    `json:"trace"`
	Artifact         *Artifact       `json:"artifact,omitempty"`
}

type OperationalError struct {
	OutputVersion string       `json:"outputVersion"`
	Tool          Tool         `json:"tool"`
	Command       string       `json:"command"`
	Status        string       `json:"status"`
	Diagnostics   []Diagnostic `json:"diagnostics"`
}

func NewOperationalError(command, code, message string) OperationalError {
	return NewOperationalResult(command, "error", code, message)
}

func NewOperationalResult(command, status, code, message string) OperationalError {
	return OperationalError{
		OutputVersion: OutputVersion,
		Tool:          CurrentTool(),
		Command:       command,
		Status:        status,
		Diagnostics: []Diagnostic{
			ErrorDiagnostic(code, "operation", "", message),
		},
	}
}
