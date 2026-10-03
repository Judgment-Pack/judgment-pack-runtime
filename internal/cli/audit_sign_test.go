package cli

import (
	"encoding/json"
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
func generatedKey(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
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
		output.Findings[0].Name != audit.FindingSignatureRecordMismatch || output.Findings[1].Name != audit.FindingSignatureMissing {
		t.Fatalf("an edited last record: exit=%d %+v", code, output.Findings)
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

// audit key rotate hands signing to the next key: the old key signs nothing
// more, records are unsigned until the next key is named, and the trail
// verifies under the first key, and under both pinned in order. A revocation
// of the first key from the rotation on changes nothing for the genuine trail.
func TestAuditKeyRotateHandsSigningOver(t *testing.T) {
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
	inside := filepath.Join(filepath.Dir(configPath), "next.seed")
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
	configPath := auditProject(t)
	inside := filepath.Join(filepath.Dir(configPath), "keys", "seed")
	if err := os.MkdirAll(filepath.Dir(inside), 0o700); err != nil {
		t.Fatal(err)
	}
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

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
