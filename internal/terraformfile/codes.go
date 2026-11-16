// Package terraformfile reads the JSON that `terraform show -json` writes for a
// saved plan or for state, as spec RFC 0038 §2 to §5 propose for the dialect's
// `terraform-file` source kind (ADR-0097). It reads bytes the caller supplies,
// and nothing else: it never runs Terraform, never reads a state backend or a
// binary plan, and never contacts a network.
//
// Read takes the steps of RFC 0038 §2 in order: the size and sha256 of the
// whole input, strict JSON, the accepted format table, the document's shape,
// the sensitivity mirror on every resource of the read section, the removal of
// every position marked sensitive, and then selection. Only the sanitized
// values of the selected resources reach the acquired value. A refusal carries
// its code, the input's sha256 and its size, and a fixed message per code:
// never a value, a member name, a pointer or a position from the input, since
// before removal any of those could be part of a sensitive value.
//
// The package does not depend on the carrier: a carrier diagnostic carries a
// pointer, and the carrier's node bound would refuse a file the RFC accepts.
package terraformfile

import "strconv"

// The bounds of RFC 0038, and the one this runtime adds (MaxValueBytes).
const (
	// DefaultMaxBytes is maxBytes when the source does not declare it.
	DefaultMaxBytes int64 = 67108864
	// MaxBytesLimit is the largest maxBytes a source may declare.
	MaxBytesLimit int64 = 268435456
	// MaxDepth is the deepest a value may nest: the top-level value is at
	// depth 1, and each value inside an object or array is one deeper than
	// its container. A member name is not a value.
	MaxDepth = 128
	// MaxTypes is the most resource type names select.types may list.
	MaxTypes = 64
	// MaxPatternChars is the most characters (code points) select.address
	// may hold.
	MaxPatternChars = 512
	// DefaultSelectMax is select.max when the source does not declare it.
	DefaultSelectMax = 1000
	// SelectMaxLimit is the largest select.max a source may declare.
	SelectMaxLimit = 10000
	// MaxNumberChars is the longest normal form a number of the acquired
	// value may have.
	MaxNumberChars = 64
	// MaxValueBytes bounds the canonical JSON of the acquired value. RFC 0038
	// names no bound here; numbers and afterUnknown pointers can make the
	// value longer than the file, so this runtime holds it to the largest
	// file a source can read (ADR-0097).
	MaxValueBytes int64 = MaxBytesLimit
)

// FormatTable is the release of the accepted format_version table this reader
// implements. The companion has not released a table yet (RFC 0038,
// Unresolved question 2); "proposed-1" is this runtime's provisional first
// table, recorded in ADR-0097.
const FormatTable = "proposed-1"

// acceptedFormats is the table FormatTable names, per document. A version not
// listed, a newer minor of a listed major included, is TF-FORMAT.
var acceptedFormats = map[string][]string{
	DocumentPlan:  {"1.0", "1.1", "1.2"},
	DocumentState: {"1.0"},
}

// AcceptedFormats returns a copy of the accepted format_version values for a
// document, in table order.
func AcceptedFormats(document string) []string {
	return append([]string(nil), acceptedFormats[document]...)
}

// The refusal codes. RFC 0038 names every one but CodeValueLimit, which is
// this runtime's (ADR-0097). All are provisional until the dialect companion
// releases the kind (VERSIONING.md).
const (
	CodeSize        = "TF-SIZE"
	CodeJSON        = "TF-JSON"
	CodeFormat      = "TF-FORMAT"
	CodeDocument    = "TF-DOCUMENT"
	CodeSensitivity = "TF-SENSITIVITY"
	CodeSelectBound = "TF-SELECT-BOUND"
	CodeNumber      = "TF-NUMBER"
	CodeValueLimit  = "TF-VALUE-LIMIT"
)

// messages holds the one fixed message of each code. A message never carries
// anything read from the file.
var messages = map[string]string{
	CodeSize:        "The file is longer than the source's maxBytes, and was not parsed.",
	CodeJSON:        "The file is not one strict JSON value: UTF-8 with no byte-order mark and no lone surrogate escape, white space only after the value, nesting at most 128 deep, and no duplicate member name in any object. A binary plan file is refused here: pass the output of terraform show -json instead.",
	CodeFormat:      "The file's top-level value is not an object whose format_version is a string in this reader's accepted table for the source's document. A raw state file has no format_version: pass the output of terraform show -json instead.",
	CodeDocument:    "The file does not have the form of the source's document: a plan has a planned_values object and its resource_changes are an array, a state has neither and its values are an object holding a root_module, and every resource of the read section names its address, type, name, mode and provider as strings.",
	CodeSensitivity: "A resource of the read section, selected or not, does not carry its sensitivity mirror, or carries one that is not true, false, an object or an array at every position.",
	CodeSelectBound: "The source selects more resources than its select.max.",
	CodeNumber:      "A number in the acquired value has a normal form longer than 64 characters.",
	CodeValueLimit:  "The acquired value's canonical JSON is longer than this runtime's bound of 268435456 bytes.",
}

// Codes returns every refusal code, in a fixed order.
func Codes() []string {
	return []string{CodeSize, CodeJSON, CodeFormat, CodeDocument, CodeSensitivity, CodeSelectBound, CodeNumber, CodeValueLimit}
}

// Message returns the fixed message of a code, or "" for a code this package
// does not report.
func Message(code string) string { return messages[code] }

// Refusal is a refused acquisition: the code of the first step that failed,
// the sha256 (bare lower-case hex) of the whole input, its length in bytes, and
// the code's fixed message. It holds nothing else.
type Refusal struct {
	Code    string `json:"code"`
	SHA256  string `json:"sha256"`
	Bytes   int64  `json:"bytes"`
	Message string `json:"message"`
}

func newRefusal(code, digest string, size int64) *Refusal {
	message, ok := messages[code]
	if !ok {
		panic("terraformfile: unlisted code " + code)
	}
	return &Refusal{Code: code, SHA256: digest, Bytes: size, Message: message}
}

// Error makes a Refusal usable as an error. It names the code only.
func (r *Refusal) Error() string { return "terraformfile: refused " + r.Code }

// JSON returns the refusal as RFC 8785 canonical JSON. Every member is fixed
// text, a hex digest or an integer, so no escaping can arise.
func (r *Refusal) JSON() []byte {
	out := []byte(`{"bytes":`)
	out = strconv.AppendInt(out, r.Bytes, 10)
	out = append(out, `,"code":`...)
	out = appendJSONString(out, r.Code)
	out = append(out, `,"message":`...)
	out = appendJSONString(out, r.Message)
	out = append(out, `,"sha256":`...)
	out = appendJSONString(out, r.SHA256)
	return append(out, '}')
}
