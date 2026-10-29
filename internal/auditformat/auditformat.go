// Package auditformat fingerprints the audit trail's persisted line formats
// (#327). It keeps three records beside this file:
//
//   - catalog.json, generated: every persisted line type, its normalized type
//     and that type's SHA-256 digest.
//   - changes.json, written by hand: the format decision behind every digest a
//     line type has had, appended and never rewritten.
//   - status.json, written by hand and kept apart: per trail file, the format
//     the code writes, the last accepted compatibility baseline and the last
//     format released. A format that shipped in any release, a pre-release
//     included, carries compatibility obligations.
//
// Check regenerates the catalog from the Go types and refuses a catalog that
// differs, a digest no change record names, and a status the writer constants
// contradict.
package auditformat

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
)

// Normalization names the grammar Normalize writes. Changing the grammar
// changes every digest, so it is versioned with the catalog.
const Normalization = "audit-line-type/1"

// The three hand-kept files and the generated catalog, by name.
const (
	CatalogFile = "catalog.json"
	ChangesFile = "changes.json"
	StatusFile  = "status.json"
)

// The decisions a change record may name.
const (
	DecisionBaseline           = "baseline"
	DecisionCompatibleAddition = "compatible-addition"
	DecisionNewFormatVersion   = "new-format-version"
)

// Entry is one line type in the catalog.
type Entry struct {
	File   string `json:"file"`
	Line   string `json:"line"`
	Type   string `json:"type"`
	SHA256 string `json:"sha256"`
}

// Catalog is the generated inventory.
type Catalog struct {
	CatalogVersion int     `json:"catalogVersion"`
	Normalization  string  `json:"normalization"`
	Lines          []Entry `json:"lines"`
}

// Change is one committed format decision for one line type's digest.
type Change struct {
	File          string `json:"file"`
	Line          string `json:"line"`
	SHA256        string `json:"sha256"`
	FormatVersion string `json:"formatVersion"`
	Decision      string `json:"decision"`
	// ReadingRule is required for a new format version: how a reader tells
	// the versions apart and what it does with each.
	ReadingRule string `json:"readingRule,omitempty"`
	Reason      string `json:"reason"`
}

// Changes is the change-record file.
type Changes struct {
	Changes []Change `json:"changes"`
}

// FileStatus is the format status of one trail file.
type FileStatus struct {
	File     string `json:"file"`
	Writes   string `json:"writes"`
	Baseline string `json:"baseline"`
	Released string `json:"released"`
}

// Status is the format-status file.
type Status struct {
	StatusVersion int          `json:"statusVersion"`
	Files         []FileStatus `json:"files"`
}

// Generate fingerprints every line type the audit package persists.
func Generate() (Catalog, map[string]string) {
	catalog := Catalog{CatalogVersion: 1, Normalization: Normalization}
	writers := map[string]string{}
	for _, format := range audit.LineFormats() {
		catalog.Lines = append(catalog.Lines, entry(format.File, format.Line, format.Type))
		writers[format.File] = format.Version
	}
	return catalog, writers
}

func entry(file, line string, t reflect.Type) Entry {
	normalized := Normalize(t)
	sum := sha256.Sum256([]byte(normalized))
	return Entry{file, line, normalized, "sha256:" + hex.EncodeToString(sum[:])}
}

// Encode is the catalog file's exact bytes.
func Encode(catalog Catalog) []byte {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(catalog) // strings and integers only
	return buffer.Bytes()
}

var rawMessage = reflect.TypeOf(json.RawMessage(nil))
var marshaler = reflect.TypeOf((*json.Marshaler)(nil)).Elem()

// Normalize is the JSON shape encoding/json writes for t: members in encoding
// order with their names (bare when an identifier, quoted otherwise),
// omitempty marked by "?" and ",string" by "~", "|null" where a nil pointer,
// slice or map encodes as null, and
// member types reduced to JSON kinds. Go type and field names do not appear,
// so a rename that keeps the encoded shape keeps the digest. A type with its
// own MarshalJSON is marked "custom" and normalized by its fields, so a change
// to its fields is caught; a change to the method's code alone is not.
func Normalize(t reflect.Type) string {
	return normalize(t, map[reflect.Type]bool{})
}

