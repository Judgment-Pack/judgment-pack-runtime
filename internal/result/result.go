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
