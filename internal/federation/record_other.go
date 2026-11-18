//go:build !unix

package federation

// recordModesChecked: on Windows the record directory's and files' ACLs are
// not read, so the refusal of a record its group or other users can write is
// not applied there; the directory's and the file's form still are.
const recordModesChecked = false

// syncDirectory is no flush on Windows, where a directory is not opened to
// be flushed. What stands in for it, said as it is: the temporary file's
// bytes are flushed (FlushFileBuffers, through os.File.Sync) before the
// rename, and os.Rename is MoveFileEx with MOVEFILE_REPLACE_EXISTING, which
// replaces the record in one step but without MOVEFILE_WRITE_THROUGH. So the
// rename itself is durable once the file system writes its metadata, not
// when the verifier reports the lock accepted: a power loss just after may
// leave the record before it, which a floor no lower than the last serial
// accepted covers (§11, A record that cannot be read).
func syncDirectory(string) error { return nil }
