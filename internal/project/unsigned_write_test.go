package project

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// What a deciding run is told after its record is written is true of what was
// written (ADR-0071 §1): nothing when the key signed it; for a key that was
// not the key in force by the time of the write, a signature that could not
// be written, or a trail no lock could be taken on, that reason, though
// nothing was found before evaluating; and before's reason, unchanged, for a
// writer that does not sign. Under requireSigning the same writer refuses the
// rotated append, with nothing written.
func TestADecisionIsToldWhatItsWriteFound(t *testing.T) {
	t.Setenv(audit.SigningKeyEnv, "")
	t.Setenv(audit.SigningPublicKeyEnv, "")
	if !signsHere(t) {
		t.Skip("no key signs on this platform: its privacy cannot be checked")
	}
	key := keyOutside(t, testSeed)
	signer, err := audit.ReadKey(key)
	if err != nil {
		t.Fatal(err)
	}
	next, err := audit.ReadKey(keyOutside(t, strings.Repeat("cd", 32)))
	if err != nil {
		t.Fatal(err)
	}
	record := audit.Record{Kind: "evaluation"}
	// told appends one record with a deciding writer, after arrange, and
	// returns what the run is told.
	told := func(t *testing.T, loaded *Project, arrange func(*testing.T, *Project) (restore func())) (*Unsigned, error) {
		t.Helper()
		writer, before := loaded.DecisionWriter()
		if before != nil || !writer.Signs() {
			t.Fatalf("nothing is found before evaluating: %+v", before)
		}
		restore := arrange(t, loaded)
		err := writer.Append(record)
		restore()
		return loaded.UnsignedAfter(writer, before), err
	}
	nothing := func(*testing.T, *Project) func() { return func() {} }
	rotated := func(t *testing.T, loaded *Project) func() {
		if _, err := loaded.AuditWriter().Rotate(next); err != nil {
			t.Fatal(err)
		}
		return func() {}
	}
	sidecarFails := func(*testing.T, *Project) func() {
		return audit.StandInSidecarWriteForTests(errors.New("the sidecar's disk is full"))
	}
	noLock := func(*testing.T, *Project) func() { return fssecure.StandInNoLockForTests() }

	signs := unsignedProject(t, key, false)
	if reason, err := told(t, signs, nothing); err != nil || reason != nil {
		t.Fatalf("a signed record: %v %+v", err, reason)
	}
	for name, tc := range map[string]struct {
		arrange func(*testing.T, *Project) func()
		want    string
	}{
		"a rotation since the key was read": {rotated,
			"Key " + signer.KeyID() + " is not the key in force in the signature sidecar, so it signs nothing and records are written unsigned: rotate to it from the key in force with jpack audit key rotate, or name the key in force."},
		"a signature that cannot be written": {sidecarFails,
			"The signature sidecar could not be written, so key " + signer.KeyID() + " signed nothing and the records written are unsigned."},
		"a trail no lock can be taken on": {noLock,
			"No lock can be taken on the audit trail here, so records are written unchained and unsigned."},
	} {
		t.Run(name, func(t *testing.T) {
			loaded := unsignedProject(t, key, false)
			if err := loaded.AuditWriter().Append(record); err != nil {
				t.Fatal(err)
			}
			reason, err := told(t, loaded, tc.arrange)
			if err != nil || reason == nil {
				t.Fatalf("written and told: %v %+v", err, reason)
			}
			if reason.Detail() != tc.want || reason.Diagnostic() != (result.Diagnostic{Code: UnsignedCode, CodeStability: "provisional", Layer: "audit", Severity: "warning", InstancePath: "", Message: tc.want}) {
				t.Fatalf("told:\n got %q\nwant %q", reason.Detail(), tc.want)
			}
		})
	}

	// A writer that does not sign is told what it was told before.
	unsigned := &Unsigned{subject: "before", consequence: "unchanged"}
	plain := unsignedProject(t, "", false)
	writer, _ := plain.DecisionWriter()
	if got := plain.UnsignedAfter(writer, unsigned); got != unsigned {
		t.Fatalf("a writer that does not sign: %+v", got)
	}
	if got := plain.UnsignedAfter(nil, nil); got != nil {
		t.Fatalf("no writer: %+v", got)
	}

	// The writer of a project that sets requireSigning refuses the rotated
	// append and writes nothing.
	required := unsignedProject(t, key, true)
	if err := required.AuditWriter().Append(record); err != nil {
		t.Fatal(err)
	}
	trail := filepath.Join(required.Root, "audit", audit.FileName)
	before, err := os.ReadFile(trail)
	if err != nil {
		t.Fatal(err)
	}
	writer, _ = required.DecisionWriter()
	rotated(t, required)
	if err := writer.Append(record); !errors.Is(err, audit.ErrUnsignedRefused) || !errors.Is(err, audit.ErrKeyNotInForce) {
		t.Fatalf("refused: %v", err)
	}
	if after, err := os.ReadFile(trail); err != nil || string(after) != string(before) {
		t.Fatalf("nothing is written: %v %q", err, after)
	}
}
