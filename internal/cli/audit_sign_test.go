package cli

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// generatedKey runs audit key generate into a directory of its own and
// returns the seed's path and the public key file holding what it printed.
// The directory is made its owner's alone, as a signing key's must be:
// t.TempDir leaves it 0775 under umask 0002.
func generatedKey(t *testing.T) (string, string) {
	t.Helper()
	dir := privateTempDir(t)
	seed := filepath.Join(dir, "seed")
	code, stdout, stderr := runTest(t, []string{"audit", "key", "generate", seed}, "")
	if code != 0 || len(stdout) != 65 || !strings.HasSuffix(stdout, "\n") || !strings.Contains(stderr, "keyId") {
		t.Fatalf("generate: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	public := filepath.Join(dir, "key.pub")
	if err := os.WriteFile(public, []byte(stdout), 0o600); err != nil {
		t.Fatal(err)
	}
	return seed, public
}

// privateTempDir is a fresh directory named by its real path and writable by
// its owner alone, which a signing key's directory must be. The machine's
// directories above it, which the test does not choose, are held as root's
// 0755 for the rest of the test, so nothing here depends on where TMPDIR is.
func privateTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(audit.StandInKeyAncestorsForTests(dir))
	return dir
}

// keysSignHere says whether a key can sign on this platform: where its
// privacy cannot be checked, every key is refused and records are unsigned.
func keysSignHere(t *testing.T) bool {
	t.Helper()
	seed, _ := generatedKey(t)
	_, err := audit.LoadSigner(seed, nil, nil)
	return !errors.Is(err, audit.ErrKeyPrivacyUnchecked)
}

// skipWhereKeysCannotSign skips a test of signed records on a platform where
// no key signs; TestNoKeySignsWhereItsPrivacyCannotBeChecked covers it there.
func skipWhereKeysCannotSign(t *testing.T) {
	t.Helper()
	if !keysSignHere(t) {
		t.Skip("no key signs on this platform: its privacy cannot be checked")
	}
}

// evaluateOnce records one decision in the project.
func evaluateOnce(t *testing.T, configPath string) {
	t.Helper()
	facts := writeDocument(t, "facts.json", hardFailFacts)
	if code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake", "--config", configPath, "--facts", facts}, ""); code != 0 {
		t.Fatalf("exit=%d stderr=%q stdout=%q", code, stderr, stdout)
	}
}

// audit key generate writes a new seed its owner alone can read, prints only
// the public key, and never writes over anything; audit key public prints the
// same key back.
func TestAuditKeyGenerateAndPublic(t *testing.T) {
	seed, public := generatedKey(t)
	data, err := os.ReadFile(seed)
	if err != nil {
		t.Fatal(err)
	}
	printed, err := os.ReadFile(public)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 65 || strings.Contains(string(printed), strings.TrimSpace(string(data))) {
		t.Fatalf("the seed is 64 hexadecimal characters and a newline, and is not printed: %d bytes", len(data))
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(seed)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("the seed's mode: %v %v", info.Mode().Perm(), err)
		}
	}
	code, stdout, _ := runTest(t, []string{"audit", "key", "public", seed}, "")
	if code != 0 || stdout != string(printed) {
		t.Fatalf("public: exit=%d %q, want %q", code, stdout, printed)
	}
	code, stdout, _ = runTest(t, []string{"audit", "key", "public", "--format", "json", seed}, "")
	var key result.AuditKey
	if err := json.Unmarshal([]byte(stdout), &key); err != nil || code != 0 || key.PublicKey+"\n" != string(printed) || len(key.KeyID) != 32 || key.Command != "audit key public" {
		t.Fatalf("public json: exit=%d %q %v", code, stdout, err)
	}
	code, stdout, _ = runTest(t, []string{"audit", "key", "generate", "--format", "json", seed}, "")
	if code != result.ExitIO || !strings.Contains(stdout, `"JPS-AUDIT-KEY-EXISTS"`) {
		t.Fatalf("generate over an existing file: exit=%d %q", code, stdout)
	}
	after, err := os.ReadFile(seed)
	if err != nil || string(after) != string(data) {
		t.Fatal("the existing seed is untouched")
	}
	if runtime.GOOS == "windows" {
		return
	}
	if err := os.Chmod(seed, 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ = runTest(t, []string{"audit", "key", "public", "--format", "json", seed}, "")
	if code != result.ExitInvalid || !strings.Contains(stdout, `"JPS-AUDIT-KEY-REFUSED"`) || !strings.Contains(stdout, audit.ErrKeyTooOpen.Error()) {
		t.Fatalf("a key others can read: exit=%d %q", code, stdout)
	}
}

