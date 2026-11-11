package audit

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
)

// fileBytes is a file's bytes, or nil when nothing is at its name.
func fileBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// requiringWriter is a chaining writer at dir's audit directory, signing with
// signer, that requires signed records when require is set.
func requiringWriter(t *testing.T, root *fssecure.Root, signer *Signer, require bool) *Writer {
	t.Helper()
	writer := NewWriter(root, "audit", true)
	writer.SignWith(signer)
	if require {
		writer.RequireSigned()
	}
	return writer
}

func appendOne(writer *Writer) error {
	return writer.Evaluation(evaluated(), Inputs{Facts: []byte(`{"n":2}`)}, nil, []byte(`{}`), nil)
}

// A writer that requires signed records (ADR-0071 §2) refuses, under the
// trail's lock and before anything is appended, an append its key would not
// sign: a rotation written since the run read its key (a), a trail no lock can
// be taken on (c), and a writer with no key. An append whose signature then
// cannot be written (b) is reported, its record written. Each refusal's
// message is exact and names no key, path or value. The same appends by a
// writer that does not require signed records write the record unsigned, as
// before, and keep why.
func TestAWriterThatRequiresSignedRecordsRefusesWhatItWouldNotSign(t *testing.T) {
	first := signerOf(t, vectorSeed1)
	next := signerOf(t, vectorSeed2)
	type outcome struct {
		err     error
		trail   []byte
		sidecar []byte
		failure error
	}
	// run lays out a trail with one record first signed, applies arrange,
	// and appends one record with a writer signing with first.
	run := func(t *testing.T, require bool, arrange func(t *testing.T, root *fssecure.Root) (restore func())) (outcome, outcome) {
		t.Helper()
		writer, dir := writerAt(t, "audit")
		appendSigned(t, writer.root, first, `{"n":1}`)
		restore := arrange(t, writer.root)
		defer restore()
		trail, sidecar := filepath.Join(dir, "audit", FileName), filepath.Join(dir, "audit", SidecarName)
		before := outcome{trail: fileBytes(t, trail), sidecar: fileBytes(t, sidecar)}
		deciding := requiringWriter(t, writer.root, first, require)
		err := appendOne(deciding)
		return before, outcome{err: err, trail: fileBytes(t, trail), sidecar: fileBytes(t, sidecar), failure: deciding.SigningFailure()}
	}
	rotated := func(t *testing.T, root *fssecure.Root) func() {
		rotating := NewWriter(root, "audit", true)
		rotating.SignWith(first)
		if _, err := rotating.Rotate(next); err != nil {
			t.Fatal(err)
		}
		return func() {}
	}
	sidecarFails := func(*testing.T, *fssecure.Root) func() {
		return StandInSidecarWriteForTests(errors.New("the sidecar's disk is full"))
	}
	noLock := func(*testing.T, *fssecure.Root) func() { return fssecure.StandInNoLockForTests() }

	for name, tc := range map[string]struct {
		arrange      func(*testing.T, *fssecure.Root) func()
		causes       []error
		message      string
		recorded     bool
		keptAsReason error
	}{
		"(a) a rotation since the key was read": {rotated, []error{ErrUnsignedRefused, ErrKeyNotInForce},
			"Audit record could not be written: the project requires every record to be signed, and the signing key is not the key in force in the signature sidecar, so the record would be unsigned.",
			false, ErrKeyNotInForce},
		"(b) a signature that cannot be written": {sidecarFails, []error{ErrWrittenUnsigned},
			"Audit record was written unsigned: the project requires every record to be signed, and the record's signature could not be written to the signature sidecar.",
			true, nil},
		"(c) a trail no lock can be taken on": {noLock, []error{ErrUnsignedRefused, ErrTrailNotLocked},
			"Audit record could not be written: the project requires every record to be signed, and no lock can be taken on the audit trail here, so no record would be chained or signed.",
			false, ErrTrailNotLocked},
	} {
		t.Run(name, func(t *testing.T) {
			before, after := run(t, true, tc.arrange)
			for _, cause := range tc.causes {
				if !errors.Is(after.err, cause) {
					t.Fatalf("err = %v, want %v", after.err, cause)
				}
			}
			if got := FailureMessageFor(after.err); got != tc.message {
				t.Fatalf("message:\n got %q\nwant %q", got, tc.message)
			}
			if !bytes.Equal(after.sidecar, before.sidecar) {
				t.Fatalf("nothing is signed: %q", after.sidecar)
			}
			if tc.recorded {
				if !bytes.HasPrefix(after.trail, before.trail) || bytes.Count(after.trail[len(before.trail):], []byte("\n")) != 1 {
					t.Fatalf("the record is written and stays: %q", after.trail)
				}
			} else if !bytes.Equal(after.trail, before.trail) {
				t.Fatalf("nothing is appended: %q", after.trail)
			}

			// Without the requirement the record is written unsigned, the
			// append reports no failure, and the writer keeps why.
			before, after = run(t, false, tc.arrange)
			if after.err != nil || !bytes.Equal(after.sidecar, before.sidecar) || bytes.Count(after.trail[len(before.trail):], []byte("\n")) != 1 {
				t.Fatalf("written unsigned: err=%v sidecar=%q", after.err, after.sidecar)
			}
			if want := tc.keptAsReason; want != nil && !errors.Is(after.failure, want) || want == nil && (after.failure == nil || after.failure.Error() != "the sidecar's disk is full") {
				t.Fatalf("SigningFailure = %v", after.failure)
			}
		})
	}

	// A writer that requires signed records and holds no key refuses before
	// anything is created.
	writer, dir := writerAt(t, "audit")
	writer.RequireSigned()
	err := appendOne(writer)
	if !errors.Is(err, ErrUnsignedRefused) || !errors.Is(err, ErrNoSigningKey) || FailureMessageFor(err) != "Audit record could not be written: the project requires every record to be signed, and no signing key signs this trail." {
		t.Fatalf("no key: %v %q", err, FailureMessageFor(err))
	}
	if _, err := os.Stat(filepath.Join(dir, "audit")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("nothing is created: %v", err)
	}

	// A signed append keeps no failure, and a trail that was not there,
	// refused for want of a lock, is left as the open made it: empty.
	signedWriter, signedDir := writerAt(t, "audit")
	signing := requiringWriter(t, signedWriter.root, first, true)
	if err := appendOne(signing); err != nil || signing.SigningFailure() != nil {
		t.Fatalf("a signed append: %v %v", err, signing.SigningFailure())
	}
	if lines := bytes.Count(readSidecar(t, signedDir), []byte("\n")); lines != 1 {
		t.Fatalf("and signed: %d", lines)
	}
	fresh, freshDir := writerAt(t, "audit")
	restore := fssecure.StandInNoLockForTests()
	err = appendOne(requiringWriter(t, fresh.root, first, true))
	restore()
	if !errors.Is(err, ErrTrailNotLocked) {
		t.Fatalf("a fresh trail without a lock: %v", err)
	}
	if trail := fileBytes(t, filepath.Join(freshDir, "audit", FileName)); trail == nil || len(trail) != 0 {
		t.Fatalf("the trail the open created stays empty: %q", trail)
	}
}
