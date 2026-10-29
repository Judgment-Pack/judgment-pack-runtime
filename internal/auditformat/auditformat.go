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
//     included, carries compatibility obligations. It also pins the released
//     prefix of change records, which stays unchanged and in order.
//
// CheckFiles regenerates the catalog from the Go types and refuses a catalog
// that differs, a digest no change record names, a rewritten released record,
// a status the writer constants contradict, and an exemplar line from the
// writers' own encoders that does not conform to its declared shape. The
// fingerprint covers shape and declared grammars, not other value encodings or
// spellings, such as a timestamp's.
package auditformat

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
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
	StatusVersion   int             `json:"statusVersion"`
	ReleasedChanges ReleasedChanges `json:"releasedChanges"`
	Files           []FileStatus    `json:"files"`
}

// Generate fingerprints every line type the audit package persists.
func Generate() (Catalog, map[string]string) {
	return generate(audit.LineFormats())
}

func generate(formats []audit.LineFormat) (Catalog, map[string]string) {
	catalog := Catalog{CatalogVersion: 1, Normalization: Normalization}
	writers := map[string]string{}
	for _, format := range formats {
		catalog.Lines = append(catalog.Lines, entry(format.File, format.Line, format.Type, format.Embedded))
		writers[format.File] = format.Version
	}
	return catalog, writers
}

func entry(file, line string, t reflect.Type, embedded map[string]reflect.Type) Entry {
	normalized := NormalizeLine(t, embedded)
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
// to its fields is caught; a change to the method's code alone is not. A
// string member's `format` tag is written after its kind, as "string(base64)".
func Normalize(t reflect.Type) string {
	return NormalizeLine(t, nil)
}

// NormalizeLine is Normalize with the line's embedded members (a top-level
// member name mapped to the type declaring its shape) in place of their own
// types, so a json.RawMessage the runtime builds is not an opaque "json".
func NormalizeLine(t reflect.Type, embedded map[string]reflect.Type) string {
	return normalize(t, map[reflect.Type]bool{}, embedded)
}

func normalize(t reflect.Type, open map[reflect.Type]bool, embedded map[string]reflect.Type) string {
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
		return normalize(t.Elem(), open, nil) + "|null"
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return prefix + "base64|null"
		}
		return prefix + "[" + normalize(t.Elem(), open, nil) + "]|null"
	case reflect.Array:
		return prefix + "[" + normalize(t.Elem(), open, nil) + "]"
	case reflect.Map:
		return prefix + "{" + normalize(t.Key(), open, nil) + ":" + normalize(t.Elem(), open, nil) + "}|null"
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
			typ := field.typ
			if declared, ok := embedded[field.name]; ok {
				typ = declared
			}
			text := normalize(typ, open, nil)
			if field.format != "" {
				text += "(" + field.format + ")"
			}
			members = append(members, name+":"+text)
		}
		return prefix + "{" + strings.Join(members, ",") + "}"
	}
	return "unsupported:" + t.Kind().String()
}

