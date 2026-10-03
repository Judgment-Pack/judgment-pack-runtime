//go:build !unix

package audit

import (
	"errors"
	"path/filepath"
	"testing"
)

// Where a key's privacy cannot be checked, no key signs: LoadSigner refuses
// every key, so a project's records are unsigned, while ReadKey still shows a
// seed's public half, which signs nothing.
func TestNoKeySignsWhereItsPrivacyCannotBeChecked(t *testing.T) {
	good := writeKeyFile(t, filepath.Join(realTempDir(t), "seed"), vectorSeed1+"\n", 0o600)
	if _, err := LoadSigner(good, nil); !errors.Is(err, ErrKeyPrivacyUnchecked) {
		t.Fatalf("LoadSigner: %v", err)
	}
	if signer, err := ReadKey(good); err != nil || signer.PublicKey() != signerOf(t, vectorSeed1).PublicKey() {
		t.Fatalf("ReadKey: %v", err)
	}
}
