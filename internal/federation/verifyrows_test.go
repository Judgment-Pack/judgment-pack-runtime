package federation_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/federation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
)

// verifyRow is a verify row's members beyond a lock row's (the rows'
// README, Operations).
type verifyRow struct {
	LockSignatures []string        `json:"lockSignatures"`
	Record         json.RawMessage `json:"record"`
	Floor          *int64          `json:"floor"`
	Concurrent     json.RawMessage `json:"concurrent"`
	WriteFails     bool            `json:"writeFails"`
	ExpectedRecord json.RawMessage `json:"expectedRecord"`
}

// recordPath is where a store in dir keeps a registry id's record: the
// SHA-256 of the id, in hexadecimal, and .json.
func recordPath(dir, registryID string) string {
	sum := sha256.Sum256([]byte(registryID))
	return filepath.Join(dir, hex.EncodeToString(sum[:])+".json")
}

// recordBytes is the record file a row's record is: an object's members with
// the file's version and the registry id, in canonical JSON with a final
// line feed, as the store writes one; any other value as it is.
func recordBytes(t testing.TB, registryID string, raw json.RawMessage) []byte {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	object, isObject := value.(map[string]any)
	if !isObject {
		return append([]byte(nil), raw...)
	}
	object["formatVersion"] = federation.RecordFormatVersion
	object["registryId"] = registryID
	data, err := jcs.EncodeDocument(object)
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}

func isNull(raw json.RawMessage) bool {
	return len(raw) == 0 || string(raw) == "null"
}

// layOutRecord writes a row's record into dir: nothing for null, a directory
// where the record is for "unreadable", and the file otherwise.
func layOutRecord(t testing.TB, dir, registryID string, raw json.RawMessage) {
	t.Helper()
	if isNull(raw) {
		return
	}
	path := recordPath(dir, registryID)
	if string(raw) == `"unreadable"` {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := os.WriteFile(path, recordBytes(t, registryID, raw), 0o600); err != nil {
		t.Fatal(err)
	}
}

// holdsRecord fails unless dir holds the record a row expects once the
// verification ends.
func holdsRecord(t testing.TB, id, dir, registryID string, raw json.RawMessage) {
	t.Helper()
	path := recordPath(dir, registryID)
	info, err := os.Lstat(path)
	switch {
	case isNull(raw):
		if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s: a record is written where the row expects none", id)
		}
	case string(raw) == `"unreadable"`:
		if err != nil || !info.IsDir() {
			t.Errorf("%s: the unreadable record is changed", id)
		}
	default:
		data, err := os.ReadFile(path)
		if want := recordBytes(t, registryID, raw); err != nil || !bytes.Equal(data, want) {
			t.Errorf("%s: record\n got %s (%v)\nwant %s", id, data, err, want)
		}
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, "*.lock"))
	temporaries, _ := filepath.Glob(filepath.Join(dir, "*.tmp"))
	if len(leftovers)+len(temporaries) > 0 {
		t.Errorf("%s: left behind %v %v", id, leftovers, temporaries)
	}
}

// verifyInput is a verify row's input, with the store in dir.
func verifyInput(t testing.TB, r row, dir string) federation.VerifyInput {
	in := federation.VerifyInput{Lock: rowDocument(t, r.Lock), Expect: rowExpect(r), Floor: r.Floor, Records: federation.NewRecordStore(dir)}
	for _, name := range r.LockSignatures {
		in.LockSignatures = append(in.LockSignatures, federation.Held(rowDocument(t, name)))
	}
	for _, name := range r.SourceSignatures {
		in.SourceSignatures = append(in.SourceSignatures, federation.Held(rowDocument(t, name)))
	}
	if r.Signers != nil {
		in.Signers = &federation.Signers{Key: r.Signers.Key, Repositories: r.Signers.Repositories}
	}
	if r.Index != "" {
		index := rowDocument(t, r.Index)
		in.Index = &index
	}
	if r.Registry != "" {
		document := rowDocument(t, r.Registry)
		in.Registry = &document
		for _, name := range r.Kinds {
			in.Kinds = append(in.Kinds, rowDocument(t, name))
		}
	}
	if r.Resolved != "" {
		resolved := rowDocument(t, r.Resolved)
		in.Resolved = &resolved
	}
	return in
}

