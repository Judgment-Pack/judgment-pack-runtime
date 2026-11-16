package terraformfile

import (
	"bytes"
	"strconv"
)

// RFC 0038 §2 steps 5 and 6: the sensitivity mirror on every resource of the
// read section, and the removal of every position it marks.
//
// A mirror position is matched to the value as a JSON Pointer token would be:
// a mirror member named "0" names element 0 of an array value, and a mirror
// array's element 0 names the member "0" of an object value (ADR-0097). That
// reading removes at least as much as any narrower one.

// isMirror reports whether m is true, false, or an object or array of
// mirrors, at every position.
func (d *document) isMirror(m uint32) bool {
	switch d.kind(m) {
	case 't', 'f':
		return true
	case '{':
		for child, end := m+1, d.end(m); child < end; child = d.next(child + 1) {
			if !d.isMirror(child + 1) {
				return false
			}
		}
		return true
	case '[':
		for child, end := m+1, d.end(m); child < end; child = d.next(child) {
			if !d.isMirror(child) {
				return false
			}
		}
		return true
	}
	return false
}

// nameOf returns a member name node's decoded bytes in storage of their own:
// the input's bytes when the name has no escape, a new slice otherwise.
func (d *document) nameOf(n uint32) []byte {
	content := d.raw(n)
	content = content[1 : len(content)-1]
	if bytes.IndexByte(content, '\\') < 0 {
		return content
	}
	return appendDecoded(nil, content)
}

// arrayIndex reads a pointer token as an index of an array of length n: "0",
// or a digit 1 to 9 followed by digits, less than n.
func arrayIndex(token []byte, n int) (int, bool) {
	if len(token) == 0 || len(token) > 10 || (token[0] == '0' && len(token) > 1) {
		return 0, false
	}
	value := 0
	for _, c := range token {
		if c < '0' || c > '9' {
			return 0, false
		}
		value = value*10 + int(c-'0')
	}
	return value, value < n
}

// strip walks mirror m over value v, marking what is removed. It returns the
// object members removed under v, and whether v itself is to be removed: a
// mirror of true at v, or at an element of an array v is or holds on the way
// to the nearest object member. When v is removed its count is dropped, since
// a removal counts once however much it contained.
func (d *document) strip(m, v uint32) (int, bool) {
	switch d.kind(m) {
	case 't':
		return 0, true
	case '{':
		if d.end(m) == m+1 {
			return 0, false
		}
		switch d.kind(v) {
		case '{':
			index := d.index(v)
			count := 0
			for child, end := m+1, d.end(m); child < end; child = d.next(child + 1) {
				target, ok := index.lookup(d.nameOf(child))
				if !ok {
					continue
				}
				count += d.stripMember(child+1, target)
			}
			return count, false
		case '[':
			elements := d.elements(v)
			count := 0
			for child, end := m+1, d.end(m); child < end; child = d.next(child + 1) {
				at, ok := arrayIndex(d.nameOf(child), len(elements))
				if !ok {
					continue
				}
				inner, remove := d.strip(child+1, elements[at])
				if remove {
					return 0, true
				}
				count += inner
			}
			return count, false
		}
	case '[':
		if d.end(m) == m+1 {
			return 0, false
		}
		switch d.kind(v) {
		case '[':
			count := 0
			element, valueEnd := v+1, d.end(v)
			for child, end := m+1, d.end(m); child < end && element < valueEnd; child, element = d.next(child), d.next(element) {
				inner, remove := d.strip(child, element)
				if remove {
					return 0, true
				}
				count += inner
			}
			return count, false
		case '{':
			index := d.index(v)
			count, k := 0, 0
			for child, end := m+1, d.end(m); child < end; child, k = d.next(child), k+1 {
				target, ok := index.lookup(strconv.AppendInt(nil, int64(k), 10))
				if !ok {
					continue
				}
				count += d.stripMember(child, target)
			}
			return count, false
		}
	}
	return 0, false
}

// stripMember applies mirror m to the value of an object member, v. A removed
// member counts one.
func (d *document) stripMember(m, v uint32) int {
	inner, remove := d.strip(m, v)
	if remove {
		d.remove(v)
		return 1
	}
	return inner
}

// stripRoot applies mirror m to a value of the read section, which counts as
// an object member of its resource: a removed value is absent from the
// acquired value, and counts one.
func (d *document) stripRoot(m uint32, v optional) int {
	if !v.ok {
		return 0
	}
	return d.stripMember(m, v.node)
}

// pointers collects afterUnknown pointers into one buffer, within a budget of
// bytes. path is the pointer of the position being walked; it is one buffer
// for the whole walk, so a long prefix is never copied once per child.
type pointers struct {
	data   []byte
	spans  [][2]uint32
	budget int64
	path   []byte
}

func (p *pointers) add() bool {
	if int64(len(p.data)+len(p.path)) > p.budget {
		return false
	}
	start := len(p.data)
	p.data = append(p.data, p.path...)
	p.spans = append(p.spans, [2]uint32{uint32(start), uint32(len(p.data))})
	return true
}

func (p *pointers) at(i int) []byte { return p.data[p.spans[i][0]:p.spans[i][1]] }

// push appends one escaped token to the path, and returns the length to
// truncate it back to.
func (p *pointers) push(token []byte) int {
	mark := len(p.path)
	p.path = append(p.path, '/')
	for _, c := range token {
		switch c {
		case '~':
			p.path = append(p.path, '~', '0')
		case '/':
			p.path = append(p.path, '~', '1')
		default:
			p.path = append(p.path, c)
		}
	}
	return mark
}

// children prepares the lookups of a's children for a walk of u's: an
// object's member index, or an array's elements.
func (d *document) children(u uint32, a optional) (*members, []uint32) {
	if !a.ok || d.end(u) == u+1 {
		return nil, nil
	}
	switch d.kind(a.node) {
	case '{':
		return d.index(a.node), nil
	case '[':
		return nil, d.elements(a.node)
	}
	return nil, nil
}

// child finds the child of a that a pointer token names.
func child(index *members, elements []uint32, token []byte) optional {
	if index != nil {
		node, ok := index.lookup(token)
		return optional{node, ok}
	}
	if at, ok := arrayIndex(token, len(elements)); ok {
		return optional{elements[at], true}
	}
	return optional{}
}

// unknown walks after_unknown, u, over after, a, and adds the pointer of each
// position u marks true. A position at or below one removed from after is
// dropped. after may lack a position u marks, since Terraform leaves a value
// unknown at plan time out of after. It reports false when the pointers pass
// their budget.
func (d *document) unknown(u uint32, a optional, out *pointers) bool {
	if a.ok && d.isRemoved(a.node) {
		return true
	}
	switch d.kind(u) {
	case 't':
		return out.add()
	case '{':
		index, elements := d.children(u, a)
		for name, end := u+1, d.end(u); name < end; name = d.next(name + 1) {
			token := d.nameOf(name)
			mark := out.push(token)
			ok := d.unknown(name+1, child(index, elements, token), out)
			out.path = out.path[:mark]
			if !ok {
				return false
			}
		}
	case '[':
		index, elements := d.children(u, a)
		var token []byte
		for element, end, k := u+1, d.end(u), 0; element < end; element, k = d.next(element), k+1 {
			token = strconv.AppendInt(token[:0], int64(k), 10)
			mark := out.push(token)
			ok := d.unknown(element, child(index, elements, token), out)
			out.path = out.path[:mark]
			if !ok {
				return false
			}
		}
	}
	return true
}
