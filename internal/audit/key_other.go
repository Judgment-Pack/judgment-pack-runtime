//go:build !unix

package audit

import "os"

// keyAccessCheck checks nothing where there is no unix owner and mode: on
// Windows who may read the key is whatever its ACL allows, and that is the
// operator's to set.
func keyAccessCheck(os.FileInfo) error { return nil }
