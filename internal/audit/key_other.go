//go:build !unix

package audit

import (
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

// holdKeyPlace holds nothing here: without unix owners and modes no
// directory's can be read, and no key signs (keyPrivacyChecked).
func holdKeyPlace(string) error { return nil }
