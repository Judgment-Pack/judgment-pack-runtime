package packsig

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// The vector is written by hand and checked with an independent Ed25519
// implementation, not with this package: the key is RFC 8032's first test
// vector (seed 9d61b19d…7f60), the pack is the three bytes "{}\n", whose
// SHA-256 any sha256sum gives, and the signature is the one OpenSSL's
// pkeyutl -sign -rawin makes over vectorMessage with that seed.
const (
	vectorSeed      = "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60"
	vectorPublicKey = "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a"
	vectorPack      = "{}\n"
	vectorDigest    = "sha256:ca3d163bab055381827226140568f3bef7eaac187cebd76878e0b63e9e442356"
	vectorMessage   = `judgment-pack-runtime/pack-signature/1:{"packDigest":"sha256:ca3d163bab055381827226140568f3bef7eaac187cebd76878e0b63e9e442356","signatureVersion":"1.0.0"}`
	vectorSignature = "1f07dbff1c693a4372aa8fd6548e77bf6d52f43f591f24427acdb83611988a52efe325fe485d0e320d11369267d3b516cbacac704ffaa03ab46d0d524567070e"
	vectorDocument  = `{"algorithm":"ed25519","packDigest":"` + vectorDigest + `","publicKey":"` + vectorPublicKey + `","signature":"` + vectorSignature + `","signatureVersion":"1.0.0"}` + "\n"
	// otherSeed is RFC 8032's second test vector's seed.
	otherSeed = "4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb"
)

