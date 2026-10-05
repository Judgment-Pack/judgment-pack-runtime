//go:build go1.25

package fssecure

// renamesThroughHandle says this build renames through the directory handle.
const renamesThroughHandle = true

// renameWithin renames oldname to newname, both beneath this root, through
// the handle: the directory renamed in is the one the handle holds.
func (r *Root) renameWithin(oldname, newname string) error {
	return classify(r.root.Rename(oldname, newname))
}