// verifyHooks stand in a row's concurrent record and its failed write.
func verifyHooks(t testing.TB, r row, dir, registryID string) federation.RecordHooks {
	var hooks federation.RecordHooks
	if !isNull(r.Concurrent) {
		hooks.BeforeRecording = func() {
			if err := os.WriteFile(recordPath(dir, registryID), recordBytes(t, registryID, r.Concurrent), 0o600); err != nil {
				t.Error(err)
			}
		}
	}
	if r.WriteFails {
		hooks.BeforeRename = func() error { return errors.New("the row's record cannot be written") }
	}
	return hooks
}

// runVerifier runs a verify row with its record in a directory of its own,
// and compares the report and the record once the verification ends.
func runVerifier(t *testing.T, r row) {
	dir := t.TempDir()
	registryID := ""
	if r.Expect.RegistryID != nil {
		registryID = *r.Expect.RegistryID
	}
	layOutRecord(t, dir, registryID, r.Record)
	t.Cleanup(federation.StandInRecordHooksForTests(verifyHooks(t, r, dir, registryID)))
	out, err := federation.Verify(verifyInput(t, r, dir))
	if err != nil {
		t.Fatalf("%s: %v", r.ID, err)
	}
	compareReport(t, r, out.Report)
	expected := r.ExpectedRecord
	if !isNull(r.Concurrent) && bytes.Equal(r.ExpectedRecord, r.Concurrent) {
		expected = r.Concurrent
	}
	holdsRecord(t, r.ID, dir, registryID, expected)
	hasState := false
	for _, d := range out.Report.Diagnostics {
		hasState = hasState || d.Code == "LOCK-SERIAL-STATE"
	}
	if hasState != (out.RecordProblem != "") {
		t.Errorf("%s: record problem %q with the report's LOCK-SERIAL-STATE %v", r.ID, out.RecordProblem, hasState)
	}
}

// A verification that finds another's lock file beside the record, after
// the bounded wait, is LOCK-SERIAL-STATE, and says which file; the lock file
// is left as it was and no record is written.
func TestVerifyNamesTheLockFileItFinds(t *testing.T) {
	r := rowByID(t, "verify-first-contact")
	dir := t.TempDir()
	lock := strings.TrimSuffix(recordPath(dir, *r.Expect.RegistryID), ".json") + ".lock"
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(federation.StandInRecordHooksForTests(federation.RecordHooks{LockWait: 20 * time.Millisecond}))
	out, err := federation.Verify(verifyInput(t, r, dir))
	if err != nil {
		t.Fatal(err)
	}
	if out.Report.Valid || len(out.Report.Diagnostics) != 1 || out.Report.Diagnostics[0].Code != "LOCK-SERIAL-STATE" || !strings.Contains(out.RecordProblem, lock) {
		t.Fatalf("%+v %q", out.Report, out.RecordProblem)
	}
	if _, err := os.Lstat(lock); err != nil {
		t.Fatal("the lock file was removed")
	}
	if _, err := os.Lstat(recordPath(dir, *r.Expect.RegistryID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a record was written")
	}
}

// A verification needs a record store: there is no default.
func TestVerifyNeedsARecordStore(t *testing.T) {
	in := verifyInput(t, rowByID(t, "verify-first-contact"), t.TempDir())
	in.Records = nil
	var input *federation.InputError
	if _, err := federation.Verify(in); !errors.As(err, &input) {
		t.Fatalf("%v", err)
	}
}

// A floor not from 1 to 2^53-1 is LOCK-EXPECTED, before anything is read.
func TestVerifyRefusesAFloorOutOfRange(t *testing.T) {
	for _, floor := range []int64{0, -1, federation.MaxSerial + 1} {
		dir := t.TempDir()
		in := verifyInput(t, rowByID(t, "verify-first-contact"), dir)
		in.Floor = &floor
		out, err := federation.Verify(in)
		if err != nil || out.Report.Valid || out.Report.Diagnostics[0].Code != "LOCK-EXPECTED" || len(out.Report.Diagnostics) != 1 {
			t.Fatalf("floor %d: %+v %v", floor, out.Report, err)
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Fatalf("floor %d: wrote %v", floor, entries)
		}
	}
}
