package fssecure

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ErrNotExact reports a path that Open would follow but OpenExact refuses: a
// component that is a link or junction, or one whose name on disk is not
// spelled exactly as the path gives it.
var ErrNotExact = errors.New("path has a linked component or one not spelled as on disk")

// OpenExact is Open for a caller whose paths name files, not aliases of
// them: it refuses a link or junction in any component, not only the final
// one, and a component whose name on disk is not spelled byte for byte as
// the path gives it, so that a case-insensitive filesystem does not open
// "registry.json" for "Registry.json".
//
// Open keeps following an inward link in a directory component, because a
// symlinked directory inside a project is a legitimate layout there; this
// is the opt-in for a reader that holds a path to name one file at one
// spelling. Each component is examined without following it (Lstat) and
// read from its parent's entries through a handle on that parent, and the
// directory opened next is checked to be the one just examined, so a
// component swapped for a link between the check and the open is refused.
// An absent component is fs.ErrNotExist, as Open reports it.
func (r *Root) OpenExact(relative string) (*os.File, error) {
	cleaned, err := Relative(relative)
	if err != nil {
		return nil, err
	}
	if cleaned == "." {
		return nil, errors.New("path is not a regular file")
	}
	components := strings.Split(cleaned, string(filepath.Separator))
	dir := r.root
	defer func() {
		if dir != r.root {
			dir.Close()
		}
	}()
	for i, name := range components {
		info, err := dir.Lstat(name)
		if err != nil {
			return nil, classify(err)
		}
		if LinkLike(info) {
			return nil, ErrNotExact
		}
		if err := spelledExactly(dir, name); err != nil {
			return nil, err
		}
		if i == len(components)-1 {
			file, err := dir.OpenFile(name, os.O_RDONLY|nonBlockingOpen, 0)
			if err != nil {
				return nil, classify(err)
			}
			if _, err := sameRegularFileIn(dir, file, name); err != nil {
				file.Close()
				return nil, err
			}
			return file, nil
		}
		next, err := dir.OpenRoot(name)
		if err != nil {
			return nil, classify(err)
		}
		if dir != r.root {
			dir.Close()
		}
		dir = next
		opened, err := dir.Stat(".")
		if err != nil || !os.SameFile(opened, info) {
			return nil, errors.New("path changed between the check and the open")
		}
	}
	return nil, errors.New("path is not a regular file")
}

// spelledExactly reports whether the directory the handle holds has an entry
// named exactly name. A name the filesystem resolves to an entry spelled
// otherwise, as a case-insensitive one does, is ErrNotExact.
func spelledExactly(dir *os.Root, name string) error {
	handle, err := dir.Open(".")
	if err != nil {
		return classify(err)
	}
	defer handle.Close()
	for {
		entries, err := handle.ReadDir(256)
		for _, entry := range entries {
			if entry.Name() == name {
				return nil
			}
		}
		if errors.Is(err, io.EOF) {
			return ErrNotExact
		}
		if err != nil {
			return err
		}
	}
}
