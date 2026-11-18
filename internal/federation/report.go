package federation

import (
	"bytes"
	"cmp"
	"encoding/json"
	"slices"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// ReportFormatVersion is the selector of the report an assembly, an index
// check, a lock check or a verification writes (§2.1). A plain validation of
// one registry still writes registry.ReportFormatVersion.
const ReportFormatVersion = "1.1.0"

// The location documents §2.1 adds to registry §7.3's.
const (
	DocumentFederation = "federation"
	DocumentIndex      = "index"
	DocumentBounds     = "bounds"
	DocumentLock       = "lock"
	DocumentSignature  = "signature"
	DocumentEmitter    = "emitter"
)

// MaxReportDiagnostics is registry §7.4's bound, which holds unchanged
// (§2.1): a report lists its first 1000 diagnostics in its order, and
// REG-REPORT-TRUNCATED after them when more were found.
const MaxReportDiagnostics = 1000

// Location is registry §7.3's location with §2.1's two members: Source, the
// name of the source whose document a diagnostic is about, and KeyID, the
// key identifier of a signature document's public key (§11). Each is absent
// when empty.
type Location struct {
	Document     string              `json:"document"`
	Source       string              `json:"source,omitempty"`
	KeyID        string              `json:"keyId,omitempty"`
	KindDocument string              `json:"kindDocument,omitempty"`
	Kind         string              `json:"kind,omitempty"`
	Entry        *registry.EntryName `json:"entry,omitempty"`
	Environment  string              `json:"environment,omitempty"`
	Pointer      string              `json:"pointer"`
	Line         int                 `json:"line,omitempty"`
	Column       int                 `json:"column,omitempty"`
}

// Diagnostic is one finding: a code, its severity, where it applies, and the
// code's fixed message. It carries no value from the documents.
type Diagnostic struct {
	Code     string   `json:"code"`
	Severity string   `json:"severity"`
	Location Location `json:"location"`
	Message  string   `json:"message"`
}

// Report is the validation report of registry §7.4 under the selector
// "1.1.0". Registry is the digest of the registry the operation validates by
// registry §7, present once that validation passed its step 3.
type Report struct {
	ReportFormatVersion string             `json:"reportFormatVersion"`
	Valid               bool               `json:"valid"`
	Registry            *registry.Digested `json:"registry,omitempty"`
	Diagnostics         []Diagnostic       `json:"diagnostics"`
}

// Canonical returns the report's RFC 8785 bytes, the form a command writes.
func (r Report) Canonical() ([]byte, error) {
	raw, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return jcs.EncodeDocument(value)
}

// The messages of the format 1.0.0 codes this package raises itself, beside
// the ones a 1.0.0 validation reports with its own: they are registry.Codes'
// (a test holds them to what a 1.0.0 validation reports).
const (
	messageTruncated   = "The report lists the first 1000 diagnostics in its order, and more were found; whether the document is valid is judged on all of them."
	messageScopeShared = "A project is listed in the secret scopes of two environments."
)

// fromRegistry is a format 1.0.0 diagnostic as a diagnostic of this report,
// about the named source's document ("" for none).
func fromRegistry(d registry.Diagnostic, source string) Diagnostic {
	return Diagnostic{
		Code:     d.Code,
		Severity: d.Severity,
		Message:  d.Message,
		Location: Location{
			Document:     d.Location.Document,
			Source:       source,
			KindDocument: d.Location.KindDocument,
			Kind:         d.Location.Kind,
			Entry:        d.Location.Entry,
			Environment:  d.Location.Environment,
			Pointer:      d.Location.Pointer,
			Line:         d.Location.Line,
			Column:       d.Location.Column,
		},
	}
}

// collector gathers a report's diagnostics as registry's does: it keeps the
// MaxReportDiagnostics smallest distinct ones in the report's order, so a
// capped report is the first of the uncapped one whatever order they were
// found in, and counts every error, kept or not.
type collector struct {
	kept    []Diagnostic
	dropped bool
	errors  int
}

// add records one of this package's codes.
func (c *collector) add(code string, location Location) {
	info, ok := Codes[code]
	if !ok {
		panic("federation: unlisted diagnostic code " + code)
	}
	c.insert(Diagnostic{Code: code, Severity: info.severity, Location: location, Message: info.message})
}

// insert records one diagnostic. A REG-REPORT-TRUNCATED of a validation run
// inside an operation says that validation found more than it listed: the
// report is then truncated too, and the warning is written once, at its end.
func (c *collector) insert(d Diagnostic) {
	if d.Code == "REG-REPORT-TRUNCATED" {
		c.dropped = true
		return
	}
	if d.Severity == SeverityError {
		c.errors++
	}
	at, found := slices.BinarySearchFunc(c.kept, d, compareDiagnostics)
	switch {
	case found:
		return
	case at >= MaxReportDiagnostics:
		c.dropped = true
		return
	case len(c.kept) == MaxReportDiagnostics:
		c.kept = c.kept[:len(c.kept)-1]
		c.dropped = true
	}
	c.kept = slices.Insert(c.kept, at, d)
}

// failed records that a validation run inside the operation found an error,
// whether or not its report lists one: a validation's report lists its first
// 1000 diagnostics, and an error after them still fails the operation.
func (c *collector) failed() { c.errors++ }

// hasErrors reports whether any error was found, listed or not.
func (c *collector) hasErrors() bool { return c.errors > 0 }

// report is the collected diagnostics as a report.
func (c *collector) report(digest string) Report {
	out := Report{ReportFormatVersion: ReportFormatVersion, Valid: c.errors == 0, Diagnostics: slices.Clone(c.kept)}
	if out.Diagnostics == nil {
		out.Diagnostics = []Diagnostic{}
	}
	if c.dropped {
		out.Diagnostics = append(out.Diagnostics, Diagnostic{Code: "REG-REPORT-TRUNCATED", Severity: SeverityWarning, Message: messageTruncated, Location: Location{Document: registry.DocumentRegistry}})
	}
	if digest != "" {
		out.Registry = &registry.Digested{Digest: digest}
	}
	return out
}

// compareDiagnostics is registry §7.4's order with §2.1's members: document,
// source, keyId, kindDocument, kind, entry kind, entry id, environment,
// pointer, line, column, then code and message. An absent member sorts as
// "" or 0.
func compareDiagnostics(a, b Diagnostic) int {
	entry := func(d Diagnostic) (string, string) {
		if d.Location.Entry == nil {
			return "", ""
		}
		return d.Location.Entry.Kind, d.Location.Entry.ID
	}
	aKind, aID := entry(a)
	bKind, bID := entry(b)
	return cmp.Or(
		cmp.Compare(a.Location.Document, b.Location.Document),
		cmp.Compare(a.Location.Source, b.Location.Source),
		cmp.Compare(a.Location.KeyID, b.Location.KeyID),
		cmp.Compare(a.Location.KindDocument, b.Location.KindDocument),
		cmp.Compare(a.Location.Kind, b.Location.Kind),
		cmp.Compare(aKind, bKind),
		cmp.Compare(aID, bID),
		cmp.Compare(a.Location.Environment, b.Location.Environment),
		cmp.Compare(a.Location.Pointer, b.Location.Pointer),
		cmp.Compare(a.Location.Line, b.Location.Line),
		cmp.Compare(a.Location.Column, b.Location.Column),
		cmp.Compare(a.Code, b.Code),
		cmp.Compare(a.Message, b.Message),
	)
}
