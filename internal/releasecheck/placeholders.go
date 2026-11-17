package releasecheck

import (
	"bytes"
	"fmt"
	"io/fs"
	"strings"
)

// PlaceholderMarker opens a note left where release text waits for something
// that does not exist yet, such as a tag. A release settles the text and
// removes the note first.
const PlaceholderMarker = "PLACEHOLDER("

// placeholderFiles are the files outside docs/ that CheckPlaceholders reads.
var placeholderFiles = []string{"VERSIONING.md", "CHANGELOG.md", "SECURITY.md"}

// CheckPlaceholders refuses a release whose text still holds a placeholder
// marker: in VERSIONING.md, CHANGELOG.md, SECURITY.md or any file under docs/
// of fsys, the repository root. A named file or docs/ that cannot be read, or
// an entry under docs/ that is neither a directory nor a regular file, fails
// the check as well, so a run from another directory never passes.
func CheckPlaceholders(fsys fs.FS) error {
	paths := append([]string(nil), placeholderFiles...)
	err := fs.WalkDir(fsys, "docs", func(path string, entry fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case entry.IsDir():
			return nil
		case entry.Type().IsRegular():
			paths = append(paths, path)
			return nil
		default:
			return fmt.Errorf("%s is not a regular file", path)
		}
	})
	if err != nil {
		return fmt.Errorf("read docs/: %w", err)
	}
	var found []string
	for _, path := range paths {
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		for i, line := range bytes.Split(data, []byte("\n")) {
			if bytes.Contains(line, []byte(PlaceholderMarker)) {
				found = append(found, fmt.Sprintf("%s:%d", path, i+1))
			}
		}
	}
	if len(found) > 0 {
		return fmt.Errorf("placeholder markers %q remain; settle the text and remove them before a release: %s",
			PlaceholderMarker, strings.Join(found, ", "))
	}
	return nil
}
