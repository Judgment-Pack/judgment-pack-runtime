// Package baseline holds the revalidation baseline of ADR-0091: what one
// runtime gave for a project's own matrix rows, written down so that another
// runtime's results can be compared with it byte for byte before a pinned
// runtime version is moved.
//
// A baseline is read in one of two forms. The document experimental
// revalidate --write writes (baselineVersion "1") holds each pack's digest and
// its matrix's digest beside the rows. A packs test --format json report
// (outputVersion "2"), which released runtimes already write, holds the same
// row results and no digest; read as a baseline, its rows are compared and its
// bytes are said not to be.
package baseline

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

const (
	// Version is the one baselineVersion this runtime writes and reads.
	Version = "1"
	// ReportOutputVersion is the one packs test outputVersion read as a
	// baseline.
	ReportOutputVersion = "2"
	// FormBaseline and FormReport name the two forms a baseline is read in.
	FormBaseline = "baseline"
	FormReport   = "packs-test-report"
	// MaxBytes bounds the baselines one run reads, together, and the one
	// document a run writes. A packs test report carries coverage and
	// profile members beside its rows, so the bound is set for it.
	MaxBytes = int64(64 << 20)
	// MaxNodes bounds the JSON values one baseline holds.
	MaxNodes = 4_000_000
)

// VersionError is a document that declares a version this runtime does not
// read: a later baselineVersion, or a packs test outputVersion other than 2.
type VersionError struct{ message string }

func (e *VersionError) Error() string { return e.message }

// ErrTooLarge marks a baseline that would not fit in what the reader accepts.
var ErrTooLarge = errors.New("baseline exceeds the byte limit")

// Row is one matrix row's result: the members the comparison reads.
type Row struct {
	ID            string
	Status        string
	Disposition   string
	ErrorClass    string
	ErrorPhase    string
	HandoffTarget string
}

// Canonical is the row's compared form: the RFC 8785 canonical JSON of the
// object {id, status, disposition, errorClass, errorPhase, handoffTarget},
// each member a string and the last four present only when not empty. Two
// rows are the same exactly when these bytes are.
func (r Row) Canonical() (string, error) {
	object := map[string]any{"id": r.ID, "status": r.Status}
	for _, member := range []struct{ name, value string }{
		{"disposition", r.Disposition},
		{"errorClass", r.ErrorClass},
		{"errorPhase", r.ErrorPhase},
		{"handoffTarget", r.HandoffTarget},
	} {
		if member.value != "" {
			object[member.name] = member.value
		}
	}
	encoded, err := jcs.Encode(object)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// RowFrom is one packs test row's result as a baseline row: the actual
// members, never the expectation.
func RowFrom(row result.EvaluationCorpusCase) Row {
	return Row{
		ID:            row.ID,
		Status:        row.Status,
		Disposition:   row.Actual,
		ErrorClass:    row.ActualErrorClass,
		ErrorPhase:    row.ActualErrorPhase,
		HandoffTarget: row.ActualHandoffTarget,
	}
}

// Pack is one pack's results in a baseline. PackDigest and MatrixDigest are
// empty when the baseline's form records none.
type Pack struct {
	ID           string
	PackID       string
	PackVersion  string
	SpecVersion  string
	PackDigest   string
	MatrixDigest string
	Rows         []Row
}

// PackFrom is one run as a baseline pack. The run must have Ran.
func PackFrom(run project.PackRun) Pack {
	rows := make([]Row, 0, len(run.Rows))
	for _, row := range run.Rows {
		rows = append(rows, RowFrom(row))
	}
	return Pack{ID: run.ID, PackID: run.PackID, PackVersion: run.PackVersion, SpecVersion: run.SpecVersion, PackDigest: run.PackDigest, MatrixDigest: run.MatrixDigest, Rows: rows}
}

// Source is one baseline as read.
type Source struct {
	Form    string
	Version string
	Tool    result.Tool
	Packs   []Pack
}

type document struct {
	BaselineVersion string         `json:"baselineVersion"`
	Tool            result.Tool    `json:"tool"`
	Packs           []documentPack `json:"packs"`
}

type documentPack struct {
	ID           string        `json:"id"`
	PackID       string        `json:"packId"`
	PackVersion  string        `json:"packVersion"`
	SpecVersion  string        `json:"specVersion"`
	PackDigest   string        `json:"packDigest"`
	MatrixDigest string        `json:"matrixDigest"`
	Rows         []documentRow `json:"rows"`
}

type documentRow struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	Disposition   string `json:"disposition,omitempty"`
	ErrorClass    string `json:"errorClass,omitempty"`
	ErrorPhase    string `json:"errorPhase,omitempty"`
	HandoffTarget string `json:"handoffTarget,omitempty"`
}