// A project that names a key signs each record it writes, and audit verify
// --public-key checks every signature and reports how far they reach. A key
// inside the project signs nothing, and the decision is recorded all the same.
func TestAuditVerifyChecksTheSignatures(t *testing.T) {
	skipWhereKeysCannotSign(t)
	seed, public := generatedKey(t)
	t.Setenv(audit.SigningKeyEnv, seed)
	configPath, trail := recordedProject(t, 3)
	code, output := verification(t, "--config", configPath, "--public-key", public, "--require-signed-through", "3")
	if code != 0 || output.Status != "valid" || output.Coverage.Signed.Status != "through" || output.Coverage.Signed.Through != 3 ||
		output.Coverage.SignedRecords != 3 || output.Coverage.UnsignedRecords != 0 || output.Signatures == nil || output.Signatures.Lines != 3 ||
		output.RequiredSigned == nil || output.RequiredSigned.Status != "met" {
		t.Fatalf("exit=%d %+v %+v %+v", code, output.Coverage, output.Signatures, output.RequiredSigned)
	}
	// --trail reads the sidecar beside the trail, and --signatures names one.
	sidecar := filepath.Join(filepath.Dir(trail), audit.SidecarName)
	if code, named := verification(t, "--trail", trail, "--public-key", public); code != 0 || named.Coverage.SignedRecords != 3 {
		t.Fatalf("--trail: exit=%d %+v", code, named.Coverage)
	}
	moved := filepath.Join(t.TempDir(), "elsewhere.jsonl")
	data, err := os.ReadFile(sidecar)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(moved, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if code, named := verification(t, "--config", configPath, "--public-key", public, "--signatures", moved); code != 0 || named.Coverage.SignedRecords != 3 {
		t.Fatalf("--signatures: exit=%d %+v", code, named.Coverage)
	}
	code, stdout, _ := runTest(t, []string{"audit", "verify", "--config", configPath, "--public-key", public}, "")
	if code != 0 || !strings.Contains(stdout, "signed: through sequence 3") || !strings.Contains(stdout, "3 with a valid signature of their own, 0 without") ||
		!strings.Contains(stdout, "NOT ESTABLISHED: Anything against the operator") || !strings.Contains(stdout, "ESTABLISHED: Lines 1 to 3 are as they stood") {
		t.Fatalf("human output: %q", stdout)
	}
	// The last record edited: the chain alone cannot see it, the signature can.
	rewriteLine(t, trail, 2, func(line []byte) []byte {
		return []byte(strings.Replace(string(line), `"evidenceSupplied":false`, `"evidenceSupplied":true`, 1))
	})
	if code, plain := verification(t, "--config", configPath); code != 0 || plain.Status != "valid" {
		t.Fatalf("without a key: exit=%d %s", code, plain.Status)
	}
	code, output = verification(t, "--config", configPath, "--public-key", public, "--require-signed-through", "3")
	if code != result.ExitInvalid || output.Status != "invalid" || len(output.Findings) != 2 ||
		output.Findings[0].Name != audit.FindingSignatureRecordMismatch || output.Findings[1].Name != audit.FindingSignatureMissing ||
		output.RequiredSigned == nil || output.RequiredSigned.Status != "unmet" || !endsWithAttempts(output.DoesNotEstablish) {
		t.Fatalf("an edited last record: exit=%d %+v %q", code, output.Findings, output.DoesNotEstablish)
	}
	// Another key does not verify the trail.
	_, otherPublic := generatedKey(t)
	if code, other := verification(t, "--config", configPath, "--public-key", otherPublic); code != result.ExitInvalid || other.Findings[0].Name != audit.FindingSignatureInvalid {
		t.Fatalf("another key: exit=%d %+v", code, other.Findings)
	}
}

// The flags that read signatures need a key to read them by, and the inputs
// are refused when they are not of their shape.
func TestAuditVerifySignatureFlagsAreChecked(t *testing.T) {
	configPath, _ := recordedProject(t, 1)
	for _, args := range [][]string{
		{"--require-signed-through", "1"},
		{"--revoked", "revoked.jsonl"},
		{"--signatures", "signatures.jsonl"},
	} {
		code, stdout, _ := runTest(t, append([]string{"audit", "verify", "--format", "json", "--config", configPath}, args...), "")
		if code != result.ExitInvocation || !strings.Contains(stdout, `"JPS-INVOCATION-AUDIT-SIGNATURES"`) {
			t.Fatalf("%v: exit=%d %q", args, code, stdout)
		}
	}
	notAKey := writeDocument(t, "key.pub", "not a key\n")
	code, stdout, _ := runTest(t, []string{"audit", "verify", "--format", "json", "--config", configPath, "--public-key", notAKey}, "")
	if code != result.ExitInvocation || !strings.Contains(stdout, `"JPS-AUDIT-PUBLIC-KEY-INVALID"`) {
		t.Fatalf("not a key: exit=%d %q", code, stdout)
	}
	_, public := generatedKey(t)
	badRevoked := writeDocument(t, "revoked.jsonl", `{"from":0,"publicKey":"x"}`)
	code, stdout, _ = runTest(t, []string{"audit", "verify", "--format", "json", "--config", configPath, "--public-key", public, "--revoked", badRevoked}, "")
	if code != result.ExitInvocation || !strings.Contains(stdout, `"JPS-AUDIT-REVOKED-INVALID"`) {
		t.Fatalf("bad revocations: exit=%d %q", code, stdout)
	}
	code, stdout, _ = runTest(t, []string{"audit", "verify", "--format", "json", "--config", configPath, "--public-key", public, "--signatures", filepath.Join(t.TempDir(), "absent")}, "")
	if code != result.ExitIO || !strings.Contains(stdout, `"JPS-AUDIT-SIGNATURES-READ"`) {
		t.Fatalf("an absent --signatures: exit=%d %q", code, stdout)
	}
	code, stdout, _ = runTest(t, []string{"audit", "verify", "--format", "json", "--config", configPath, "--public-key", public, "--require-signed-through", "-1"}, "")
	if code != result.ExitInvocation || !strings.Contains(stdout, `"JPS-INVOCATION-AUDIT-REQUIRE"`) {
		t.Fatalf("a negative requirement: exit=%d %q", code, stdout)
	}
	// An unsigned trail with a key supplied: nothing is signed, and that is
	// not a failure unless coverage is required.
	code, output := verification(t, "--config", configPath, "--public-key", public)
	if code != 0 || output.Coverage.Signed.Status != "none" || output.Coverage.UnsignedRecords != 1 || output.Signatures.Lines != 0 {
		t.Fatalf("an unsigned trail: exit=%d %+v", code, output.Coverage)
	}
}

// A public key that permits forgery is refused before anything is read with
// it (#216), as --public-key and in --revoked, and the refusal names why,
// never the key. Under the identity, crypto/ed25519 accepts the forged
// signature beside this trail, made with no private key; a verifier that
// took the key would count the record signed.
func TestAuditVerifyRefusesAKeyThatPermitsForgery(t *testing.T) {
	dir := t.TempDir()
	const trail = "00112233445566778899aabbccddeeff"
	line := fmt.Sprintf(`{"recordVersion":"1","trail":"%s","sequence":1,"previous":"%s","kind":"evaluation"}`, trail, audit.Digest(nil))
	identity := "01" + strings.Repeat("0", 62)
	public, _ := hex.DecodeString(identity)
	forged := append(append([]byte{}, public...), make([]byte, 32)...)
	if !ed25519.Verify(public, audit.RecordMessage(trail, 1, audit.Digest([]byte(line))), forged) {
		t.Fatal("the forgery does not verify under crypto/ed25519, so the test shows nothing")
	}
	sidecar := fmt.Sprintf(`{"keyId":"%s","kind":"record-signature","record":"%s","sequence":1,"sidecarVersion":"1","signature":"%s","trail":"%s"}`+"\n",
		audit.KeyID(public), audit.Digest([]byte(line)), hex.EncodeToString(forged), trail)
	trailPath := filepath.Join(dir, "evaluations.jsonl")
	if err := os.WriteFile(trailPath, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, audit.SidecarName), []byte(sidecar), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, refused := range []struct{ key, why string }{
		{identity, "small order"},
		{strings.Repeat("0", 64), "small order"},
		{"ed" + strings.Repeat("f", 60) + "7f", "not the canonical encoding"},
		{"01" + strings.Repeat("0", 60) + "80", "not the canonical encoding"},
		{"02" + strings.Repeat("0", 62), "not a point"},
	} {
		keyPath := writeDocument(t, "refused.pub", refused.key+"\n")
		code, stdout, _ := runTest(t, []string{"audit", "verify", "--format", "json", "--trail", trailPath, "--public-key", keyPath}, "")
		if code != result.ExitInvocation || !strings.Contains(stdout, `"JPS-AUDIT-PUBLIC-KEY-INVALID"`) || !strings.Contains(stdout, refused.why) || strings.Contains(stdout, refused.key) {
			t.Fatalf("%s: exit=%d %q", refused.key, code, stdout)
		}
	}
	_, good := generatedKey(t)
	revoked := writeDocument(t, "revoked.jsonl", `{"from":1,"publicKey":"`+identity+`"}`)
	code, stdout, _ := runTest(t, []string{"audit", "verify", "--format", "json", "--trail", trailPath, "--public-key", good, "--revoked", revoked}, "")
	if code != result.ExitInvocation || !strings.Contains(stdout, `"JPS-AUDIT-REVOKED-INVALID"`) || !strings.Contains(stdout, "small order") {
		t.Fatalf("a revocation of a key of small order: exit=%d %q", code, stdout)
	}
	// A key a verifier accepts reads the same trail: the forged line is
	// another key's, and the record is unsigned.
	code, output := verification(t, "--trail", trailPath, "--public-key", good)
	if code != result.ExitInvalid || len(output.Findings) != 1 || output.Findings[0].Name != audit.FindingSignatureInvalid || output.Coverage.SignedRecords != 0 {
		t.Fatalf("under a good key: exit=%d %+v %+v", code, output.Findings, output.Coverage)
	}
}

// audit key rotate hands signing to the next key: the old key signs nothing
// more, records are unsigned until the next key is named, and the trail
// verifies under the first key, and under both pinned in order. A revocation
// of the first key from the rotation on changes nothing for the genuine trail.
func TestAuditKeyRotateHandsSigningOver(t *testing.T) {
	skipWhereKeysCannotSign(t)
	firstSeed, firstPublic := generatedKey(t)
	nextSeed, nextPublic := generatedKey(t)
	t.Setenv(audit.SigningKeyEnv, firstSeed)
	configPath, _ := recordedProject(t, 2)
	code, stdout, stderr := runTest(t, []string{"audit", "key", "rotate", "--format", "json", "--config", configPath, "--next", nextSeed}, "")
	var rotation result.AuditRotation
	if err := json.Unmarshal([]byte(stdout), &rotation); err != nil || code != 0 || rotation.Status != "rotated" || rotation.At != 2 || rotation.NextPublicKey+"\n" != readFile(t, nextPublic) {
		t.Fatalf("rotate: exit=%d %q %q %v", code, stdout, stderr, err)
	}
	evaluateOnce(t, configPath) // still the first key: unsigned
	t.Setenv(audit.SigningKeyEnv, nextSeed)
	evaluateOnce(t, configPath)
	code, output := verification(t, "--config", configPath, "--public-key", firstPublic)
	if code != 0 || output.Signatures.Rotations != 1 || output.Coverage.SignedRecords != 3 || output.Coverage.UnsignedRecords != 1 || output.Signatures.KeyInForce != rotation.Next {
		t.Fatalf("under the first key: exit=%d %+v %+v %+v", code, output.Coverage, output.Signatures, output.Findings)
	}
	if code, pinned := verification(t, "--config", configPath, "--public-key", firstPublic, "--public-key", nextPublic); code != 0 || pinned.Signatures.KeysSupplied != 2 {
		t.Fatalf("pinned: exit=%d %+v", code, pinned.Findings)
	}
	revoked := writeDocument(t, "revoked.jsonl", `{"from":3,"publicKey":"`+strings.TrimSpace(readFile(t, firstPublic))+`"}`+"\n")
	if code, out := verification(t, "--config", configPath, "--public-key", firstPublic, "--revoked", revoked); code != 0 || out.Signatures.Revocations != 1 {
		t.Fatalf("revoked from the rotation on: exit=%d %+v", code, out.Findings)
	}
	// The old key cannot rotate again: it is not the key in force.
	t.Setenv(audit.SigningKeyEnv, firstSeed)
	thirdSeed, _ := generatedKey(t)
	code, stdout, _ = runTest(t, []string{"audit", "key", "rotate", "--format", "json", "--config", configPath, "--next", thirdSeed}, "")
	if code != result.ExitInvalid || !strings.Contains(stdout, `"JPS-AUDIT-KEY-NOT-IN-FORCE"`) {
		t.Fatalf("a rotation from a key not in force: exit=%d %q", code, stdout)
	}
	// The next key named to rotate to itself, a next key inside the project,
	// and a project naming no key are refused.
	t.Setenv(audit.SigningKeyEnv, nextSeed)
	code, stdout, _ = runTest(t, []string{"audit", "key", "rotate", "--format", "json", "--config", configPath, "--next", nextSeed}, "")
	if code != result.ExitInvocation || !strings.Contains(stdout, `"JPS-AUDIT-KEY-SAME"`) {
		t.Fatalf("a rotation to the same key: exit=%d %q", code, stdout)
	}
	realDir, err := filepath.EvalSymlinks(filepath.Dir(configPath))
	if err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(realDir, "next.seed")
	if err := os.WriteFile(inside, []byte(readFile(t, thirdSeed)), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ = runTest(t, []string{"audit", "key", "rotate", "--format", "json", "--config", configPath, "--next", inside}, "")
	if code != result.ExitInvalid || !strings.Contains(stdout, `"JPS-AUDIT-KEY-REFUSED"`) || !strings.Contains(stdout, audit.ErrKeyInsideProject.Error()) {
		t.Fatalf("a next key inside the project: exit=%d %q", code, stdout)
	}
	t.Setenv(audit.SigningKeyEnv, "")
	code, stdout, _ = runTest(t, []string{"audit", "key", "rotate", "--format", "json", "--config", configPath, "--next", thirdSeed}, "")
	if code != result.ExitInvocation || !strings.Contains(stdout, `"JPS-AUDIT-KEY-NONE"`) {
		t.Fatalf("no key named: exit=%d %q", code, stdout)
	}
	code, stdout, _ = runTest(t, []string{"audit", "key", "rotate", "--format", "json", "--config", configPath}, "")
	if code != result.ExitInvocation || !strings.Contains(stdout, `"JPS-INVOCATION-AUDIT-KEY"`) {
		t.Fatalf("no --next: exit=%d %q", code, stdout)
	}
}

// A key the project names inside its own directory signs nothing; the
// decision is recorded, packs validate says why, and audit verify reports the
// record unsigned.
func TestASigningKeyInsideTheProjectSignsNothing(t *testing.T) {
	skipWhereKeysCannotSign(t)
	configPath := auditProject(t)
	realDir, err := filepath.EvalSymlinks(filepath.Dir(configPath))
	if err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(realDir, "keys", "seed")
	if err := os.MkdirAll(filepath.Dir(inside), 0o700); err != nil {
		t.Fatal(err)
	}
	// generate holds the directories it writes into: the project's own is
	// made with what the umask leaves, and those above it are the machine's.
	if err := os.Chmod(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(audit.StandInKeyAncestorsForTests(realDir))
	code, stdout, _ := runTest(t, []string{"audit", "key", "generate", inside}, "")
	if code != 0 {
		t.Fatalf("generate: exit=%d", code)
	}
	public := writeDocument(t, "key.pub", stdout)
	t.Setenv(audit.SigningKeyEnv, inside)
	evaluateOnce(t, configPath)
	code, stdout, _ = runTest(t, []string{"packs", "validate", "--config", configPath}, "")
	if code == 0 || !strings.Contains(stdout, "audit-signing-key: failed") || !strings.Contains(stdout, audit.ErrKeyInsideProject.Error()) {
		t.Fatalf("validate: exit=%d %q", code, stdout)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(configPath), "audit", audit.SidecarName)); !os.IsNotExist(err) {
		t.Fatalf("no sidecar is written: %v", err)
	}
	code, output := verification(t, "--config", configPath, "--public-key", public)
	if code != 0 || output.Coverage.UnsignedRecords != 1 || output.Coverage.Signed.Status != "none" {
		t.Fatalf("verify: exit=%d %+v", code, output.Coverage)
	}
}

// A key in a directory its group or other users can write, or under one,
// signs nothing (#221). audit key generate writes no seed there; public and
// rotate --next refuse such a key; a decision is recorded unsigned, packs
// validate names the directory and the chmod go-w that fixes it, and audit
// verify reports the record unsigned. A directory its owner's alone, or a
// sticky one, is where a key signs, and once fixed the next signed record
// covers the unsigned one through the chain.
func TestAKeyInADirectoryOthersCanWriteSignsNothing(t *testing.T) {
	skipWhereKeysCannotSign(t)
	base := privateTempDir(t)
	directory := func(mode os.FileMode, names ...string) string {
		t.Helper()
		path := filepath.Join(append([]string{base}, names...)...)
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		return path
	}
	// generate: (b) 0777, (c) 0775 and (d) a 0777 ancestor are refused, and
	// nothing is written there.
	open := directory(0o777, "open")
	group := directory(0o775, "group")
	under := directory(0o700, "shared", "k")
	directory(0o777, "shared")
	for _, row := range []struct{ name, dir, named string }{
		{"(b) 0777", open, open},
		{"(c) 0775", group, group},
		{"(d) under a 0777 ancestor", under, filepath.Join(base, "shared")},
	} {
		code, stdout, _ := runTest(t, []string{"audit", "key", "generate", "--format", "json", filepath.Join(row.dir, "seed")}, "")
		if code != result.ExitInvalid || !strings.Contains(stdout, `"JPS-AUDIT-KEY-REFUSED"`) || !strings.Contains(stdout, "chmod go-w "+row.named+" fixes it") {
			t.Fatalf("generate into %s: exit=%d %q", row.name, code, stdout)
		}
		entries, err := os.ReadDir(row.dir)
		if err != nil || len(entries) != 0 {
			t.Fatalf("generate into %s wrote %d entries: %v", row.name, len(entries), err)
		}
	}
	// (a) and (e): generate writes there as before.
	private := directory(0o700, "private")
	sticky := directory(os.ModeSticky|0o777, "sticky")
	keys := map[string]string{}
	for _, dir := range []string{private, sticky} {
		seed := filepath.Join(dir, "seed")
		code, stdout, stderr := runTest(t, []string{"audit", "key", "generate", seed}, "")
		if code != 0 || len(stdout) != 65 {
			t.Fatalf("generate into %s: exit=%d %q %q", dir, code, stdout, stderr)
		}
		keys[dir] = writeDocument(t, filepath.Base(dir)+".pub", stdout)
	}
	seed, public := filepath.Join(private, "seed"), keys[private]
	// The directory is loosened after the key was made.
	if err := os.Chmod(private, 0o777); err != nil {
		t.Fatal(err)
	}
	fix := "chmod go-w " + private + " fixes it"
	code, stdout, _ := runTest(t, []string{"audit", "key", "public", "--format", "json", seed}, "")
	if code != result.ExitInvalid || !strings.Contains(stdout, `"JPS-AUDIT-KEY-REFUSED"`) || !strings.Contains(stdout, fix) {
		t.Fatalf("public: exit=%d %q", code, stdout)
	}
	// Named by JPACK_SIGNING_KEY, it signs nothing, and the decision is
	// recorded all the same.
	t.Setenv(audit.SigningKeyEnv, seed)
	configPath, _ := recordedProject(t, 1)
	code, stdout, _ = runTest(t, []string{"packs", "validate", "--config", configPath}, "")
	if code == 0 || !strings.Contains(stdout, "audit-signing-key: failed") || !strings.Contains(stdout, audit.SigningKeyEnv+" names") || !strings.Contains(stdout, fix) {
		t.Fatalf("validate: exit=%d %q", code, stdout)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(configPath), "audit", audit.SidecarName)); !os.IsNotExist(err) {
		t.Fatalf("no sidecar is written: %v", err)
	}
	code, output := verification(t, "--config", configPath, "--public-key", public)
	if code != 0 || output.Coverage.UnsignedRecords != 1 || output.Coverage.SignedRecords != 0 || output.Coverage.Signed.Status != "none" {
		t.Fatalf("verify: exit=%d %+v", code, output.Coverage)
	}
	code, output = verification(t, "--config", configPath, "--public-key", public, "--require-signed-through", "1")
	if code != 1 || len(output.Findings) == 0 || output.Findings[0].Name != "signature-missing" {
		t.Fatalf("verify --require-signed-through 1: exit=%d %+v", code, output.Findings)
	}
	// rotate --next refuses such a key as the next one.
	t.Setenv(audit.SigningKeyEnv, filepath.Join(sticky, "seed"))
	signed, _ := recordedProject(t, 1)
	code, stdout, _ = runTest(t, []string{"audit", "key", "rotate", "--format", "json", "--config", signed, "--next", seed}, "")
	if code != result.ExitInvalid || !strings.Contains(stdout, `"JPS-AUDIT-KEY-REFUSED"`) || !strings.Contains(stdout, fix) {
		t.Fatalf("rotate --next: exit=%d %q", code, stdout)
	}
	// chmod go-w restores signing, and the next signed record covers the
	// unsigned one through the chain.
	if err := os.Chmod(private, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(audit.SigningKeyEnv, seed)
	if code, stdout, _ := runTest(t, []string{"packs", "validate", "--config", configPath}, ""); code != 0 || !strings.Contains(stdout, "audit-signing-key: passed") {
		t.Fatalf("validate after chmod go-w: exit=%d %q", code, stdout)
	}
	evaluateOnce(t, configPath)
	code, output = verification(t, "--config", configPath, "--public-key", public, "--require-signed-through", "2")
	if code != 0 || output.Coverage.SignedRecords != 1 || output.Coverage.UnsignedRecords != 1 || output.Coverage.Signed.Through != 2 {
		t.Fatalf("verify after chmod go-w: exit=%d %+v %+v", code, output.Coverage, output.Findings)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// Where a key's privacy cannot be checked, Windows among them, no key signs:
// the decision is recorded unsigned, packs validate fails the
// audit-signing-key check and says why, and audit key generate and public
// still work, since they sign nothing.
func TestNoKeySignsWhereItsPrivacyCannotBeChecked(t *testing.T) {
	if keysSignHere(t) {
		t.Skip("keys sign on this platform")
	}
	seed, public := generatedKey(t)
	t.Setenv(audit.SigningKeyEnv, seed)
	configPath, _ := recordedProject(t, 1)
	code, stdout, _ := runTest(t, []string{"packs", "validate", "--config", configPath}, "")
	if code == 0 || !strings.Contains(stdout, "audit-signing-key: failed") || !strings.Contains(stdout, audit.ErrKeyPrivacyUnchecked.Error()) {
		t.Fatalf("validate: exit=%d %q", code, stdout)
	}
	code, output := verification(t, "--config", configPath, "--public-key", public)
	if code != 0 || output.Coverage.UnsignedRecords != 1 || output.Coverage.Signed.Status != "none" {
		t.Fatalf("verify: exit=%d %+v", code, output.Coverage)
	}
}

// signingKeyDetail runs packs validate in format and returns its exit code,
// its whole output and the audit-signing-key check's status and detail, read
// from the JSON payload or from the human report's line.
func signingKeyDetail(t *testing.T, configPath, format string) (int, string, string, string) {
	t.Helper()
	code, stdout, stderr := runTest(t, []string{"packs", "validate", "--format", format, "--config", configPath}, "")
	if format == "json" {
		var output result.PackValidation
		if err := json.Unmarshal([]byte(stdout), &output); err != nil {
			t.Fatalf("undecodable output %q (stderr %q): %v", stdout, stderr, err)
		}
		for _, check := range output.Checks {
			if check.Name == "audit-signing-key" {
				return code, stdout + stderr, check.Status, check.Detail
			}
		}
		return code, stdout + stderr, "", ""
	}
	for _, line := range strings.Split(stdout, "\n") {
		if status, ok := strings.CutPrefix(strings.TrimSpace(line), "audit-signing-key: "); ok {
			status, detail, _ := strings.Cut(status, " ")
			return code, stdout + stderr, status, detail
		}
	}
	return code, stdout + stderr, "", ""
}

// A seed replaced at its path by another valid seed, with the expected public
// key named, signs nothing (#255), in either output format: the decision is
// made and recorded, the record is unsigned, audit verify counts it unsigned,
// packs validate gives the reason, and nothing printed names the path or
// either key. A fresh trail, where no earlier signature makes the replacement
// a key not in force, is refused the same way: only the comparison keeps the
// replacement from signing there.
func TestASeedReplacedAtItsPathSignsNothing(t *testing.T) {
	skipWhereKeysCannotSign(t)
	for _, format := range []string{"human", "json"} {
		t.Run(format, func(t *testing.T) {
			seed, public := generatedKey(t)
			replacement, replacementPublic := generatedKey(t)
			expected := strings.TrimSpace(readFile(t, public))
			replaced := strings.TrimSpace(readFile(t, replacementPublic))
			t.Setenv(audit.SigningKeyEnv, seed)
			t.Setenv(audit.SigningPublicKeyEnv, expected)
			configPath, _ := recordedProject(t, 1)
			sidecar := filepath.Join(filepath.Dir(configPath), "audit", audit.SidecarName)
			before := readFile(t, sidecar)
			if code, output := verification(t, "--config", configPath, "--public-key", public); code != 0 || output.Coverage.SignedRecords != 1 || output.Coverage.UnsignedRecords != 0 {
				t.Fatalf("the expected key signs: exit=%d %+v", code, output.Coverage)
			}
			if code, _, status, detail := signingKeyDetail(t, configPath, format); code != 0 || status != "passed" || !strings.Contains(detail, "it is the expected key "+audit.SigningPublicKeyEnv+" names") {
				t.Fatalf("validate with the expected key: exit=%d %q %q", code, status, detail)
			}
			// Another valid seed is put at the path.
			original, substitute := strings.TrimSpace(readFile(t, seed)), strings.TrimSpace(readFile(t, replacement))
			if err := os.WriteFile(seed, []byte(substitute+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			secrets := []string{seed, filepath.Dir(seed), replacement, expected, replaced, original, substitute}
			names := func(what, text string) {
				t.Helper()
				for _, secret := range secrets {
					if strings.Contains(text, secret) {
						t.Errorf("%s names %q: %q", what, secret, text)
					}
				}
			}
			facts := writeDocument(t, "facts.json", hardFailFacts)
			evaluate := func(configPath string) {
				t.Helper()
				code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", "--format", format, "--pack-id", "intake", "--config", configPath, "--facts", facts}, "")
				if code != 0 {
					t.Fatalf("the decision is made: exit=%d %q %q", code, stdout, stderr)
				}
				names("evaluate", stdout+stderr)
			}
			evaluate(configPath)
			code, printed, status, detail := signingKeyDetail(t, configPath, format)
			if code == 0 || status != "failed" || !strings.Contains(detail, audit.ErrKeyNotExpected.Error()) || !strings.Contains(detail, audit.SigningPublicKeyEnv+" names the expected key") {
				t.Errorf("validate after the replacement: exit=%d %q %q", code, status, detail)
			}
			names("packs validate", printed)
			code, output := verification(t, "--config", configPath, "--public-key", public)
			if code != 0 || output.Coverage.SignedRecords != 1 || output.Coverage.UnsignedRecords != 1 || output.Coverage.Signed.Through != 1 {
				t.Errorf("the record after the replacement is unsigned: exit=%d %+v", code, output.Coverage)
			}
			if after := readFile(t, sidecar); after != before {
				t.Errorf("the sidecar changed: %q, was %q", after, before)
			}
			// A fresh trail: the replacement would be its first key.
			fresh := auditProject(t)
			evaluate(fresh)
			if _, err := os.Stat(filepath.Join(filepath.Dir(fresh), "audit", audit.SidecarName)); !os.IsNotExist(err) {
				t.Errorf("a fresh trail gets no sidecar from the replacement: %v", err)
			}
			code, output = verification(t, "--config", fresh, "--public-key", replacementPublic)
			if code != 0 || output.Coverage.SignedRecords != 0 || output.Coverage.UnsignedRecords != 1 {
				t.Errorf("a fresh trail's record is unsigned: exit=%d %+v", code, output.Coverage)
			}
			// The key cannot be rotated away from either.
			next, _ := generatedKey(t)
			code, stdout, stderr := runTest(t, []string{"audit", "key", "rotate", "--format", format, "--config", configPath, "--next", next}, "")
			if code != result.ExitInvalid || !strings.Contains(stdout+stderr, audit.ErrKeyNotExpected.Error()) {
				t.Errorf("rotate from a replaced seed: exit=%d %q %q", code, stdout, stderr)
			}
			names("audit key rotate", stdout+stderr)
		})
	}
}

// An expected public key that is not 64 lowercase hexadecimal characters is
// refused before any seed is read (#255): packs validate gives that reason,
// not the seed's, though no seed is there, and names neither; the decision is
// recorded unsigned. JPACK_SIGNING_PUBLIC_KEY is read exactly as set, so
// whitespace, alone or around a key, is such a key too.
func TestAMalformedExpectedKeySignsNothing(t *testing.T) {
	absent := filepath.Join(privateTempDir(t), "absent.seed")
	_, public := generatedKey(t)
	good := strings.TrimSpace(readFile(t, public))
	t.Setenv(audit.SigningKeyEnv, absent)
	for _, malformed := range []string{strings.ToUpper(good), good[:62], "x" + good[1:], "  ", " " + good, good + "\n"} {
		t.Setenv(audit.SigningPublicKeyEnv, malformed)
		configPath, _ := recordedProject(t, 1)
		for _, format := range []string{"human", "json"} {
			code, printed, status, detail := signingKeyDetail(t, configPath, format)
			if code == 0 || status != "failed" || !strings.Contains(detail, audit.ErrExpectedKeyMalformed.Error()) || strings.Contains(detail, "no file is there") {
				t.Fatalf("%s, %s: exit=%d %q %q", malformed, format, code, status, detail)
			}
			if strings.Contains(printed, absent) || (strings.TrimSpace(malformed) != "" && strings.Contains(printed, malformed)) || strings.Contains(printed, good) {
				t.Fatalf("%s, %s: the output names the path or the key: %q", malformed, format, printed)
			}
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(configPath), "audit", audit.SidecarName)); !os.IsNotExist(err) {
			t.Fatalf("nothing signs: %v", err)
		}
	}
}

// A configured signingPublicKey of another form than a public key is refused
// as a key, not as a configuration (#255 review): the decision is made and
// recorded, unsigned, in either output format, packs validate says the
// member's value is not a public key, and nothing printed repeats the value. A
// seed pasted into the member is refused the same way, and not echoed.
func TestAMalformedConfiguredExpectedKeyIsRefusedAsAKey(t *testing.T) {
	skipWhereKeysCannotSign(t)
	seed, public := generatedKey(t)
	t.Setenv(audit.SigningKeyEnv, seed)
	t.Setenv(audit.SigningPublicKeyEnv, "")
	good := strings.TrimSpace(readFile(t, public))
	seedText := strings.TrimSpace(readFile(t, seed))
	facts := writeDocument(t, "facts.json", hardFailFacts)
	for _, row := range []struct{ name, value, reason string }{
		{"the right key in upper case", strings.ToUpper(good), audit.ErrExpectedKeyMalformed.Error()},
		{"65 characters", good + "0", audit.ErrExpectedKeyMalformed.Error()},
		{"the seed itself", seedText, ""},
	} {
		member, err := json.Marshal(row.value)
		if err != nil {
			t.Fatal(err)
		}
		configPath := writeProjectFixture(t, `{"configVersion":"7","audit":{"dir":"audit","signingPublicKey":`+string(member)+`},"packs":{"intake":{"path":"packs/intake-0.1.0.pack.json","expectedVersion":"0.1.0"}}}`,
			map[string]string{"packs/intake-0.1.0.pack.json": evaluatorPack(t)})
		echoes := func(text string) bool {
			lower := strings.ToLower(text)
			return strings.Contains(lower, strings.ToLower(row.value)) || strings.Contains(lower, good) || strings.Contains(lower, seedText) || strings.Contains(text, seed)
		}
		for _, format := range []string{"human", "json"} {
			code, stdout, stderr := runTest(t, []string{"experimental", "evaluate", "--format", format, "--pack-id", "intake", "--config", configPath, "--facts", facts}, "")
			if code != 0 || echoes(stdout+stderr) {
				t.Errorf("%s, %s: the decision is made, and the value not repeated: exit=%d %q %q", row.name, format, code, stdout, stderr)
			}
			code, printed, status, detail := signingKeyDetail(t, configPath, format)
			reasonGiven := strings.Contains(detail, audit.ErrExpectedKeyMalformed.Error()) || strings.Contains(detail, audit.ErrKeyNotExpected.Error())
			if row.reason != "" {
				reasonGiven = strings.Contains(detail, row.reason)
			}
			if code == 0 || status != "failed" || !reasonGiven || !strings.Contains(detail, "the audit member's signingPublicKey names the expected key") || echoes(printed) {
				t.Errorf("%s, %s: validate: exit=%d %q %q", row.name, format, code, status, detail)
			}
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(configPath), "audit", audit.SidecarName)); !os.IsNotExist(err) {
			t.Errorf("%s: nothing is signed: %v", row.name, err)
		}
		code, output := verification(t, "--config", configPath, "--public-key", public)
		if code != 0 || output.Coverage.SignedRecords != 0 || output.Coverage.UnsignedRecords != 2 {
			t.Errorf("%s: both decisions are recorded, unsigned: exit=%d %+v", row.name, code, output.Coverage)
		}
	}
}

// audit key rotate says, in its own help and in what it prints when done,
// that where an expected public key is named it must name the next key too
// (#255 review): naming only the next seed leaves the records unsigned.
func TestAuditKeyRotateSaysToNameTheExpectedKeyToo(t *testing.T) {
	code, stdout, _ := runTest(t, []string{"audit", "key", "rotate", "--help"}, "")
	if code != 0 || !strings.Contains(stdout, "names its public key too, by the audit member's signingPublicKey or JPACK_SIGNING_PUBLIC_KEY") {
		t.Fatalf("rotate --help: exit=%d %q", code, stdout)
	}
	if !keysSignHere(t) {
		return
	}
	seed, _ := generatedKey(t)
	next, _ := generatedKey(t)
	t.Setenv(audit.SigningKeyEnv, seed)
	configPath, _ := recordedProject(t, 1)
	code, stdout, stderr := runTest(t, []string{"audit", "key", "rotate", "--config", configPath, "--next", next}, "")
	if code != 0 || !strings.Contains(stdout, "its public key by signingPublicKey or JPACK_SIGNING_PUBLIC_KEY: until then records are written unsigned.") {
		t.Fatalf("rotate: exit=%d %q %q", code, stdout, stderr)
	}
}