func normalize(t reflect.Type, open map[reflect.Type]bool) string {
	if t == rawMessage {
		return "json"
	}
	prefix := ""
	if t.Kind() != reflect.Pointer && t.Kind() != reflect.Interface && (t.Implements(marshaler) || reflect.PointerTo(t).Implements(marshaler)) {
		prefix = "custom"
	}
	switch t.Kind() {
	case reflect.Bool:
		return prefix + "boolean"
	case reflect.String:
		return prefix + "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return prefix + "integer"
	case reflect.Float32, reflect.Float64:
		return prefix + "number"
	case reflect.Interface:
		return "any"
	case reflect.Pointer:
		return normalize(t.Elem(), open) + "|null"
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return prefix + "base64|null"
		}
		return prefix + "[" + normalize(t.Elem(), open) + "]|null"
	case reflect.Array:
		return prefix + "[" + normalize(t.Elem(), open) + "]"
	case reflect.Map:
		return prefix + "{" + normalize(t.Key(), open) + ":" + normalize(t.Elem(), open) + "}|null"
	case reflect.Struct:
		if open[t] {
			return "recursive"
		}
		open[t] = true
		defer delete(open, t)
		var members []string
		for _, field := range fields(t) {
			name := field.name
			if field.omitempty {
				name += "?"
			}
			if field.quoted {
				name += "~"
			}
			if !plainName.MatchString(field.name) {
				name = strconv.Quote(name)
			}
			members = append(members, name+":"+normalize(field.typ, open))
		}
		return prefix + "{" + strings.Join(members, ",") + "}"
	}
	return "unsupported:" + t.Kind().String()
}

type field struct {
	name      string
	omitempty bool
	quoted    bool
	typ       reflect.Type
}

