package federation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
)

// The serial record (§11, Rollback) is the verifier's own state: for each
// registry id it has accepted a lock of, the serial and the lock digest of
// the last one. A RecordStore keeps each in one file of a directory, named by
// the SHA-256 of the registry id, so no id is ever spelled as a path:
//
//	{"digest":"sha256:<64 hex>","formatVersion":"1.0.0","registryId":"<id>","serial":<n>}
//
// in canonical JSON with one final line feed. formatVersion is the record
// file's own version, not the text's. The directory is created 0700 and each
// file 0600; on unix a directory or a record that its group or other users
// can write is refused, as is a record that is not one regular file, or a
// directory that is a link.
//
// A record is written by compare and set: a lock file created beside the
// record with O_EXCL is held while the record is read again, the lock is held
// to the rule once more against what is read, and a temporary file is
// written, flushed, renamed over the record, the directory flushed, and the
// record read back. A lock file found is waited on for a bounded time and is
// never removed by a verifier: one that a stopped verifier left behind is
// LOCK-SERIAL-STATE, naming the lock file, until its operator removes it.

// RecordFormatVersion is the record file's own version.
const RecordFormatVersion = "1.0.0"

// MaxRecordBytes bounds a record file: an absolute URI and three short
// members.
const MaxRecordBytes = 64 * 1024

// recordLockWait is how long a verifier waits for another's lock file.
const recordLockWait = 5 * time.Second

// Record is one registry id's record: the serial and digest of the last lock
// accepted.
type Record struct {
	Serial int64
	Digest string
}

// RecordStore keeps the serial records in one local directory.
type RecordStore struct {
	dir string
}

// NewRecordStore is the store in dir. Nothing is read or created until a
// verification reads or writes a record: a directory that does not exist
// holds no record, and is created 0700 when the first is written.
func NewRecordStore(dir string) *RecordStore {
	return &RecordStore{dir: filepath.Clean(dir)}
}

// RecordError is a record that could not be read, written or read back: the
// file, and what failed. A verification reports it as LOCK-SERIAL-STATE.
type RecordError struct {
	Path   string
	Reason string
}

func (e *RecordError) Error() string { return "serial record " + e.Path + ": " + e.Reason }

// RecordHooks stand in for what a test cannot otherwise arrange: another
// verifier writing between a verification's first read and its compare and
// set, a write that fails between the temporary file and its rename, a
// record that is not read back as written, and the lock file's wait.
type RecordHooks struct {
	BeforeRecording func()
	BeforeRename    func() error
	AfterRename     func(path string)
	LockWait        time.Duration
}

var (
	recordHooksMu sync.Mutex
	recordHooks   RecordHooks
)

// StandInRecordHooksForTests sets the hooks until restore is called. It is
// for tests only; nothing in this runtime calls it.
func StandInRecordHooksForTests(hooks RecordHooks) (restore func()) {
	recordHooksMu.Lock()
	previous := recordHooks
	recordHooks = hooks
	recordHooksMu.Unlock()
	return func() {
		recordHooksMu.Lock()
		recordHooks = previous
		recordHooksMu.Unlock()
	}
}

func currentRecordHooks() RecordHooks {
	recordHooksMu.Lock()
	defer recordHooksMu.Unlock()
	return recordHooks
}

// paths are a registry id's record, lock file and temporary file.
func (s *RecordStore) paths(registryID string) (record, lock, temporary string) {
	sum := sha256.Sum256([]byte(registryID))
	base := filepath.Join(s.dir, hex.EncodeToString(sum[:]))
	return base + ".json", base + ".lock", base + ".tmp"
}

// directory holds the store's directory to its rules. A directory that does
// not exist is reported as such (exists false), not refused.
func (s *RecordStore) directory() (exists bool, problem *RecordError) {
	info, err := os.Lstat(s.dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	case err != nil:
		return false, &RecordError{Path: s.dir, Reason: "the record directory could not be read"}
	case info.Mode()&fs.ModeSymlink != 0 || !info.IsDir():
		return false, &RecordError{Path: s.dir, Reason: "the record directory is not a directory, or is a link"}
	case recordModesChecked && info.Mode().Perm()&0o022 != 0:
		return false, &RecordError{Path: s.dir, Reason: "the record directory can be written by its group or by other users"}
	}
	return true, nil
}

