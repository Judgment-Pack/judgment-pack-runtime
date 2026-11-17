package emit

import "io/fs"

// What the conformance harness (spec RFC 0036 §5, ADR-0099) reuses from the
// host besides Run and ParseResponse: the rule that refuses two paths of one
// set of files, the request version the host writes for a manifest, and the
// mode a file on disk is read as. Nothing here changes what the host does.

// PathSet holds the paths of one set of files, and refuses two that cannot
// both be written, as the host refuses them within a response and across the
// runs of one manifest: equal once case is folded and normalized, or one a
// directory of the other.
type PathSet struct{ set *pathSet }

// NewPathSet returns an empty set.
func NewPathSet() *PathSet { return &PathSet{set: newPathSet()} }

// Add records a path and reports whether it collides with one added before.
func (s *PathSet) Add(path string) bool { return s.set.add(path) }

// RequestVersion returns the request version the host writes for a
// manifest: the highest it writes that the manifest lists, or false when it
// writes none of them (EMIT-REQUEST-VERSION).
func RequestVersion(m *Manifest) (string, bool) { return requestVersion(m) }

// ModeOf reads a file's mode as one of the two a response gives, as check
// mode compares it: executable when the owner may execute it.
func ModeOf(info fs.FileInfo) string { return modeOf(info) }