// Encode writes one baseline document: two-space indentation, members in a
// fixed order, no HTML escaping, and a final newline, so the same results
// encode to the same bytes. It refuses a document its own reader would refuse,
// one past MaxBytes among them (ErrTooLarge, still reachable through
// errors.Is): a baseline that cannot be read back is not one.
func Encode(tool result.Tool, packs []Pack) ([]byte, error) {
	out := document{BaselineVersion: Version, Tool: tool, Packs: make([]documentPack, 0, len(packs))}
	for _, pack := range packs {
		rows := make([]documentRow, 0, len(pack.Rows))
		for _, row := range pack.Rows {
			rows = append(rows, documentRow(row))
		}
		out.Packs = append(out.Packs, documentPack{ID: pack.ID, PackID: pack.PackID, PackVersion: pack.PackVersion, SpecVersion: pack.SpecVersion, PackDigest: pack.PackDigest, MatrixDigest: pack.MatrixDigest, Rows: rows})
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(out); err != nil {
		return nil, err
	}
	encoded := buffer.Bytes()
	if _, err := Decode(encoded); err != nil {
		return nil, fmt.Errorf("the baseline this run would write is not one this runtime reads back: %w", err)
	}
	return encoded, nil
}

// Decode reads one baseline in either form. The form is the version member
// the root declares: baselineVersion for a baseline document, outputVersion
// for a packs test report. The declared version is settled first, so a later
// version is told it is a later version rather than that one of its members is
// unknown. Every member is read by its exact name, at every level the
// comparison reads, and a member neither form writes is refused.
func Decode(data []byte) (Source, error) {
	if int64(len(data)) > MaxBytes {
		return Source{}, ErrTooLarge
	}
	decoded, failure := carrier.Decode(data, carrier.Limits{MaxDepth: carrier.DefaultMaxDepth, MaxNodes: MaxNodes, MaxStringBytes: int(carrier.HardMaxBytes)})
	if failure != nil {
		return Source{}, fmt.Errorf("it is not acceptable JSON: %s", display.Sanitize(failure.Diagnostic.Message))
	}
	root, ok := decoded.(map[string]any)
	if !ok {
		return Source{}, errors.New("its root is not a JSON object")
	}
	_, native := root["baselineVersion"]
	_, report := root["outputVersion"]
	switch {
	case native && report:
		return Source{}, errors.New("it declares both baselineVersion and outputVersion, and no document this runtime writes declares both")
	case native:
		return decodeBaseline(root)
	case report:
		return decodeReport(root)
	default:
		return Source{}, errors.New("it declares neither baselineVersion, as experimental revalidate --write writes, nor outputVersion, as packs test --format json writes")
	}
}

var (
	baselineMembers    = members("baselineVersion", "tool", "packs")
	toolMembers        = members("name", "version")
	baselinePackFields = members("id", "packId", "packVersion", "specVersion", "packDigest", "matrixDigest", "rows")
	baselineRowFields  = members("id", "status", "disposition", "errorClass", "errorPhase", "handoffTarget")
)

func decodeBaseline(root map[string]any) (Source, error) {
	declared, ok := root["baselineVersion"].(string)
	if !ok {
		return Source{}, errors.New("its baselineVersion is not a string")
	}
	if declared != Version {
		return Source{}, &VersionError{fmt.Sprintf("it declares baselineVersion %s, and this runtime reads baselineVersion %s", shown(declared), Version)}
	}
	if err := closed(root, baselineMembers, "at its root"); err != nil {
		return Source{}, err
	}
	tool, err := toolOf(root)
	if err != nil {
		return Source{}, err
	}
	entries, ok := root["packs"].([]any)
	if !ok {
		return Source{}, errors.New("its packs member is not an array")
	}
	if len(entries) == 0 {
		return Source{}, errors.New("it holds no pack, and a baseline of nothing compares nothing")
	}
	source := Source{Form: FormBaseline, Version: declared, Tool: tool, Packs: make([]Pack, 0, len(entries))}
	seen := map[string]bool{}
	for index, raw := range entries {
		entry, ok := raw.(map[string]any)
		if !ok {
			return Source{}, fmt.Errorf("pack %d is not a JSON object", index)
		}
		if err := closed(entry, baselinePackFields, fmt.Sprintf("in pack %d", index)); err != nil {
			return Source{}, err
		}
		pack := Pack{}
		for _, member := range []struct {
			name     string
			into     *string
			nonEmpty bool
		}{
			{"id", &pack.ID, true},
			{"packId", &pack.PackID, false},
			{"packVersion", &pack.PackVersion, false},
			{"specVersion", &pack.SpecVersion, true},
			{"packDigest", &pack.PackDigest, true},
			{"matrixDigest", &pack.MatrixDigest, true},
		} {
			value, err := requiredString(entry, member.name, member.nonEmpty)
			if err != nil {
				return Source{}, fmt.Errorf("pack %d %w", index, err)
			}
			*member.into = value
		}
		if seen[pack.ID] {
			return Source{}, fmt.Errorf("the pack %s appears more than once", shown(pack.ID))
		}
		seen[pack.ID] = true
		for _, digest := range []struct{ name, value string }{{"packDigest", pack.PackDigest}, {"matrixDigest", pack.MatrixDigest}} {
			if !validDigest(digest.value) {
				return Source{}, fmt.Errorf("the pack %s has a %s that is not \"sha256:\" and sixty-four lowercase hexadecimal digits", shown(pack.ID), digest.name)
			}
		}
		rows, err := rowsOf(entry, pack.ID, false)
		if err != nil {
			return Source{}, err
		}
		pack.Rows = rows
		source.Packs = append(source.Packs, pack)
	}
	return source, nil
}

// The member names packs test --format json writes at the three levels a
// baseline reads: the json names of result.PackTest, result.PackTestEntry and
// result.EvaluationCorpusCase. A test holds these lists to those types.
var (
	reportMembers = members("outputVersion", "tool", "command", "status", "experimental", "evaluatorSpecVersion",
		"conformanceClaimReference", "label", "kind", "configPath", "configVersion", "requireMatrix", "summary", "packs",
		"minCoverage", "requireBoundaries", "strength")
	reportPackMembers = members("id", "packId", "packVersion", "path", "matrixPath", "status", "summary", "rows",
		"coverage", "origins", "profile", "detail", "unmetRequirements", "strength")
	reportRowMembers = members("id", "origin", "specSection", "packId", "packVersion", "status", "expected", "actual",
		"expectedErrorClass", "actualErrorClass", "expectedErrorPhase", "actualErrorPhase", "expectedHandoffTarget",
		"actualHandoffTarget", "cites", "detail")
)

func decodeReport(root map[string]any) (Source, error) {
	declared, ok := root["outputVersion"].(string)
	if !ok {
		return Source{}, errors.New("its outputVersion is not a string")
	}
	if declared != ReportOutputVersion {
		return Source{}, &VersionError{fmt.Sprintf("it declares outputVersion %s, and this runtime reads a packs test report of outputVersion %s", shown(declared), ReportOutputVersion)}
	}
	command, err := requiredString(root, "command", true)
	if err != nil {
		return Source{}, errors.New("it " + err.Error())
	}
	if command != "packs test" {
		return Source{}, fmt.Errorf("it is a report of %s, not of packs test, and only a packs test report holds matrix rows", shown(command))
	}
	if err := closed(root, reportMembers, "at its root"); err != nil {
		return Source{}, err
	}
	tool, err := toolOf(root)
	if err != nil {
		return Source{}, err
	}
	specVersion, err := requiredString(root, "evaluatorSpecVersion", true)
	if err != nil {
		return Source{}, errors.New("it " + err.Error())
	}
	entries, ok := root["packs"].([]any)
	if !ok {
		return Source{}, errors.New("its packs member is not an array")
	}
	source := Source{Form: FormReport, Version: declared, Tool: tool, Packs: []Pack{}}
	seen := map[string]bool{}
	for index, raw := range entries {
		entry, ok := raw.(map[string]any)
		if !ok {
			return Source{}, fmt.Errorf("pack %d is not a JSON object", index)
		}
		if err := closed(entry, reportPackMembers, fmt.Sprintf("in pack %d", index)); err != nil {
			return Source{}, err
		}
		pack := Pack{SpecVersion: specVersion}
		for _, member := range []struct {
			name     string
			into     *string
			nonEmpty bool
		}{
			{"id", &pack.ID, true},
			{"packId", &pack.PackID, false},
			{"packVersion", &pack.PackVersion, false},
		} {
			value, err := requiredString(entry, member.name, member.nonEmpty)
			if err != nil {
				return Source{}, fmt.Errorf("pack %d %w", index, err)
			}
			*member.into = value
		}
		if seen[pack.ID] {
			return Source{}, fmt.Errorf("the pack %s appears more than once", shown(pack.ID))
		}
		seen[pack.ID] = true
		matrixPath, err := optionalString(entry, "matrixPath")
		if err != nil {
			return Source{}, fmt.Errorf("the pack %s %w", shown(pack.ID), err)
		}
		rows, err := rowsOf(entry, pack.ID, true)
		if err != nil {
			return Source{}, err
		}
		if matrixPath == "" {
			// packs test reports a pack with no matrix, and runs no row of
			// it; such a pack has no result to compare and is not in the
			// baseline, exactly as a baseline document leaves it out.
			if len(rows) != 0 {
				return Source{}, fmt.Errorf("the pack %s declares no matrixPath and has rows, which packs test never reports", shown(pack.ID))
			}
			continue
		}
		if len(rows) == 0 {
			detail, _ := entry["detail"].(string)
			return Source{}, fmt.Errorf("the report shows that the matrix of the pack %s did not run under the runtime that wrote it (%s), so it holds no result to compare; make the matrix run under that runtime and take the report again", shown(pack.ID), display.Sanitize(detail))
		}
		pack.Rows = rows
		source.Packs = append(source.Packs, pack)
	}
	if len(source.Packs) == 0 {
		return Source{}, errors.New("it holds no pack whose matrix ran, and a baseline of nothing compares nothing")
	}
	return source, nil
}

// rowsOf reads one pack's rows in either form: in a report the result
// members are named actual, actualErrorClass, actualErrorPhase and
// actualHandoffTarget, and in a baseline disposition, errorClass, errorPhase
// and handoffTarget. A report row always carries actual, empty when no
// disposition was produced; every other optional member is absent when empty
// in both forms, so an empty one present is refused as a second spelling of
// absence.
func rowsOf(entry map[string]any, packID string, report bool) ([]Row, error) {
	raw, ok := entry["rows"].([]any)
	if !ok {
		return nil, fmt.Errorf("the pack %s has no rows array", shown(packID))
	}
	if len(raw) > project.MaxMatrixCases {
		return nil, fmt.Errorf("the pack %s has more than the %d rows a matrix may declare", shown(packID), project.MaxMatrixCases)
	}
	if !report && len(raw) == 0 {
		return nil, fmt.Errorf("the pack %s has no rows; a baseline holds only packs whose matrix ran", shown(packID))
	}
	fields, names := baselineRowFields, [4]string{"disposition", "errorClass", "errorPhase", "handoffTarget"}
	if report {
		fields, names = reportRowMembers, [4]string{"actual", "actualErrorClass", "actualErrorPhase", "actualHandoffTarget"}
	}
	rows := make([]Row, 0, len(raw))
	seen := map[string]bool{}
	for index, item := range raw {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("row %d of the pack %s is not a JSON object", index, shown(packID))
		}
		where := fmt.Sprintf("in row %d of the pack %s", index, shown(packID))
		if err := closed(object, fields, where); err != nil {
			return nil, err
		}
		row := Row{}
		var err error
		if row.ID, err = requiredString(object, "id", true); err != nil {
			return nil, fmt.Errorf("row %d of the pack %s %w", index, shown(packID), err)
		}
		if seen[row.ID] {
			return nil, fmt.Errorf("the row %s appears more than once in the pack %s", shown(row.ID), shown(packID))
		}
		seen[row.ID] = true
		if row.Status, err = requiredString(object, "status", true); err != nil {
			return nil, fmt.Errorf("the row %s of the pack %s %w", shown(row.ID), shown(packID), err)
		}
		if row.Status != "passed" && row.Status != "mismatch" {
			return nil, fmt.Errorf("the row %s of the pack %s has the status %s; a row's status is passed or mismatch", shown(row.ID), shown(packID), shown(row.Status))
		}
		targets := [4]*string{&row.Disposition, &row.ErrorClass, &row.ErrorPhase, &row.HandoffTarget}
		for position, name := range names {
			if report && position == 0 {
				*targets[0], err = requiredString(object, name, false)
			} else {
				*targets[position], err = optionalString(object, name)
				if err == nil && *targets[position] == "" {
					if _, present := object[name]; present {
						err = fmt.Errorf("has an empty %s, which is written only when it is not empty", name)
					}
				}
			}
			if err != nil {
				return nil, fmt.Errorf("the row %s of the pack %s %w", shown(row.ID), shown(packID), err)
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func toolOf(root map[string]any) (result.Tool, error) {
	object, ok := root["tool"].(map[string]any)
	if !ok {
		return result.Tool{}, errors.New("it has no tool object")
	}
	if err := closed(object, toolMembers, "in its tool"); err != nil {
		return result.Tool{}, err
	}
	name, err := requiredString(object, "name", true)
	if err != nil {
		return result.Tool{}, errors.New("its tool " + err.Error())
	}
	version, err := requiredString(object, "version", true)
	if err != nil {
		return result.Tool{}, errors.New("its tool " + err.Error())
	}
	return result.Tool{Name: name, Version: version}, nil
}

func members(names ...string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[name] = true
	}
	return set
}

// closed refuses an object holding a member not exactly one of known. The
// least such name, in sorted order, is the one named, so the refusal is the
// same on every run.
func closed(object map[string]any, known map[string]bool, where string) error {
	unknown := []string{}
	for name := range object {
		if !known[name] {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return fmt.Errorf("it has a member %s %s that no document this runtime reads as a baseline carries", shown(unknown[0]), where)
}

func requiredString(object map[string]any, name string, nonEmpty bool) (string, error) {
	raw, present := object[name]
	if !present {
		return "", fmt.Errorf("has no %s", name)
	}
	value, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("has a %s that is not a string", name)
	}
	if nonEmpty && value == "" {
		return "", fmt.Errorf("has an empty %s", name)
	}
	return value, nil
}

func optionalString(object map[string]any, name string) (string, error) {
	raw, present := object[name]
	if !present {
		return "", nil
	}
	value, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("has a %s that is not a string", name)
	}
	return value, nil
}

// validDigest is the one digest form this runtime writes: "sha256:" and
// sixty-four lowercase hexadecimal digits.
func validDigest(digest string) bool {
	body, found := strings.CutPrefix(digest, "sha256:")
	if !found || len(body) != 64 {
		return false
	}
	for _, character := range body {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

// maxShown bounds how much of a name read from a baseline a refusal quotes.
const maxShown = 64

func shown(name string) string {
	text := display.Sanitize(name)
	if characters := []rune(text); len(characters) > maxShown {
		text = string(characters[:maxShown]) + "…"
	}
	return fmt.Sprintf("%q", text)
}
