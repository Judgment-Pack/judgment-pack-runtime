package emit

import (
	"errors"
	"slices"
	"strings"
)

// Glob is one owns pattern (RFC 0036 §1): segments separated by "/", where a
// segment "**" matches zero or more path segments, or one or more at the end
// of the glob, as in gitignore ("a/**/b" matches "a/b"; "gen/**" never
// matches "gen"); "*" inside a segment matches any run of characters other
// than "/"; and every other character is itself. There is no escape, no "?"
// and no class. "*" matches a segment that begins with ".": a glob names
// paths, not what a shell would list.
//
// A trailing "**" is held as "*" followed by "**", so that every "**" the
// matchers meet matches zero or more segments.
type Glob struct {
	source   string
	segments []globSegment
}

type globSegment struct {
	any     bool   // "**"
	pattern []rune // literal runes and '*'
}

// String returns the glob as written.
func (g Glob) String() string { return g.source }

var errGlob = errors.New("emit: not an owns glob")

// parseGlob reads one owns glob. It is held to the response path rules
// (checkPathForm) with "*" allowed in a segment, and "**" only as a whole
// segment; so a glob is relative and cannot climb out of the repository.
func parseGlob(source string) (Glob, error) {
	if checkPathForm(source) != nil {
		return Glob{}, errGlob
	}
	g := Glob{source: source}
	for _, segment := range strings.Split(source, "/") {
		if segment == "**" {
			g.segments = append(g.segments, globSegment{any: true})
			continue
		}
		if strings.Contains(segment, "**") {
			return Glob{}, errGlob
		}
		g.segments = append(g.segments, globSegment{pattern: []rune(segment)})
	}
	if last := len(g.segments) - 1; g.segments[last].any {
		g.segments = append(g.segments[:last], globSegment{pattern: []rune("*")}, globSegment{any: true})
	}
	return g, nil
}

// matchSegment reports whether one path segment matches a segment pattern.
func matchSegment(pattern []rune, segment []rune) bool {
	p, s := 0, 0
	star, mark := -1, 0
	for s < len(segment) {
		switch {
		case p < len(pattern) && pattern[p] == '*':
			star, mark = p, s
			p++
		case p < len(pattern) && pattern[p] == segment[s]:
			p++
			s++
		case star >= 0:
			p = star + 1
			mark++
			s = mark
		default:
			return false
		}
	}
	for p < len(pattern) && pattern[p] == '*' {
		p++
	}
	return p == len(pattern)
}

func splitRunes(path string) [][]rune {
	parts := strings.Split(path, "/")
	out := make([][]rune, len(parts))
	for i, part := range parts {
		out[i] = []rune(part)
	}
	return out
}

// Match reports whether the glob matches a path of "/"-separated segments.
func (g Glob) Match(path string) bool {
	return g.matchSegments(splitRunes(path), false)
}

// MatchBelow reports whether the glob can match some path strictly below the
// directory dir ("" for the repository root): a walk descends only where it
// can.
func (g Glob) MatchBelow(dir string) bool {
	var segments [][]rune
	if dir != "" {
		segments = splitRunes(dir)
	}
	return g.matchSegments(segments, true)
}

// matchSegments matches the glob against path; with below, path is a
// directory and the glob must match it followed by at least one more segment.
func (g Glob) matchSegments(path [][]rune, below bool) bool {
	type state struct{ i, j int }
	memo := map[state]bool{}
	var match func(i, j int) bool
	match = func(i, j int) bool {
		key := state{i, j}
		if result, ok := memo[key]; ok {
			return result
		}
		var result bool
		switch {
		case j == len(path) && below:
			// Some segment of the glob is left to match what lies below.
			result = i < len(g.segments)
		case i == len(g.segments):
			result = j == len(path)
		case g.segments[i].any:
			result = match(i+1, j) || j < len(path) && match(i, j+1)
		case j == len(path):
			result = false
		default:
			result = matchSegment(g.segments[i].pattern, path[j]) && match(i+1, j+1)
		}
		memo[key] = result
		return result
	}
	return match(0, 0)
}

