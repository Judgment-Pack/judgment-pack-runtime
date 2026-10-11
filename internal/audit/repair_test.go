package audit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// shownDamage is the damaged end a caller checked and showed before asking for
// a repair (#230): eight bytes after the trail's last newline.
const shownDamage = `{"torn":`

func shownTail() DamagedTail {
	return DamagedTail{Bytes: int64(len(shownDamage)), Digest: Digest([]byte(shownDamage))}
}

// auditFiles is every file in a project's audit directory by name, with its
// bytes, so a test can say a refusal changed nothing and created nothing.
func auditFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, "audit"))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(dir, "audit", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[entry.Name()] = string(data)
	}
	return files
}

// lastRecord decodes a trail's last line without its at, the one member two
// repairs of the same bytes by the same run cannot share.
func lastRecord(t *testing.T, data []byte) map[string]any {
	t.Helper()
	lines := splitLines(data)
	var record map[string]any
	if err := json.Unmarshal(lines[len(lines)-1], &record); err != nil {
		t.Fatal(err)
	}
	delete(record, "at")
	return record
}

// A repair bound to the damaged bytes it is about to set aside repairs exactly
// as an unbound one: the same bytes kept in place, the same discontinuity
// record, the same report.
func TestARepairBoundToItsDamageRepairsAsAnUnboundOne(t *testing.T) {
	unbound, unboundDir := writerAt(t, "audit")
	writeRecords(t, unbound.root, `{"n":1}`, `{"n":2}`)
	appendRaw(t, unboundDir, shownDamage)
	before := readTrailFile(t, unboundDir)
	bound, boundDir := writerAt(t, "audit")
	appendRaw(t, boundDir, string(before))
	bound.run = unbound.run

	want, err := unbound.Repair()
	if err != nil {
		t.Fatal(err)
	}
	got, err := bound.RepairExpecting(shownTail())
	if err != nil {
		t.Fatal(err)
	}
	if got != want || got.Bytes != int64(len(shownDamage)) || got.Digest != Digest([]byte(shownDamage)) {
		t.Fatalf("bound %+v, unbound %+v", got, want)
	}
	unboundAfter, boundAfter := readTrailFile(t, unboundDir), readTrailFile(t, boundDir)
	if !bytes.HasPrefix(boundAfter, append(append([]byte{}, before...), '\n')) || len(splitLines(boundAfter)) != len(splitLines(unboundAfter)) ||
		!reflect.DeepEqual(lastRecord(t, boundAfter), lastRecord(t, unboundAfter)) {
		t.Fatalf("bound:\n%s\nunbound:\n%s", boundAfter, unboundAfter)
	}
}

// Where the damaged end is not exactly the bytes the repair was bound to, it
// refuses, names what it found, and changes nothing: no newline, no record, no
// signature, no file. One byte more than was shown, one fewer, other bytes of
// the same length, and a length and digest that are not of one sequence of
// bytes are each refused. The lock is released: a repair bound to what was
// found then goes ahead.
func TestARepairBoundToOtherDamageChangesNothing(t *testing.T) {
	appended := shownDamage + "x"
	for name, test := range map[string]struct {
		damage   string
		expected DamagedTail
	}{
		"a byte appended after the check": {appended, shownTail()},
		"a damaged end that shrank":       {shownDamage[:len(shownDamage)-1], shownTail()},
		"other bytes of the same length":  {`{"evil":`, shownTail()},
		"a length not of the digest":      {appended, DamagedTail{Bytes: int64(len(shownDamage)), Digest: Digest([]byte(appended))}},
	} {
		t.Run(name, func(t *testing.T) {
			signer := signerOf(t, vectorSeed1)
			root, dir := signedTrail(t, signer, 2)
			appendRaw(t, dir, test.damage)
			before := auditFiles(t, dir)
			repairer := NewWriter(root, "audit", true)
			repairer.SignWith(signer)
			_, err := repairer.RepairExpecting(test.expected)
			var changed *TailChangedError
			found := DamagedTail{Bytes: int64(len(test.damage)), Digest: Digest([]byte(test.damage))}
			if !errors.Is(err, ErrRepairTailChanged) || !errors.As(err, &changed) || changed.Expected != test.expected || changed.Found != found {
				t.Fatalf("err = %v (%+v), want what was found: %+v", err, changed, found)
			}
			if after := auditFiles(t, dir); !reflect.DeepEqual(after, before) {
				t.Fatalf("a refused repair changed the audit directory:\nbefore %q\nafter  %q", before, after)
			}
			repaired, err := repairer.RepairExpecting(changed.Found)
			if err != nil || repaired.Bytes != found.Bytes || repaired.Digest != found.Digest {
				t.Fatalf("a repair bound to what was found: %+v %v", repaired, err)
			}
		})
	}
}

