package dialect

// This opt-in internal reader implements document/retained-frame structure from
// the installed immutable official release leaf below. It executes no conversions.
import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

const (
	RetainedColumnDraftContract              = "retained-column/1"
	RetainedColumnDraftSelector              = RetainedColumnDraftContract + "@" + RetainedColumnDraftRevision
	RetainedColumnDraftRevision              = "27cb29bf13e4a2d58d69fe19c7ebfeb762b014fc"
	RetainedColumnDraftSchema                = "sha256:0cb49f238c78391cb163a65474cc9af962df2bb1024a55c5310c2b784b2f0557"
	RetainedColumnDraftCorpus                = "sha256:17b16ff7a9b74fe4cc81eb6f6129a6aabdd9834621d9e756aa2d4c1a3f245863"
	RetainedColumnDraftDocumentBytes         = 1048576
	RetainedColumnDraftFrameBytes            = 2097152
	RetainedColumnDraftExtendedDocumentBytes = 2097152
	RetainedColumnDraftExtendedFrameBytes    = 4194304
	RetainedColumnDraftExtendedNodes         = 147456
	RetainedColumnDraftExtendedStringBytes   = 262144
)

type RetainedColumnDraftIdentity struct {
	Version      string `json:"version"`
	Revision     string `json:"revision"`
	SchemaSHA256 string `json:"schemaSha256"`
	CorpusSHA256 string `json:"corpusSha256"`
}

// RetainedColumnDraftSelection is an explicit trusted selector, not an observed
// capability report or a negotiation implementation. No default opts into it.
type RetainedColumnDraftSelection struct {
	Contract string
	Spec     RetainedColumnDraftIdentity
	Profile  string
}

func RetainedColumnDraftBase() RetainedColumnDraftSelection {
	return RetainedColumnDraftSelection{RetainedColumnDraftContract, RetainedColumnDraftIdentity{
		"0.3.0", RetainedColumnDraftRevision, RetainedColumnDraftSchema, RetainedColumnDraftCorpus}, "base"}
}

type RetainedColumnDraftLimits struct {
	DocumentBytes int `json:"documentBytes"`
	FrameBytes    int `json:"frameBytes"`
}

// RetainedColumnDraftReport is deliberately separate from the released report
// capabilities. Document validity makes no preparation or authenticity claim.
type RetainedColumnDraftReport struct {
	Report
	Contract          string                           `json:"contract"`
	Spec              RetainedColumnDraftIdentity      `json:"spec"`
	Profile           string                           `json:"profile"`
	Role              string                           `json:"role"`
	Limits            RetainedColumnDraftLimits        `json:"limits"`
	DraftCapabilities *RetainedColumnDraftCapabilities `json:"capabilities,omitempty"`
}

func retainedDraftReport(command string) RetainedColumnDraftReport {
	s := RetainedColumnDraftBase()
	r := report(command)
	r.SpecRevision = RetainedColumnDraftRevision
	r.Specification = "https://github.com/Judgment-Pack/judgment-pack-spec/blob/" + RetainedColumnDraftRevision + "/spec/retained-column.md"
	r.Maturity = "draft"
	return RetainedColumnDraftReport{r, s.Contract, s.Spec, s.Profile, "document-validation",
		RetainedColumnDraftLimits{RetainedColumnDraftDocumentBytes, RetainedColumnDraftFrameBytes}, nil}
}

func retainedDraftRefused(r RetainedColumnDraftReport, code, path, message string, exit int) (RetainedColumnDraftReport, int) {
	r.Report, exit = refused(r.Report, code, path, message, exit)
	return r, exit
}

// Selection is checked before inspecting artifact bytes, even malformed bytes.
func retainedDraftSelection(r RetainedColumnDraftReport, selection RetainedColumnDraftSelection) (RetainedColumnDraftReport, int) {
	if selection.Contract != r.Contract || selection.Spec != r.Spec {
		return retainedDraftRefused(r, "capability-unsupported", "", "The exact installed retained-column contract and specification identity are required.", result.ExitUnsupported)
	}
	if err := VerifyRetainedColumnArtifacts(); err != nil {
		return retainedDraftRefused(r, "capability-unsupported", "", "The selected retained-column installation failed verification.", result.ExitUnsupported)
	}
	switch selection.Profile {
	case "base":
		return r, result.ExitSuccess
	case "extended":
		r.Profile = "extended"
		r.Limits = RetainedColumnDraftLimits{RetainedColumnDraftExtendedDocumentBytes, RetainedColumnDraftExtendedFrameBytes}
		return r, result.ExitSuccess
	default:
		return retainedDraftRefused(r, "input-malformed", "/profile", "Unknown retained-column profile.", result.ExitInvalid)
	}
}

