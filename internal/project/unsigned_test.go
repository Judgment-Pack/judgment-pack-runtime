package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// The audit member's requireSigning is the "8" shape's one addition (#354):
// refused under any earlier version naming "8", refused beside chain false
// and when it is not a boolean, and an "8" still reads every earlier member.
func TestConfigVersionEightMayRequireSigning(t *testing.T) {
	t.Setenv(audit.SigningKeyEnv, "")
	t.Setenv(audit.SigningPublicKeyEnv, "")
	for _, version := range []string{"3", "4", "5", "6", "7"} {
		_, failure := Load(writeProject(t, `{"configVersion":"`+version+`","audit":{"dir":"audit","requireSigning":true},"packs":{}}`, nil))
		if failure == nil || failure.Code != "JPS-PROJECT-CONFIG-SCHEMA" || !strings.Contains(failure.Message, "'8'") {
			t.Fatalf("requireSigning under %s names the version to change: %+v", version, failure)
		}
	}
	for _, config := range []string{
		`{"configVersion":"8","audit":{"dir":"audit","chain":false,"requireSigning":true},"packs":{}}`,
		`{"configVersion":"8","audit":{"dir":"audit","chain":false,"requireSigning":false},"packs":{}}`,
		`{"configVersion":"8","audit":{"dir":"audit","requireSigning":"true"},"packs":{}}`,
		`{"configVersion":"8","requireSigning":true,"packs":{}}`,
	} {
		if _, failure := Load(writeProject(t, config, nil)); failure == nil || failure.Code != "JPS-PROJECT-CONFIG-SCHEMA" {
			t.Fatalf("%s is refused: %+v", config, failure)
		}
	}
	key := quoted(t, keyOutside(t, testSeed))
	every := mustLoad(t, writeProject(t, `{"configVersion":"8","requireReviewed":false,"requireComparableFacts":false,"audit":{"dir":"audit","chain":true,"signingKey":`+key+`,"signingPublicKey":"`+publicOf(t, testSeed)+`","timestampAuthority":"https://tsa.invalid/","requireSigning":true},"packs":{}}`, nil))
	if !every.RequiresSigning() {
		t.Fatal("an \"8\" reads requireSigning beside every earlier member")
	}
	for config, want := range map[string]bool{
		`{"configVersion":"8","audit":{"dir":"audit","requireSigning":false},"packs":{}}`: false,
		`{"configVersion":"8","audit":{"dir":"audit"},"packs":{}}`:                        false,
		`{"configVersion":"8","packs":{}}`:                                                false,
		`{"configVersion":"7","audit":{"dir":"audit"},"packs":{}}`:                        false,
	} {
		if got := mustLoad(t, writeProject(t, config, nil)).RequiresSigning(); got != want {
			t.Fatalf("%s requires signing: %v", config, got)
		}
	}
	var none *Project
	if none.RequiresSigning() || none.RequireSigning(nil) != nil {
		t.Fatal("no project asks for nothing")
	}
}

// unsignedProject loads a chained project, under configVersion "8" with
// requireSigning as given, whose audit member names keyPath (none when empty).
func unsignedProject(t *testing.T, keyPath string, require bool) *Project {
	t.Helper()
	members := `"dir":"audit"`
	if keyPath != "" {
		members += `,"signingKey":` + quoted(t, keyPath)
	}
	if require {
		members += `,"requireSigning":true`
	}
	return mustLoad(t, writeProject(t, `{"configVersion":"8","audit":{`+members+`},"packs":{}}`, nil))
}

