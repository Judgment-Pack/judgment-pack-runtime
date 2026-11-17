package terraformfile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"slices"
	"strconv"
)

// Resource is one selected resource of the acquired value: its address and
// its object of RFC 0038 §5 as canonical JSON. JSON is a slice of the
// acquired value's bytes, and must not be modified.
type Resource struct {
	Address string
	JSON    []byte
}

// Acquisition is an accepted read: the members of RFC 0038 §5's acquisition
// record, and the acquired value. It holds no path, and nothing from a
// position marked sensitive.
type Acquisition struct {
	Document         string
	FormatVersion    string
	FormatTable      string
	SHA256           string // bare lower-case hex of the whole input
	Bytes            int64
	RemovedSensitive int
	Selected         int
	// Resources are the selected resources, sorted by address in code-point
	// order; resources that share an address keep the order of the read
	// section.
	Resources []Resource
	value     []byte
}

// Value returns the acquired value as RFC 8785 canonical JSON: an array of
// the Resources' objects, in their order. It must not be modified.
func (a *Acquisition) Value() []byte { return a.value }

// Record returns the acquisition record as RFC 8785 canonical JSON:
// {"kind": "terraform-file", "document", "formatVersion", "formatTable",
// "sha256", "bytes", "removedSensitive", "selected"}.
func (a *Acquisition) Record() []byte {
	out := []byte(`{"bytes":`)
	out = strconv.AppendInt(out, a.Bytes, 10)
	out = append(out, `,"document":`...)
	out = appendJSONString(out, a.Document)
	out = append(out, `,"formatTable":`...)
	out = appendJSONString(out, a.FormatTable)
	out = append(out, `,"formatVersion":`...)
	out = appendJSONString(out, a.FormatVersion)
	out = append(out, `,"kind":"terraform-file","removedSensitive":`...)
	out = strconv.AppendInt(out, int64(a.RemovedSensitive), 10)
	out = append(out, `,"selected":`...)
	out = strconv.AppendInt(out, int64(a.Selected), 10)
	out = append(out, `,"sha256":`...)
	out = appendJSONString(out, a.SHA256)
	return append(out, '}')
}

// optional is a node that may be absent.
type optional struct {
	node uint32
	ok   bool
}

// resource is one resource of the read section, by its nodes.
type resource struct {
	address, typ, name, mode, provider uint32
	index                              optional
	// a plan's resource change
	actions                         uint32
	before, after, afterUnknown     optional
	beforeSensitive, afterSensitive optional
	// a state's resource
	values, sensitiveValues optional
	addressText             string
}

// Read reads one file supplied by the caller as src's document, by RFC 0038
// §2 to §5. Exactly one result is non-nil: the acquisition, the refusal of the
// first step that failed, or an error. The error is ErrInvalidSource for a
// source that fails Validate, which makes the dialect invalid and not the file,
// or ErrRead when the reader fails. A file longer than maxBytes is read to its
// end, so that TF-SIZE carries the whole file's sha256 and size.
func Read(r io.Reader, src Source) (*Acquisition, *Refusal, error) {
	return read(r, src, false, MaxValueBytes, ValueBytesPerInputByte)
}

// ReadStream is Read for a stream, such as standard input, that need not end.
// The caller says the input is a stream; the reader never guesses it from r's
// type. It reads at most maxBytes and one byte more. A longer stream is TF-SIZE
// with neither sha256 nor size, since its whole length is never read (a
// Refusal with Stream set). Every other result is Read's.
func ReadStream(r io.Reader, src Source) (*Acquisition, *Refusal, error) {
	return read(r, src, true, MaxValueBytes, ValueBytesPerInputByte)
}

