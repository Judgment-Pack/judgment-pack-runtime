package packsig

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"

	internal "github.com/Judgment-Pack/judgment-pack-runtime/internal/packsig"
	"github.com/Judgment-Pack/judgment-pack-runtime/sdk"
)

// Load admits the bytes it checked: the caller's slice, overwritten between
// the signature check and the load, changes neither what was loaded nor its
// digest.
func TestLoadLoadsTheBytesItChecked(t *testing.T) {
	pack, err := os.ReadFile("../../internal/artifacts/jps/0.2.0-draft/cases/valid/minimal-expense-approval.json")
	if err != nil {
		t.Fatal(err)
	}
	seed, _ := hex.DecodeString("9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60")
	private := ed25519.NewKeyFromSeed(seed)
	digest := sha256.Sum256(pack)
	signature := internal.Encode(private.Public().(ed25519.PublicKey), digest, ed25519.Sign(private, internal.Message(digest)))
	callers := append([]byte{}, pack...)
	previous := beforeLoad
	beforeLoad = func() {
		for index := range callers {
			callers[index] = ' '
		}
	}
	defer func() { beforeLoad = previous }()
	loaded, accepted, err := Load(context.Background(), callers, signature, private.Public().(ed25519.PublicKey), sdk.LoadOptions{})
	if err != nil {
		t.Fatalf("the bytes checked were not the bytes loaded: %v", err)
	}
	if loaded.Identity().Digest != internal.DigestText(digest) || accepted.PackDigest != internal.DigestText(digest) {
		t.Fatalf("digests: %s %s", loaded.Identity().Digest, accepted.PackDigest)
	}
}
