package regsig_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/regsig"
)

const digest = "sha256:17daddbd0d76af8847e788444afc5db180a97c3aafe1167cc7aad184a72360fd"

// Each message is its prefix and the two members, written out by hand as
// §10.3 shows them: the JCS of the two members is their concatenation, since
// the digest member sorts before the version member.
func TestMessagesAreTheTextsBytes(t *testing.T) {
	for kind, want := range map[regsig.Kind]string{
		regsig.Lock:   `judgment-pack-registry/lock-signature/1:{"lockDigest":"` + digest + `","lockSignatureVersion":"1.1.0"}`,
		regsig.Source: `judgment-pack-registry/source-signature/1:{"sourceDigest":"` + digest + `","sourceSignatureVersion":"1.1.0"}`,
	} {
		got, err := kind.Message(digest)
		if err != nil || string(got) != want {
			t.Errorf("%s: message %q, %v; want %q", kind.Prefix, got, err, want)
		}
	}
}

// Neither prefix begins the other, and no message of one kind begins with the
// other's prefix.
func TestPrefixesAreApart(t *testing.T) {
	lock, source := regsig.Lock.Prefix, regsig.Source.Prefix
	if strings.HasPrefix(lock, source) || strings.HasPrefix(source, lock) {
		t.Fatalf("%q and %q", lock, source)
	}
	message, _ := regsig.Lock.Message(digest)
	if bytes.HasPrefix(message, []byte(source)) {
		t.Fatal("a lock message begins with the source prefix")
	}
}

// A digest not of its form makes no message and no document.
func TestRefusesADigestNotOfItsForm(t *testing.T) {
	public := make([]byte, ed25519.PublicKeySize)
	signature := make([]byte, ed25519.SignatureSize)
	for _, bad := range []string{"", "sha256:" + strings.Repeat("A", 64), "sha256:" + strings.Repeat("0", 63), "sha512:" + strings.Repeat("0", 64), `sha256:"` + strings.Repeat("0", 63)} {
		if _, err := regsig.Lock.Message(bad); err == nil {
			t.Errorf("message of %q", bad)
		}
		if _, err := regsig.Source.Encode(public, bad, signature); err == nil {
			t.Errorf("document of %q", bad)
		}
	}
	if _, err := regsig.Lock.Encode(public[:31], digest, signature); err == nil {
		t.Error("a short key encoded")
	}
	if _, err := regsig.Lock.Encode(public, digest, signature[:63]); err == nil {
		t.Error("a short signature encoded")
	}
}

// A document is its five members in code-point order, with no white space
// and one final line feed, and each member a string.
func TestDocumentsAreInCodePointOrder(t *testing.T) {
	public := bytes.Repeat([]byte{0xab}, ed25519.PublicKeySize)
	signature := bytes.Repeat([]byte{0xcd}, ed25519.SignatureSize)
	for _, kind := range []regsig.Kind{regsig.Lock, regsig.Source} {
		document, err := kind.Encode(public, digest, signature)
		if err != nil {
			t.Fatal(err)
		}
		values := map[string]string{"algorithm": "ed25519", kind.DigestMember: digest, kind.VersionMember: "1.1.0", "publicKey": hex.EncodeToString(public), "signature": hex.EncodeToString(signature)}
		var want strings.Builder
		want.WriteByte('{')
		for i, name := range kind.Members() {
			if i > 0 {
				want.WriteByte(',')
			}
			want.WriteString(`"` + name + `":"` + values[name] + `"`)
		}
		want.WriteString("}\n")
		if string(document) != want.String() {
			t.Errorf("document\n%s\nwant\n%s", document, want.String())
		}
		var parsed map[string]string
		if err := json.Unmarshal(document, &parsed); err != nil || len(parsed) != 5 {
			t.Errorf("%s: %v %v", kind.Prefix, parsed, err)
		}
		members := kind.Members()
		for i := 1; i < len(members); i++ {
			if members[i-1] >= members[i] {
				t.Errorf("%s: %s before %s", kind.Prefix, members[i-1], members[i])
			}
		}
	}
}