// read holds the acquired value to valueLimit bytes and to perInputByte times
// the input's size, whichever is less. A stream is read to limit+1 bytes at
// most.
func read(r io.Reader, src Source, stream bool, valueLimit, perInputByte int64) (*Acquisition, *Refusal, error) {
	if err := src.Validate(); err != nil {
		return nil, nil, err
	}
	limit := src.maxBytes()
	// Step 1: the size and sha256 of the whole input, or, for a stream, of
	// its first limit+1 bytes.
	data, digest, size, err := readAll(r, limit, stream)
	if err != nil {
		return nil, nil, err
	}
	refuse := func(code string) (*Acquisition, *Refusal, error) {
		return nil, newRefusal(code, digest, size), nil
	}
	if size > limit {
		if stream {
			return nil, newStreamSizeRefusal(), nil
		}
		return refuse(CodeSize)
	}
	// Step 2: strict JSON.
	doc, ok := parseStrict(data)
	if !ok {
		return refuse(CodeJSON)
	}
	// Step 3: the format table.
	format, ok := doc.format(src.Document)
	if !ok {
		return refuse(CodeFormat)
	}
	// Step 4: the document's shape, and its read section.
	var section []resource
	if src.Document == DocumentPlan {
		section, ok = doc.planSection()
	} else {
		section, ok = doc.stateSection()
	}
	if !ok {
		return refuse(CodeDocument)
	}
	// Step 5: every resource carries its mirror, selected or not.
	for _, res := range section {
		if src.Document == DocumentPlan {
			ok = res.beforeSensitive.ok && res.afterSensitive.ok && doc.isMirror(res.beforeSensitive.node) && doc.isMirror(res.afterSensitive.node)
		} else {
			ok = res.sensitiveValues.ok && doc.isMirror(res.sensitiveValues.node)
		}
		if !ok {
			return refuse(CodeSensitivity)
		}
	}
	// Step 6: remove every position marked sensitive, in the whole section.
	removed := 0
	for _, res := range section {
		if src.Document == DocumentPlan {
			removed += doc.stripRoot(res.beforeSensitive.node, res.before)
			removed += doc.stripRoot(res.afterSensitive.node, res.after)
		} else {
			removed += doc.stripRoot(res.sensitiveValues.node, res.values)
		}
	}
	// Step 7: selection, then the acquired value.
	selected := doc.selectResources(section, src)
	if len(selected) > src.Select.max() {
		return refuse(CodeSelectBound)
	}
	slices.SortStableFunc(selected, func(a, b resource) int {
		return compareCodePoints(a.addressText, b.addressText)
	})
	for _, res := range selected {
		if !doc.resourceNumbers(res) {
			return refuse(CodeNumber)
		}
	}
	value, spans, ok := doc.writeAcquired(selected, src.Document, min(valueLimit, perInputByte*size))
	if !ok {
		return refuse(CodeValueLimit)
	}
	acquisition := &Acquisition{
		Document:         src.Document,
		FormatVersion:    format,
		FormatTable:      FormatTable,
		SHA256:           digest,
		Bytes:            size,
		RemovedSensitive: removed,
		Selected:         len(selected),
		Resources:        make([]Resource, len(selected)),
		value:            value,
	}
	for i, res := range selected {
		acquisition.Resources[i] = Resource{Address: res.addressText, JSON: value[spans[i][0]:spans[i][1]:spans[i][1]]}
	}
	return acquisition, nil, nil
}

// ErrRead is the error Read returns when its reader fails. Its message is
// fixed, so a path or other text the reader's error carries never reaches a
// message; errors.Is still matches the reader's own error through it.
var ErrRead = errors.New("terraformfile: the input could not be read")

type readError struct{ err error }

func (e readError) Error() string   { return ErrRead.Error() }
func (e readError) Unwrap() []error { return []error{ErrRead, e.err} }

