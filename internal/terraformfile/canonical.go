package terraformfile

import (
	"slices"
	"unicode/utf8"
)

// The canonical writer: RFC 8785 bytes written straight from the arena.
// Members are ordered by the UTF-16 code units of their decoded names, strings
// carry ECMAScript's minimal escapes, every number is written as its normal
// form in a string, and a removed member is skipped. No whitespace is written.

// writer writes canonical bytes in one of two passes. The counting pass
// stores nothing and only counts, so a value over limit costs no allocation;
// the writing pass then fills a buffer of exactly the counted size.
type writer struct {
	counting bool
	buf      []byte
	written  int64 // bytes counted or written
	limit    int64
	over     bool
}

func (w *writer) full() bool {
	if w.written > w.limit {
		w.over = true
	}
	return w.over
}

func (w *writer) byte(c byte) {
	w.written++
	if !w.counting {
		w.buf = append(w.buf, c)
	}
}

func (w *writer) raw(b []byte) {
	w.written += int64(len(b))
	if !w.counting {
		w.buf = append(w.buf, b...)
	}
}

func (w *writer) text(s string) {
	w.written += int64(len(s))
	if !w.counting {
		w.buf = append(w.buf, s...)
	}
}

// string writes s, UTF-8, as an RFC 8785 string.
func (w *writer) string(s []byte) {
	if w.counting {
		w.written += jsonStringLength(s)
		return
	}
	before := len(w.buf)
	w.buf = appendJSONBytes(w.buf, s)
	w.written += int64(len(w.buf) - before)
}

// jsonStringLength is the length of appendJSONBytes's output for s.
func jsonStringLength(s []byte) int64 {
	n := int64(2)
	for _, c := range s {
		switch {
		case c == '"' || c == '\\' || c == '\b' || c == '\t' || c == '\n' || c == '\f' || c == '\r':
			n += 2
		case c < 0x20:
			n += 6
		default:
			n++
		}
	}
	return n
}

const hexDigits = "0123456789abcdef"

// appendJSONBytes appends s, UTF-8, as an RFC 8785 string.
func appendJSONBytes(dst, s []byte) []byte {
	dst = append(dst, '"')
	start := 0
	for i, c := range s {
		if c >= 0x20 && c != '"' && c != '\\' {
			continue
		}
		dst = append(dst, s[start:i]...)
		start = i + 1
		switch c {
		case '"':
			dst = append(dst, '\\', '"')
		case '\\':
			dst = append(dst, '\\', '\\')
		case '\b':
			dst = append(dst, '\\', 'b')
		case '\t':
			dst = append(dst, '\\', 't')
		case '\n':
			dst = append(dst, '\\', 'n')
		case '\f':
			dst = append(dst, '\\', 'f')
		case '\r':
			dst = append(dst, '\\', 'r')
		default:
			dst = append(dst, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xF])
		}
	}
	dst = append(dst, s[start:]...)
	return append(dst, '"')
}

func appendJSONString(dst []byte, s string) []byte { return appendJSONBytes(dst, []byte(s)) }

// compareUTF16 orders two UTF-8 names by their UTF-16 code units, as RFC 8785
// orders members. Two different code points compare by their first code unit;
// when those are equal, both are supplementary and the second unit decides.
func compareUTF16(a, b []byte) int {
	for len(a) > 0 && len(b) > 0 {
		ra, na := utf8.DecodeRune(a)
		rb, nb := utf8.DecodeRune(b)
		if ra != rb {
			ua, la := utf16Units(ra)
			ub, lb := utf16Units(rb)
			if ua != ub {
				return int(ua) - int(ub)
			}
			return int(la) - int(lb)
		}
		a, b = a[na:], b[nb:]
	}
	return len(a) - len(b)
}

func utf16Units(r rune) (uint16, uint16) {
	if r < 0x10000 {
		return uint16(r), 0
	}
	r -= 0x10000
	return uint16(0xD800 + (r >> 10)), uint16(0xDC00 + (r & 0x3FF))
}

// writeValue writes node v's canonical form. Every number under v has
// already passed checkNumbers. An object's member keys are kept on one stack
// for the whole write, so a large object's keys are collected once per pass
// into storage that is reused.
func (d *document) writeValue(w *writer, v uint32) {
	if w.full() {
		return
	}
	switch c := d.kind(v); {
	case c == '{':
		mark, nameMark := len(d.keyStack), len(d.nameStack)
		d.keyStack, d.nameStack = d.memberKeys(v, d.keyStack, d.nameStack)
		keys := slices.DeleteFunc(d.keyStack[mark:], func(key nameKey) bool { return d.isRemoved(key.value) })
		slices.SortFunc(keys, func(a, b nameKey) int {
			return compareUTF16(keyBytes(d.data, d.nameStack, a), keyBytes(d.data, d.nameStack, b))
		})
		w.byte('{')
		for k, key := range keys {
			if k > 0 {
				w.byte(',')
			}
			w.string(keyBytes(d.data, d.nameStack, key))
			w.byte(':')
			d.writeValue(w, key.value)
			if w.over {
				break
			}
		}
		w.byte('}')
		d.keyStack, d.nameStack = d.keyStack[:mark], d.nameStack[:nameMark]
	case c == '[':
		w.byte('[')
		for child, end, first := v+1, d.end(v), true; child < end; child, first = d.next(child), false {
			if !first {
				w.byte(',')
			}
			d.writeValue(w, child)
			if w.over {
				return
			}
		}
		w.byte(']')
	case c == '"':
		w.string(d.decoded(v))
	case c == 't' || c == 'f' || c == 'n':
		w.raw(d.raw(v))
	default:
		text, ok := normalNumber(d.raw(v))
		if !ok {
			panic("terraformfile: a number reached the writer unchecked")
		}
		w.byte('"')
		w.text(text)
		w.byte('"')
	}
	w.full()
}

// decoded returns string node v's decoded bytes, decoding into the document's
// scratch buffer when the string has an escape. The result is valid until the
// next use of the buffer.
func (d *document) decoded(v uint32) []byte {
	content := d.raw(v)
	content = content[1 : len(content)-1]
	for _, c := range content {
		if c == '\\' {
			d.scratch = appendDecoded(d.scratch[:0], content)
			return d.scratch
		}
	}
	return content
}

// checkNumbers reports whether every number under v that the writer would
// write, removed positions skipped, has a normal form within MaxNumberChars.
func (d *document) checkNumbers(v uint32) bool {
	if d.isRemoved(v) {
		return true
	}
	switch c := d.kind(v); {
	case c == '{':
		for child, end := v+1, d.end(v); child < end; child = d.next(child + 1) {
			if !d.checkNumbers(child + 1) {
				return false
			}
		}
	case c == '[':
		for child, end := v+1, d.end(v); child < end; child = d.next(child) {
			if !d.checkNumbers(child) {
				return false
			}
		}
	case d.isNumber(v):
		_, ok := normalNumber(d.raw(v))
		return ok
	}
	return true
}
