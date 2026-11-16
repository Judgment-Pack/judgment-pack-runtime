//go:build !unix

package startup

import (
	"errors"
	"path/filepath"
	"testing"
)

const onUnix = false

func TestReadFileRefusesEveryFileOutsideUnix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mounted")
	writeFile(t, path, "value\n", 0o600)
	if _, err := ReadFile(path); !errors.Is(err, ErrPermissionsUnknown) {
		t.Errorf("got %v, want ErrPermissionsUnknown", err)
	}
}
