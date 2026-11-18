//go:build unix

package federation

import "os"

// recordModesChecked: on unix a record directory or file that its group or
// other users can write is refused.
const recordModesChecked = true

// syncDirectory flushes the directory, so the rename of a record over the
// last is durable before the lock is reported accepted.
func syncDirectory(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	if closeErr := d.Close(); err == nil {
		err = closeErr
	}
	return err
}