// ValidateRetainedColumnDraft checks the actual Dialect document, including all
// ordinary declarations, targets and references, with the Draft copy variants.
// It neither resolves source pointers against records nor executes conversion.
func ValidateRetainedColumnDraft(command string, data []byte, selection RetainedColumnDraftSelection) (RetainedColumnDraftReport, int) {
	r, _, code := validateRetainedColumnDraftDocument(command, data, selection)
	return r, code
}

func retainedDraftDocumentLimits(profile string) carrier.Limits {
	if profile == "extended" {
		return carrier.Limits{MaxDepth: 128, MaxNodes: RetainedColumnDraftExtendedNodes, MaxStringBytes: RetainedColumnDraftExtendedStringBytes}
	}
	return documentLimits()
}

func retainedDraftDocumentReport(r RetainedColumnDraftReport, checked Report, scope string) RetainedColumnDraftReport {
	checked.SpecRevision, checked.Specification, checked.Maturity = r.SpecRevision, r.Specification, r.Maturity
	checked.ValidationScope = "experimental-retained-column-" + r.Profile + "-" + scope
	for i := range checked.Diagnostics {
		switch checked.Diagnostics[i].Code {
		case "JPS-DIALECT-FORM":
			checked.Diagnostics[i].Code = "input-malformed"
		case "JPS-DIALECT-COUNT-LIMIT":
			checked.Diagnostics[i].Code = "input-over-bound"
		}
	}
	r.Report = checked
	return r
}

func validateRetainedColumnDraftDocument(command string, data []byte, selection RetainedColumnDraftSelection) (RetainedColumnDraftReport, *documentCheck, int) {
	r, code := retainedDraftSelection(retainedDraftReport(command), selection)
	if code != result.ExitSuccess {
		return r, nil, code
	}
	if len(data) > r.Limits.DocumentBytes {
		r, code = retainedDraftRefused(r, "input-over-bound", "", "The selected retained-column document exceeds its byte limit.", result.ExitInvalid)
		return r, nil, code
	}
	// Byte admission and parser work budgets are independent. The extended node
	// allowance preserves the old envelope plus bounded new leaves; its string
	// budget preserves any string from that old envelope.
	checked, document, code := validateDocumentWithProfile(command, data, r.Limits.DocumentBytes, retainedDraftDocumentLimits(r.Profile), true, r.Profile)
	return retainedDraftDocumentReport(r, checked, "document"), document, code
}

// SelectRetainedColumnDraft resolves an explicit public selector to trusted
// compiled pins before any artifact is read. Caller-supplied hashes are not proof.
func SelectRetainedColumnDraft(command, selector, profile string) (RetainedColumnDraftReport, RetainedColumnDraftSelection, int) {
	selection := RetainedColumnDraftBase()
	selection.Profile = profile
	if selector != RetainedColumnDraftSelector {
		selection.Contract = ""
	}
	r, code := retainedDraftSelection(retainedDraftReport(command), selection)
	return r, selection, code
}

type RetainedColumnDraftCapability struct {
	Contract string                      `json:"contract"`
	Spec     RetainedColumnDraftIdentity `json:"spec"`
	Profile  string                      `json:"profile"`
	Role     string                      `json:"role"`
	Limits   RetainedColumnDraftLimits   `json:"limits"`
}

type RetainedColumnDraftCapabilities struct {
	ConversionContracts    []RetainedColumnDraftCapability `json:"conversionContracts"`
	Operations             []string                        `json:"operations"`
	PackCheckScope         string                          `json:"packCheckScope"`
	MaxPackBytes           int64                           `json:"maxPackBytes"`
	MaxCoverageComparisons int64                           `json:"maxCoverageComparisons"`
	MaxCoverageReportBytes int                             `json:"maxCoverageReportBytes"`
}

// ContractRetainedColumnDraft advertises only this experimental document/frame
// reader and its declared inventory check; it advertises no producer or preparer.
func ContractRetainedColumnDraft(command string, selection RetainedColumnDraftSelection) (RetainedColumnDraftReport, int) {
	r, code := retainedDraftSelection(retainedDraftReport(command), selection)
	if code != result.ExitSuccess {
		return r, code
	}
	r.Status = "available"
	r.DraftCapabilities = &RetainedColumnDraftCapabilities{
		ConversionContracts: []RetainedColumnDraftCapability{{r.Contract, r.Spec, r.Profile, r.Role, r.Limits}},
		Operations:          []string{"contract", "canonicalize", "digest", "validate", "check"},
		PackCheckScope:      "declared-inventory-coverage", MaxPackBytes: MaxPackBytes,
		MaxCoverageComparisons: MaxCoverageComparisons, MaxCoverageReportBytes: MaxCoverageReportBytes,
	}
	return r, result.ExitSuccess
}