// Read is the record for a registry id, or nil when there is none. A record
// that exists and cannot be read, or is not of its form, is an error.
func (s *RecordStore) Read(registryID string) (*Record, error) {
	record, problem := s.read(registryID)
	if problem != nil {
		return nil, problem
	}
	return record, nil
}

func (s *RecordStore) read(registryID string) (*Record, *RecordError) {
	exists, problem := s.directory()
	if problem != nil || !exists {
		return nil, problem
	}
	path, _, _ := s.paths(registryID)
	refused := func(reason string) (*Record, *RecordError) {
		return nil, &RecordError{Path: path, Reason: reason}
	}
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil
	case err != nil:
		return refused("the record could not be read")
	case !info.Mode().IsRegular():
		return refused("the record is not one regular file")
	case recordModesChecked && info.Mode().Perm()&0o022 != 0:
		return refused("the record can be written by its group or by other users")
	}
	file, err := os.Open(path)
	if err != nil {
		return refused("the record could not be read")
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return refused("the record changed while it was opened")
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxRecordBytes+1))
	if err != nil {
		return refused("the record could not be read")
	}
	if len(data) > MaxRecordBytes {
		return refused("the record is over its bound")
	}
	record, reason := decodeRecord(data, registryID)
	if record == nil {
		return refused(reason)
	}
	return record, nil
}

var recordSerial = regexp.MustCompile(`^[1-9][0-9]{0,15}$`)

// decodeRecord reads a record file's bytes for a registry id: exactly its
// four members, its version, the id, a serial from 1 to 2^53-1 written as a
// plain integer, and a digest of its form. Anything else is a reason.
func decodeRecord(data []byte, registryID string) (*Record, string) {
	value, failure := carrier.Decode(data, carrier.Limits{MaxDepth: 4, MaxNodes: 16, MaxStringBytes: MaxRecordBytes})
	root, isObject := value.(map[string]any)
	if failure != nil || !isObject {
		return nil, "the record is not one JSON object"
	}
	if !exactly(root, "digest", "formatVersion", "registryId", "serial") {
		return nil, "the record has other members than its four"
	}
	if root["formatVersion"] != RecordFormatVersion {
		return nil, "the record is of another format version"
	}
	if root["registryId"] != registryID {
		return nil, "the record is another registry id's"
	}
	number, isNumber := root["serial"].(json.Number)
	if !isNumber || !recordSerial.MatchString(string(number)) {
		return nil, "the record's serial is not an integer from 1 to 2^53-1"
	}
	serial, err := strconv.ParseInt(string(number), 10, 64)
	if err != nil || serial > MaxSerial {
		return nil, "the record's serial is not an integer from 1 to 2^53-1"
	}
	if !isDigest(root["digest"]) {
		return nil, "the record's digest is not of its form"
	}
	return &Record{Serial: serial, Digest: root["digest"].(string)}, ""
}

// encodeRecord is a record file's bytes.
func encodeRecord(registryID string, record Record) ([]byte, error) {
	data, err := jcs.EncodeDocument(map[string]any{
		"digest":        record.Digest,
		"formatVersion": RecordFormatVersion,
		"registryId":    registryID,
		"serial":        json.Number(strconv.FormatInt(record.Serial, 10)),
	})
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// serialRule is §11's rule for a lock's serial and digest under a record and
// a floor (Rollback), or "": under a record, unless a floor above its serial
// governs, a lower serial is LOCK-SERIAL-ROLLBACK and an equal one with
// another digest LOCK-SERIAL-CONFLICT; a floor that governs refuses a serial
// below it as LOCK-SERIAL-ROLLBACK. A higher serial is admitted, however far
// above.
func serialRule(serial int64, digest string, record *Record, floor *int64) string {
	if record != nil && (floor == nil || *floor <= record.Serial) {
		switch {
		case serial < record.Serial:
			return "LOCK-SERIAL-ROLLBACK"
		case serial == record.Serial && digest != record.Digest:
			return "LOCK-SERIAL-CONFLICT"
		}
		return ""
	}
	if floor != nil && serial < *floor {
		return "LOCK-SERIAL-ROLLBACK"
	}
	return ""
}

// record writes a lock's serial and digest for a registry id by compare and
// set against first, the record verification's step 1 read, and returns the
// rule's code for a record another verifier wrote since, or the problem that
// left the record unwritten or unread. The lock file is released before it
// returns, and only then is the lock accepted. A record that step 1 read and
// that is gone now is refused, never taken for a first contact.
func (s *RecordStore) record(registryID string, first *Record, serial int64, digest string, floor *int64) (code string, problem *RecordError) {
	hooks := currentRecordHooks()
	path, lockPath, temporary := s.paths(registryID)
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return "", &RecordError{Path: s.dir, Reason: "the record directory could not be created"}
	}
	if _, problem := s.directory(); problem != nil {
		return "", problem
	}
	wait := recordLockWait
	if hooks.LockWait > 0 {
		wait = hooks.LockWait
	}
	if problem := acquireRecordLock(lockPath, wait); problem != nil {
		return "", problem
	}
	code, problem = s.compareAndSet(registryID, first, serial, digest, floor, path, temporary, hooks)
	if err := os.Remove(lockPath); err != nil && problem == nil {
		return "", &RecordError{Path: lockPath, Reason: "the lock file could not be removed, so the lock is not accepted; remove it once no verifier runs"}
	}
	return code, problem
}