func decode(t *testing.T, text string) []byte {
	t.Helper()
	data, err := hex.DecodeString(text)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func privateKey(t *testing.T, seed string) ed25519.PrivateKey {
	t.Helper()
	return ed25519.NewKeyFromSeed(decode(t, seed))
}

// signDocument is a document by the key of seed over pack, made the way
// audit.Signer.SignPack makes one.
func signDocument(t *testing.T, seed string, pack []byte) []byte {
	t.Helper()
	private := privateKey(t, seed)
	digest := sha256.Sum256(pack)
	return Encode(private.Public().(ed25519.PublicKey), digest, ed25519.Sign(private, Message(digest)))
}

func TestTheVectorIsFixed(t *testing.T) {
	digest := sha256.Sum256([]byte(vectorPack))
	if DigestText(digest) != vectorDigest {
		t.Fatalf("digest = %s", DigestText(digest))
	}
	if string(Message(digest)) != vectorMessage {
		t.Fatalf("message = %s", Message(digest))
	}
	public := privateKey(t, vectorSeed).Public().(ed25519.PublicKey)
	if hex.EncodeToString(public) != vectorPublicKey {
		t.Fatalf("public key = %x", public)
	}
	if got := string(signDocument(t, vectorSeed, []byte(vectorPack))); got != vectorDocument {
		t.Fatalf("document =\n%s\nwant\n%s", got, vectorDocument)
	}
	if len(vectorDocument) != 360 {
		t.Fatalf("a document is %d bytes", len(vectorDocument))
	}
	document, refusal := Verify([]byte(vectorPack), []byte(vectorDocument), public)
	if refusal != nil {
		t.Fatalf("the vector is refused: %s %s", refusal.Code, refusal.Message)
	}
	if hex.EncodeToString(document.PublicKey) != vectorPublicKey || DigestText(document.PackDigest) != vectorDigest || hex.EncodeToString(document.Signature) != vectorSignature {
		t.Fatalf("document read = %+v", document)
	}
}

// Each way a document is not of its form is refused with its own code.
func TestParseRefusesEveryDocumentNotOfItsForm(t *testing.T) {
	replace := func(old, new string) string {
		if !strings.Contains(vectorDocument, old) {
			t.Fatalf("the vector has no %q", old)
		}
		return strings.Replace(vectorDocument, old, new, 1)
	}
	closing := `"signatureVersion":"1.0.0"}`
	cases := []struct {
		name, document, code string
	}{
		{"one byte over the bound", vectorDocument + strings.Repeat(" ", MaxBytes-len(vectorDocument)+1), CodeTooLarge},
		{"empty", "", CodeMalformed},
		{"truncated", vectorDocument[:len(vectorDocument)/2], CodeMalformed},
		{"truncated by its last brace", strings.TrimSuffix(vectorDocument, "}\n"), CodeMalformed},
		{"two texts", strings.TrimSuffix(vectorDocument, "\n") + "{}", CodeMalformed},
		{"an array", "[" + strings.TrimSuffix(vectorDocument, "\n") + "]", CodeMalformed},
		{"a string", `"` + vectorSignature + `"`, CodeMalformed},
		{"an escaped member name", replace(`"algorithm"`, `"\u0061lgorithm"`), CodeMalformed},
		{"an escaped value", replace(`"ed25519"`, `"ed2551\u0039"`), CodeMalformed},
		{"a member twice", replace(closing, `"signatureVersion":"1.0.0","algorithm":"ed25519"}`), CodeDuplicateMember},
		{"a member twice, the same value", replace(closing, closing[:len(closing)-1]+`,"signatureVersion":"1.0.0"}`), CodeDuplicateMember},
		{"a member it does not define", replace(closing, `"signatureVersion":"1.0.0","comment":"signed"}`), CodeUnknownMember},
		{"a member it does not define, an object", replace(closing, `"signatureVersion":"1.0.0","keyId":{}}`), CodeUnknownMember},
		{"no signature", replace(`"signature":"`+vectorSignature+`",`, ""), CodeMissingMember},
		{"no publicKey", replace(`"publicKey":"`+vectorPublicKey+`",`, ""), CodeMissingMember},
		{"no signatureVersion", replace(`,"signatureVersion":"1.0.0"`, ""), CodeMissingMember},
		{"another version", replace(`"1.0.0"`, `"1.0.1"`), CodeVersion},
		{"a two-part version", replace(`"1.0.0"`, `"1.0"`), CodeVersion},
		{"a version that is a number", replace(`"1.0.0"`, `1`), CodeVersion},
		{"another algorithm", replace(`"ed25519"`, `"Ed25519"`), CodeAlgorithm},
		{"an algorithm that is not a string", replace(`"ed25519"`, `null`), CodeAlgorithm},
		{"a public key in capitals", replace(vectorPublicKey, strings.ToUpper(vectorPublicKey)), CodePublicKey},
		{"a short public key", replace(vectorPublicKey, vectorPublicKey[:62]), CodePublicKey},
		{"a public key of small order", replace(vectorPublicKey, strings.Repeat("0", 64)), CodePublicKey},
		{"a public key that is base64", replace(vectorPublicKey, "11qYAYKxCrfVS/7TyWQHOg7hcvPapiMlrwIaaPcHURo="), CodePublicKey},
		{"a digest in capitals", replace(vectorDigest, "sha256:"+strings.ToUpper(vectorDigest[7:])), CodePackDigest},
		{"a digest without its prefix", replace(vectorDigest, vectorDigest[7:]), CodePackDigest},
		{"a digest of another algorithm", replace("sha256:", "sha512:"), CodePackDigest},
		{"a short signature", replace(vectorSignature, vectorSignature[:126]), CodeValue},
		{"a signature in capitals", replace(vectorSignature, strings.ToUpper(vectorSignature)), CodeValue},
		{"a signature that is base64", replace(vectorSignature, "Hwfb/xxpOkNyqo/WVI53v21S9D9ZHyRCes24NhGYilLv4yX+SF0OMg0RNpJn07UWy6ysbE/6oDq0bQ1SRWcHDg=="), CodeValue},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, refusal := Parse([]byte(c.document))
			if refusal == nil || refusal.Code != c.code {
				t.Fatalf("refusal = %+v, want %s", refusal, c.code)
			}
			if strings.Contains(refusal.Message, vectorSignature[:16]) || strings.Contains(refusal.Message, "comment") {
				t.Fatalf("the message repeats what it read: %s", refusal.Message)
			}
		})
	}
}

// White space between tokens is admitted, up to the bound exactly.
func TestParseAdmitsWhiteSpaceUpToTheBound(t *testing.T) {
	spaced := strings.ReplaceAll(vectorDocument, `,"`, ",\n  \"")
	if _, refusal := Parse([]byte(spaced)); refusal != nil {
		t.Fatalf("spaced: %+v", refusal)
	}
	padded := vectorDocument + strings.Repeat(" ", MaxBytes-len(vectorDocument))
	if len(padded) != MaxBytes {
		t.Fatal("the padding is wrong")
	}
	if _, refusal := Parse([]byte(padded)); refusal != nil {
		t.Fatalf("a document of exactly the bound: %+v", refusal)
	}
}