// The reason a deciding run is told its record will be unsigned is packs
// validate's audit-signing-key detail, byte for byte, for each way a named key
// does not sign: where it is (a key others can read, a key through a symbolic
// link), which it is (not the expected key), and whether it is in force (a
// sidecar that cannot be read, another key in force). The writer is the one
// AuditWriter makes. Nothing is reported where nothing is named, where the
// trail is not chained, or where the key signs.
func TestADecisionIsToldWhyItsRecordWillBeUnsigned(t *testing.T) {
	t.Setenv(audit.SigningKeyEnv, "")
	t.Setenv(audit.SigningPublicKeyEnv, "")
	if !signsHere(t) {
		writer, unsigned := unsignedProject(t, keyOutside(t, testSeed), false).DecisionWriter()
		if writer == nil || writer.Signs() || unsigned == nil || !strings.Contains(unsigned.Detail(), audit.ErrKeyPrivacyUnchecked.Error()) {
			t.Fatalf("no key signs here, and the reason says why: %+v", unsigned)
		}
		return
	}
	// the reason, the check's detail, and the warning are one text.
	same := func(t *testing.T, loaded *Project, want string) {
		t.Helper()
		writer, unsigned := loaded.DecisionWriter()
		if writer == nil || unsigned == nil {
			t.Fatalf("a reason is given: %v %+v", writer, unsigned)
		}
		if got := unsigned.Detail(); got != want {
			t.Fatalf("reason:\n got %q\nwant %q", got, want)
		}
		check, status, _ := signingKeyCheck(t, loaded)
		if check.Status != result.PackCheckFailed || status != "invalid" || check.Detail != want {
			t.Fatalf("packs validate gives the same reason: %+v %s", check, status)
		}
		warning := unsigned.Diagnostic()
		if warning != (result.Diagnostic{Code: UnsignedCode, CodeStability: "provisional", Layer: "audit", Severity: "warning", InstancePath: "", Message: want}) {
			t.Fatalf("warning = %+v", warning)
		}
	}
	key := keyOutside(t, testSeed)
	signer, err := audit.ReadKey(key)
	if err != nil {
		t.Fatal(err)
	}

	// Where it is: readable by others.
	if err := os.Chmod(key, 0o644); err != nil {
		t.Fatal(err)
	}
	same(t, unsignedProject(t, key, false), "The signing key the audit member's signingKey names, "+key+", is refused, so records are written unsigned: the signing key can be read or written by its group or by other users.")
	if err := os.Chmod(key, 0o600); err != nil {
		t.Fatal(err)
	}
	// Where it is: through a symbolic link, named by the environment.
	link := filepath.Join(filepath.Dir(key), "link")
	if err := os.Symlink(key, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv(audit.SigningKeyEnv, link)
	same(t, unsignedProject(t, "", false), "The signing key JPACK_SIGNING_KEY names, "+link+", is refused, so records are written unsigned: the signing key's path goes through a symbolic link; name the key by its real path.")
	t.Setenv(audit.SigningKeyEnv, "")

	// Which it is: not the expected key. Neither the path nor either key is
	// named.
	other := publicOf(t, strings.Repeat("ab", 32))
	t.Setenv(audit.SigningPublicKeyEnv, other)
	loaded := unsignedProject(t, key, false)
	same(t, loaded, "The signing key the audit member's signingKey names is refused, so records are written unsigned: the key at the signing key's path is not the expected key (JPACK_SIGNING_PUBLIC_KEY names the expected key).")
	if _, unsigned := loaded.DecisionWriter(); strings.Contains(unsigned.Detail(), key) || strings.Contains(unsigned.Detail(), other) || strings.Contains(unsigned.Detail(), signer.PublicKey()) {
		t.Fatalf("the reason names the path or a key: %q", unsigned.Detail())
	}
	t.Setenv(audit.SigningPublicKeyEnv, "")

	// It signs: no reason, and the writer signs.
	if writer, unsigned := loaded.DecisionWriter(); unsigned != nil || !writer.Signs() {
		t.Fatalf("a key that signs: %+v %v", unsigned, writer.Signs())
	}
	if err := loaded.AuditWriter().Append(audit.Record{Kind: "evaluation"}); err != nil {
		t.Fatal(err)
	}
	// Whether it is in force: another key signed last. The writer is given
	// the key, as AuditWriter gives it, and signs nothing with it.
	otherKey := keyOutside(t, strings.Repeat("cd", 32))
	otherSigner, err := audit.ReadKey(otherKey)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(audit.SigningKeyEnv, otherKey)
	same(t, loaded, "Key "+otherSigner.KeyID()+" is not the key in force in the signature sidecar, so it signs nothing and records are written unsigned: rotate to it from the key in force with jpack audit key rotate, or name the key in force.")
	if writer, _ := loaded.DecisionWriter(); !writer.Signs() {
		t.Fatal("the writer is given the key that loaded, as AuditWriter gives it")
	}
	t.Setenv(audit.SigningKeyEnv, "")
	// Whether it is in force: the sidecar cannot be read.
	unreadable := unsignedProject(t, key, false)
	if err := os.MkdirAll(filepath.Join(unreadable.Root, "audit", audit.SidecarName), 0o700); err != nil {
		t.Fatal(err)
	}
	same(t, unreadable, "The signature sidecar could not be read to see whether key "+signer.KeyID()+" is the key in force, so records may be written unsigned.")

	// Nothing named, nothing chained, nothing declared: no reason.
	if writer, unsigned := unsignedProject(t, "", false).DecisionWriter(); writer == nil || writer.Signs() || unsigned != nil {
		t.Fatalf("no key named: %+v", unsigned)
	}
	t.Setenv(audit.SigningKeyEnv, link)
	unchained := mustLoad(t, writeProject(t, `{"configVersion":"8","audit":{"dir":"audit","chain":false},"packs":{}}`, nil))
	if writer, unsigned := unchained.DecisionWriter(); writer == nil || writer.Signs() || unsigned != nil {
		t.Fatalf("an unchained trail, which no key signs: %+v", unsigned)
	}
	noAudit := mustLoad(t, writeProject(t, `{"configVersion":"8","packs":{}}`, nil))
	if writer, unsigned := noAudit.DecisionWriter(); writer != nil || unsigned != nil {
		t.Fatalf("no audit member: %v %+v", writer, unsigned)
	}
	var none *Project
	if writer, unsigned := none.DecisionWriter(); writer != nil || unsigned != nil {
		t.Fatal("no project")
	}
}

// A project whose audit member sets requireSigning refuses a decision whose
// record would not be signed, with the reason packs validate gives, and every
// decision while no key is named; it passes one whose key signs. The message
// is exact. Without the member nothing is refused, whatever the reason.
func TestRequireSigningRefusesARecordThatWouldNotBeSigned(t *testing.T) {
	t.Setenv(audit.SigningKeyEnv, "")
	t.Setenv(audit.SigningPublicKeyEnv, "")
	refused := func(t *testing.T, loaded *Project, want string) {
		t.Helper()
		_, unsigned := loaded.DecisionWriter()
		failure := loaded.RequireSigning(unsigned)
		if failure == nil || failure.Code != SigningRequiredCode || failure.ExitCode != result.ExitInvalid {
			t.Fatalf("refused: %+v", failure)
		}
		if failure.Message != want {
			t.Fatalf("message:\n got %q\nwant %q", failure.Message, want)
		}
	}
	lead := func(loaded *Project) string {
		return "This evaluation was refused because " + loaded.ConfigPath + " requires every decision's record to be signed, and "
	}
	nothingNamed := unsignedProject(t, "", true)
	refused(t, nothingNamed, lead(nothingNamed)+"no signing key is named. Name one by the audit member's signingKey or by JPACK_SIGNING_KEY, or declare the run a rehearsal.")
	if failure := unsignedProject(t, "", false).RequireSigning(nil); failure != nil {
		t.Fatalf("without the member, no key named is not refused: %+v", failure)
	}
	if !signsHere(t) {
		loaded := unsignedProject(t, keyOutside(t, testSeed), true)
		_, unsigned := loaded.DecisionWriter()
		if failure := loaded.RequireSigning(unsigned); failure == nil || !strings.Contains(failure.Message, audit.ErrKeyPrivacyUnchecked.Error()) {
			t.Fatalf("no key signs here, so every decision is refused: %+v", failure)
		}
		return
	}
	key := keyOutside(t, testSeed)
	signs := unsignedProject(t, key, true)
	if _, unsigned := signs.DecisionWriter(); unsigned != nil || signs.RequireSigning(unsigned) != nil {
		t.Fatalf("a key that signs is not refused: %+v", unsigned)
	}
	if err := os.Chmod(key, 0o644); err != nil {
		t.Fatal(err)
	}
	refused(t, signs, lead(signs)+"the signing key the audit member's signingKey names, "+key+", is refused: the signing key can be read or written by its group or by other users. Make the named key one that signs, or declare the run a rehearsal.")
	if err := os.Chmod(key, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(audit.SigningPublicKeyEnv, publicOf(t, strings.Repeat("ab", 32)))
	refused(t, signs, lead(signs)+"the signing key the audit member's signingKey names is refused: the key at the signing key's path is not the expected key (JPACK_SIGNING_PUBLIC_KEY names the expected key). Make the named key one that signs, or declare the run a rehearsal.")
	// Without the member, the same reason is a warning and nothing more.
	warned := unsignedProject(t, key, false)
	if _, unsigned := warned.DecisionWriter(); unsigned == nil || warned.RequireSigning(unsigned) != nil {
		t.Fatalf("without the member a key that will not sign is not refused: %+v", unsigned)
	}
}
