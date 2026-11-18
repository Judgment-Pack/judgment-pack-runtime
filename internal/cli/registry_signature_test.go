package cli

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/federation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
)

// The RFC 8032 §7.1 TEST 1, 2 and 3 seeds: TEST 1 signed the rows' locks and
// the standard source, TEST 2 the billing source; TEST 3 signed nothing.
var registryTestSeeds = []string{
	"9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60",
	"4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb",
	"c5aa8df43f9f837bedb7442f31dcb7b166d38535076f094b85ce3a2e0b4458f7",
}

func registryTestPublic(t *testing.T, seed string) string {
	t.Helper()
	raw, err := hex.DecodeString(seed)
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(ed25519.NewKeyFromSeed(raw).Public().(ed25519.PublicKey))
}

func registryTestKeyID(t *testing.T, seed string) string {
	t.Helper()
	public, err := hex.DecodeString(registryTestPublic(t, seed))
	if err != nil {
		t.Fatal(err)
	}
	return audit.KeyID(public)
}

// registryKeyFiles writes a seed, at 0600 in a directory its owner alone
// can write, and the file naming the public key it is expected to have.
func registryKeyFiles(t *testing.T, seed, public string) (string, string) {
	t.Helper()
	dir := privateTempDir(t)
	key := filepath.Join(dir, "registry.seed")
	if err := os.WriteFile(key, []byte(seed+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(key, 0o600); err != nil {
		t.Fatal(err)
	}
	named := filepath.Join(dir, "registry.pub")
	if err := os.WriteFile(named, []byte(public+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return key, named
}

// runSigning runs a signing command and fails when anything it writes, to
// standard output or standard error, carries a test seed or its public key,
// in either case: a command names a key by its identifier alone.
func runSigning(t *testing.T, args []string) (int, string, string) {
	t.Helper()
	code, stdout, stderr := runTest(t, args, "")
	for _, seed := range registryTestSeeds {
		for _, secret := range []string{seed, registryTestPublic(t, seed)} {
			for _, form := range []string{secret, strings.ToUpper(secret)} {
				if strings.Contains(stdout, form) || strings.Contains(stderr, form) {
					t.Errorf("%v wrote a seed or a public key: stdout %q stderr %q", args, stdout, stderr)
				}
			}
		}
	}
	return code, stdout, stderr
}

// lockSignArguments are jpack registry lock sign's flags for a checker row's
// lock check.
func lockSignArguments(t *testing.T, id string) []string {
	t.Helper()
	args := lockCheckArguments(t, lockRowNamed(t, "lock-checker", id))
	return append([]string{"registry", "lock", "sign"}, args[3:]...)
}

func withoutFlag(args []string, flag string) []string {
	var kept []string
	for i := 0; i < len(args); i++ {
		if args[i] == flag {
			i++
			continue
		}
		kept = append(kept, args[i])
	}
	return kept
}

func replaced(args []string, flag, value string) []string {
	out := append([]string(nil), args...)
	for i := 0; i+1 < len(out); i++ {
		if out[i] == flag {
			out[i+1] = value
		}
	}
	return out
}

// jpack registry lock sign checks the lock, signs it and writes the rows'
// own signature document, at <lock>.sig.json by default; it prints the key's
// identifier, the digest and the path, and never writes over a document.
func TestRegistryLockSignWritesTheRowsSignature(t *testing.T) {
	skipWhereKeysCannotSign(t)
	key, public := registryKeyFiles(t, registryTestSeeds[0], registryTestPublic(t, registryTestSeeds[0]))
	lock := filepath.Join(t.TempDir(), "registry.lock.json")
	data := federationFile(t, "locks/single-42.lock.json")
	if err := os.WriteFile(lock, data, 0o600); err != nil {
		t.Fatal(err)
	}
	args := append(replaced(lockSignArguments(t, "check-single"), "--lock", lock), "--key", key, "--public-key", public)
	code, stdout, stderr := runSigning(t, args)
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	want := federationFile(t, "locks/single-42.lock.json.sig.json")
	if got, err := os.ReadFile(lock + ".sig.json"); err != nil || !bytes.Equal(got, want) {
		t.Fatalf("signature\n got %s (%v)\nwant %s", got, err, want)
	}
	var printed map[string]any
	if err := json.Unmarshal([]byte(stdout), &printed); err != nil || len(printed) != 3 || printed["lockDigest"] != federation.LockDigest(data) || printed["signature"] != lock+".sig.json" || printed["keyId"] != registryTestKeyID(t, registryTestSeeds[0]) {
		t.Errorf("output %q", stdout)
	}
	if canonical, err := jcs.EncodeDocument(printed); err != nil || string(canonical)+"\n" != stdout {
		t.Errorf("output not canonical: %q", stdout)
	}
	code, stdout, _ = runSigning(t, args)
	if code != 4 || !strings.Contains(stdout, "JPS-OUTPUT-WRITE") {
		t.Errorf("signing again: exit %d %q", code, stdout)
	}
	if got, _ := os.ReadFile(lock + ".sig.json"); !bytes.Equal(got, want) {
		t.Error("a signature was written over")
	}
}

// A lock that does not check is not signed: the check's report, exit 1, and
// no document.
func TestRegistryLockSignRefusesALockThatDoesNotCheck(t *testing.T) {
	skipWhereKeysCannotSign(t)
	key, public := registryKeyFiles(t, registryTestSeeds[0], registryTestPublic(t, registryTestSeeds[0]))
	r := lockRowNamed(t, "lock-checker", "check-registry-mismatch")
	output := filepath.Join(t.TempDir(), "refused.sig.json")
	code, stdout, stderr := runSigning(t, append(lockSignArguments(t, r.ID), "--key", key, "--public-key", public, "--output", output))
	checkFederationReport(t, r.federationRow, code, stdout, stderr)
	if _, err := os.Lstat(output); !errors.Is(err, fs.ErrNotExist) {
		t.Error("a lock that does not check was signed")
	}
}

// sourceSignArguments are jpack registry source sign's flags for one source
// of an assembly row, laid out as the assembler reads it.
func sourceSignArguments(t *testing.T, id, source string) []string {
	t.Helper()
	given := federationArguments(t, federationRowNamed(t, "assembler", id))
	args := []string{"registry", "source", "sign"}
	for i := 0; i < len(given); i++ {
		switch given[i] {
		case "--federation":
			args = append(args, given[i], given[i+1])
			i++
		case "--source":
			if strings.HasPrefix(given[i+1], source+"=") {
				args = append(args, given[i], given[i+1])
			}
			i++
		case "--known-index", "--known-lock":
			i++
		}
	}
	return args
}

// jpack registry source sign builds one source's statement from its own
// documents and writes the rows' own source signature document.
func TestRegistrySourceSignWritesTheRowsSignatures(t *testing.T) {
	skipWhereKeysCannotSign(t)
	for source, signed := range map[string]struct {
		seed int
		file string
	}{"standard": {0, "signatures/standard.json"}, "billing": {1, "signatures/billing.json"}} {
		key, public := registryKeyFiles(t, registryTestSeeds[signed.seed], registryTestPublic(t, registryTestSeeds[signed.seed]))
		output := filepath.Join(t.TempDir(), source+".sig.json")
		code, stdout, stderr := runSigning(t, append(sourceSignArguments(t, "assemble-two-sources", source), "--key", key, "--public-key", public, "--output", output))
		if code != 0 || stderr != "" {
			t.Fatalf("%s: exit %d stdout %q stderr %q", source, code, stdout, stderr)
		}
		want := federationFile(t, signed.file)
		if got, err := os.ReadFile(output); err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s: signature\n got %s (%v)\nwant %s", source, got, err, want)
		}
		if !strings.Contains(stdout, `"sourceDigest":"sha256:`) {
			t.Errorf("%s: output %q", source, stdout)
		}
	}
}

// A seed file another user could read, one that is not a regular file, one
// named through a link, and a seed whose public key is not the one named are
// each refused before anything is signed, and nothing names the seed or a
// key.
func TestRegistrySignRefusesASeedFile(t *testing.T) {
	skipWhereKeysCannotSign(t)
	seed := registryTestSeeds[0]
	for _, c := range []struct {
		label  string
		lay    func(t *testing.T, key string)
		public string
		reason string
	}{
		{"group-readable", func(t *testing.T, key string) { chmod(t, key, 0o640) }, "", audit.ErrKeyTooOpen.Error()},
		{"world-readable", func(t *testing.T, key string) { chmod(t, key, 0o604) }, "", audit.ErrKeyTooOpen.Error()},
		{"not regular", func(t *testing.T, key string) {
			if err := os.Remove(key); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(key, 0o700); err != nil {
				t.Fatal(err)
			}
		}, "", audit.ErrKeyNotRegular.Error()},
		{"link", func(t *testing.T, key string) {
			target := key + ".target"
			if err := os.Rename(key, target); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, key); err != nil {
				t.Fatal(err)
			}
		}, "", audit.ErrKeyThroughLink.Error()},
		{"another key", func(*testing.T, string) {}, registryTestPublic(t, registryTestSeeds[2]), audit.ErrKeyNotExpected.Error()},
	} {
		t.Run(c.label, func(t *testing.T) {
			public := c.public
			if public == "" {
				public = registryTestPublic(t, seed)
			}
			key, named := registryKeyFiles(t, seed, public)
			c.lay(t, key)
			lockOutput := filepath.Join(t.TempDir(), "lock.sig.json")
			sourceOutput := filepath.Join(t.TempDir(), "source.sig.json")
			for _, args := range [][]string{
				append(lockSignArguments(t, "check-single"), "--key", key, "--public-key", named, "--output", lockOutput),
				append(sourceSignArguments(t, "assemble-two-sources", "standard"), "--key", key, "--public-key", named, "--output", sourceOutput),
			} {
				code, stdout, stderr := runSigning(t, args)
				if code != 1 || !strings.Contains(stdout, "JPS-AUDIT-KEY-REFUSED") || !strings.Contains(stdout, c.reason) || stderr != "" {
					t.Errorf("%s: exit %d stdout %q stderr %q", args[1], code, stdout, stderr)
				}
			}
			for _, output := range []string{lockOutput, sourceOutput} {
				if _, err := os.Lstat(output); !errors.Is(err, fs.ErrNotExist) {
					t.Errorf("signed with a refused seed: %s", output)
				}
			}
		})
	}
}

func chmod(t *testing.T, path string, mode fs.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

// The signing commands' usage refusals: no seed or public key, a seed or
// output on standard input, more or fewer sources than one, no output for a
// source or for a lock read from standard input.
func TestRegistrySignInvocation(t *testing.T) {
	key, public := filepath.Join(t.TempDir(), "absent.seed"), filepath.Join(t.TempDir(), "absent.pub")
	lock := append(lockSignArguments(t, "check-single"), "--key", key, "--public-key", public, "--output", filepath.Join(t.TempDir(), "x.sig.json"))
	source := append(sourceSignArguments(t, "assemble-two-sources", "billing"), "--key", key, "--public-key", public, "--output", filepath.Join(t.TempDir(), "x.sig.json"))
	both := append(sourceSignArguments(t, "assemble-two-sources", ""), "--key", key, "--public-key", public, "--output", filepath.Join(t.TempDir(), "x.sig.json"))
	for _, c := range []struct {
		label string
		args  []string
		code  int
	}{
		{"lock: no key", withoutFlag(lock, "--key"), 3},
		{"lock: no public key", withoutFlag(lock, "--public-key"), 3},
		{"lock: key on stdin", replaced(lock, "--key", "-"), 3},
		{"lock: output to stdout", replaced(lock, "--output", "-"), 3},
		{"lock: stdin lock, no output", replaced(withoutFlag(lock, "--output"), "--lock", "-"), 3},
		{"lock: public key unreadable", lock, 4},
		{"source: no output", withoutFlag(source, "--output"), 3},
		{"source: two sources", both, 3},
		{"source: no source", withoutFlag(source, "--source"), 3},
		{"source: no key", withoutFlag(source, "--key"), 3},
	} {
		code, stdout, stderr := runSigning(t, c.args)
		if code != c.code || !strings.Contains(stdout, `"code":"JPS-`) || stderr != "" {
			t.Errorf("%s: exit %d (want %d) stdout %q stderr %q", c.label, code, c.code, stdout, stderr)
		}
	}
}

type verifyCLIRow struct {
	lockCLIRow
	LockSignatures []string        `json:"lockSignatures"`
	Record         json.RawMessage `json:"record"`
	Floor          *int64          `json:"floor"`
	Concurrent     json.RawMessage `json:"concurrent"`
	WriteFails     bool            `json:"writeFails"`
	ExpectedRecord json.RawMessage `json:"expectedRecord"`
}

func verifyCLIRows(t *testing.T) []verifyCLIRow {
	t.Helper()
	var m struct {
		Vectors []verifyCLIRow `json:"vectors"`
	}
	if err := json.Unmarshal(federationFile(t, "manifest.json"), &m); err != nil {
		t.Fatal(err)
	}
	var out []verifyCLIRow
	for _, r := range m.Vectors {
		if r.Class == "lock-signature-verifier" {
			out = append(out, r)
		}
	}
	return out
}

// verifyRecordPath is where the store keeps a registry id's record.
func verifyRecordPath(dir, registryID string) string {
	sum := sha256.Sum256([]byte(registryID))
	return filepath.Join(dir, hex.EncodeToString(sum[:])+".json")
}

// verifyRecordBytes is the record file a row's record is, as the store
// writes one.
func verifyRecordBytes(t *testing.T, registryID string, raw json.RawMessage) []byte {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil {
		t.Fatal(err)
	}
	object["formatVersion"] = federation.RecordFormatVersion
	object["registryId"] = registryID
	data, err := jcs.EncodeDocument(object)
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}

func verifyNull(raw json.RawMessage) bool { return len(raw) == 0 || string(raw) == "null" }

// verifyArguments are a verify row's flags, its expected signer set in a
// keys file and its record laid out in dir.
func verifyArguments(t *testing.T, r verifyCLIRow, dir, registryID string) []string {
	t.Helper()
	args := []string{"registry", "lock", "verify", "--lock", federationPath(t, r.Lock), "--record-dir", dir}
	for _, name := range r.LockSignatures {
		args = append(args, "--signature", federationPath(t, name))
	}
	for _, name := range r.SourceSignatures {
		args = append(args, "--source-signature", federationPath(t, name))
	}
	if r.Index != "" {
		args = append(args, "--index", federationPath(t, r.Index))
	}
	if r.Signers != nil {
		keys := map[string]any{"key": r.Signers.Key}
		if r.Signers.Repositories != nil {
			keys["repositories"] = r.Signers.Repositories
		}
		data, err := json.Marshal(keys)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "keys.json")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		args = append(args, "--keys", path)
	}
	if r.Floor != nil {
		args = append(args, "--floor", strconv.FormatInt(*r.Floor, 10))
	}
	if r.Registry != "" {
		args = append(args, "--registry", federationPath(t, r.Registry))
		for _, kind := range r.Kinds {
			args = append(args, "--kind", federationPath(t, kind))
		}
	}
	if r.Resolved != "" {
		args = append(args, "--resolved", federationPath(t, r.Resolved))
	}
	switch {
	case verifyNull(r.Record):
	case string(r.Record) == `"unreadable"`:
		if err := os.Mkdir(verifyRecordPath(dir, registryID), 0o700); err != nil {
			t.Fatal(err)
		}
	default:
		if err := os.WriteFile(verifyRecordPath(dir, registryID), verifyRecordBytes(t, registryID, r.Record), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return append(args, lockExpectArguments(r.lockCLIRow)...)
}

// TestRegistryLockVerifyRows runs every lock signature verifier row through
// jpack registry lock verify, in process, with its record in a directory of
// its own: the report, the exit code, the record once the command ends, and
// a record problem on standard error exactly when the report is
// LOCK-SERIAL-STATE.
func TestRegistryLockVerifyRows(t *testing.T) {
	rows := verifyCLIRows(t)
	if len(rows) != 63 {
		t.Fatalf("%d lock-signature-verifier rows, want 63", len(rows))
	}
	for _, r := range rows {
		t.Run(r.ID, func(t *testing.T) {
			dir := t.TempDir()
			chmod(t, dir, 0o700)
			registryID := ""
			if r.Expect.RegistryID != nil {
				registryID = *r.Expect.RegistryID
			}
			args := verifyArguments(t, r, dir, registryID)
			var hooks federation.RecordHooks
			if !verifyNull(r.Concurrent) {
				hooks.BeforeRecording = func() {
					if err := os.WriteFile(verifyRecordPath(dir, registryID), verifyRecordBytes(t, registryID, r.Concurrent), 0o600); err != nil {
						t.Error(err)
					}
				}
			}
			if r.WriteFails {
				hooks.BeforeRename = func() error { return errors.New("the row's record cannot be written") }
			}
			t.Cleanup(federation.StandInRecordHooksForTests(hooks))
			code, stdout, stderr := runTest(t, args, "")
			state := strings.Contains(stdout, `"LOCK-SERIAL-STATE"`)
			if state != (stderr != "") || (state && !strings.HasPrefix(stderr, "registry lock verify: serial record ")) {
				t.Errorf("stderr %q with LOCK-SERIAL-STATE %v", stderr, state)
			}
			checkFederationReport(t, r.federationRow, code, stdout, "")
			path := verifyRecordPath(dir, registryID)
			switch {
			case verifyNull(r.ExpectedRecord):
				if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
					t.Error("a record is written where the row expects none")
				}
			case string(r.ExpectedRecord) == `"unreadable"`:
				if info, err := os.Lstat(path); err != nil || !info.IsDir() {
					t.Error("the unreadable record is changed")
				}
			default:
				want := verifyRecordBytes(t, registryID, r.ExpectedRecord)
				if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, want) {
					t.Errorf("record\n got %s (%v)\nwant %s", got, err, want)
				}
			}
			if entries, _ := os.ReadDir(dir); len(entries) > 1 {
				t.Errorf("left behind %v", entries)
			}
		})
	}
}

// The verify command's usage refusals come before anything is read or
// recorded: there is no default record directory, a floor is a whole number,
// and kinds or emitters need a registry.
func TestRegistryLockVerifyInvocation(t *testing.T) {
	var r verifyCLIRow
	for _, row := range verifyCLIRows(t) {
		if row.ID == "verify-first-contact" {
			r = row
		}
	}
	dir := t.TempDir()
	chmod(t, dir, 0o700)
	args := verifyArguments(t, r, dir, *r.Expect.RegistryID)
	for _, c := range []struct {
		label string
		args  []string
	}{
		{"no record directory", withoutFlag(args, "--record-dir")},
		{"remote record directory", replaced(args, "--record-dir", "https://example.test/records")},
		{"floor not a whole number", replaced(args, "--floor", "1e3")},
		{"kind without a registry", append(args, "--kind", federationPath(t, "kinds/database.json"))},
		{"file source without a file", append(args, "--file-source", "billing")},
		{"no lock", withoutFlag(args, "--lock")},
		{"keys not an object of strings", replaced(args, "--keys", federationPath(t, "kinds/database.json"))},
	} {
		code, stdout, stderr := runTest(t, c.args, "")
		if code != 3 || !strings.Contains(stdout, "JPS-INVOCATION-INPUT") || stderr != "" {
			t.Errorf("%s: exit %d stdout %q stderr %q", c.label, code, stdout, stderr)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("a refused invocation wrote %v", entries)
	}
}