// fields is the members encoding/json writes for a struct, in its order.
// Untagged embedded structs are promoted; the persisted line types use none
// with conflicting names, so dominance rules are not reproduced.
func fields(t reflect.Type) []field {
	var out []field
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, options, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" {
			embedded := f.Type
			if embedded.Kind() == reflect.Pointer {
				embedded = embedded.Elem()
			}
			if embedded.Kind() == reflect.Struct {
				out = append(out, fields(embedded)...)
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		option := func(want string) bool {
			for _, o := range strings.Split(options, ",") {
				if o == want {
					return true
				}
			}
			return false
		}
		out = append(out, field{name, option("omitempty"), option("string"), f.Type})
	}
	return out
}

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// plainName is a member name written bare; any other is written quoted.
var plainName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Check compares the committed catalog, change records and status with the
// generated catalog and the writer constants (file -> version). It returns
// every disagreement, so one run names them all.
func Check(catalogRaw, changesRaw, statusRaw []byte, generated Catalog, writers map[string]string) []error {
	var problems []error
	fail := func(format string, args ...any) { problems = append(problems, fmt.Errorf(format, args...)) }
	var committed Catalog
	if err := strictDecode(catalogRaw, &committed); err != nil {
		fail("%s: %v", CatalogFile, err)
	} else if !reflect.DeepEqual(committed, generated) {
		fail("%s differs from the line types the code persists; regenerate it with `go run ./tools/audit-formats -write` and commit a change record for each changed digest", CatalogFile)
	}
	var changes Changes
	if err := strictDecode(changesRaw, &changes); err != nil {
		fail("%s: %v", ChangesFile, err)
		return problems
	}
	var status Status
	if err := strictDecode(statusRaw, &status); err != nil {
		fail("%s: %v", StatusFile, err)
		return problems
	}
	known := map[string]Entry{}
	for _, line := range generated.Lines {
		known[line.File+"\x00"+line.Line] = line
	}
	// Walk each line type's records in order: the first is its baseline, each
	// later one names a new digest and the decision that admitted it.
	last := map[string]Change{}
	for i, change := range changes.Changes {
		key := change.File + "\x00" + change.Line
		where := fmt.Sprintf("%s change %d (%s %s)", ChangesFile, i+1, change.File, change.Line)
		if _, ok := known[key]; !ok {
			fail("%s: no persisted line type has this name", where)
			continue
		}
		version, err := formatVersion(change.FormatVersion)
		if err != nil || !digestPattern.MatchString(change.SHA256) {
			fail("%s: needs a sha256: digest and a positive integer format version", where)
			continue
		}
		previous, seen := last[key]
		switch {
		case !seen && change.Decision != DecisionBaseline:
			fail("%s: a line type's first record is its baseline", where)
		case seen && change.Decision == DecisionBaseline:
			fail("%s: a line type has one baseline", where)
		case seen && change.SHA256 == previous.SHA256:
			fail("%s: names the digest the previous record already admitted", where)
		case seen && strings.TrimSpace(change.Reason) == "":
			fail("%s: a change record names its reason", where)
		case change.Decision == DecisionCompatibleAddition:
			if prior, _ := formatVersion(previous.FormatVersion); version != prior {
				fail("%s: a compatible addition keeps format version %s", where, previous.FormatVersion)
			}
		case change.Decision == DecisionNewFormatVersion:
			if prior, _ := formatVersion(previous.FormatVersion); version != prior+1 || strings.TrimSpace(change.ReadingRule) == "" {
				fail("%s: a new format version is the next integer and states its reading rule", where)
			}
		case change.Decision != DecisionBaseline:
			fail("%s: decision %q is none of %s, %s, %s", where, change.Decision, DecisionBaseline, DecisionCompatibleAddition, DecisionNewFormatVersion)
		}
		last[key] = change
	}
	files := map[string][]Entry{}
	for _, line := range generated.Lines {
		files[line.File] = append(files[line.File], line)
		change, ok := last[line.File+"\x00"+line.Line]
		switch {
		case !ok:
			fail("%s %s: no change record admits this line type; add its baseline to %s", line.File, line.Line, ChangesFile)
		case change.SHA256 != line.SHA256:
			fail("%s %s: digest %s has no change record naming its format decision (the last admitted is %s)", line.File, line.Line, line.SHA256, change.SHA256)
		case change.FormatVersion != writers[line.File]:
			fail("%s %s: the last change record is format version %s, the writer writes %s", line.File, line.Line, change.FormatVersion, writers[line.File])
		}
	}
	statuses := map[string]bool{}
	for _, file := range status.Files {
		where := fmt.Sprintf("%s %s", StatusFile, file.File)
		if _, ok := files[file.File]; !ok || statuses[file.File] {
			fail("%s: not a trail file, or named twice", where)
			continue
		}
		statuses[file.File] = true
		writes, errW := formatVersion(file.Writes)
		baseline, errB := formatVersion(file.Baseline)
		released, errR := formatVersion(file.Released)
		switch {
		case errW != nil || errB != nil || errR != nil:
			fail("%s: writes, baseline and released are positive integer format versions", where)
		case file.Writes != writers[file.File]:
			fail("%s: says the code writes format %s; the writer constant is %s", where, file.Writes, writers[file.File])
		case baseline > released || released > writes:
			// A released format carries compatibility obligations: the
			// baseline readers keep accepting reaches back to it.
			fail("%s: needs baseline <= released <= writes", where)
		}
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !statuses[name] {
			fail("%s: %s has no format status", StatusFile, name)
		}
	}
	return problems
}

func formatVersion(text string) (int, error) {
	value, err := strconv.Atoi(text)
	if err != nil || value < 1 || strconv.Itoa(value) != text {
		return 0, fmt.Errorf("not a positive integer: %q", text)
	}
	return value, nil
}

func strictDecode(raw []byte, into any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return err
	}
	if decoder.More() {
		return fmt.Errorf("trailing data")
	}
	return nil
}
