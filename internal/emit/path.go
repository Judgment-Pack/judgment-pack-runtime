package emit

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

var errPath = errors.New("emit: the path is outside the path rules")

// checkPathForm applies RFC 0036 §3's path rules that a path has on its own:
// at most MaxPathBytes bytes of UTF-8; "/" separators; relative; no ".",
// ".." or empty segment, and none that begins with "~"; no trailing "/"; no
// NUL, backslash or control character. It also refuses a first segment that
// is a drive letter and a colon ("C:"), which a Windows host reads as a
// drive: the RFC's conformance list names drive letters among the forms a
// host refuses. A UNC form ("\\host\share") holds a backslash.
func checkPathForm(path string) error {
	if path == "" || len(path) > MaxPathBytes || !utf8.ValidString(path) {
		return errPath
	}
	for _, r := range path {
		if r == 0 || r == '\\' || unicode.IsControl(r) {
			return errPath
		}
	}
	segments := strings.Split(path, "/")
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." || strings.HasPrefix(segment, "~") {
			return errPath
		}
	}
	if first := segments[0]; len(first) >= 2 && first[1] == ':' && (first[0]|0x20 >= 'a' && first[0]|0x20 <= 'z') {
		return errPath
	}
	return nil
}

// foldKey is the key two paths collide under: Unicode canonical caseless
// matching (The Unicode Standard §3.13, D145), NFD(fold(NFD(path))). The RFC
// asks for uniqueness once case is folded, so that a case-insensitive
// filesystem cannot merge two paths; the normalization also keeps a
// filesystem that normalizes names from merging a composed and a decomposed
// spelling of one name.
func foldKey(path string) string {
	return norm.NFD.String(cases.Fold().String(norm.NFD.String(path)))
}

// pathSet holds the paths of one set of files, to refuse two that cannot
// both be written: equal once case is folded and normalized (foldKey), or one
// a directory of the other, which would replace it or fail to be made.
type pathSet struct {
	files, directories map[string]bool
}

func newPathSet() *pathSet {
	return &pathSet{files: map[string]bool{}, directories: map[string]bool{}}
}

// add records a path and reports whether it collides with one added before.
func (s *pathSet) add(path string) bool {
	key := foldKey(path)
	collides := s.files[key] || s.directories[key]
	s.files[key] = true
	// Folding and normalizing keep "/" and never make one, so the key's
	// prefixes at "/" are the keys of the path's directories. A directory
	// already recorded has had those above it recorded with it.
	for end := strings.LastIndexByte(key, '/'); end > 0; end = strings.LastIndexByte(key[:end], '/') {
		directory := key[:end]
		if s.directories[directory] {
			break
		}
		collides = collides || s.files[directory]
		s.directories[directory] = true
	}
	return collides
}
