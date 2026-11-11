package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
)

// A key path a reason names is cut to MaxEchoedKeyPathBytes, on a character
// boundary, and ends with an ellipsis when it is longer; a path of that
// length or less is named whole, sanitized as before.
func TestAReasonNamesAtMostMaxEchoedKeyPathBytesOfTheKeyPath(t *testing.T) {
	for keyPath, want := range map[string]string{
		"/keys/seed": "/keys/seed",
		strings.Repeat("a", MaxEchoedKeyPathBytes):         strings.Repeat("a", MaxEchoedKeyPathBytes),
		strings.Repeat("a", MaxEchoedKeyPathBytes+1):       strings.Repeat("a", MaxEchoedKeyPathBytes) + "…",
		strings.Repeat("a", MaxEchoedKeyPathBytes-1) + "é": strings.Repeat("a", MaxEchoedKeyPathBytes-1) + "…",
	} {
		if got := echoedKeyPath(keyPath); got != want {
			t.Fatalf("echoedKeyPath(%d bytes) = %q (%d bytes), want %d bytes", len(keyPath), got, len(got), len(want))
		}
	}
}

// A refused key whose path is longer than MaxEchoedKeyPathBytes is named by
// its first MaxEchoedKeyPathBytes and an ellipsis, in packs validate's
// detail, the warning and the refusal alike, and never whole.
func TestALongKeyPathIsCutInTheReasonAndTheRefusal(t *testing.T) {
	t.Setenv(audit.SigningKeyEnv, "")
	t.Setenv(audit.SigningPublicKeyEnv, "")
	if !signsHere(t) {
		t.Skip("no key signs on this platform: its privacy cannot be checked")
	}
	dir := filepath.Dir(keyOutside(t, testSeed))
	for len(dir) <= MaxEchoedKeyPathBytes {
		dir = filepath.Join(dir, strings.Repeat("d", 200))
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	key := filepath.Join(dir, "seed")
	if err := os.WriteFile(key, []byte(testSeed+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(key, 0o644); err != nil {
		t.Fatal(err)
	}
	named := key[:MaxEchoedKeyPathBytes] + "…"
	want := "The signing key the audit member's signingKey names, " + named + ", is refused, so records are written unsigned: the signing key can be read or written by its group or by other users."
	loaded := unsignedProject(t, key, false)
	_, unsigned := loaded.DecisionWriter()
	if unsigned == nil || unsigned.Detail() != want || unsigned.Diagnostic().Message != want {
		t.Fatalf("reason:\n got %+v\nwant %q", unsigned, want)
	}
	if check, _, _ := signingKeyCheck(t, loaded); check.Detail != want {
		t.Fatalf("packs validate:\n got %q\nwant %q", check.Detail, want)
	}
	required := unsignedProject(t, key, true)
	_, unsigned = required.DecisionWriter()
	failure := required.RequireSigning(unsigned)
	if failure == nil || failure.Message != "This evaluation was refused because "+required.ConfigPath+" requires every decision's record to be signed, and the signing key the audit member's signingKey names, "+named+", is refused: the signing key can be read or written by its group or by other users. Make the named key one that signs, or declare the run a rehearsal." {
		t.Fatalf("refusal: %+v", failure)
	}
	if strings.Contains(want, key) || strings.Contains(failure.Message, key) {
		t.Fatal("the whole path is named")
	}
}
