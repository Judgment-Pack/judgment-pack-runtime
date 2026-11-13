package audit

import (
	"crypto/ed25519"
	"crypto/sha256"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/packsig"
)

// A signer's pack signature is the document packsig reads, over the pack's
// exact bytes, and the RFC 8032 first-vector key gives the document the
// independent vector in internal/packsig states.
func TestSignPackWritesTheDocumentPacksigReads(t *testing.T) {
	signer := signerOf(t, vectorSeed1)
	document := signer.SignPack([]byte("{}\n"))
	want := `{"algorithm":"ed25519","packDigest":"sha256:ca3d163bab055381827226140568f3bef7eaac187cebd76878e0b63e9e442356",` +
		`"publicKey":"d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a",` +
		`"signature":"1f07dbff1c693a4372aa8fd6548e77bf6d52f43f591f24427acdb83611988a52efe325fe485d0e320d11369267d3b516cbacac704ffaa03ab46d0d524567070e",` +
		`"signatureVersion":"1.0.0"}` + "\n"
	if string(document) != want {
		t.Fatalf("document =\n%s\nwant\n%s", document, want)
	}
	if _, refusal := packsig.Verify([]byte("{}\n"), document, signer.public); refusal != nil {
		t.Fatalf("refused: %+v", refusal)
	}
}

// The messages a key signs here begin with prefixes none of which is a prefix
// of another, so no message of one kind is a message of another kind.
func TestEverySignedMessageKindHasItsOwnPrefix(t *testing.T) {
	digest := sha256.Sum256([]byte("{}\n"))
	pack := string(packsig.Message(digest))
	packPrefix := pack[:strings.IndexByte(pack, ':')+1]
	if packPrefix != "judgment-pack-runtime/pack-signature/1:" {
		t.Fatalf("the pack message's prefix is %q", packPrefix)
	}
	prefixes := []string{recordSignaturePrefix, keyRotationPrefix, witnessPrefix, packPrefix}
	for i, one := range prefixes {
		for j, other := range prefixes {
			if i != j && strings.HasPrefix(one, other) {
				t.Fatalf("%q begins with %q", one, other)
			}
		}
	}
	if !strings.HasPrefix(string(RecordMessage(strings.Repeat("0", 32), 1, packsig.DigestText(digest))), recordSignaturePrefix) ||
		!strings.HasPrefix(string(RotationMessage(strings.Repeat("0", 32), 1, strings.Repeat("0", 64))), keyRotationPrefix) {
		t.Fatal("a trail message does not begin with its prefix")
	}
}

// An audit trail's record signature made with a key is not a pack signature
// by that key, even for a "pack" whose bytes are exactly the record message,
// and a pack signature is not a record signature, even for a pack whose bytes
// are exactly a record message. Neither kind signs bare bytes.
func TestTrailAndPackSignaturesCannotStandInForEachOther(t *testing.T) {
	signer := signerOf(t, vectorSeed1)
	trail := "00112233445566778899aabbccddeeff"
	line := []byte(`{"recordVersion":"1","trail":"00112233445566778899aabbccddeeff","sequence":1,"previous":"sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855","kind":"evaluation"}`)
	signedLine, err := signer.SignRecordLine(line)
	if err != nil {
		t.Fatal(err)
	}
	item := parseSidecarLine(signedLine[:len(signedLine)-1])
	if !item.readable {
		t.Fatalf("the record signature is unreadable: %s", signedLine)
	}
	recordMessage := RecordMessage(trail, 1, Digest(line))
	if !ed25519.Verify(signer.public, recordMessage, item.signature) {
		t.Fatal("the record signature does not hold as a record signature")
	}

	// A record signature presented as a pack signature: over the record's
	// line, and over the record message itself, as the pack's bytes.
	for name, presented := range map[string][]byte{"the record line": line, "the record message": recordMessage} {
		document := packsig.Encode(signer.public, sha256.Sum256(presented), item.signature)
		if _, refusal := packsig.Verify(presented, document, signer.public); refusal == nil || refusal.Code != packsig.CodeInvalid {
			t.Fatalf("a record signature over %s is accepted as a pack signature: %+v", name, refusal)
		}
	}
	// A rotation signature, likewise.
	rotation := parseSidecarLine(signer.RotationLine(trail, 1, signerOf(t, vectorSeed2).public))
	rotationMessage := RotationMessage(trail, 1, signerOf(t, vectorSeed2).PublicKey())
	document := packsig.Encode(signer.public, sha256.Sum256(rotationMessage), rotation.signature)
	if _, refusal := packsig.Verify(rotationMessage, document, signer.public); refusal == nil || refusal.Code != packsig.CodeInvalid {
		t.Fatalf("a rotation signature is accepted as a pack signature: %+v", refusal)
	}

	// A pack signature presented as a record signature: of a pack whose bytes
	// are the record message, and of one whose bytes are the record line.
	for name, packBytes := range map[string][]byte{"the record message": recordMessage, "the record line": line} {
		packDocument, refusal := packsig.Parse(signer.SignPack(packBytes))
		if refusal != nil {
			t.Fatal(refusal)
		}
		if ed25519.Verify(signer.public, recordMessage, packDocument.Signature) {
			t.Fatalf("a pack signature of %s holds as a record signature", name)
		}
		forged := strings.Replace(string(signedLine[:len(signedLine)-1]), hexKey(item.signature), hexKey(packDocument.Signature), 1)
		if again := parseSidecarLine([]byte(forged)); !again.readable || ed25519.Verify(signer.public, RecordMessage(again.trail, again.sequence, again.record), again.signature) {
			t.Fatalf("a sidecar line carrying a pack signature of %s holds", name)
		}
	}
}
