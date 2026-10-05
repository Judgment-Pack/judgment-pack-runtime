//go:build !unix

package audit

import (
	"fmt"
	"os"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
)

// keyPrivacyChecked says this platform cannot establish that a key is its
// owner's alone: on Windows who may read a file is whatever its ACL allows,
// and this runtime does not read ACLs. LoadSigner refuses every key here, so
// records are unsigned and packs validate says why.
const keyPrivacyChecked = false

// readKey reads a key only to show its public half (ReadKey): one regular
// file named by its own path, of the seed's form. Nothing signs with it here.
func readKey(keyPath string, _ os.FileInfo) (*Signer, error) {
	file, err := fssecure.OpenRegular(keyPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readSeedFrom(file)
}

// writeSeed is WriteSeed here, where no directory's owner or mode can be read
// and no key signs (keyPrivacyChecked): the seed is created by its path, as
// before, and removed when the write or the read back fails.
func writeSeed(keyPath string, data []byte) (*Signer, error) {
	file, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	if err := fillSeed(file, data); err != nil {
		_ = os.Remove(keyPath)
		return nil, err
	}
	signer, err := ReadKey(keyPath)
	if err != nil {
		_ = os.Remove(keyPath)
		return nil, fmt.Errorf("%w: %w", ErrSeedNotReadBack, err)
	}
	return signer, nil
}

// StandInKeyAncestorsForTests does nothing here: no directory is held.
func StandInKeyAncestorsForTests(string) (restore func()) { return func() {} }