func (s *RecordStore) compareAndSet(registryID string, first *Record, serial int64, digest string, floor *int64, path, temporary string, hooks RecordHooks) (string, *RecordError) {
	current, problem := s.read(registryID)
	if problem != nil {
		return "", problem
	}
	if first != nil && current == nil {
		return "", &RecordError{Path: path, Reason: "the record read when the verification began is gone"}
	}
	if code := serialRule(serial, digest, current, floor); code != "" {
		return code, nil
	}
	want := Record{Serial: serial, Digest: digest}
	if current != nil && *current == want {
		return "", nil
	}
	data, err := encodeRecord(registryID, want)
	if err != nil {
		return "", &RecordError{Path: path, Reason: "the record has no canonical form"}
	}
	if problem := writeTemporary(temporary, data); problem != nil {
		return "", problem
	}
	if hooks.BeforeRename != nil {
		if err := hooks.BeforeRename(); err != nil {
			os.Remove(temporary)
			return "", &RecordError{Path: path, Reason: "the record could not be written"}
		}
	}
	if err := os.Rename(temporary, path); err != nil {
		os.Remove(temporary)
		return "", &RecordError{Path: path, Reason: "the record could not be written"}
	}
	if err := syncDirectory(s.dir); err != nil {
		return "", &RecordError{Path: s.dir, Reason: "the record directory could not be flushed, so the record is not known to be durable"}
	}
	if hooks.AfterRename != nil {
		hooks.AfterRename(path)
	}
	back, problem := s.read(registryID)
	if problem != nil {
		return "", problem
	}
	if back == nil || *back != want {
		return "", &RecordError{Path: path, Reason: "the record read back is not the one written"}
	}
	return "", nil
}

// acquireRecordLock creates the lock file, waiting up to wait for one that
// is there. A lock file is never removed here but by the verifier that
// created it.
func acquireRecordLock(path string, wait time.Duration) *RecordError {
	deadline := time.Now().Add(wait)
	pause := 5 * time.Millisecond
	for {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			if err := file.Close(); err != nil {
				os.Remove(path)
				return &RecordError{Path: path, Reason: "the lock file could not be created"}
			}
			return nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return &RecordError{Path: path, Reason: "the lock file could not be created"}
		}
		if !time.Now().Before(deadline) {
			return &RecordError{Path: path, Reason: "the lock file is held: another verifier holds it, or one that stopped left it behind, and it is never removed automatically; remove it once no verifier runs"}
		}
		time.Sleep(pause)
		pause = min(2*pause, 100*time.Millisecond)
	}
}

// writeTemporary writes and flushes the temporary file, created 0600 with
// O_EXCL once one left by an interrupted write is removed.
func writeTemporary(path string, data []byte) *RecordError {
	refused := &RecordError{Path: path, Reason: "the temporary record could not be written"}
	if info, err := os.Lstat(path); err == nil {
		if info.IsDir() || os.Remove(path) != nil {
			return refused
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return refused
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return refused
	}
	_, err = io.Copy(file, bytes.NewReader(data))
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(path)
		return refused
	}
	return nil
}