// The binding is held under the lock, to the bytes the repair sets aside, not
// to an earlier reading: a byte another writer appends after the caller's
// check, while the repair holds nothing on the trail, is refused.
func TestARepairBoundToItsDamageChecksItUnderTheLock(t *testing.T) {
	writer, dir := writerAt(t, "audit")
	writeRecords(t, writer.root, `{"n":1}`)
	appendRaw(t, dir, shownDamage)
	original := beforeRepairLock
	t.Cleanup(func() { beforeRepairLock = original })
	beforeRepairLock = func() { appendRaw(t, dir, "x") }
	_, err := writer.RepairExpecting(shownTail())
	var changed *TailChangedError
	if !errors.As(err, &changed) || changed.Found.Bytes != int64(len(shownDamage))+1 || changed.Found.Digest != Digest([]byte(shownDamage+"x")) {
		t.Fatalf("err = %v (%+v)", err, changed)
	}
	if lines := splitLines(readTrailFile(t, dir)); len(lines) != 2 || string(lines[1]) != shownDamage+"x" {
		t.Fatalf("a refused repair appends nothing: %q", lines)
	}
}

// Where nothing is damaged at the trail's end any more when the lock is taken,
// because another writer finished the line, the trail was emptied, or it was
// moved aside, a bound repair refuses as one that found no damaged bytes:
// TailChangedError, never ErrNothingToRepair, and nothing is written or
// created; no trail appears at the name of one moved aside. A repair bound to
// nothing refuses as before, and creates nothing either.
func TestARepairBoundToDamageThatIsGoneChangesNothing(t *testing.T) {
	none := DamagedTail{Bytes: 0, Digest: Digest(nil)}
	for name, test := range map[string]struct {
		change  func(t *testing.T, trail string)
		unbound error
		gone    bool
	}{
		"the line finished": {func(t *testing.T, trail string) {
			file, err := os.OpenFile(trail, os.O_WRONLY|os.O_APPEND, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if _, err := file.WriteString("\n"); err != nil {
				t.Fatal(err)
			}
		}, ErrNothingToRepair, false},
		"the trail emptied": {func(t *testing.T, trail string) {
			if err := os.Truncate(trail, 0); err != nil {
				t.Fatal(err)
			}
		}, ErrNothingToRepair, false},
		"the trail moved aside": {func(t *testing.T, trail string) {
			if err := os.Rename(trail, trail+".kept"); err != nil {
				t.Fatal(err)
			}
		}, ErrNoTrail, true},
	} {
		for _, bound := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s, bound %v", name, bound), func(t *testing.T) {
				writer, dir := writerAt(t, "audit")
				writeRecords(t, writer.root, `{"n":1}`)
				appendRaw(t, dir, shownDamage)
				trail := filepath.Join(dir, "audit", FileName)
				var changed map[string]string
				original := beforeRepairLock
				t.Cleanup(func() { beforeRepairLock = original })
				beforeRepairLock = func() {
					test.change(t, trail)
					changed = auditFiles(t, dir)
				}
				var err error
				if bound {
					_, err = writer.RepairExpecting(shownTail())
					var refused *TailChangedError
					if !errors.As(err, &refused) || refused.Found != none || refused.Expected != shownTail() {
						t.Fatalf("err = %v (%+v), want no damaged bytes found", err, refused)
					}
				} else if _, err = writer.Repair(); !errors.Is(err, test.unbound) || errors.Is(err, ErrRepairTailChanged) {
					t.Fatalf("err = %v, want %v", err, test.unbound)
				}
				if after := auditFiles(t, dir); !reflect.DeepEqual(after, changed) {
					t.Fatalf("a refused repair changed or created something:\nbefore %q\nafter  %q", changed, after)
				}
				if _, err := os.Lstat(trail); test.gone && !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("a refused repair created a trail at the name of one moved aside: %v", err)
				}
			})
		}
	}
	t.Run("no trail from the start", func(t *testing.T) {
		writer, dir := writerAt(t, "audit")
		_, err := writer.RepairExpecting(shownTail())
		var refused *TailChangedError
		if !errors.As(err, &refused) || refused.Found != none {
			t.Fatalf("err = %v (%+v)", err, refused)
		}
		if _, err := os.Lstat(filepath.Join(dir, "audit")); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("a refused repair created the audit directory: %v", err)
		}
	})
}

// unreadableDamage is a trail whose damaged end cannot be read: every other
// read succeeds.
type unreadableDamage struct {
	io.ReaderAt
	start, length int64
}

var errUnreadable = errors.New("unreadable")

func (u unreadableDamage) ReadAt(p []byte, offset int64) (int, error) {
	if offset == u.start && int64(len(p)) == u.length {
		return 0, errUnreadable
	}
	return u.ReaderAt.ReadAt(p, offset)
}

// Damaged bytes that cannot be read under the lock are that read's failure,
// never a mismatch: what could not be read now is not therefore other bytes.
func TestUnreadableDamageIsNotOtherDamage(t *testing.T) {
	writer, _ := writerAt(t, "audit")
	data := []byte("{\"n\":1}\n" + shownDamage)
	contents := unreadableDamage{ReaderAt: bytes.NewReader(data), start: int64(len(data) - len(shownDamage)), length: int64(len(shownDamage))}
	if _, _, err := writer.discontinuityAfter(contents, int64(len(data)), &DamagedTail{Bytes: 1, Digest: Digest(nil)}); !errors.Is(err, errUnreadable) || errors.Is(err, ErrRepairTailChanged) {
		t.Fatalf("err = %v", err)
	}
}
