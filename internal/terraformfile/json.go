package terraformfile

import (
	"bytes"
	"slices"
	"unicode/utf16"
	"unicode/utf8"
)

// The strict JSON of RFC 0038 §2 step 2, read into an arena.
//
// The input is scanned twice by one validating parser: the first pass only
// counts nodes, so the second can fill an arena of exactly that many nodes
// with no growth. A node is 12 bytes, so the arena's worst case is `[0,0,...]`
// at one node per two input bytes: 6 bytes of arena per input byte
// (ADR-0097). Numbers stay as their text; nothing is decoded until it is read.

// node is one value or member name, in pre-order: a node's first child is the
// next node, and its next sibling is size nodes on. An object's children
// alternate member name and value. The kind is the node's first byte.
type node struct {
	start, end uint32 // the node's bytes, end exclusive
	size       uint32 // the nodes of the subtree, this one included
}

// document is a parsed input: its bytes and its arena.
type document struct {
	data    []byte
	nodes   []node
	removed []uint64 // a bit per node: removed by §2 step 6
	scratch []byte   // reused for decoding escaped strings
	// The writer's stacks of member keys and decoded names.
	keyStack  []nameKey
	nameStack []byte
}

type parser struct {
	data  []byte
	pos   int
	count int
	nodes []node // nil while counting
}

// parseStrict reads data as one strict JSON value, or reports false. It
// refuses a byte-order mark, invalid UTF-8, a lone surrogate escape, anything
// but white space after the value, a value deeper than MaxDepth, and a
// duplicate member name in any object.
func parseStrict(data []byte) (*document, bool) {
	if bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}) || !utf8.Valid(data) {
		return nil, false
	}
	if int64(len(data)) > MaxBytesLimit {
		return nil, false
	}
	counter := &parser{data: data}
	if !counter.document() {
		return nil, false
	}
	builder := &parser{data: data, nodes: make([]node, 0, counter.count)}
	if !builder.document() {
		return nil, false
	}
	doc := &document{data: data, nodes: builder.nodes, removed: make([]uint64, (len(builder.nodes)+63)/64)}
	if doc.hasDuplicateMember() {
		return nil, false
	}
	return doc, true
}

func (p *parser) document() bool {
	p.space()
	if !p.value(1) {
		return false
	}
	p.space()
	return p.pos == len(p.data)
}

