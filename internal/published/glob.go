package published

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

// Glob is one slash-separated pattern over a repository path or a tag name.
// Each segment is matched by path.Match, so "*" and "?" never cross a slash;
// a segment that is exactly "**" matches any number of whole segments, none
// included. Matching is on bytes, case-sensitive, on every platform.
type Glob struct {
	text     string
	segments []string
}

// maxGlobBytes bounds one pattern.
const maxGlobBytes = 1024

// CompileGlob checks and compiles one pattern.
func CompileGlob(text string) (Glob, error) {
	if text == "" {
		return Glob{}, errors.New("a pattern cannot be empty")
	}
	if len(text) > maxGlobBytes {
		return Glob{}, fmt.Errorf("a pattern is limited to %d bytes", maxGlobBytes)
	}
	segments := strings.Split(text, "/")
	compacted := segments[:0]
	for _, segment := range segments {
		if segment == "" {
			return Glob{}, fmt.Errorf("pattern %q has an empty segment", text)
		}
		if segment == "**" {
			if len(compacted) > 0 && compacted[len(compacted)-1] == "**" {
				continue
			}
		} else if strings.Contains(segment, "**") {
			return Glob{}, fmt.Errorf("pattern %q: ** must be a whole segment", text)
		} else if _, err := path.Match(segment, ""); err != nil {
			return Glob{}, fmt.Errorf("pattern %q is malformed", text)
		}
		compacted = append(compacted, segment)
	}
	return Glob{text: text, segments: compacted}, nil
}

// String is the pattern as given.
func (g Glob) String() string { return g.text }

// Match reports whether the whole name matches.
func (g Glob) Match(name string) bool {
	return matchSegments(g.segments, strings.Split(name, "/"))
}

func matchSegments(pattern, name []string) bool {
	for len(pattern) > 0 {
		if pattern[0] == "**" {
			rest := pattern[1:]
			for skip := 0; skip <= len(name); skip++ {
				if matchSegments(rest, name[skip:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		if matched, err := path.Match(pattern[0], name[0]); err != nil || !matched {
			return false
		}
		pattern, name = pattern[1:], name[1:]
	}
	return len(name) == 0
}

// mayMatchBelow reports whether some path under the directory dir (its
// segments) could match: false only when none can, so a walk may skip it.
func (g Glob) mayMatchBelow(dir []string) bool {
	pattern := g.segments
	for _, segment := range dir {
		if len(pattern) == 0 {
			return false
		}
		if pattern[0] == "**" {
			return true
		}
		if matched, err := path.Match(pattern[0], segment); err != nil || !matched {
			return false
		}
		pattern = pattern[1:]
	}
	return len(pattern) > 0
}

// coversAllBelow reports whether every path under the directory dir matches:
// true only when that is certain, so a walk may skip the directory for an
// exclusion.
func (g Glob) coversAllBelow(dir []string) bool {
	pattern := g.segments
	for _, segment := range dir {
		if len(pattern) == 0 || pattern[0] == "**" {
			return len(pattern) == 1
		}
		if matched, err := path.Match(pattern[0], segment); err != nil || !matched {
			return false
		}
		pattern = pattern[1:]
	}
	return len(pattern) == 1 && pattern[0] == "**"
}

// Selection is the paths a run reads: those that match an include pattern
// and no exclude pattern.
type Selection struct {
	Include []Glob
	Exclude []Glob
}

// DefaultInclude is the include pattern when none is given: every file
// whose name ends in .json, at any depth.
const DefaultInclude = "**/*.json"

func (s Selection) selects(name string) bool {
	for _, exclude := range s.Exclude {
		if exclude.Match(name) {
			return false
		}
	}
	for _, include := range s.Include {
		if include.Match(name) {
			return true
		}
	}
	return false
}

// descends reports whether a walk must read the directory dir.
func (s Selection) descends(dir string) bool {
	segments := strings.Split(dir, "/")
	for _, exclude := range s.Exclude {
		if exclude.coversAllBelow(segments) {
			return false
		}
	}
	for _, include := range s.Include {
		if include.mayMatchBelow(segments) {
			return true
		}
	}
	return false
}