// Overlaps reports whether some path matches both globs.
func (g Glob) Overlaps(other Glob) bool {
	a, b := g.segments, other.segments
	type state struct {
		i, j     int
		nonEmpty bool
	}
	memo := map[state]bool{}
	var overlap func(i, j int, nonEmpty bool) bool
	overlap = func(i, j int, nonEmpty bool) bool {
		key := state{i, j, nonEmpty}
		if result, ok := memo[key]; ok {
			return result
		}
		memo[key] = false // a state met again on its own path adds nothing
		var result bool
		switch {
		case i == len(a) && j == len(b):
			result = nonEmpty
		case i < len(a) && a[i].any && j < len(b) && b[j].any:
			result = overlap(i+1, j, nonEmpty) || overlap(i, j+1, nonEmpty) || overlap(i, j, true)
		case i < len(a) && a[i].any:
			result = overlap(i+1, j, nonEmpty) || j < len(b) && overlap(i, j+1, true)
		case j < len(b) && b[j].any:
			result = overlap(i, j+1, nonEmpty) || i < len(a) && overlap(i+1, j, true)
		case i == len(a) || j == len(b):
			result = false
		default:
			result = segmentsIntersect(a[i].pattern, b[j].pattern) && overlap(i+1, j+1, true)
		}
		memo[key] = result
		return result
	}
	return overlap(0, 0, false)
}

// segmentsIntersect reports whether some non-empty segment matches both
// segment patterns: a search of the product of the two patterns' automata,
// where '*' loops on any character and a literal steps on itself.
func segmentsIntersect(a, b []rune) bool {
	type state struct {
		i, j     int
		consumed bool
	}
	seen := map[state]bool{}
	queue := []state{{0, 0, false}}
	for len(queue) > 0 {
		s := queue[0]
		queue = queue[1:]
		if seen[s] {
			continue
		}
		seen[s] = true
		if s.i == len(a) && s.j == len(b) && s.consumed {
			return true
		}
		starA := s.i < len(a) && a[s.i] == '*'
		starB := s.j < len(b) && b[s.j] == '*'
		if starA {
			queue = append(queue, state{s.i + 1, s.j, s.consumed})
		}
		if starB {
			queue = append(queue, state{s.i, s.j + 1, s.consumed})
		}
		if s.i == len(a) || s.j == len(b) {
			continue
		}
		// Consume one character both sides accept.
		nextA, nextB := s.i+1, s.j+1
		if starA {
			nextA = s.i
		}
		if starB {
			nextB = s.j
		}
		if starA || starB || a[s.i] == b[s.j] {
			queue = append(queue, state{nextA, nextB, true})
		}
	}
	return false
}

// NameKey is the key under which a file system that reads names loosely may
// read two slash paths as one: the code points HFS+ ignores in a name left
// out (hfsIgnorable), then Unicode canonical caseless matching (foldKey),
// then upper case, as NTFS compares names, so that "ı" and "i" share a key;
// then, as Windows reads a name, a backslash read as a separator and each
// segment's trailing dots and spaces dropped, so that "venv." and "venv"
// share a key. A segment made only of dots, spaces or ignored code points
// has an empty key, as Windows reads it as no name at all. It is meant for a
// comparison that must not miss such a match; two paths no file system
// reads as one may share a key too.
func NameKey(path string) string {
	kept := strings.Map(func(r rune) rune {
		if hfsIgnorable(r) {
			return -1
		}
		return r
	}, path)
	segments := strings.Split(strings.ReplaceAll(strings.ToUpper(foldKey(kept)), `\`, "/"), "/")
	for i, segment := range segments {
		segments[i] = strings.TrimRight(segment, ". ")
	}
	return strings.Join(segments, "/")
}

// Loose returns the glob as it may match on a file system that reads names
// loosely, for paths given by their NameKey: each segment's characters
// through NameKey, and a segment holding "~", which Windows can read as the
// short name of a longer one, as "*". A path such a file system reads as one
// the glob matches has a NameKey the loose glob matches; a short name set by
// hand, without "~", is not followed. (No glob segment ends in a dot or a
// space, or holds a backslash: parseGlob refuses them.)
func (g Glob) Loose() Glob {
	loose := Glob{source: g.source, segments: make([]globSegment, len(g.segments))}
	for i, segment := range g.segments {
		switch {
		case segment.any:
			loose.segments[i] = segment
		case slices.Contains(segment.pattern, '~'):
			loose.segments[i] = globSegment{pattern: []rune("*")}
		default:
			loose.segments[i] = globSegment{pattern: []rune(NameKey(string(segment.pattern)))}
		}
	}
	return loose
}

// MayOwnAtOrBelow reports whether an owns glob of the manifest may match
// path, or a path below it: the host's EMIT-LINK rule for a link at path
// (scanOwned), with names read loosely as well (Glob.Loose), so that a
// response path a file system reads as path, or as one below it, counts. A
// response could write at path or through it only when this holds.
func (m *Manifest) MayOwnAtOrBelow(path string) bool {
	key := NameKey(path)
	for _, glob := range m.Owns {
		loose := glob.Loose()
		if glob.Match(path) || glob.MatchBelow(path) || loose.Match(key) || loose.MatchBelow(key) {
			return true
		}
	}
	return false
}