// readAll reads the whole input, hashing all of it, and keeps it only while
// it is at most limit bytes. A stream is read to limit+1 bytes at most, enough
// to see that it is longer, and its digest is then of those bytes only. A
// reader that tells its size (a file, or an in-memory reader) is read into one
// buffer of that size and one byte more, to see a longer input. Any other is
// read in chunks that grow to 8 MiB and are joined once, so it costs about
// twice its size while it is read.
func readAll(r io.Reader, limit int64, stream bool) ([]byte, string, int64, error) {
	hash := sha256.New()
	var chunks [][]byte
	var spill []byte
	var size int64
	chunk := 64 << 10
	if hint := sizeHint(r); hint >= 0 && hint <= limit {
		chunk = int(hint) + 1
	}
	if stream {
		r = io.LimitReader(r, limit+1)
	}
	for {
		var buffer []byte
		if size > limit {
			if spill == nil {
				spill = make([]byte, 1<<20)
			}
			buffer = spill
		} else {
			buffer = make([]byte, chunk)
		}
		n, err := io.ReadFull(r, buffer)
		hash.Write(buffer[:n])
		size += int64(n)
		if size <= limit {
			chunks = append(chunks, buffer[:n])
		} else {
			chunks = nil
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return nil, "", 0, readError{err}
		}
		chunk = min(max(chunk*2, 64<<10), 8<<20)
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	if size > limit {
		return nil, digest, size, nil
	}
	if len(chunks) == 1 {
		return chunks[0], digest, size, nil
	}
	data := make([]byte, 0, size)
	for _, c := range chunks {
		data = append(data, c...)
	}
	return data, digest, size, nil
}

// sizeHint returns the bytes a reader says it holds, or -1.
func sizeHint(r io.Reader) int64 {
	switch typed := r.(type) {
	case interface{ Len() int }:
		return int64(typed.Len())
	case interface{ Stat() (fs.FileInfo, error) }:
		if info, err := typed.Stat(); err == nil && info.Mode().IsRegular() {
			return info.Size()
		}
	}
	return -1
}

// compareCodePoints orders two UTF-8 strings by code point, which for valid
// UTF-8 is the order of their bytes.
func compareCodePoints(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// format is step 3: the top-level value is an object whose format_version is
// a string in the accepted table for document.
func (d *document) format(document string) (string, bool) {
	if d.kind(0) != '{' {
		return "", false
	}
	version, ok := d.member(0, "format_version")
	if !ok || d.kind(version) != '"' {
		return "", false
	}
	text := d.stringValue(version)
	if !slices.Contains(acceptedFormats[document], text) {
		return "", false
	}
	return text, true
}

func (d *document) objectMember(object uint32, name string, kind byte) (optional, bool) {
	node, ok := d.member(object, name)
	if !ok {
		return optional{}, true
	}
	return optional{node, true}, d.kind(node) == kind
}

// identity reads the members every resource of the read section names, as
// strings, and its index, a string or a number when present (ADR-0097).
func (d *document) identity(object uint32) (resource, bool) {
	var res resource
	for _, member := range []struct {
		name string
		into *uint32
	}{{"address", &res.address}, {"type", &res.typ}, {"name", &res.name}, {"mode", &res.mode}, {"provider_name", &res.provider}} {
		node, ok := d.member(object, member.name)
		if !ok || d.kind(node) != '"' {
			return res, false
		}
		*member.into = node
	}
	if index, ok := d.member(object, "index"); ok {
		if d.kind(index) != '"' && !d.isNumber(index) {
			return res, false
		}
		res.index = optional{index, true}
	}
	res.addressText = d.stringValue(res.address)
	return res, true
}

// planSection is step 4 for a plan. planned_values is an object;
// resource_changes, when present, is an array of resource changes, each an
// object with its identity and a change object whose actions are an array of
// strings, and whose after_unknown, when present, has a mirror's form.
func (d *document) planSection() ([]resource, bool) {
	planned, ok := d.objectMember(0, "planned_values", '{')
	if !planned.ok || !ok {
		return nil, false
	}
	changes, ok := d.objectMember(0, "resource_changes", '[')
	if !ok {
		return nil, false
	}
	if !changes.ok {
		return nil, true
	}
	var section []resource
	for item, end := changes.node+1, d.end(changes.node); item < end; item = d.next(item) {
		if d.kind(item) != '{' {
			return nil, false
		}
		res, ok := d.identity(item)
		if !ok {
			return nil, false
		}
		change, ok := d.member(item, "change")
		if !ok || d.kind(change) != '{' {
			return nil, false
		}
		actions, ok := d.member(change, "actions")
		if !ok || d.kind(actions) != '[' {
			return nil, false
		}
		for action, actionsEnd := actions+1, d.end(actions); action < actionsEnd; action = d.next(action) {
			if d.kind(action) != '"' {
				return nil, false
			}
		}
		res.actions = actions
		res.before = d.optionalMember(change, "before")
		res.after = d.optionalMember(change, "after")
		res.afterUnknown = d.optionalMember(change, "after_unknown")
		if res.afterUnknown.ok && !d.isMirror(res.afterUnknown.node) {
			return nil, false
		}
		res.beforeSensitive = d.optionalMember(change, "before_sensitive")
		res.afterSensitive = d.optionalMember(change, "after_sensitive")
		section = append(section, res)
	}
	return section, true
}

func (d *document) optionalMember(object uint32, name string) optional {
	node, ok := d.member(object, name)
	return optional{node, ok}
}

// stateSection is step 4 for a state. It has neither planned_values nor
// resource_changes. Its values, when present, are an object; absent values,
// or values without a root_module, read as a state with no resources (RFC 0038
// §2 step 4). A root_module, when present, is a module: an object whose
// resources, when present, are an array of resource objects with their
// identity, and whose child_modules, when present, are an array of modules.
// The section is in the order of a walk that takes a module's resources before
// its child modules.
func (d *document) stateSection() ([]resource, bool) {
	if _, ok := d.member(0, "planned_values"); ok {
		return nil, false
	}
	if _, ok := d.member(0, "resource_changes"); ok {
		return nil, false
	}
	values, ok := d.objectMember(0, "values", '{')
	if !ok {
		return nil, false
	}
	if !values.ok {
		return nil, true
	}
	root, ok := d.objectMember(values.node, "root_module", '{')
	if !ok {
		return nil, false
	}
	if !root.ok {
		return nil, true
	}
	var section []resource
	if !d.module(root.node, &section) {
		return nil, false
	}
	return section, true
}

func (d *document) module(module uint32, section *[]resource) bool {
	resources, ok := d.objectMember(module, "resources", '[')
	if !ok {
		return false
	}
	if resources.ok {
		for item, end := resources.node+1, d.end(resources.node); item < end; item = d.next(item) {
			if d.kind(item) != '{' {
				return false
			}
			res, ok := d.identity(item)
			if !ok {
				return false
			}
			res.values = d.optionalMember(item, "values")
			res.sensitiveValues = d.optionalMember(item, "sensitive_values")
			*section = append(*section, res)
		}
	}
	children, ok := d.objectMember(module, "child_modules", '[')
	if !ok {
		return false
	}
	if children.ok {
		for child, end := children.node+1, d.end(children.node); child < end; child = d.next(child) {
			if d.kind(child) != '{' || !d.module(child, section) {
				return false
			}
		}
	}
	return true
}

// selectResources keeps the resources src selects, in section order: type
// among select.types, mode equal to select.mode, the whole address matching
// select.address, and, for a plan, a change action among select.actions.
func (d *document) selectResources(section []resource, src Source) []resource {
	types := make(map[string]bool, len(src.Select.Types))
	for _, name := range src.Select.Types {
		types[name] = true
	}
	mode := src.Select.mode()
	var address *pattern
	if src.Select.Address != nil {
		address = compilePattern(*src.Select.Address)
	}
	var actions map[string]bool
	if src.Select.Actions != nil {
		actions = make(map[string]bool, len(src.Select.Actions))
		for _, action := range src.Select.Actions {
			actions[action] = true
		}
	}
	var out []resource
	for _, res := range section {
		if !types[d.stringValue(res.typ)] || d.stringValue(res.mode) != mode {
			continue
		}
		if address != nil && !address.match(res.addressText) {
			continue
		}
		if actions != nil && !d.anyAction(res.actions, actions) {
			continue
		}
		out = append(out, res)
		if len(out) > src.Select.max() {
			break // already past the bound
		}
	}
	return out
}

func (d *document) anyAction(list uint32, actions map[string]bool) bool {
	for action, end := list+1, d.end(list); action < end; action = d.next(action) {
		if actions[d.stringValue(action)] {
			return true
		}
	}
	return false
}

// resourceNumbers checks every number the resource's object would hold.
func (d *document) resourceNumbers(res resource) bool {
	if res.index.ok && !d.checkNumbers(res.index.node) {
		return false
	}
	for _, value := range []optional{res.before, res.after, res.values} {
		if value.ok && !d.checkNumbers(value.node) {
			return false
		}
	}
	return true
}

// writeAcquired writes the acquired value: an array of one object per
// selected resource, with each object's span in it. A counting pass measures
// it first, so it is written into one buffer of exactly its size, and a value
// over limit is refused before anything is allocated for it.
func (d *document) writeAcquired(selected []resource, document string, limit int64) ([]byte, [][2]int, bool) {
	unknowns := make([]*pointers, len(selected))
	counter := &writer{counting: true, limit: limit}
	if !d.writeAll(counter, selected, document, unknowns, nil) {
		return nil, nil, false
	}
	w := &writer{limit: limit, buf: make([]byte, 0, counter.written)}
	spans := make([][2]int, len(selected))
	if !d.writeAll(w, selected, document, unknowns, spans) || int64(len(w.buf)) != counter.written {
		panic("terraformfile: the writing pass differs from the counting pass")
	}
	return w.buf, spans, true
}

// writeAll writes every selected resource. The first pass also collects each
// plan resource's afterUnknown pointers, within what remains of the limit, by
// counting them before building them, and sorts them; the second reuses them.
func (d *document) writeAll(w *writer, selected []resource, document string, unknowns []*pointers, spans [][2]int) bool {
	w.byte('[')
	for i, res := range selected {
		if i > 0 {
			w.byte(',')
		}
		start := len(w.buf)
		if document == DocumentPlan && unknowns[i] == nil {
			list := &pointers{}
			if res.afterUnknown.ok {
				var ok bool
				if list, ok = d.collect(res.afterUnknown.node, res.after, res.afterSensitive, w.limit-w.written); !ok {
					w.over = true
					return false
				}
			}
			slices.SortFunc(list.spans, func(a, b [2]uint32) int {
				return bytes.Compare(list.data[a[0]:a[1]], list.data[b[0]:b[1]])
			})
			unknowns[i] = list
		}
		if !d.writeResource(w, res, document, unknowns[i]) {
			return false
		}
		if spans != nil {
			spans[i] = [2]int{start, len(w.buf)}
		}
	}
	w.byte(']')
	return !w.full()
}

// writeResource writes one resource's object, its members in canonical order:
// for a plan actions, address, after, afterUnknown, before, index, mode, name,
// providerName and type; for a state address, index, mode, name, providerName,
// type and values. A value that is absent or removed is not written.
func (d *document) writeResource(w *writer, res resource, document string, unknown *pointers) bool {
	present := func(value optional) bool { return value.ok && !d.isRemoved(value.node) }
	member := func(name string, value uint32) {
		w.text(`,"`)
		w.text(name)
		w.text(`":`)
		d.writeValue(w, value)
	}
	if document == DocumentPlan {
		w.text(`{"actions":`)
		d.writeValue(w, res.actions)
		member("address", res.address)
		if present(res.after) {
			member("after", res.after.node)
		}
		w.text(`,"afterUnknown":[`)
		for k := range unknown.spans {
			if k > 0 {
				w.byte(',')
			}
			w.string(unknown.at(k))
			if w.full() {
				return false
			}
		}
		w.byte(']')
		if present(res.before) {
			member("before", res.before.node)
		}
	} else {
		w.text(`{"address":`)
		d.writeValue(w, res.address)
	}
	if res.index.ok {
		member("index", res.index.node)
	}
	member("mode", res.mode)
	member("name", res.name)
	member("providerName", res.provider)
	member("type", res.typ)
	if document == DocumentState && present(res.values) {
		member("values", res.values.node)
	}
	w.byte('}')
	return !w.full()
}
