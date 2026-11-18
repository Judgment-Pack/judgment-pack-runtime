package emit

// ValidPath reports whether path has registry §11.3's form of a response
// path, its owns condition aside, within MaxPathBytes: the form registry
// format 1.1.0 (§3, proposed; internal/federation) requires of each path a
// federation manifest names. It is checkPathForm, exported as it is.
func ValidPath(path string) bool { return checkPathForm(path) == nil }
