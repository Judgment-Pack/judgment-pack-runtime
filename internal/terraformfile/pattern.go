package terraformfile

import "strings"

// The select.address pattern (RFC 0038 §4): it matches a whole address, `*`
// matches any run, possibly empty, of characters other than `.`, `[` and `]`,
// and every other character matches itself.
//
// Since `*` never crosses a delimiter, a pattern and the address it matches
// have the same delimiters in the same order. The matcher splits both at them
// and matches segment to segment. Within a segment, the literal pieces between
// stars are found leftmost, in order, which is exact for a glob whose only
// operator is `*`. Nothing backtracks, so a match costs time linear in the
// address for a pattern of bounded length.

func isDelimiter(c byte) bool { return c == '.' || c == '[' || c == ']' }

type pattern struct {
	segments   [][]string // each segment's literal pieces, split at `*`
	delimiters []byte
}

func compilePattern(text string) *pattern {
	p := &pattern{}
	start := 0
	for i := 0; i <= len(text); i++ {
		if i < len(text) && !isDelimiter(text[i]) {
			continue
		}
		p.segments = append(p.segments, strings.Split(text[start:i], "*"))
		if i < len(text) {
			p.delimiters = append(p.delimiters, text[i])
		}
		start = i + 1
	}
	return p
}

func (p *pattern) match(address string) bool {
	segment, start := 0, 0
	for i := 0; i <= len(address); i++ {
		if i < len(address) && !isDelimiter(address[i]) {
			continue
		}
		if segment >= len(p.segments) || !matchSegment(p.segments[segment], address[start:i]) {
			return false
		}
		if i < len(address) && (segment >= len(p.delimiters) || p.delimiters[segment] != address[i]) {
			return false
		}
		segment++
		start = i + 1
	}
	return segment == len(p.segments)
}

// matchSegment matches one delimiter-free segment against pieces, the
// pattern segment split at its stars.
func matchSegment(pieces []string, text string) bool {
	if len(pieces) == 1 {
		return pieces[0] == text
	}
	first, last := pieces[0], pieces[len(pieces)-1]
	if len(text) < len(first)+len(last) || !strings.HasPrefix(text, first) || !strings.HasSuffix(text, last) {
		return false
	}
	middle := text[len(first) : len(text)-len(last)]
	for _, piece := range pieces[1 : len(pieces)-1] {
		at := strings.Index(middle, piece)
		if at < 0 {
			return false
		}
		middle = middle[at+len(piece):]
	}
	return true
}
