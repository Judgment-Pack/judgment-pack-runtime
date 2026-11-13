package packsig_test

// These tests use the package the way a program outside this module does:
// the public paths and the standard library. The signature documents are
// made here with crypto/ed25519 over the message the package documentation
// states, not with any signer of this module.

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/sdk"
	"github.com/Judgment-Pack/judgment-pack-runtime/sdk/packsig"
)

// The seeds of RFC 8032's first two test vectors.
const (
	seed1 = "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60"
	seed2 = "4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb"
)

func key(t *testing.T, seed string) ed25519.PrivateKey {
	t.Helper()
	raw, err := hex.DecodeString(seed)
	if err != nil {
		t.Fatal(err)
	}
	return ed25519.NewKeyFromSeed(raw)
}

func public(t *testing.T, seed string) ed25519.PublicKey {
	return key(t, seed).Public().(ed25519.PublicKey)
}

// sign is a signature document over pack, written from the convention.
func sign(t *testing.T, seed string, pack []byte) []byte {
	t.Helper()
	sum := sha256.Sum256(pack)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	message := `judgment-pack-runtime/pack-signature/1:{"packDigest":"` + digest + `","signatureVersion":"1.0.0"}`
	signature := ed25519.Sign(key(t, seed), []byte(message))
	return []byte(`{"algorithm":"ed25519","packDigest":"` + digest + `","publicKey":"` + hex.EncodeToString(public(t, seed)) +
		`","signature":"` + hex.EncodeToString(signature) + `","signatureVersion":"1.0.0"}` + "\n")
}

// validPack is a Core 0.2.0-draft pack sdk.Load admits, from the bundled
// corpus.
func validPack(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../internal/artifacts/jps/0.2.0-draft/cases/valid/minimal-expense-approval.json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func refusalCode(err error) string {
	var refusal *packsig.Refusal
	if errors.As(err, &refusal) {
		return refusal.Code
	}
	return ""
}

func TestLoadAdmitsASignedPackAndReportsTheSignature(t *testing.T) {
	pack := validPack(t)
	loaded, accepted, err := packsig.Load(context.Background(), pack, sign(t, seed1, pack), public(t, seed1), sdk.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(pack)
	if accepted.PackDigest != "sha256:"+hex.EncodeToString(sum[:]) || loaded.Identity().Digest != accepted.PackDigest {
		t.Fatalf("digests: %s %s", accepted.PackDigest, loaded.Identity().Digest)
	}
	if accepted.PublicKey != hex.EncodeToString(public(t, seed1)) || accepted.KeyID != "21fe31dfa154a261626bf854046fd227" || accepted.SignatureVersion != packsig.SignatureVersion {
		t.Fatalf("signature = %+v", accepted)
	}
}

// Nothing is loaded, and no rule read, without an accepted signature: no
// expected key, another key, changed bytes, and a malformed document each
// refuse with a *packsig.Refusal, never a *sdk.Refusal.
func TestLoadRefusesBeforeLoading(t *testing.T) {
	pack := validPack(t)
	signed := sign(t, seed1, pack)
	changed := append([]byte{' '}, pack...)
	cases := []struct {
		name            string
		pack, signature []byte
		expected        ed25519.PublicKey
		code            string
	}{
		{"no expected key", pack, signed, nil, packsig.CodeExpectedKey},
		{"another expected key", pack, signed, public(t, seed2), packsig.CodeUnexpectedKey},
		{"signed by another key", pack, sign(t, seed2, pack), public(t, seed1), packsig.CodeUnexpectedKey},
		{"changed bytes", changed, signed, public(t, seed1), packsig.CodeDigestMismatch},
		{"no signature document", pack, nil, public(t, seed1), packsig.CodeMalformed},
		{"a truncated document", pack, signed[:len(signed)-3], public(t, seed1), packsig.CodeMalformed},
		{"an oversized document", pack, append(append([]byte{}, signed...), strings.Repeat(" ", packsig.MaxSignatureBytes)...), public(t, seed1), packsig.CodeTooLarge},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			loaded, accepted, err := packsig.Load(context.Background(), c.pack, c.signature, c.expected, sdk.LoadOptions{})
			if loaded != nil || accepted != (packsig.Signature{}) || refusalCode(err) != c.code {
				t.Fatalf("Load = %v %+v %v, want %s", loaded, accepted, err, c.code)
			}
			var loadRefusal *sdk.Refusal
			if errors.As(err, &loadRefusal) {
				t.Fatal("a signature refusal is a *sdk.Refusal")
			}
			if _, err := packsig.Verify(c.pack, c.signature, c.expected); refusalCode(err) != c.code {
				t.Fatalf("Verify = %v, want %s", err, c.code)
			}
		})
	}
}

// A signature over bytes that are not a pack is accepted as a signature, and
// sdk.Load then refuses them with its own refusal; the accepted signature is
// returned beside it.
func TestLoadReportsTheSignatureOfAPackItRefuses(t *testing.T) {
	notAPack := []byte(`{"specVersion":"0.5.0"}`)
	loaded, accepted, err := packsig.Load(context.Background(), notAPack, sign(t, seed1, notAPack), public(t, seed1), sdk.LoadOptions{})
	var loadRefusal *sdk.Refusal
	if loaded != nil || !errors.As(err, &loadRefusal) || refusalCode(err) != "" {
		t.Fatalf("Load = %v %v", loaded, err)
	}
	if accepted.PublicKey != hex.EncodeToString(public(t, seed1)) {
		t.Fatalf("the accepted signature is not returned: %+v", accepted)
	}
}

func TestLoadRefusesANilOrDoneContext(t *testing.T) {
	pack := validPack(t)
	//nolint:staticcheck // a nil context is what is tested
	if _, _, err := packsig.Load(nil, pack, sign(t, seed1, pack), public(t, seed1), sdk.LoadOptions{}); err == nil || refusalCode(err) != "" {
		t.Fatalf("nil context: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := packsig.Load(ctx, pack, sign(t, seed1, pack), public(t, seed1), sdk.LoadOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("done context: %v", err)
	}
}

func TestRefusalErrorNamesItsCode(t *testing.T) {
	_, err := packsig.Verify([]byte("{}"), nil, nil)
	if err == nil || !strings.HasPrefix(err.Error(), packsig.CodeExpectedKey+": ") {
		t.Fatalf("error = %v", err)
	}
}
