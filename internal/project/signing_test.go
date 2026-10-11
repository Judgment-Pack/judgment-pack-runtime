package project

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// testSeed is the seed of RFC 8032's first Ed25519 test vector.
const testSeed = "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60"

// keyOutside writes a seed into a directory of its own, outside any project,
// readable and writable by its owner alone, and returns its path. The
// directory is its owner's alone too, as a signing key's must be: t.TempDir
// leaves it 0775 under umask 0002. The machine's directories above it, which
// the test does not choose, are held as root's 0755 for the rest of the test.
func keyOutside(t *testing.T, seed string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(audit.StandInKeyAncestorsForTests(dir))
	path := filepath.Join(dir, "seed")
	if err := os.WriteFile(path, []byte(seed+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// signsHere says whether a key can sign on this platform at all: where a
// key's privacy cannot be checked, every key is refused.
func signsHere(t *testing.T) bool {
	t.Helper()
	_, err := audit.LoadSigner(keyOutside(t, testSeed), nil, "")
	return !errors.Is(err, audit.ErrKeyPrivacyUnchecked)
}

func quoted(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

// signingKeyCheck runs packs validate's project checks and returns the
// audit-signing-key check, with whether it was reported.
func signingKeyCheck(t *testing.T, loaded *Project) (result.PackCheck, string, bool) {
	t.Helper()
	output, failure := loaded.Validate(newValidator(t), "", "packs validate")
	if failure != nil {
		t.Fatal(failure.Message)
	}
	for _, check := range output.Checks {
		if check.Name == CheckAuditSigningKey {
			return check, output.Status, true
		}
	}
	return result.PackCheck{}, output.Status, false
}

// The audit member's signingKey is the "6" shape's, like chain: refused under
// an earlier version naming "6", refused with chain false, and refused when it
// is not an absolute path.
func TestConfigVersionSixMayNameASigningKey(t *testing.T) {
	t.Setenv(audit.SigningKeyEnv, "")
	key := quoted(t, keyOutside(t, testSeed))
	for _, version := range []string{"3", "4", "5"} {
		_, failure := Load(writeProject(t, `{"configVersion":"`+version+`","audit":{"dir":"audit","signingKey":`+key+`},"packs":{}}`, nil))
		if failure == nil || failure.Code != "JPS-PROJECT-CONFIG-SCHEMA" || !strings.Contains(failure.Message, "'6'") {
			t.Fatalf("signingKey under %s names the version to change: %+v", version, failure)
		}
	}
	for _, config := range []string{
		`{"configVersion":"6","audit":{"dir":"audit","chain":false,"signingKey":` + key + `},"packs":{}}`,
		`{"configVersion":"6","audit":{"dir":"audit","signingKey":"keys/seed"},"packs":{}}`,
		`{"configVersion":"6","audit":{"dir":"audit","signingKey":7},"packs":{}}`,
	} {
		if _, failure := Load(writeProject(t, config, nil)); failure == nil || failure.Code != "JPS-PROJECT-CONFIG-SCHEMA" {
			t.Fatalf("%s is refused: %+v", config, failure)
		}
	}
	for _, config := range []string{
		`{"configVersion":"6","audit":{"dir":"audit","signingKey":` + key + `},"packs":{}}`,
		`{"configVersion":"6","audit":{"dir":"audit","chain":true,"signingKey":` + key + `},"packs":{}}`,
	} {
		loaded := mustLoad(t, writeProject(t, config, nil))
		if path, source := loaded.SigningKeyPath(); path == "" || source != SigningKeyFromConfiguration {
			t.Fatalf("%s names its key: %q %q", config, path, source)
		}
	}
}

// The environment variable names the key instead of the configuration, and
// signs a chained trail on its own; an unchained trail is signed by neither.
func TestTheEnvironmentNamesTheSigningKeyFirst(t *testing.T) {
	configured, fromEnv := keyOutside(t, testSeed), keyOutside(t, strings.Repeat("ab", 32))
	t.Setenv(audit.SigningKeyEnv, fromEnv)
	loaded := mustLoad(t, writeProject(t, `{"configVersion":"6","audit":{"dir":"audit","signingKey":`+quoted(t, configured)+`},"packs":{}}`, nil))
	if path, source := loaded.SigningKeyPath(); path != fromEnv || source != SigningKeyFromEnvironment {
		t.Fatalf("the environment's key: %q %q", path, source)
	}
	plain := mustLoad(t, writeProject(t, `{"configVersion":"3","audit":{"dir":"audit"},"packs":{}}`, nil))
	if path, _ := plain.SigningKeyPath(); path != fromEnv || plain.AuditWriter().Signs() != signsHere(t) {
		t.Fatalf("the environment alone signs a chained trail: %q", path)
	}
	unchained := mustLoad(t, writeProject(t, `{"configVersion":"6","audit":{"dir":"audit","chain":false},"packs":{}}`, nil))
	if signer, err := unchained.SigningKey(); signer != nil || err != nil || unchained.AuditWriter().Signs() {
		t.Fatal("an unchained trail is not signed")
	}
	check, status, reported := signingKeyCheck(t, unchained)
	if !reported || check.Status != result.PackCheckSkipped || status != "valid" || !strings.Contains(check.Detail, audit.SigningKeyEnv) {
		t.Fatalf("the check on an unchained trail: %+v %s", check, status)
	}
	none := mustLoad(t, writeProject(t, `{"configVersion":"6","packs":{}}`, nil))
	if path, _ := none.SigningKeyPath(); path != "" {
		t.Fatalf("no audit member names no key: %q", path)
	}
	t.Setenv(audit.SigningKeyEnv, "  ")
	if path, source := loaded.SigningKeyPath(); path != configured || source != SigningKeyFromConfiguration {
		t.Fatalf("an empty environment variable names nothing: %q %q", path, source)
	}
}

// packs validate reports whether the named key signs: passed with its keyId,
// failed when it is refused or not the key in force, and absent when no key
// is named. A refused key leaves the writer unsigned rather than failing.
func TestPacksValidateReportsWhetherTheSigningKeySigns(t *testing.T) {
	t.Setenv(audit.SigningKeyEnv, "")
	key := keyOutside(t, testSeed)
	loaded := mustLoad(t, writeProject(t, `{"configVersion":"6","audit":{"dir":"audit","signingKey":`+quoted(t, key)+`},"packs":{}}`, nil))
	check, status, reported := signingKeyCheck(t, loaded)
	if !signsHere(t) {
		if !reported || check.Status != result.PackCheckFailed || status != "invalid" || !strings.Contains(check.Detail, audit.ErrKeyPrivacyUnchecked.Error()) || loaded.AuditWriter().Signs() {
			t.Fatalf("no key signs here, and the check says why: %+v %s", check, status)
		}
		return
	}
	signer, err := audit.ReadKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if !reported || check.Status != result.PackCheckPassed || status != "valid" || !strings.Contains(check.Detail, signer.KeyID()) {
		t.Fatalf("a good key: %+v %s", check, status)
	}
	if strings.Contains(check.Detail, testSeed) {
		t.Fatal("the check carries the key's contents")
	}
	if !loaded.AuditWriter().Signs() {
		t.Fatal("a good key signs")
	}
	// A key in a directory its group or others can write is refused, the
	// check naming the directory and the fix, and signs nothing (#221).
	keyDir := filepath.Dir(key)
	for _, mode := range []os.FileMode{0o777, 0o775} {
		if err := os.Chmod(keyDir, mode); err != nil {
			t.Fatal(err)
		}
		check, status, _ = signingKeyCheck(t, loaded)
		if check.Status != result.PackCheckFailed || status != "invalid" || !strings.Contains(check.Detail, "The signing key the audit member's signingKey names") ||
			!strings.Contains(check.Detail, "the directory "+keyDir+" on the signing key's path") || !strings.Contains(check.Detail, "chmod go-w "+keyDir+" fixes it") {
			t.Fatalf("a key in a directory of mode %o: %+v %s", mode, check, status)
		}
		if loaded.AuditWriter().Signs() {
			t.Fatalf("a key in a directory of mode %o signs", mode)
		}
	}
	if err := os.Chmod(keyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// A key inside the project is refused, and the writer then signs nothing.
	configPath := writeProject(t, `{"configVersion":"6","audit":{"dir":"audit"},"packs":{}}`, map[string]string{"keys/seed": testSeed + "\n"})
	realDir, err := filepath.EvalSymlinks(filepath.Dir(configPath))
	if err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(realDir, "keys", "seed")
	if err := os.WriteFile(configPath, []byte(`{"configVersion":"6","audit":{"dir":"audit","signingKey":`+quoted(t, inside)+`},"packs":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	insideProject := mustLoad(t, configPath)
	check, status, _ = signingKeyCheck(t, insideProject)
	if check.Status != result.PackCheckFailed || status != "invalid" || !strings.Contains(check.Detail, audit.ErrKeyInsideProject.Error()) || strings.Contains(check.Detail, testSeed) {
		t.Fatalf("a key inside the project: %+v %s", check, status)
	}
	if insideProject.AuditWriter().Signs() {
		t.Fatal("a refused key signs nothing")
	}
	// A key that is not the key in force: the sidecar's last line is another
	// key's signature.
	writer := loaded.AuditWriter()
	if err := writer.Append(audit.Record{Kind: "evaluation"}); err != nil {
		t.Fatal(err)
	}
	other := keyOutside(t, strings.Repeat("cd", 32))
	t.Setenv(audit.SigningKeyEnv, other)
	check, status, _ = signingKeyCheck(t, loaded)
	if check.Status != result.PackCheckFailed || status != "invalid" || !strings.Contains(check.Detail, "not the key in force") {
		t.Fatalf("a key not in force: %+v %s", check, status)
	}
	t.Setenv(audit.SigningKeyEnv, "")
	if err := os.Chmod(key, 0o644); err != nil {
		t.Fatal(err)
	}
	check, status, _ = signingKeyCheck(t, loaded)
	if check.Status != result.PackCheckFailed || status != "invalid" || !strings.Contains(check.Detail, audit.ErrKeyTooOpen.Error()) {
		t.Fatalf("a key others can read: %+v %s", check, status)
	}
	// No key named: no check.
	plain := mustLoad(t, writeProject(t, `{"configVersion":"6","audit":{"dir":"audit"},"packs":{}}`, nil))
	if _, _, reported := signingKeyCheck(t, plain); reported {
		t.Fatal("no key named, no check")
	}
}

// mustDecode decodes hexadecimal.
func mustDecode(t *testing.T, text string) []byte {
	t.Helper()
	raw, err := hex.DecodeString(text)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// publicOf is the public key of a seed given in hexadecimal.
func publicOf(t *testing.T, seed string) string {
	t.Helper()
	signer, err := audit.NewSignerFromSeed(mustDecode(t, seed))
	if err != nil {
		t.Fatal(err)
	}
	return signer.PublicKey()
}

// The audit member's signingPublicKey is the "7" shape's one addition (#255):
// refused under any earlier version naming "7", refused with chain false,
// held to 64 lowercase hexadecimal characters, and a "7" still reads every
// earlier member.
func TestConfigVersionSevenMayNameTheExpectedSigningKey(t *testing.T) {
	t.Setenv(audit.SigningPublicKeyEnv, "")
	expected := publicOf(t, testSeed)
	for _, version := range []string{"3", "4", "5", "6"} {
		_, failure := Load(writeProject(t, `{"configVersion":"`+version+`","audit":{"dir":"audit","signingPublicKey":"`+expected+`"},"packs":{}}`, nil))
		if failure == nil || failure.Code != "JPS-PROJECT-CONFIG-SCHEMA" || !strings.Contains(failure.Message, "'7'") {
			t.Fatalf("signingPublicKey under %s names the version to change: %+v", version, failure)
		}
	}
	for _, config := range []string{
		`{"configVersion":"7","audit":{"dir":"audit","chain":false,"signingPublicKey":"` + expected + `"},"packs":{}}`,
		`{"configVersion":"7","audit":{"dir":"audit","signingPublicKey":"` + strings.ToUpper(expected) + `"},"packs":{}}`,
		`{"configVersion":"7","audit":{"dir":"audit","signingPublicKey":"` + expected[:62] + `"},"packs":{}}`,
		`{"configVersion":"7","audit":{"dir":"audit","signingPublicKey":"` + expected + `\n"},"packs":{}}`,
		`{"configVersion":"7","audit":{"dir":"audit","signingPublicKey":""},"packs":{}}`,
		`{"configVersion":"7","audit":{"dir":"audit","signingPublicKey":7},"packs":{}}`,
	} {
		if _, failure := Load(writeProject(t, config, nil)); failure == nil || failure.Code != "JPS-PROJECT-CONFIG-SCHEMA" {
			t.Fatalf("%s is refused: %+v", config, failure)
		}
	}
	key := quoted(t, keyOutside(t, testSeed))
	loaded := mustLoad(t, writeProject(t, `{"configVersion":"7","requireReviewed":false,"requireComparableFacts":false,"audit":{"dir":"audit","chain":true,"signingKey":`+key+`,"signingPublicKey":"`+expected+`"},"packs":{}}`, nil))
	if named, source := loaded.ExpectedSigningKey(); named != expected || source != SigningKeyFromConfiguration {
		t.Fatalf("the member names the expected key: %q %q", named, source)
	}
	plain := mustLoad(t, writeProject(t, `{"configVersion":"6","audit":{"dir":"audit","signingKey":`+key+`},"packs":{}}`, nil))
	if named, _ := plain.ExpectedSigningKey(); named != "" {
		t.Fatalf("a \"6\" names no expected key: %q", named)
	}
}

// The environment variable names the expected key instead of the member, as
// JPACK_SIGNING_KEY names the seed; an empty one falls back to the member.
// Whichever names it, a seed that is not that key signs nothing, packs
// validate says so and which input named the expected key, and the detail
// names neither the seed's path nor either key.
func TestTheEnvironmentNamesTheExpectedSigningKeyFirst(t *testing.T) {
	t.Setenv(audit.SigningKeyEnv, "")
	seedPath := keyOutside(t, testSeed)
	expected, other := publicOf(t, testSeed), publicOf(t, strings.Repeat("ab", 32))
	loaded := mustLoad(t, writeProject(t, `{"configVersion":"7","audit":{"dir":"audit","signingKey":`+quoted(t, seedPath)+`,"signingPublicKey":"`+expected+`"},"packs":{}}`, nil))
	t.Setenv(audit.SigningPublicKeyEnv, other)
	if named, source := loaded.ExpectedSigningKey(); named != other || source != SigningKeyFromEnvironment {
		t.Fatalf("the environment's expected key: %q %q", named, source)
	}
	check, status, _ := signingKeyCheck(t, loaded)
	if signsHere(t) {
		if _, err := loaded.SigningKey(); !errors.Is(err, audit.ErrKeyNotExpected) || loaded.AuditWriter().Signs() {
			t.Fatalf("the environment's expected key is not the seed's: %v", err)
		}
		if check.Status != result.PackCheckFailed || status != "invalid" || !strings.Contains(check.Detail, audit.ErrKeyNotExpected.Error()) || !strings.Contains(check.Detail, audit.SigningPublicKeyEnv+" names the expected key") {
			t.Fatalf("the check: %+v %s", check, status)
		}
		for _, named := range []string{seedPath, filepath.Dir(seedPath), expected, other, testSeed, audit.KeyID(mustDecode(t, expected)), audit.KeyID(mustDecode(t, other))} {
			if strings.Contains(check.Detail, named) {
				t.Fatalf("the detail names %q: %q", named, check.Detail)
			}
		}
	}
	for _, empty := range []string{"", "  "} {
		t.Setenv(audit.SigningPublicKeyEnv, empty)
		if named, source := loaded.ExpectedSigningKey(); named != expected || source != SigningKeyFromConfiguration {
			t.Fatalf("an empty environment variable %q names nothing: %q %q", empty, named, source)
		}
		check, status, _ := signingKeyCheck(t, loaded)
		if !signsHere(t) {
			continue
		}
		if !loaded.AuditWriter().Signs() || check.Status != result.PackCheckPassed || status != "valid" || !strings.Contains(check.Detail, "it is the expected key the audit member's signingPublicKey names") {
			t.Fatalf("the member's expected key is the seed's: %+v %s", check, status)
		}
	}
	// The member names another key: refused, and the detail says the member
	// named it.
	mismatched := mustLoad(t, writeProject(t, `{"configVersion":"7","audit":{"dir":"audit","signingKey":`+quoted(t, seedPath)+`,"signingPublicKey":"`+other+`"},"packs":{}}`, nil))
	if check, _, _ := signingKeyCheck(t, mismatched); signsHere(t) && (check.Status != result.PackCheckFailed || !strings.Contains(check.Detail, "the audit member's signingPublicKey names the expected key")) {
		t.Fatalf("the member's expected key is another: %+v", check)
	}
	// A malformed expected key is refused whatever the seed, before it is
	// read, here too.
	t.Setenv(audit.SigningPublicKeyEnv, strings.ToUpper(expected))
	if _, err := loaded.SigningKey(); !errors.Is(err, audit.ErrExpectedKeyMalformed) {
		t.Fatalf("a malformed expected key: %v", err)
	}
	if check, _, _ := signingKeyCheck(t, loaded); check.Status != result.PackCheckFailed || !strings.Contains(check.Detail, audit.ErrExpectedKeyMalformed.Error()) || strings.Contains(check.Detail, seedPath) {
		t.Fatalf("the check on a malformed expected key: %+v", check)
	}
	// The next key of a rotation is not held to the expected key.
	t.Setenv(audit.SigningPublicKeyEnv, "")
	if next, err := loaded.LoadKey(keyOutside(t, strings.Repeat("cd", 32))); signsHere(t) && (err != nil || next == nil) {
		t.Fatalf("a next key is loaded without the expected key: %v", err)
	}
}