// What Verify refuses beyond the document's form: no expected key, one no
// verifier may trust, another key, other bytes, and a signature that does not
// hold.
func TestVerifyRefusals(t *testing.T) {
	public := privateKey(t, vectorSeed).Public().(ed25519.PublicKey)
	other := privateKey(t, otherSeed).Public().(ed25519.PublicKey)
	pack := []byte(vectorPack)
	otherPack := []byte("{ }\n")
	byOther := signDocument(t, otherSeed, pack)
	flipped := []byte(strings.Replace(vectorDocument, vectorSignature[:2], "00", 1))
	// A document that names the expected key over the other pack's digest,
	// carrying the other key's signature over this pack: every member of its
	// form, and nothing in it the expected key signed.
	claimed := []byte(strings.Replace(string(byOther), hex.EncodeToString(other), vectorPublicKey, 1))
	// A document whose packDigest was edited to name the other pack, its
	// signature still over this one.
	reDigested := []byte(strings.Replace(vectorDocument, vectorDigest, DigestText(sha256.Sum256(otherPack)), 1))
	cases := []struct {
		name           string
		pack, document []byte
		expected       ed25519.PublicKey
		code           string
	}{
		{"no expected key", pack, []byte(vectorDocument), nil, CodeExpectedKey},
		{"an empty expected key", pack, []byte(vectorDocument), ed25519.PublicKey{}, CodeExpectedKey},
		{"an expected key of small order", pack, []byte(vectorDocument), make(ed25519.PublicKey, 32), CodeExpectedKey},
		{"a short expected key", pack, []byte(vectorDocument), public[:31], CodeExpectedKey},
		{"a document by another key, verified against the expected one", pack, byOther, public, CodeUnexpectedKey},
		{"the expected document, against another expected key", pack, []byte(vectorDocument), other, CodeUnexpectedKey},
		{"changed pack bytes", []byte("{}\r\n"), []byte(vectorDocument), public, CodeDigestMismatch},
		{"a byte appended", append([]byte(vectorPack), ' '), []byte(vectorDocument), public, CodeDigestMismatch},
		{"another pack's signature moved beside this pack", otherPack, []byte(vectorDocument), public, CodeDigestMismatch},
		{"a flipped signature", pack, flipped, public, CodeInvalid},
		{"another key's signature claiming the expected key", pack, claimed, public, CodeInvalid},
		{"a packDigest edited to name other bytes", otherPack, reDigested, public, CodeInvalid},
		{"a malformed document is refused before any key comparison", pack, []byte(vectorDocument[:40]), other, CodeMalformed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, refusal := Verify(c.pack, c.document, c.expected)
			if refusal == nil || refusal.Code != c.code {
				t.Fatalf("refusal = %+v, want %s", refusal, c.code)
			}
		})
	}
	// The document by the other key is a good signature: it is refused above
	// for naming a key the verifier did not expect, not for being broken.
	if _, refusal := Verify(pack, byOther, other); refusal != nil {
		t.Fatalf("the other key's document against the other key: %+v", refusal)
	}
	if _, refusal := Verify(otherPack, signDocument(t, vectorSeed, otherPack), public); refusal != nil {
		t.Fatalf("the other pack's own document: %+v", refusal)
	}
}

// The message is a pack's and nothing else's: it begins with its own prefix,
// which names the pack signature and its version, and never with the bare
// digest or the bare pack bytes.
func TestTheMessageIsDomainSeparated(t *testing.T) {
	digest := sha256.Sum256([]byte(vectorPack))
	message := string(Message(digest))
	if !strings.HasPrefix(message, "judgment-pack-runtime/pack-signature/1:") {
		t.Fatalf("message = %s", message)
	}
	if strings.Contains(message, vectorPack) || strings.HasPrefix(message, string(digest[:])) {
		t.Fatalf("the message is not the bare bytes or digest: %q", message)
	}
	if !strings.Contains(message, `"signatureVersion":"`+Version+`"`) {
		t.Fatalf("the message binds the version: %s", message)
	}
}
