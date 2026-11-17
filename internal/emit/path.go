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
// NUL, backslash or control character. A UNC form ("\\host\share") holds a
// backslash. It also refuses what a Windows host would read as another name,
// on every system, so that a manifest and its response mean the same
// everywhere:
//   - a ":" in any segment, so no drive letter ("C:"), which the RFC's
//     conformance list names among the forms a host refuses, and no NTFS
//     stream ("a:b");
//   - a segment that ends in "." or a space, which Windows drops;
//   - a reserved device name (CON, PRN, AUX, NUL, COM1-9, LPT1-9), in any
//     case and with or without an extension ("nul.txt").
//
// And it refuses a segment that a file system reads as ".git" (isGitName),
// so that no owns glob names git's directory and no response writes into
// it; scanOwned does not enter it either.
func checkPathForm(path string) error {
	if path == "" || len(path) > MaxPathBytes || !utf8.ValidString(path) {
		return errPath
	}
	for _, r := range path {
		if r == 0 || r == '\\' || r == ':' || unicode.IsControl(r) {
			return errPath
		}
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "" || segment == "." || segment == ".." || strings.HasPrefix(segment, "~") || isGitName(segment) ||
			strings.HasSuffix(segment, ".") || strings.HasSuffix(segment, " ") || isDeviceName(segment) {
			return errPath
		}
	}
	return nil
}

// isDeviceName says whether Windows reads a segment as a device: its name
// before the first ".", without trailing spaces, is a reserved one in any
// case.
func isDeviceName(segment string) bool {
	base, _, _ := strings.Cut(segment, ".")
	base = strings.TrimRight(base, " ")
	switch len(base) {
	case 3:
		for _, name := range []string{"CON", "PRN", "AUX", "NUL"} {
			if strings.EqualFold(base, name) {
				return true
			}
		}
	case 4:
		prefix := base[:3]
		return (strings.EqualFold(prefix, "COM") || strings.EqualFold(prefix, "LPT")) && base[3] >= '1' && base[3] <= '9'
	}
	return false
}

// isGitName says whether a path segment, or a name found on disk, is one a
// file system reads as git's directory, ".git". It makes git's own two checks
// (git's path.c is_ntfs_dotgit and utf8.c is_hfs_dotgit), which git applies
// to every path it checks out when core.protectNTFS and core.protectHFS are
// set, so that a name git refuses to check out is never owned here either.
func isGitName(name string) bool { return isNTFSDotGit(name) || isHFSDotGit(name) }

// isNTFSDotGit is git's is_ntfs_dotgit: ".git", or its 8.3 short name
// "git~1", in ASCII case, followed only by dots and spaces (which NTFS drops)
// up to the end of the name, a separator, or a ":" that begins a stream name
// (".git::$INDEX_ALLOCATION" is the directory itself).
func isNTFSDotGit(name string) bool {
	var rest string
	switch {
	case len(name) >= 4 && name[0] == '.' && asciiEqualFold(name[1:4], "git"):
		rest = name[4:]
	case len(name) >= 5 && asciiEqualFold(name[:3], "git") && name[3:5] == "~1":
		rest = name[5:]
	default:
		return false
	}
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case '/', '\\', ':':
			return true
		case '.', ' ':
		default:
			return false
		}
	}
	return true
}

// isHFSDotGit is git's is_hfs_dotgit: ".git" in ASCII case once the code
// points HFS+ ignores in a name are skipped (git's utf8.c next_hfs_char,
// after Apple's TN1150), so ".g\u200cit" is ".git" there. As in git,
// malformed UTF-8 ends the name.
func isHFSDotGit(name string) bool {
	r, i := nextHFSChar(name, 0)
	if r != '.' {
		return false
	}
	for _, want := range "git" {
		r, i = nextHFSChar(name, i)
		if r > 127 || asciiLower(r) != want {
			return false
		}
	}
	r, _ = nextHFSChar(name, i)
	return r == 0 || r == '/'
}

// nextHFSChar returns the code point of name at byte i, skipping those HFS+
// ignores, and the byte after it; at the end of name, or at malformed UTF-8,
// it returns 0.
func nextHFSChar(name string, i int) (rune, int) {
	for i < len(name) {
		r, size := utf8.DecodeRuneInString(name[i:])
		if r == utf8.RuneError && size == 1 {
			return 0, len(name)
		}
		i += size
		if !hfsIgnorable(r) {
			return r, i
		}
	}
	return 0, i
}

// hfsIgnorable is the list git's next_hfs_char skips.
func hfsIgnorable(r rune) bool {
	switch {
	case r >= 0x200c && r <= 0x200f, // zero width non-joiner and joiner, left-to-right and right-to-left marks
		r >= 0x202a && r <= 0x202e, // directional embeddings and overrides
		r >= 0x206a && r <= 0x206f, // symmetric swapping, Arabic form shaping, digit shapes
		r == 0xfeff:                // zero width no-break space
		return true
	}
	return false
}

func asciiLower(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + 'a' - 'A'
	}
	return r
}

// asciiEqualFold compares two strings in ASCII case only, as git's
// strncasecmp does.
func asciiEqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if asciiLower(rune(a[i])) != asciiLower(rune(b[i])) {
			return false
		}
	}
	return true
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
