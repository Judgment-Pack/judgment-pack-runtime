package federation

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const recordID = "https://example.invalid/registries/platform"

var (
	digestA = "sha256:" + strings.Repeat("a", 64)
	digestB = "sha256:" + strings.Repeat("b", 64)
)

func one() *int64 { floor := int64(1); return &floor }

func writeRecord(t *testing.T, store *RecordStore, record Record) {
	t.Helper()
	data, err := encodeRecord(recordID, record)
	if err != nil {
		t.Fatal(err)
	}
	path, _, _ := store.paths(recordID)
	if err := os.MkdirAll(store.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func holds(t *testing.T, store *RecordStore, want *Record) {
	t.Helper()
	got, problem := store.read(recordID)
	if problem != nil || (got == nil) != (want == nil) || (got != nil && *got != *want) {
		t.Fatalf("record %+v (%v), want %+v", got, problem, want)
	}
	_, lock, temporary := store.paths(recordID)
	for _, left := range []string{lock, temporary} {
		if _, err := os.Lstat(left); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s is left behind", filepath.Base(left))
		}
	}
}

// A record is written over the last by a rename, on every system: the record
// holds the new serial and digest, and no lock file or temporary file is
// left.
func TestRecordRenamesOverTheLast(t *testing.T) {
	store := NewRecordStore(filepath.Join(t.TempDir(), "records"))
	if code, problem := store.record(recordID, nil, 1, digestA, one()); code != "" || problem != nil {
		t.Fatalf("%s %v", code, problem)
	}
	holds(t, store, &Record{1, digestA})
	first := &Record{1, digestA}
	if code, problem := store.record(recordID, first, 2, digestB, nil); code != "" || problem != nil {
		t.Fatalf("%s %v", code, problem)
	}
	holds(t, store, &Record{2, digestB})
}

// Two verifiers that accept two locks of one serial at once, each having
// read no record: exactly one is recorded, and the other is
// LOCK-SERIAL-CONFLICT once it reads the record again. Two of one lock are
// both accepted, and it is recorded once.
func TestRecordTwoWritersOfOneSerial(t *testing.T) {
	for round := 0; round < 20; round++ {
		store := NewRecordStore(filepath.Join(t.TempDir(), "records"))
		digests := []string{digestA, digestB}
		codes := make([]string, 2)
		problems := make([]*RecordError, 2)
		var start, done sync.WaitGroup
		start.Add(1)
		for i := range digests {
			done.Add(1)
			go func() {
				defer done.Done()
				start.Wait()
				codes[i], problems[i] = store.record(recordID, nil, 5, digests[i], one())
			}()
		}
		start.Done()
		done.Wait()
		if problems[0] != nil || problems[1] != nil {
			t.Fatalf("round %d: %v %v", round, problems[0], problems[1])
		}
		winner := -1
		for i, code := range codes {
			switch code {
			case "":
				if winner >= 0 {
					t.Fatalf("round %d: both locks of serial 5 recorded", round)
				}
				winner = i
			case "LOCK-SERIAL-CONFLICT":
			default:
				t.Fatalf("round %d: %s", round, code)
			}
		}
		if winner < 0 {
			t.Fatalf("round %d: neither recorded", round)
		}
		holds(t, store, &Record{5, digests[winner]})

		same := NewRecordStore(filepath.Join(t.TempDir(), "records"))
		var both sync.WaitGroup
		for range 2 {
			both.Add(1)
			go func() {
				defer both.Done()
				if code, problem := same.record(recordID, nil, 5, digestA, one()); code != "" || problem != nil {
					t.Errorf("round %d: one lock twice: %s %v", round, code, problem)
				}
			}()
		}
		both.Wait()
		holds(t, same, &Record{5, digestA})
	}
}

// A lock file left behind is never removed: after the bounded wait the
// write is refused, naming the lock file, and the record is unchanged.
func TestRecordNeverStealsALockFile(t *testing.T) {
	store := NewRecordStore(filepath.Join(t.TempDir(), "records"))
	writeRecord(t, store, Record{3, digestA})
	_, lock, _ := store.paths(recordID)
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(StandInRecordHooksForTests(RecordHooks{LockWait: 50 * time.Millisecond}))
	began := time.Now()
	code, problem := store.record(recordID, &Record{3, digestA}, 4, digestB, nil)
	if code != "" || problem == nil || problem.Path != lock || !strings.Contains(problem.Error(), lock) || !strings.Contains(problem.Reason, "never removed") {
		t.Fatalf("%s %+v", code, problem)
	}
	if time.Since(began) < 50*time.Millisecond {
		t.Fatal("no wait for the lock file")
	}
	if _, err := os.Lstat(lock); err != nil {
		t.Fatal("the lock file was removed")
	}
	if got, _ := store.read(recordID); got == nil || *got != (Record{3, digestA}) {
		t.Fatalf("record %+v", got)
	}
}

// A write that stops between the temporary file and its rename leaves the
// last record, and the next verification writes its own. So does a
// temporary file a stopped write left behind.
func TestRecordInterruptedWriteLeavesTheLast(t *testing.T) {
	store := NewRecordStore(filepath.Join(t.TempDir(), "records"))
	writeRecord(t, store, Record{3, digestA})
	restore := StandInRecordHooksForTests(RecordHooks{BeforeRename: func() error { return errors.New("stopped") }})
	code, problem := store.record(recordID, &Record{3, digestA}, 4, digestB, nil)
	restore()
	if code != "" || problem == nil {
		t.Fatalf("%s %v", code, problem)
	}
	holds(t, store, &Record{3, digestA})
	_, _, temporary := store.paths(recordID)
	if err := os.WriteFile(temporary, []byte(`{"serial":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, problem := store.record(recordID, &Record{3, digestA}, 4, digestB, nil); code != "" || problem != nil {
		t.Fatalf("%s %v", code, problem)
	}
	holds(t, store, &Record{4, digestB})
}

// A record not read back as it was written is refused.
func TestRecordReadBack(t *testing.T) {
	store := NewRecordStore(filepath.Join(t.TempDir(), "records"))
	t.Cleanup(StandInRecordHooksForTests(RecordHooks{AfterRename: func(path string) {
		data, _ := encodeRecord(recordID, Record{9, digestB})
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Error(err)
		}
	}}))
	code, problem := store.record(recordID, nil, 4, digestA, one())
	if code != "" || problem == nil || !strings.Contains(problem.Reason, "read back") {
		t.Fatalf("%s %v", code, problem)
	}
}

// Under the lock the record is read again, and the lock held to the rule
// once more: a record another verifier wrote since refuses a lower serial,
// and a record that step 1 read and that is gone is refused, never taken
// for a first contact.
func TestRecordComparesAndSets(t *testing.T) {
	store := NewRecordStore(filepath.Join(t.TempDir(), "records"))
	writeRecord(t, store, Record{8, digestB})
	if code, problem := store.record(recordID, &Record{3, digestA}, 4, digestA, nil); code != "LOCK-SERIAL-ROLLBACK" || problem != nil {
		t.Fatalf("%s %v", code, problem)
	}
	holds(t, store, &Record{8, digestB})
	path, _, _ := store.paths(recordID)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if code, problem := store.record(recordID, &Record{8, digestB}, 9, digestA, nil); code != "" || problem == nil || !strings.Contains(problem.Reason, "gone") {
		t.Fatalf("%s %v", code, problem)
	}
	holds(t, store, nil)
}

// A lock already recorded, the same serial and digest, is accepted and its
// record is not written again: no temporary file is renamed over it, and the
// file is the one it was, its bytes and modification time as they were.
func TestRecordOfALockAlreadyRecordedIsNotWritten(t *testing.T) {
	store := NewRecordStore(filepath.Join(t.TempDir(), "records"))
	writeRecord(t, store, Record{3, digestA})
	path, _, _ := store.paths(recordID)
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	before, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	renamed := false
	t.Cleanup(StandInRecordHooksForTests(RecordHooks{BeforeRename: func() error {
		renamed = true
		return nil
	}}))
	if code, problem := store.record(recordID, &Record{3, digestA}, 3, digestA, nil); code != "" || problem != nil {
		t.Fatalf("%s %v", code, problem)
	}
	if renamed {
		t.Fatal("the record was written again")
	}
	holds(t, store, &Record{3, digestA})
	after, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	again, err := os.ReadFile(path)
	if err != nil || !os.SameFile(before, after) || !after.ModTime().Equal(before.ModTime()) || string(again) != string(data) {
		t.Fatalf("the record changed: %v %v %v", before.ModTime(), after.ModTime(), err)
	}
}

// A record file is exactly its four members: anything else is refused.
func TestDecodeRecord(t *testing.T) {
	good := `{"digest":"` + digestA + `","formatVersion":"1.0.0","registryId":"` + recordID + `","serial":42}` + "\n"
	if record, reason := decodeRecord([]byte(good), recordID); record == nil || *record != (Record{42, digestA}) {
		t.Fatalf("%v %s", record, reason)
	}
	for name, data := range map[string]string{
		"another id":         strings.Replace(good, "platform", "billing", 1),
		"another version":    strings.Replace(good, `"1.0.0"`, `"1.1.0"`, 1),
		"serial zero":        strings.Replace(good, `42`, `0`, 1),
		"serial over 2^53-1": strings.Replace(good, `42`, `9007199254740992`, 1),
		"serial exponent":    strings.Replace(good, `42`, `4.2e1`, 1),
		"serial string":      strings.Replace(good, `42`, `"42"`, 1),
		"serial negative":    strings.Replace(good, `42`, `-42`, 1),
		"serial leading 0":   strings.Replace(good, `42`, `042`, 1),
		"digest uppercase":   strings.Replace(good, digestA, strings.ToUpper(digestA), 1),
		"a member more":      strings.Replace(good, `{`, `{"x":1,`, 1),
		"a member less":      strings.Replace(good, `"formatVersion":"1.0.0",`, ``, 1),
		"a member twice":     strings.Replace(good, `{`, `{"serial":41,`, 1),
		"an array":           "[" + good + "]",
		"empty":              "",
	} {
		if record, _ := decodeRecord([]byte(data), recordID); record != nil {
			t.Errorf("%s: read as %+v", name, record)
		}
	}
	if record, _ := decodeRecord([]byte(strings.Replace(good, `42`, `9007199254740991`, 1)), recordID); record == nil || record.Serial != MaxSerial {
		t.Error("the highest serial is refused")
	}
}

// The rule of §11, Rollback.
func TestSerialRule(t *testing.T) {
	record := &Record{40, digestA}
	floor := func(n int64) *int64 { return &n }
	for _, c := range []struct {
		serial int64
		digest string
		record *Record
		floor  *int64
		want   string
	}{
		{39, digestA, record, nil, "LOCK-SERIAL-ROLLBACK"},
		{40, digestA, record, nil, ""},
		{40, digestB, record, nil, "LOCK-SERIAL-CONFLICT"},
		{41, digestB, record, nil, ""},
		{39, digestA, record, floor(30), "LOCK-SERIAL-ROLLBACK"},
		{40, digestB, record, floor(40), "LOCK-SERIAL-CONFLICT"},
		{42, digestB, record, floor(43), "LOCK-SERIAL-ROLLBACK"},
		{43, digestB, record, floor(43), ""},
		{39, digestB, record, floor(39), "LOCK-SERIAL-ROLLBACK"},
		{1, digestA, nil, floor(1), ""},
		{41, digestA, nil, floor(42), "LOCK-SERIAL-ROLLBACK"},
		{42, digestA, nil, floor(42), ""},
	} {
		if got := serialRule(c.serial, c.digest, c.record, c.floor); got != c.want {
			t.Errorf("serial %d under %+v and floor %v: %q, want %q", c.serial, c.record, c.floor, got, c.want)
		}
	}
}
