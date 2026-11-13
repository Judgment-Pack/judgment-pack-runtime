package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// specPack copies the bundled corpus's minimal valid pack of one spec version
// into a directory of its own, as pack.json, and returns its path and bytes.
func specPack(t *testing.T, version string) (string, []byte) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "artifacts", "jps", version, "cases", "valid", "minimal-expense-approval.json"))
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "pack.json")
	if err := os.WriteFile(target, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return target, data
}

func sha256Text(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// signatureCode is the code of a JSON operational result, or "" for output
// that is not one.
func signatureCode(t *testing.T, stdout string) string {
	t.Helper()
	var payload struct {
		Diagnostics []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil || len(payload.Diagnostics) != 1 {
		return ""
	}
	return payload.Diagnostics[0].Code
}

// A pack of every bundled spec version is signed and its signature accepted
// against the key it was signed with, and the document is the convention's.
func TestPacksSignAndVerifySignatureOnEverySpecVersion(t *testing.T) {
	skipWhereKeysCannotSign(t)
	seed, publicKey := generatedKey(t)
	public := strings.TrimSpace(readFile(t, publicKey))
	for _, version := range []string{"0.1.0-draft", "0.2.0-draft", "0.3.0", "0.5.0"} {
		t.Run(version, func(t *testing.T) {
			pack, data := specPack(t, version)
			code, stdout, stderr := runTest(t, []string{"packs", "sign", pack, "--key", seed, "--public-key", publicKey}, "")
			if code != 0 || stderr != "" || !strings.Contains(stdout, "signed: "+pack+" "+sha256Text(data)+"\n") || !strings.Contains(stdout, "signature: "+pack+".sig.json\n") {
				t.Fatalf("sign: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			document := readFile(t, pack+".sig.json")
			if !strings.HasPrefix(document, `{"algorithm":"ed25519","packDigest":"`+sha256Text(data)+`","publicKey":"`+public+`","signature":"`) ||
				!strings.HasSuffix(document, `","signatureVersion":"1.0.0"}`+"\n") {
				t.Fatalf("document = %s", document)
			}
			code, stdout, stderr = runTest(t, []string{"packs", "verify-signature", pack, "--public-key", publicKey}, "")
			if code != 0 || stderr != "" || !strings.Contains(stdout, "signature: accepted: ") || !strings.Contains(stdout, "pack: valid (specVersion "+version+")") {
				t.Fatalf("verify: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			code, stdout, _ = runTest(t, []string{"packs", "verify-signature", pack, "--public-key", publicKey, "--format", "json"}, "")
			var report result.PackSignatureCheck
			if err := json.Unmarshal([]byte(stdout), &report); err != nil || code != 0 || report.Status != "valid" || report.PackStatus != "valid" ||
				report.PackDigest != sha256Text(data) || report.PublicKey != public || len(report.KeyID) != 32 || report.SpecVersion != version ||
				report.SignatureVersion != "1.0.0" || report.Command != "packs verify-signature" {
				t.Fatalf("verify json: exit=%d %s %v", code, stdout, err)
			}
		})
	}
}

// packs sign --format json reports the document it wrote, and --output and
// --signature name another place for it.
func TestPacksSignJSONAndAnotherPlace(t *testing.T) {
	skipWhereKeysCannotSign(t)
	seed, publicKey := generatedKey(t)
	pack, data := specPack(t, "0.5.0")
	elsewhere := filepath.Join(t.TempDir(), "signature.json")
	code, stdout, _ := runTest(t, []string{"packs", "sign", pack, "--key", seed, "--output", elsewhere, "--format", "json"}, "")
	var report result.PackSignature
	if err := json.Unmarshal([]byte(stdout), &report); err != nil || code != 0 || report.Status != "signed" || report.Signature != elsewhere ||
		report.PackDigest != sha256Text(data) || report.PublicKey+"\n" != readFile(t, publicKey) || report.Command != "packs sign" {
		t.Fatalf("sign json: exit=%d %s %v", code, stdout, err)
	}
	if _, err := os.Stat(pack + ".sig.json"); !os.IsNotExist(err) {
		t.Fatalf("a document was written beside the pack: %v", err)
	}
	if code, stdout, _ := runTest(t, []string{"packs", "verify-signature", pack, "--public-key", publicKey, "--signature", elsewhere}, ""); code != 0 {
		t.Fatalf("verify --signature: exit=%d %s", code, stdout)
	}
}

// Every refusal of a signature exits 1 with its own code, and the pack is not
// read as a document: the refusal is the signature's even for bytes that are
// not a pack.
func TestPacksVerifySignatureRefusals(t *testing.T) {
	skipWhereKeysCannotSign(t)
	seed, publicKey := generatedKey(t)
	otherSeed, otherPublic := generatedKey(t)
	signed, data := specPack(t, "0.5.0")
	if code, stdout, stderr := runTest(t, []string{"packs", "sign", signed, "--key", seed}, ""); code != 0 {
		t.Fatalf("sign: %d %s %s", code, stdout, stderr)
	}
	document := readFile(t, signed+".sig.json")
	otherPack, _ := specPack(t, "0.3.0")
	if code, _, _ := runTest(t, []string{"packs", "sign", otherPack, "--key", seed}, ""); code != 0 {
		t.Fatal("sign the other pack")
	}
	byOtherKey, _ := specPack(t, "0.5.0")
	if code, _, _ := runTest(t, []string{"packs", "sign", byOtherKey, "--key", otherSeed}, ""); code != 0 {
		t.Fatal("sign with the other key")
	}
	// place writes a pack with these bytes and this signature document
	// beside it, and returns the pack's path.
	place := func(t *testing.T, pack []byte, signature string) string {
		t.Helper()
		target := filepath.Join(t.TempDir(), "pack.json")
		if err := os.WriteFile(target, pack, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target+".sig.json", []byte(signature), 0o600); err != nil {
			t.Fatal(err)
		}
		return target
	}
	cases := []struct {
		name, pack, publicKey, code string
		exit                        int
	}{
		{"changed bytes", place(t, append(append([]byte{}, data...), ' '), document), publicKey, "JPS-PACK-SIGNATURE-DIGEST-MISMATCH", result.ExitInvalid},
		{"changed into bytes that are no pack", place(t, []byte("not a pack"), document), publicKey, "JPS-PACK-SIGNATURE-DIGEST-MISMATCH", result.ExitInvalid},
		{"verified against another key", signed, otherPublic, "JPS-PACK-SIGNATURE-UNEXPECTED-KEY", result.ExitInvalid},
		{"signed by another key", byOtherKey, publicKey, "JPS-PACK-SIGNATURE-UNEXPECTED-KEY", result.ExitInvalid},
		{"another key's signature claiming this key", place(t, data, strings.Replace(readFile(t, byOtherKey+".sig.json"), strings.TrimSpace(readFile(t, otherPublic)), strings.TrimSpace(readFile(t, publicKey)), 1)), publicKey, "JPS-PACK-SIGNATURE-INVALID", result.ExitInvalid},
		{"another pack's signature moved beside this one", place(t, data, readFile(t, otherPack+".sig.json")), publicKey, "JPS-PACK-SIGNATURE-DIGEST-MISMATCH", result.ExitInvalid},
		{"a truncated document", place(t, data, document[:len(document)/2]), publicKey, "JPS-PACK-SIGNATURE-MALFORMED", result.ExitInvalid},
		{"an empty document", place(t, data, ""), publicKey, "JPS-PACK-SIGNATURE-MALFORMED", result.ExitInvalid},
		{"a malformed document", place(t, data, "signature: "+document), publicKey, "JPS-PACK-SIGNATURE-MALFORMED", result.ExitInvalid},
		{"a document with a member twice", place(t, data, strings.Replace(document, `{"algorithm":"ed25519",`, `{"algorithm":"ed25519","algorithm":"ed25519",`, 1)), publicKey, "JPS-PACK-SIGNATURE-DUPLICATE-MEMBER", result.ExitInvalid},
		{"a document with another member", place(t, data, strings.Replace(document, `{"algorithm"`, `{"note":"x","algorithm"`, 1)), publicKey, "JPS-PACK-SIGNATURE-UNKNOWN-MEMBER", result.ExitInvalid},
		{"an oversized document", place(t, data, document+strings.Repeat(" ", 4096)), publicKey, "JPS-PACK-SIGNATURE-TOO-LARGE", result.ExitInvalid},
		{"no document", func() string { p, _ := specPack(t, "0.5.0"); return p }(), publicKey, "JPS-PACK-SIGNATURE-MISSING", result.ExitInvalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, stdout, _ := runTest(t, []string{"packs", "verify-signature", c.pack, "--public-key", c.publicKey, "--format", "json"}, "")
			if code != c.exit || signatureCode(t, stdout) != c.code {
				t.Fatalf("exit=%d %s, want %d %s", code, stdout, c.exit, c.code)
			}
			code, _, stderr := runTest(t, []string{"packs", "verify-signature", c.pack, "--public-key", c.publicKey}, "")
			if code != c.exit || !strings.HasPrefix(stderr, "error: ") {
				t.Fatalf("human: exit=%d %q", code, stderr)
			}
		})
	}

	t.Run("a symbolic link to the document", func(t *testing.T) {
		linked := place(t, data, "")
		if err := os.Remove(linked + ".sig.json"); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(signed+".sig.json", linked+".sig.json"); err != nil {
			t.Skip("no symbolic links here:", err)
		}
		code, stdout, _ := runTest(t, []string{"packs", "verify-signature", linked, "--public-key", publicKey, "--format", "json"}, "")
		if code != result.ExitIO || signatureCode(t, stdout) != "JPS-PACK-SIGNATURE-READ" {
			t.Fatalf("exit=%d %s", code, stdout)
		}
	})
	t.Run("no expected key", func(t *testing.T) {
		code, _, stderr := runTest(t, []string{"packs", "verify-signature", signed}, "")
		if code != result.ExitInvocation || !strings.Contains(stderr, "--public-key <file> names the public key the signature must be made with, and is required") {
			t.Fatalf("exit=%d %q", code, stderr)
		}
		code, stdout, _ := runTest(t, []string{"packs", "verify-signature", signed, "--format", "json"}, "")
		if code != result.ExitInvocation || signatureCode(t, stdout) != "JPS-INVOCATION-PACK-SIGNATURE" {
			t.Fatalf("json: exit=%d %s", code, stdout)
		}
	})
	t.Run("an expected key no verifier may trust", func(t *testing.T) {
		zero := writeDocument(t, "zero.pub", strings.Repeat("0", 64)+"\n")
		code, stdout, _ := runTest(t, []string{"packs", "verify-signature", signed, "--public-key", zero, "--format", "json"}, "")
		if code != result.ExitInvocation || signatureCode(t, stdout) != "JPS-PACK-SIGNATURE-EXPECTED-KEY" || !strings.Contains(stdout, "small order") {
			t.Fatalf("exit=%d %s", code, stdout)
		}
	})
	t.Run("the pack as standard input", func(t *testing.T) {
		code, stdout, _ := runTest(t, []string{"packs", "verify-signature", "-", "--public-key", publicKey, "--signature", signed + ".sig.json", "--format", "json"}, string(data))
		if code != result.ExitInvocation || signatureCode(t, stdout) != "JPS-INVOCATION-INPUT" {
			t.Fatalf("exit=%d %s", code, stdout)
		}
	})
}

// An accepted signature is reported even when the bytes it is over then fail
// validation: the signature is checked first, and the pack after it.
func TestPacksVerifySignatureReportsTheSignatureOfAnInvalidPack(t *testing.T) {
	skipWhereKeysCannotSign(t)
	seed, publicKey := generatedKey(t)
	signer, err := audit.LoadSigner(seed, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	invalid := []byte(`{"specVersion":"0.5.0"}`)
	pack := filepath.Join(t.TempDir(), "pack.json")
	if err := os.WriteFile(pack, invalid, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pack+".sig.json", signer.SignPack(invalid), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := runTest(t, []string{"packs", "verify-signature", pack, "--public-key", publicKey, "--format", "json"}, "")
	var report result.PackSignatureCheck
	if err := json.Unmarshal([]byte(stdout), &report); err != nil || code != result.ExitInvalid || report.Status != "pack-invalid" ||
		report.PackStatus != "invalid" || report.PackDigest != sha256Text(invalid) || report.PublicKey != signer.PublicKey() {
		t.Fatalf("exit=%d %s %v", code, stdout, err)
	}
	code, stdout, _ = runTest(t, []string{"packs", "verify-signature", pack, "--public-key", publicKey}, "")
	if code != result.ExitInvalid || !strings.Contains(stdout, "signature: accepted: ") || !strings.Contains(stdout, "pack: invalid (specVersion 0.5.0); jpack spec validate reports why") {
		t.Fatalf("human: exit=%d %q", code, stdout)
	}
}

// packs sign holds its key as a trail's signing key is held, signs only a
// pack this runtime validates, and never writes over anything.
func TestPacksSignRefusals(t *testing.T) {
	skipWhereKeysCannotSign(t)
	seed, publicKey := generatedKey(t)
	_, otherPublic := generatedKey(t)
	t.Run("no key", func(t *testing.T) {
		pack, _ := specPack(t, "0.5.0")
		code, stdout, _ := runTest(t, []string{"packs", "sign", pack, "--format", "json"}, "")
		if code != result.ExitInvocation || signatureCode(t, stdout) != "JPS-INVOCATION-PACK-SIGNATURE" {
			t.Fatalf("exit=%d %s", code, stdout)
		}
	})
	t.Run("a seed that is not the expected key", func(t *testing.T) {
		pack, _ := specPack(t, "0.5.0")
		code, stdout, _ := runTest(t, []string{"packs", "sign", pack, "--key", seed, "--public-key", otherPublic, "--format", "json"}, "")
		if code != result.ExitInvalid || signatureCode(t, stdout) != "JPS-AUDIT-KEY-REFUSED" || !strings.Contains(stdout, audit.ErrKeyNotExpected.Error()) {
			t.Fatalf("exit=%d %s", code, stdout)
		}
		if _, err := os.Stat(pack + ".sig.json"); !os.IsNotExist(err) {
			t.Fatal("a document was written")
		}
	})
	t.Run("a seed others can read", func(t *testing.T) {
		dir := privateTempDir(t)
		open := filepath.Join(dir, "seed")
		if err := os.WriteFile(open, []byte(strings.Repeat("ab", 32)+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(open, 0o644); err != nil {
			t.Fatal(err)
		}
		pack, _ := specPack(t, "0.5.0")
		code, stdout, _ := runTest(t, []string{"packs", "sign", pack, "--key", open, "--format", "json"}, "")
		if code != result.ExitInvalid || signatureCode(t, stdout) != "JPS-AUDIT-KEY-REFUSED" || !strings.Contains(stdout, audit.ErrKeyTooOpen.Error()) {
			t.Fatalf("exit=%d %s", code, stdout)
		}
	})
	t.Run("a seed named through a symbolic link", func(t *testing.T) {
		link := filepath.Join(privateTempDir(t), "seed")
		if err := os.Symlink(seed, link); err != nil {
			t.Skip("no symbolic links here:", err)
		}
		pack, _ := specPack(t, "0.5.0")
		code, stdout, _ := runTest(t, []string{"packs", "sign", pack, "--key", link, "--format", "json"}, "")
		if code != result.ExitInvalid || signatureCode(t, stdout) != "JPS-AUDIT-KEY-REFUSED" {
			t.Fatalf("exit=%d %s", code, stdout)
		}
	})
	t.Run("a file this runtime does not validate", func(t *testing.T) {
		pack := writeDocument(t, "pack.json", `{"specVersion":"0.5.0"}`)
		code, stdout, _ := runTest(t, []string{"packs", "sign", pack, "--key", seed, "--format", "json"}, "")
		if code != result.ExitInvalid || signatureCode(t, stdout) != "JPS-PACK-SIGNATURE-PACK-NOT-VALID" {
			t.Fatalf("exit=%d %s", code, stdout)
		}
		if _, err := os.Stat(pack + ".sig.json"); !os.IsNotExist(err) {
			t.Fatal("a document was written")
		}
		unbundled := writeDocument(t, "pack.json", `{"specVersion":"9.9.9"}`)
		if code, stdout, _ := runTest(t, []string{"packs", "sign", unbundled, "--key", seed, "--format", "json"}, ""); code != result.ExitUnsupported || signatureCode(t, stdout) != "JPS-PACK-SIGNATURE-PACK-NOT-VALID" {
			t.Fatalf("unsupported: exit=%d %s", code, stdout)
		}
	})
	t.Run("an existing document", func(t *testing.T) {
		pack, _ := specPack(t, "0.5.0")
		if err := os.WriteFile(pack+".sig.json", []byte("kept"), 0o600); err != nil {
			t.Fatal(err)
		}
		code, stdout, _ := runTest(t, []string{"packs", "sign", pack, "--key", seed, "--public-key", publicKey, "--format", "json"}, "")
		if code != result.ExitIO || signatureCode(t, stdout) != "JPS-PACK-SIGNATURE-EXISTS" || readFile(t, pack+".sig.json") != "kept" {
			t.Fatalf("exit=%d %s", code, stdout)
		}
	})
	t.Run("the pack as standard input", func(t *testing.T) {
		_, data := specPack(t, "0.5.0")
		code, stdout, _ := runTest(t, []string{"packs", "sign", "-", "--key", seed, "--output", filepath.Join(t.TempDir(), "s.json"), "--format", "json"}, string(data))
		if code != result.ExitInvocation || signatureCode(t, stdout) != "JPS-INVOCATION-INPUT" {
			t.Fatalf("exit=%d %s", code, stdout)
		}
	})
}

// Each command reads the pack once: the file replaced between its two checks
// changes neither what sign signs nor what verify-signature validates.
func TestPackSignatureCommandsReadThePackOnce(t *testing.T) {
	skipWhereKeysCannotSign(t)
	seed, publicKey := generatedKey(t)
	replaceWith := func(t *testing.T, pack string, data []byte) {
		t.Helper()
		previous := betweenPackChecks
		betweenPackChecks = func() {
			if err := os.WriteFile(pack, data, 0o600); err != nil {
				t.Error(err)
			}
		}
		t.Cleanup(func() { betweenPackChecks = previous })
	}
	t.Run("sign", func(t *testing.T) {
		pack, data := specPack(t, "0.5.0")
		_, other := specPack(t, "0.3.0")
		replaceWith(t, pack, other)
		if code, stdout, stderr := runTest(t, []string{"packs", "sign", pack, "--key", seed}, ""); code != 0 {
			t.Fatalf("sign: %d %s %s", code, stdout, stderr)
		}
		if document := readFile(t, pack+".sig.json"); !strings.Contains(document, `"packDigest":"`+sha256Text(data)+`"`) {
			t.Fatalf("the signature is not over the bytes validated: %s", document)
		}
	})
	t.Run("verify-signature", func(t *testing.T) {
		pack, _ := specPack(t, "0.5.0")
		if code, _, _ := runTest(t, []string{"packs", "sign", pack, "--key", seed}, ""); code != 0 {
			t.Fatal("sign")
		}
		replaceWith(t, pack, []byte("not a pack"))
		code, stdout, _ := runTest(t, []string{"packs", "verify-signature", pack, "--public-key", publicKey, "--format", "json"}, "")
		var report result.PackSignatureCheck
		if err := json.Unmarshal([]byte(stdout), &report); err != nil || code != 0 || report.PackStatus != "valid" {
			t.Fatalf("the pack validated is not the pack checked: exit=%d %s %v", code, stdout, err)
		}
	})
}