// EncodeRetainedColumnDraft checks only the selected encoding domain and identity
// spelling. It does not admit conversion grammar or establish document validity.
func EncodeRetainedColumnDraft(command string, data []byte, selection RetainedColumnDraftSelection) (RetainedColumnDraftReport, int) {
	r, code := retainedDraftSelection(retainedDraftReport(command), selection)
	if code != result.ExitSuccess {
		return r, code
	}
	if len(data) > r.Limits.DocumentBytes {
		return retainedDraftRefused(r, "input-over-bound", "", "The selected retained-column document exceeds its byte limit.", result.ExitInvalid)
	}
	maxIDBytes := 1024
	if r.Profile == "extended" {
		maxIDBytes = 2048
	}
	checked, code := encodeDocumentWithIDLimit(command, data, r.Limits.DocumentBytes, retainedDraftDocumentLimits(r.Profile), maxIDBytes)
	return retainedDraftDocumentReport(r, checked, "encoding-only"), code
}

// CheckRetainedColumnDraft compares actual validated targets using the ordinary
// pack/inventory gates. Conversion values and source authenticity are not checked.
func CheckRetainedColumnDraft(command string, data, pack []byte, engine *validation.Engine, selection RetainedColumnDraftSelection) (RetainedColumnDraftReport, int) {
	r, code := retainedDraftSelection(retainedDraftReport(command), selection)
	if code != result.ExitSuccess {
		return r, code
	}
	// Bound both operands before parsing either, matching ordinary Check ordering.
	if len(data) > r.Limits.DocumentBytes {
		return retainedDraftRefused(r, "input-over-bound", "", "The selected retained-column document exceeds its byte limit.", result.ExitInvalid)
	}
	if int64(len(pack)) > MaxPackBytes {
		return retainedDraftRefused(r, "JPS-DIALECT-PACK-BYTE-LIMIT", "", "The pack exceeds the document byte limit.", result.ExitInvalid)
	}
	r, document, code := validateRetainedColumnDraftDocument(command, data, selection)
	if code != result.ExitSuccess {
		return r, code
	}
	r.ValidationScope = "experimental-retained-column-" + r.Profile + "-document-and-declared-pack-inventory"
	r.Report, code = checkDeclaredInventory(r.Report, document, pack, engine)
	return r, code
}

func (v *documentCheck) retainedConversion(value any, path string, availability bool) {
	required := []string{"contractVersion", "column"}
	if !availability {
		required = append(required, "kind")
		if obj, ok := value.(map[string]any); ok && obj["kind"] == "boolean" {
			required = append(required, "trueValue", "falseValue")
		}
	}
	c := v.object(value, path, required, nil)
	v.choice(c["contractVersion"], at(path, "contractVersion"), "1")
	v.boundedText(c["column"], at(path, "column"), 160)
	if availability {
		return
	}
	kind := v.choice(c["kind"], at(path, "kind"), "text", "decimal", "number", "boolean", "research-value")
	if kind == "boolean" {
		a := v.boundedText(c["trueValue"], at(path, "trueValue"), 160)
		b := v.boundedText(c["falseValue"], at(path, "falseValue"), 160)
		if a == b || retainedBlank(a) || retainedBlank(b) {
			v.fail(path, "Boolean spellings must be distinct and nonblank.")
		}
	}
}

func retainedBlank(s string) bool {
	return strings.TrimFunc(s, func(r rune) bool {
		return r >= 9 && r <= 13 || r == 32 || r == 0x85 || r == 0xa0 || r == 0x1680 ||
			r >= 0x2000 && r <= 0x200a || r == 0x2028 || r == 0x2029 || r == 0x202f || r == 0x205f || r == 0x3000
	}) == ""
}