type field struct {
	name      string
	omitempty bool
	quoted    bool
	format    string
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
		out = append(out, field{name, option("omitempty"), option("string"), f.Tag.Get("format"), f.Type})
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
	// Released history is append-only: the pinned prefix of change records is
	// unchanged and in order.
	if pin := status.ReleasedChanges; pin.Count < 0 || pin.Count > len(changes.Changes) || PrefixDigest(changes.Changes, pin.Count) != pin.SHA256 {
		fail("%s: the %d released change records pinned by %s are not unchanged and in order; append new records after them instead (all %d records present digest to %s)", ChangesFile, pin.Count, StatusFile, len(changes.Changes), PrefixDigest(changes.Changes, len(changes.Changes)))
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

// CheckFiles is the whole audit-formats check over the committed files: Check
// against the generated catalog and the writer constants, then Exemplars.
func CheckFiles(catalogRaw, changesRaw, statusRaw []byte) []error {
	return checkFiles(catalogRaw, changesRaw, statusRaw, audit.LineFormats())
}

func checkFiles(catalogRaw, changesRaw, statusRaw []byte, formats []audit.LineFormat) []error {
	generated, writers := generate(formats)
	return append(Check(catalogRaw, changesRaw, statusRaw, generated, writers), exemplars(formats)...)
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

// Exemplars holds every exemplar line the audit package's own encoders produce
// to its line type's declared shape and grammars, and requires each member an
// embedded shape declares to appear in at least one exemplar. A builder that
// writes a member its shape does not declare, or stops writing one it does,
// fails here even when no Go type changed.
func Exemplars() []error {
	return exemplars(audit.LineFormats())
}

func exemplars(formats []audit.LineFormat) []error {
	var problems []error
	for _, format := range formats {
		if format.Exemplars == nil {
			continue
		}
		lines, err := format.Exemplars()
		if err != nil {
			problems = append(problems, fmt.Errorf("%s %s: exemplar: %v", format.File, format.Line, err))
			continue
		}
		if err := Conform(lines, format.Type, format.Embedded); err != nil {
			problems = append(problems, fmt.Errorf("%s %s: %v", format.File, format.Line, err))
		}
	}
	return problems
}

// Conform checks lines against t (with its embedded shapes): each line is one
// JSON object whose members are all declared, with every non-omitempty member
// present and every value of its declared kind and `format` grammar. Within
// embedded shapes, every declared member must appear in at least one line.
func Conform(lines [][]byte, t reflect.Type, embedded map[string]reflect.Type) error {
	seen := map[string]bool{}
	for i, line := range lines {
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			return fmt.Errorf("exemplar %d: %v", i+1, err)
		}
		if err := conform(value, t, "", "", false, embedded, seen); err != nil {
			return fmt.Errorf("exemplar %d: %v", i+1, err)
		}
	}
	for name, declared := range embedded {
		for _, path := range declaredPaths(declared, name) {
			if !seen[path] {
				return fmt.Errorf("no exemplar writes the declared member %s", path)
			}
		}
	}
	return nil
}

// conform checks one value at member path where. Below an embedded shape
// (tracked), the path is recorded in seen for the coverage rule.
func conform(value any, t reflect.Type, format, where string, tracked bool, embedded map[string]reflect.Type, seen map[string]bool) error {
	if tracked {
		seen[where] = true
	}
	if t == rawMessage || t.Kind() == reflect.Interface {
		return nil
	}
	if t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Map {
		if value == nil {
			return nil
		}
	}
	switch t.Kind() {
	case reflect.Pointer:
		return conform(value, t.Elem(), format, where, tracked, nil, seen)
	case reflect.Bool:
		if _, ok := value.(bool); ok {
			return nil
		}
	case reflect.String:
		text, ok := value.(string)
		if ok && format == "base64" {
			decoded, err := base64.StdEncoding.Strict().DecodeString(text)
			if err != nil || base64.StdEncoding.EncodeToString(decoded) != text {
				return fmt.Errorf("member %s is not standard padded base64", where)
			}
		}
		if ok {
			return nil
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if number, ok := value.(json.Number); ok {
			if _, err := strconv.ParseInt(number.String(), 10, 64); err == nil {
				return nil
			}
		}
	case reflect.Float32, reflect.Float64:
		if _, ok := value.(json.Number); ok {
			return nil
		}
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return conform(value, reflect.TypeOf(""), "base64", where, tracked, nil, seen)
		}
		items, ok := value.([]any)
		if !ok {
			break
		}
		for _, item := range items {
			if err := conform(item, t.Elem(), "", where, tracked, nil, seen); err != nil {
				return err
			}
		}
		return nil
	case reflect.Map:
		members, ok := value.(map[string]any)
		if !ok {
			break
		}
		for _, member := range members {
			if err := conform(member, t.Elem(), "", where, tracked, nil, seen); err != nil {
				return err
			}
		}
		return nil
	case reflect.Struct:
		members, ok := value.(map[string]any)
		if !ok {
			break
		}
		declared := map[string]field{}
		for _, f := range fields(t) {
			declared[f.name] = f
			if _, present := members[f.name]; !present && !f.omitempty {
				return fmt.Errorf("required member %s is absent", join(where, f.name))
			}
		}
		for name, member := range members {
			f, ok := declared[name]
			if !ok {
				return fmt.Errorf("member %s is not declared by the line's shape", join(where, name))
			}
			typ, next, nextTracked := f.typ, join(where, name), tracked
			if shape, ok := embedded[name]; ok {
				typ, nextTracked = shape, true
			}
			if f.quoted {
				if _, ok := member.(string); !ok {
					return fmt.Errorf("member %s is not a quoted value", join(where, name))
				}
				continue
			}
			if err := conform(member, typ, f.format, next, nextTracked, nil, seen); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("member %s is not of its declared kind %s", where, t.Kind())
}

func join(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

// declaredPaths is every member path a struct shape declares, below prefix.
func declaredPaths(t reflect.Type, prefix string) []string {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || t == rawMessage {
		return nil
	}
	var paths []string
	for _, f := range fields(t) {
		path := join(prefix, f.name)
		paths = append(paths, path)
		paths = append(paths, declaredPaths(f.typ, path)...)
	}
	return paths
}

// ReleasedChanges pins the change records a release has shipped: their count
// and the digest of their content, in order. Records are appended after the
// pinned prefix; the prefix itself is never rewritten.
type ReleasedChanges struct {
	Count  int    `json:"count"`
	SHA256 string `json:"sha256"`
}

// PrefixDigest is the digest of the first count change records' content: each
// record's compact JSON encoding, one per line, so a whitespace edit to the
// file does not move it and a content edit or reordering does.
func PrefixDigest(changes []Change, count int) string {
	hash := sha256.New()
	for _, change := range changes[:count] {
		encoded, _ := json.Marshal(change) // strings only
		hash.Write(append(encoded, '\n'))
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}