func (p *parser) space() {
	for p.pos < len(p.data) {
		switch p.data[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

// open starts a node at the current position and returns its index.
func (p *parser) open() int {
	p.count++
	if p.nodes == nil {
		return -1
	}
	p.nodes = append(p.nodes, node{start: uint32(p.pos)})
	return len(p.nodes) - 1
}

// close ends node index at the current position.
func (p *parser) close(index int) {
	if index < 0 {
		return
	}
	p.nodes[index].end = uint32(p.pos)
	p.nodes[index].size = uint32(len(p.nodes) - index)
}

func (p *parser) value(depth int) bool {
	if depth > MaxDepth || p.pos >= len(p.data) {
		return false
	}
	switch c := p.data[p.pos]; {
	case c == '{':
		return p.object(depth)
	case c == '[':
		return p.array(depth)
	case c == '"':
		return p.string()
	case c == 't':
		return p.literal("true")
	case c == 'f':
		return p.literal("false")
	case c == 'n':
		return p.literal("null")
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	default:
		return false
	}
}

func (p *parser) object(depth int) bool {
	index := p.open()
	p.pos++
	p.space()
	if p.pos < len(p.data) && p.data[p.pos] == '}' {
		p.pos++
		p.close(index)
		return true
	}
	for {
		if p.pos >= len(p.data) || p.data[p.pos] != '"' || !p.string() {
			return false
		}
		p.space()
		if p.pos >= len(p.data) || p.data[p.pos] != ':' {
			return false
		}
		p.pos++
		p.space()
		if !p.value(depth + 1) {
			return false
		}
		p.space()
		if p.pos >= len(p.data) {
			return false
		}
		switch p.data[p.pos] {
		case ',':
			p.pos++
			p.space()
		case '}':
			p.pos++
			p.close(index)
			return true
		default:
			return false
		}
	}
}

func (p *parser) array(depth int) bool {
	index := p.open()
	p.pos++
	p.space()
	if p.pos < len(p.data) && p.data[p.pos] == ']' {
		p.pos++
		p.close(index)
		return true
	}
	for {
		if !p.value(depth + 1) {
			return false
		}
		p.space()
		if p.pos >= len(p.data) {
			return false
		}
		switch p.data[p.pos] {
		case ',':
			p.pos++
			p.space()
		case ']':
			p.pos++
			p.close(index)
			return true
		default:
			return false
		}
	}
}

func (p *parser) literal(word string) bool {
	if !bytes.HasPrefix(p.data[p.pos:], []byte(word)) {
		return false
	}
	index := p.open()
	p.pos += len(word)
	p.close(index)
	return true
}

func (p *parser) digits() int {
	start := p.pos
	for p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
		p.pos++
	}
	return p.pos - start
}

// number reads RFC 8259's number grammar. What may follow it is the caller's
// to check, so `01` fails there, at the `1`.
func (p *parser) number() bool {
	index := p.open()
	if p.data[p.pos] == '-' {
		p.pos++
	}
	if p.pos >= len(p.data) {
		return false
	}
	if p.data[p.pos] == '0' {
		p.pos++
	} else if p.digits() == 0 {
		return false
	}
	if p.pos < len(p.data) && p.data[p.pos] == '.' {
		p.pos++
		if p.digits() == 0 {
			return false
		}
	}
	if p.pos < len(p.data) && (p.data[p.pos] == 'e' || p.data[p.pos] == 'E') {
		p.pos++
		if p.pos < len(p.data) && (p.data[p.pos] == '+' || p.data[p.pos] == '-') {
			p.pos++
		}
		if p.digits() == 0 {
			return false
		}
	}
	p.close(index)
	return true
}

func hexValue(c byte) (rune, bool) {
	switch {
	case c >= '0' && c <= '9':
		return rune(c - '0'), true
	case c >= 'a' && c <= 'f':
		return rune(c-'a') + 10, true
	case c >= 'A' && c <= 'F':
		return rune(c-'A') + 10, true
	}
	return 0, false
}

// unit reads the four hex digits of a \u escape at data[at:].
func unit(data []byte, at int) (rune, bool) {
	if at+4 > len(data) {
		return 0, false
	}
	var value rune
	for _, c := range data[at : at+4] {
		digit, ok := hexValue(c)
		if !ok {
			return 0, false
		}
		value = value<<4 | digit
	}
	return value, true
}

// string reads one string. The input is already known to be UTF-8; a control
// character must be escaped, and a surrogate escape must be a high one
// followed at once by a low one.
func (p *parser) string() bool {
	index := p.open()
	p.pos++
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		switch {
		case c == '"':
			p.pos++
			p.close(index)
			return true
		case c < 0x20:
			return false
		case c != '\\':
			p.pos++
			continue
		}
		if p.pos+1 >= len(p.data) {
			return false
		}
		switch p.data[p.pos+1] {
		case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
			p.pos += 2
		case 'u':
			first, ok := unit(p.data, p.pos+2)
			if !ok {
				return false
			}
			p.pos += 6
			if utf16.IsSurrogate(first) {
				if first >= 0xDC00 || p.pos+1 >= len(p.data) || p.data[p.pos] != '\\' || p.data[p.pos+1] != 'u' {
					return false
				}
				second, ok := unit(p.data, p.pos+2)
				if !ok || second < 0xDC00 || second > 0xDFFF {
					return false
				}
				p.pos += 6
			}
		default:
			return false
		}
	}
	return false
}

// kind returns a node's first byte: `{`, `[`, `"`, `t`, `f`, `n`, or the
// first character of a number.
func (d *document) kind(i uint32) byte { return d.data[d.nodes[i].start] }

func (d *document) isNumber(i uint32) bool {
	c := d.kind(i)
	return c == '-' || (c >= '0' && c <= '9')
}

// next returns the node after i's subtree: its next sibling, or the end of its
// parent.
func (d *document) next(i uint32) uint32 { return i + d.nodes[i].size }

// end returns the index past i's subtree.
func (d *document) end(i uint32) uint32 { return i + d.nodes[i].size }

func (d *document) raw(i uint32) []byte {
	n := d.nodes[i]
	return d.data[n.start:n.end]
}

// stringValue returns string node i's decoded text.
func (d *document) stringValue(i uint32) string {
	return string(d.decoded(i))
}

// appendDecoded appends the decoded form of a string's content, which the
// parser has already admitted.
func appendDecoded(dst, content []byte) []byte {
	for i := 0; i < len(content); {
		c := content[i]
		if c != '\\' {
			start := i
			for i < len(content) && content[i] != '\\' {
				i++
			}
			dst = append(dst, content[start:i]...)
			continue
		}
		switch content[i+1] {
		case 'b':
			dst = append(dst, '\b')
		case 'f':
			dst = append(dst, '\f')
		case 'n':
			dst = append(dst, '\n')
		case 'r':
			dst = append(dst, '\r')
		case 't':
			dst = append(dst, '\t')
		case 'u':
			first, _ := unit(content, i+2)
			if utf16.IsSurrogate(first) {
				second, _ := unit(content, i+8)
				dst = utf8.AppendRune(dst, utf16.DecodeRune(first, second))
				i += 12
				continue
			}
			dst = utf8.AppendRune(dst, first)
			i += 6
			continue
		default: // '"', '\\', '/'
			dst = append(dst, content[i+1])
		}
		i += 2
	}
	return dst
}

// nameKey is a member name's decoded bytes, in the document's bytes or in a
// decoding buffer, with the member's value node.
type nameKey struct {
	lo, hi  uint32
	decoded bool
	value   uint32
}

// growTo returns s with room for n elements, doubling its capacity when it
// grows. append grows a large slice by about a quarter at a time, which would
// allocate several times a large object's keys while they are collected.
func growTo[T any](s []T, n int) []T {
	if cap(s) >= n {
		return s
	}
	out := make([]T, len(s), max(n, 2*cap(s)))
	copy(out, s)
	return out
}

// memberCount returns the members of object i.
func (d *document) memberCount(i uint32) int {
	count := 0
	for child, end := i+1, d.end(i); child < end; child = d.next(child + 1) {
		count++
	}
	return count
}

// memberKeys appends the keys of object i's members to keys, decoding escaped
// names into buffer. It returns both.
func (d *document) memberKeys(i uint32, keys []nameKey, buffer []byte) ([]nameKey, []byte) {
	keys = growTo(keys, len(keys)+d.memberCount(i))
	for child, end := i+1, d.end(i); child < end; child = d.next(child + 1) {
		n := d.nodes[child]
		content := d.data[n.start+1 : n.end-1]
		if bytes.IndexByte(content, '\\') < 0 {
			keys = append(keys, nameKey{lo: n.start + 1, hi: n.end - 1, value: child + 1})
			continue
		}
		lo := len(buffer)
		buffer = appendDecoded(growTo(buffer, len(buffer)+len(content)), content)
		keys = append(keys, nameKey{lo: uint32(lo), hi: uint32(len(buffer)), decoded: true, value: child + 1})
	}
	return keys, buffer
}

func keyBytes(data, buffer []byte, key nameKey) []byte {
	if key.decoded {
		return buffer[key.lo:key.hi]
	}
	return data[key.lo:key.hi]
}

// hasDuplicateMember reports whether any object holds two members with the
// same decoded name. Each object's names are sorted, so the check costs
// O(m log m) for an object of m members.
func (d *document) hasDuplicateMember() bool {
	var keys []nameKey
	var buffer []byte
	for i := range d.nodes {
		if d.data[d.nodes[i].start] != '{' || d.nodes[i].size < 5 {
			continue // fewer than two members
		}
		keys, buffer = d.memberKeys(uint32(i), keys[:0], buffer[:0])
		slices.SortFunc(keys, func(a, b nameKey) int {
			return bytes.Compare(keyBytes(d.data, buffer, a), keyBytes(d.data, buffer, b))
		})
		for k := 1; k < len(keys); k++ {
			if bytes.Equal(keyBytes(d.data, buffer, keys[k-1]), keyBytes(d.data, buffer, keys[k])) {
				return true
			}
		}
	}
	return false
}

// members is an object's member index, sorted by decoded name, for lookups.
type members struct {
	keys   []nameKey
	buffer []byte
	data   []byte
}

func (d *document) index(i uint32) *members {
	keys, buffer := d.memberKeys(i, nil, nil)
	slices.SortFunc(keys, func(a, b nameKey) int {
		return bytes.Compare(keyBytes(d.data, buffer, a), keyBytes(d.data, buffer, b))
	})
	return &members{keys: keys, buffer: buffer, data: d.data}
}

// lookup returns the value node of the member named name.
func (m *members) lookup(name []byte) (uint32, bool) {
	at, found := slices.BinarySearchFunc(m.keys, name, func(key nameKey, target []byte) int {
		return bytes.Compare(keyBytes(m.data, m.buffer, key), target)
	})
	if !found {
		return 0, false
	}
	return m.keys[at].value, true
}

// member returns the value node of object i's member named name, by a linear
// scan. It is for the format's own member names, looked up a fixed number of
// times per object.
func (d *document) member(i uint32, name string) (uint32, bool) {
	for child, end := i+1, d.end(i); child < end; child = d.next(child + 1) {
		n := d.nodes[child]
		content := d.data[n.start+1 : n.end-1]
		if bytes.IndexByte(content, '\\') >= 0 {
			d.scratch = appendDecoded(d.scratch[:0], content)
			content = d.scratch
		}
		if string(content) == name {
			return child + 1, true
		}
	}
	return 0, false
}

// elements returns the element nodes of array i.
func (d *document) elements(i uint32) []uint32 {
	var out []uint32
	for child, end := i+1, d.end(i); child < end; child = d.next(child) {
		out = append(out, child)
	}
	return out
}

func (d *document) isRemoved(i uint32) bool { return d.removed[i/64]&(1<<(i%64)) != 0 }

func (d *document) remove(i uint32) { d.removed[i/64] |= 1 << (i % 64) }