// The typed inspection result retains all original strings, ordered declaration
// entries and absent/null distinctions. Hashes identify exact supplied UTF-8;
// they do not attest the source, recipe inventory or record provenance.
type RetainedColumnDraftFrame struct {
	ContractVersion string                       `json:"contractVersion"`
	Origin          string                       `json:"origin"`
	Values          map[string]*string           `json:"values"`
	Research        *RetainedColumnDraftResearch `json:"research"`
}
type RetainedColumnDraftResearch struct {
	RecordRecipeRevision string                              `json:"recordRecipeRevision"`
	Declaration          *RetainedColumnDraftDeclaration     `json:"declaration"`
	Fields               map[string]RetainedColumnDraftField `json:"fields"`
}
type RetainedColumnDraftDeclaration struct {
	RecipeRevision string                                `json:"recipeRevision"`
	Fields         []RetainedColumnDraftDeclarationEntry `json:"fields"`
}
type RetainedColumnDraftDeclarationEntry struct {
	Name        string `json:"name"`
	FieldJSON   string `json:"fieldJson"`
	FieldSHA256 string `json:"-"`
}
type RetainedColumnDraftField struct {
	Status    string  `json:"status"`
	ValueJSON *string `json:"valueJson,omitempty"`
}
type RetainedColumnDraftFrameReport struct {
	RetainedColumnDraftReport
	// Keep typed inspection available internally. On the unpublished wire
	// surface, Document retains every exact source byte once per report;
	// repeating all decoded strings here amplifies the dual MCP response.
	Frame        *RetainedColumnDraftFrame        `json:"-"`
	FrameSummary *RetainedColumnDraftFrameSummary `json:"frameSummary,omitempty"`
}

type RetainedColumnDraftFrameSummary struct {
	ContractVersion  string `json:"contractVersion"`
	Origin           string `json:"origin"`
	ValueCount       int    `json:"valueCount"`
	DeclarationCount int    `json:"declarationCount"`
	FieldCount       int    `json:"fieldCount"`
}

// ValidateRetainedColumnDraftFrame checks a complete standalone retained frame.
// valueJson is opaque retained data: malformed typed JSON, unknown original
// types and revision mismatches remain inspectable per-record conversion issues.
func ValidateRetainedColumnDraftFrame(command string, data []byte, selection RetainedColumnDraftSelection) (RetainedColumnDraftFrameReport, int) {
	r, code := retainedDraftSelection(retainedDraftReport(command), selection)
	out := RetainedColumnDraftFrameReport{RetainedColumnDraftReport: r}
	if code != result.ExitSuccess {
		return out, code
	}
	fail := func(code, path, message string) (RetainedColumnDraftFrameReport, int) {
		out.RetainedColumnDraftReport, _ = retainedDraftRefused(out.RetainedColumnDraftReport, code, path, message, result.ExitInvalid)
		return out, result.ExitInvalid
	}
	if len(data) > r.Limits.FrameBytes {
		return fail("input-over-bound", "", "The retained frame exceeds its byte limit.")
	}
	// Closed frame structure has at most 7011 nodes (1000 values, declaration
	// entries and fields). This independent 8192-node allowance also bounds
	// malformed containers. Opaque valueJson may occupy the whole frame budget;
	// it is never recursively parsed here. Declarations are parsed separately.
	value, failure := carrier.Decode(data, carrier.Limits{MaxDepth: MaxDepth, MaxNodes: 8192, MaxStringBytes: r.Limits.FrameBytes})
	if failure != nil {
		code := "input-malformed"
		if failure.Resource {
			code = "input-over-bound"
		}
		return fail(code, failure.Diagnostic.InstancePath, failure.Diagnostic.Message)
	}
	v := documentCheck{}
	frame := v.retainedFrame(value)
	if v.issue != nil {
		code := "input-malformed"
		if v.issue.Code == "JPS-DIALECT-COUNT-LIMIT" {
			code = "input-over-bound"
		}
		return fail(code, v.issue.InstancePath, v.issue.Message)
	}
	out.Status = "retained-frame-valid"
	out.ValidationScope = "experimental-retained-column-" + r.Profile + "-frame-structure"
	out.Document = string(data)
	out.Digest = retainedDraftDigest(string(data))
	out.Frame = frame
	out.FrameSummary = &RetainedColumnDraftFrameSummary{ContractVersion: frame.ContractVersion, Origin: frame.Origin, ValueCount: len(frame.Values)}
	if frame.Research != nil {
		out.FrameSummary.FieldCount = len(frame.Research.Fields)
		if frame.Research.Declaration != nil {
			out.FrameSummary.DeclarationCount = len(frame.Research.Declaration.Fields)
		}
	}
	return out, result.ExitSuccess
}

