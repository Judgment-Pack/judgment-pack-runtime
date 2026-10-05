//go:build !go1.25

package fssecure

import (
	"errors"
	"os"
	"path/filepath"
)

// renameWithin renames oldname to newname, both beneath this root. os.Root
// renames through its handle from Go 1.25 on; a build with an earlier
// toolchain, which this module's floor allows, renames by the held
// directory's pathname, after checking that the pathname still names the
// directory the handle holds. Between that check and the rename the pathname
// can be pointed elsewhere, which a build from Go 1.25 on does not allow;
// release binaries are built with a later toolchain.
func (r *Root) renameWithin(oldname, newname string) error {
	held, err := r.root.Stat(".")
	if err != nil {
		return err
	}
	named, err := os.Stat(r.dir)
	if err != nil {
		return err
	}
	if !os.SameFile(held, named) {
		return errors.New("the directory's pathname no longer names the directory held")
	}
	return os.Rename(filepath.Join(r.dir, oldname), filepath.Join(r.dir, newname))
}