func retainedDraftDigest(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (v *documentCheck) retainedFrame(value any) *RetainedColumnDraftFrame {
	f := v.object(value, "", []string{"contractVersion", "origin", "values", "research"}, nil)
	out := &RetainedColumnDraftFrame{ContractVersion: v.choice(f["contractVersion"], "/contractVersion", "1"), Origin: v.choice(f["origin"], "/origin", "asserted", "generated"), Values: map[string]*string{}}
	values := v.dictionary(f["values"], "/values")
	if len(values) > 1000 {
		v.refuse("JPS-DIALECT-COUNT-LIMIT", "/values", "At most 1000 retained values are permitted.")
	}
	for _, name := range keys(values) {
		path := at("/values", name)
		v.boundedText(name, path, 160)
		if values[name] == nil {
			out.Values[name] = nil
		} else {
			text := v.text(values[name], path, false)
			limit := 16000
			if out.Origin == "generated" {
				limit = 524288
			}
			if len(text) > limit {
				v.refuse("JPS-DIALECT-COUNT-LIMIT", path, "The retained column exceeds its origin-specific UTF-8 byte limit.")
			}
			out.Values[name] = &text
		}
	}
	if out.Origin == "asserted" {
		if f["research"] != nil {
			v.fail("/research", "Asserted frames require research:null.")
		}
		return out
	}
	r := v.object(f["research"], "/research", []string{"recordRecipeRevision", "declaration", "fields"}, nil)
	research := &RetainedColumnDraftResearch{RecordRecipeRevision: v.boundedText(r["recordRecipeRevision"], "/research/recordRecipeRevision", 160), Fields: map[string]RetainedColumnDraftField{}}
	out.Research = research
	if r["declaration"] != nil {
		d := v.object(r["declaration"], "/research/declaration", []string{"recipeRevision", "fields"}, nil)
		decl := &RetainedColumnDraftDeclaration{RecipeRevision: v.boundedText(d["recipeRevision"], "/research/declaration/recipeRevision", 160), Fields: []RetainedColumnDraftDeclarationEntry{}}
		research.Declaration = decl
		seen := map[string]bool{}
		for i, raw := range v.list(d["fields"], "/research/declaration/fields", 0, 1000) {
			path := at("/research/declaration/fields", strconv.Itoa(i))
			e := v.object(raw, path, []string{"name", "fieldJson"}, nil)
			name := v.boundedText(e["name"], at(path, "name"), 160)
			if seen[name] {
				v.fail(at(path, "name"), "Original declaration names must be distinct.")
			}
			seen[name] = true
			text := v.text(e["fieldJson"], at(path, "fieldJson"), false)
			if len(text) > 16384 {
				v.refuse("JPS-DIALECT-COUNT-LIMIT", at(path, "fieldJson"), "Original field JSON is at most 16384 UTF-8 bytes.")
				continue
			}
			// No canonical-number restriction: original numeric spelling is data.
			// A 16KiB token budget bounds work independently of the frame ceiling.
			field, failure := carrier.Decode([]byte(text), carrier.Limits{MaxDepth: 32, MaxNodes: 16384, MaxStringBytes: 16384})
			if failure != nil {
				v.fail(at(path, "fieldJson"), "Original field JSON must be duplicate-free scalar-valid JSON with nesting at most 32.")
				continue
			}
			obj, ok := field.(map[string]any)
			if !ok || obj["name"] != name {
				v.fail(at(path, "fieldJson"), "Original field JSON must be an object with the matching name.")
			}
			if _, ok := obj["type"].(string); !ok {
				v.fail(at(path, "fieldJson"), "Original field JSON must contain a string type.")
			}
			decl.Fields = append(decl.Fields, RetainedColumnDraftDeclarationEntry{name, text, retainedDraftDigest(text)})
		}
	}
	fields := v.dictionary(r["fields"], "/research/fields")
	if len(fields) > 1000 {
		v.refuse("JPS-DIALECT-COUNT-LIMIT", "/research/fields", "At most 1000 retained fields are permitted.")
	}
	for _, name := range keys(fields) {
		path := at("/research/fields", name)
		v.boundedText(name, path, 160)
		f := v.object(fields[name], path, []string{"status"}, []string{"valueJson"})
		entry := RetainedColumnDraftField{Status: v.choice(f["status"], at(path, "status"), "supported", "interpretation", "missing", "conflicting", "inaccessible", "stale")}
		if raw, present := f["valueJson"]; present {
			text := v.text(raw, at(path, "valueJson"), false)
			entry.ValueJSON = &text
		}
		research.Fields[name] = entry
	}
	return out
}
