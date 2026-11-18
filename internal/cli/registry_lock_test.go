package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit"
)

type lockRowFile struct {
	Source string `json:"source"`
	Path   string `json:"path"`
	Mode   string `json:"mode"`
	File   string `json:"file"`
}

type lockCLIRow struct {
	federationRow
	Operation        string   `json:"operation"`
	Registry         string   `json:"registry"`
	Kinds            []string `json:"kinds"`
	SourceSignatures []string `json:"sourceSignatures"`
	Signers          *struct {
		Key          string            `json:"key"`
		Repositories map[string]string `json:"repositories"`
	} `json:"signers"`
	Previous    string `json:"previous"`
	FirstSerial *int64 `json:"firstSerial"`
	Emitters    []struct {
		Source   string        `json:"source"`
		Manifest string        `json:"manifest"`
		Path     string        `json:"path"`
		Check    *int          `json:"check"`
		Files    []lockRowFile `json:"files"`
	} `json:"emitters"`
	Lock   string `json:"lock"`
	Expect struct {
		RegistryID   *string `json:"registryId"`
		RequireIndex *bool   `json:"requireIndex"`
		Environment  *string `json:"environment"`
	} `json:"expect"`
	Resolved string       `json:"resolved"`
	File     *lockRowFile `json:"file"`
}

func lockRowsOf(t *testing.T, class string) []lockCLIRow {
	t.Helper()
	var m struct {
		Vectors []lockCLIRow `json:"vectors"`
	}
	if err := json.Unmarshal(federationFile(t, "manifest.json"), &m); err != nil {
		t.Fatal(err)
	}
	var out []lockCLIRow
	for _, r := range m.Vectors {
		if r.Class == class {
			out = append(out, r)
		}
	}
	return out
}

func lockRowNamed(t *testing.T, class, id string) lockCLIRow {
	t.Helper()
	for _, r := range lockRowsOf(t, class) {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no %s row %s", class, id)
	return lockCLIRow{}
}

// layOut writes a row file at path under dir with the mode the row gives.
func layOut(t *testing.T, dir, path, file, mode string) string {
	t.Helper()
	if !filepath.IsLocal(filepath.FromSlash(path)) {
		t.Fatalf("a row file at %q cannot be laid out", path)
	}
	target := filepath.Join(dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	perm := fs.FileMode(0o644)
	if mode == emit.ModeExecutable {
		perm = 0o755
	}
	if err := os.WriteFile(target, federationFile(t, file), perm); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, perm); err != nil {
		t.Fatal(err)
	}
	return target
}

// needsModes skips a test that reads a file's mode from disk where the file
// system records none: the command refuses it there (exit 3).
func needsModes(t *testing.T) {
	t.Helper()
	if !emit.ModesRecorded() {
		t.Skip("this file system records no executable bit")
	}
}

// lockEmitterArguments lays each emitter out as a checkout, the manifest at
// its path and each file at its own, and returns the flags that give them.
func lockEmitterArguments(t *testing.T, r lockCLIRow) []string {
	t.Helper()
	var args []string
	if len(r.Emitters) > 0 {
		needsModes(t)
	}
	for _, e := range r.Emitters {
		dir := t.TempDir()
		layOut(t, dir, e.Path, e.Manifest, emit.ModeRegular)
		for _, f := range e.Files {
			layOut(t, dir, f.Path, f.File, f.Mode)
		}
		key := e.Path
		if e.Source != "" {
			key = e.Source + "=" + e.Path
			args = append(args, "--source-emitter", key+"="+dir)
		} else {
			args = append(args, "--emitter", key+"="+dir)
		}
		if e.Check != nil {
			args = append(args, "--emitter-check", key+"="+strconv.Itoa(*e.Check))
		}
	}
	return args
}

func lockExpectArguments(r lockCLIRow) []string {
	var args []string
	if r.Expect.RegistryID != nil {
		args = append(args, "--registry-id", *r.Expect.RegistryID)
	}
	if r.Expect.RequireIndex != nil {
		args = append(args, "--require-index", strconv.FormatBool(*r.Expect.RequireIndex))
	}
	if r.Expect.Environment != nil {
		args = append(args, "--environment", *r.Expect.Environment)
	}
	return args
}

// lockWriteArguments are a writer row's flags. A row lists its source
// signatures in the manifest's source order; the command takes them by name.
func lockWriteArguments(t *testing.T, r lockCLIRow) []string {
	t.Helper()
	args := []string{"registry", "lock", "write"}
	if r.Federation != "" {
		args = append(args, federationArguments(t, r.federationRow)...)
		var manifest struct {
			Sources []struct {
				Name string `json:"name"`
			} `json:"sources"`
		}
		if err := json.Unmarshal(federationFile(t, r.Federation), &manifest); err != nil || len(manifest.Sources) != len(r.SourceSignatures) {
			t.Fatalf("%s: the source signatures are not one per source (%v)", r.ID, err)
		}
		for i, file := range r.SourceSignatures {
			args = append(args, "--source-signature", manifest.Sources[i].Name+"="+federationPath(t, file))
		}
		args = append(args, "--key", r.Signers.Key)
		for identity, key := range r.Signers.Repositories {
			args = append(args, "--repository-key", identity+"="+key)
		}
	} else {
		args = append(args, "--registry", federationPath(t, r.Registry))
		for _, kind := range r.Kinds {
			args = append(args, "--kind", federationPath(t, kind))
		}
	}
	if r.Previous != "" {
		args = append(args, "--previous", federationPath(t, r.Previous))
	}
	if r.FirstSerial != nil {
		args = append(args, "--first-serial", strconv.FormatInt(*r.FirstSerial, 10))
	}
	return append(args, lockEmitterArguments(t, r)...)
}

func refusedCodes(t *testing.T, raw []byte) []string {
	t.Helper()
	var refused struct {
		Refused []string `json:"refused"`
	}
	if err := json.Unmarshal(raw, &refused); err != nil {
		t.Fatalf("not a refusal: %q", raw)
	}
	slices.Sort(refused.Refused)
	return refused.Refused
}

// TestRegistryLockWriteRows runs every lock-writer row through jpack
// registry lock write, in process: the lock's exact bytes, or the refused
// codes alone, no lock written, and exit 1.
func TestRegistryLockWriteRows(t *testing.T) {
	rows := lockRowsOf(t, "lock-writer")
	if len(rows) != 11 {
		t.Fatalf("%d lock-writer rows, want 11", len(rows))
	}
	for _, r := range rows {
		t.Run(r.ID, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "written.lock.json")
			code, stdout, stderr := runTest(t, append(lockWriteArguments(t, r), "--out", out), "")
			if stderr != "" {
				t.Errorf("stderr %q", stderr)
			}
			expected := federationFile(t, r.Expected)
			if want := refusedCodes(t, expected); want != nil {
				if got := refusedCodes(t, []byte(stdout)); code != 1 || !slices.Equal(got, want) {
					t.Errorf("exit %d, refused %v; want 1 and %v", code, got, want)
				}
				if _, err := os.Lstat(out); !errors.Is(err, fs.ErrNotExist) {
					t.Error("a refused lock was written")
				}
				return
			}
			if code != 0 || stdout != "" {
				t.Fatalf("exit %d, stdout %q", code, stdout)
			}
			if got, err := os.ReadFile(out); err != nil || !bytes.Equal(got, expected) {
				t.Errorf("lock\n got %s\nwant %s", got, expected)
			}
		})
	}
}

func lockCheckArguments(t *testing.T, r lockCLIRow) []string {
	t.Helper()
	switch r.Operation {
	case "check-lock":
		args := []string{"registry", "lock", "check", "--lock", federationPath(t, r.Lock), "--registry", federationPath(t, r.Registry)}
		for _, kind := range r.Kinds {
			args = append(args, "--kind", federationPath(t, kind))
		}
		if r.Index != "" {
			args = append(args, "--index", federationPath(t, r.Index))
		}
		return append(append(args, lockEmitterArguments(t, r)...), lockExpectArguments(r)...)
	case "coverage":
		args := []string{"registry", "lock", "coverage", "--lock", federationPath(t, r.Lock)}
		if r.Resolved != "" {
			args = append(args, "--resolved", federationPath(t, r.Resolved))
		}
		if r.File != nil {
			needsModes(t)
			local := layOut(t, t.TempDir(), "held", r.File.File, r.File.Mode)
			args = append(args, "--file", r.File.Path+"="+local)
			if r.File.Source != "" {
				args = append(args, "--file-source", r.File.Source)
			}
		}
		return append(args, lockExpectArguments(r)...)
	}
	t.Fatalf("%s: operation %q", r.ID, r.Operation)
	return nil
}

// TestRegistryLockCheckRows runs every lock-checker row through jpack
// registry lock check or coverage, in process: the report and the exit
// code.
func TestRegistryLockCheckRows(t *testing.T) {
	rows := lockRowsOf(t, "lock-checker")
	if len(rows) != 34 {
		t.Fatalf("%d lock-checker rows, want 34", len(rows))
	}
	for _, r := range rows {
		t.Run(r.ID, func(t *testing.T) {
			code, stdout, stderr := runTest(t, lockCheckArguments(t, r), "")
			checkFederationReport(t, r.federationRow, code, stdout, stderr)
		})
	}
}

// TestRegistryLockInvocation holds the refusals that come before any lock
// is read or written: a usage error exits 3 and an input that cannot be
// read 4, and neither writes a report or a lock. Above all, a writer given
// neither a previous lock nor a first serial has no default.
func TestRegistryLockInvocation(t *testing.T) {
	needsModes(t)
	single := lockRowNamed(t, "lock-writer", "write-next-serial")
	emitters := lockRowNamed(t, "lock-writer", "write-emitters")
	assembled := lockRowNamed(t, "lock-writer", "write-assembled")
	check := lockRowNamed(t, "lock-checker", "check-emitters")
	coverage := lockRowNamed(t, "lock-checker", "coverage-resolved")
	dir := t.TempDir()
	out := []string{"--out", filepath.Join(dir, "x.lock.json")}
	without := func(args []string, flag string) []string {
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
	write := append(lockWriteArguments(t, single), out...)
	writeEmitters := append(lockWriteArguments(t, emitters), out...)
	writeAssembled := append(lockWriteArguments(t, assembled), out...)
	checkArgs := lockCheckArguments(t, check)
	coverageArgs := lockCheckArguments(t, coverage)
	absent := filepath.Join(dir, "absent.json")
	cases := []struct {
		label string
		args  []string
		code  int
	}{
		{"no serial", without(write, "--previous"), 3},
		{"both serials", append(write, "--first-serial", "1"), 3},
		{"first serial not an integer", append(without(write, "--previous"), "--first-serial", "1e3"), 3},
		{"registry and federation", append(writeAssembled, "--registry", federationPath(t, single.Registry)), 3},
		{"neither registry nor federation", without(write, "--registry"), 3},
		{"kinds for an assembly", append(writeAssembled, "--kind", federationPath(t, "kinds/database.json")), 3},
		{"signers for a single registry", append(write, "--key", assembled.Signers.Key), 3},
		{"source signature form", append(writeAssembled, "--source-signature", "billing"), 3},
		{"source signature twice", append(writeAssembled, "--source-signature", "billing="+federationPath(t, "signatures/billing.json")), 3},
		{"repository key form", append(writeAssembled, "--repository-key", "nokey"), 3},
		{"emitter without its check", without(writeEmitters, "--emitter-check"), 3},
		{"check for no emitter", append(write, "--emitter-check", "emitters/db-roles.json=0"), 3},
		{"check not an integer", append(without(writeEmitters, "--emitter-check"), "--emitter-check", "emitters/db-roles.json=zero"), 3},
		{"emitter form", append(write, "--emitter", "emitters/db-roles.json"), 3},
		{"no default lock name", append(without(write, "--out"), "--registry", "-"), 3},
		{"lock to stdout", append(write, "--out", "-"), 3},
		{"two stdin inputs", append(without(write, "--registry"), "--registry", "-", "--kind", "-"), 3},
		{"remote previous", append(without(write, "--previous"), "--previous", "https://example.test/l.json"), 3},
		{"check with no lock", without(checkArgs, "--lock"), 3},
		{"check given a writer's flag", append(checkArgs, "--emitter-check", "emitters/db-roles.json=0"), 3},
		{"require-index not a boolean", append(without(checkArgs, "--require-index"), "--require-index", "yes"), 3},
		{"coverage with nothing to cover", without(coverageArgs, "--resolved"), 3},
		{"file source with no file", append(coverageArgs, "--file-source", "billing"), 3},
		{"previous lock absent", append(without(write, "--previous"), "--previous", absent), 4},
		{"lock absent", append(without(checkArgs, "--lock"), "--lock", absent), 4},
		{"emitter manifest absent", append(without(checkArgs, "--emitter"), "--emitter", "emitters/db-roles.json="+dir), 4},
		{"generated file absent", append(without(coverageArgs, "--resolved"), "--file", "gen/grant.sh="+absent), 4},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			code, stdout, _ := runTest(t, c.args, "")
			if code != c.code || strings.Contains(stdout, `"valid"`) || strings.Contains(stdout, `"refused"`) {
				t.Fatalf("exit=%d, want %d; stdout=%q", code, c.code, stdout)
			}
			if _, err := os.Lstat(filepath.Join(dir, "x.lock.json")); !errors.Is(err, fs.ErrNotExist) {
				t.Fatal("a lock was written")
			}
		})
	}
}

// TestRegistryLockWriteDefaultName writes the lock of <name>.json to
// <name>.lock.json beside it when no --out is given.
func TestRegistryLockWriteDefaultName(t *testing.T) {
	r := lockRowNamed(t, "lock-writer", "write-first-serial")
	dir := t.TempDir()
	registryPath := layOut(t, dir, "registry.json", r.Registry, emit.ModeRegular)
	args := lockWriteArguments(t, r)
	for i := range args {
		if args[i] == "--registry" {
			args[i+1] = registryPath
		}
	}
	code, stdout, _ := runTest(t, args, "")
	if code != 0 {
		t.Fatalf("exit %d, stdout %q", code, stdout)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "registry.lock.json")); err != nil || !bytes.Equal(got, federationFile(t, r.Expected)) {
		t.Fatalf("the default lock is not the row's: %v", err)
	}
}

// TestRegistryLockOversize gives each document over its bound as such: the
// operation refuses it with its code, and reads nothing more of it.
func TestRegistryLockOversize(t *testing.T) {
	dir := t.TempDir()
	big := func(name string, size int) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, bytes.Repeat([]byte(" "), size+1), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	replace := func(args []string, flag, value string) []string {
		out := slices.Clone(args)
		for i := range out {
			if out[i] == flag {
				out[i+1] = value
				return out
			}
		}
		t.Fatalf("no %s", flag)
		return nil
	}
	check := lockCheckArguments(t, lockRowNamed(t, "lock-checker", "check-single"))
	write := append(lockWriteArguments(t, lockRowNamed(t, "lock-writer", "write-next-serial")), "--out", filepath.Join(dir, "x.lock.json"))
	assembled := lockWriteArguments(t, lockRowNamed(t, "lock-writer", "write-assembled"))
	signature := ""
	for i := range assembled {
		if assembled[i] == "--source-signature" && strings.HasPrefix(assembled[i+1], "billing=") {
			signature = assembled[i+1]
			assembled[i+1] = "billing=" + big("signature.json", 4096)
		}
	}
	if signature == "" {
		t.Fatal("no billing signature")
	}
	assembled = append(assembled, "--out", filepath.Join(dir, "x.lock.json"))
	for _, c := range []struct {
		label string
		args  []string
		code  int
		want  string
	}{
		{"lock", replace(check, "--lock", big("lock.json", 1024*1024)), 1, `"REG-DOC-SIZE"`},
		{"registry", replace(check, "--registry", big("registry.json", 8*1024*1024)), 1, `"LOCK-REGISTRY-INVALID"`},
		{"previous lock", replace(write, "--previous", big("previous.json", 1024*1024)), 1, `"LOCK-FORM"`},
		{"source signature", assembled, 1, `"LOCK-SIG-DOCUMENT"`},
	} {
		t.Run(c.label, func(t *testing.T) {
			code, stdout, _ := runTest(t, c.args, "")
			if code != c.code || !strings.Contains(stdout, c.want) {
				t.Fatalf("exit=%d, want %d with %s; stdout=%q", code, c.code, c.want, stdout)
			}
		})
	}
}

// TestRegistryLockEmitterLink refuses an owned path that is a symbolic
// link: it is read as a read error, not followed.
func TestRegistryLockEmitterLink(t *testing.T) {
	r := lockRowNamed(t, "lock-checker", "check-emitters")
	args := lockCheckArguments(t, r)
	dir := ""
	for i := range args {
		if args[i] == "--emitter" {
			dir = strings.SplitN(args[i+1], "=", 2)[1]
		}
	}
	target := filepath.Join(t.TempDir(), "elsewhere.sh")
	owned := filepath.Join(dir, "gen", "grant.sh")
	if err := os.Rename(owned, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, owned); err != nil {
		t.Skipf("no symbolic links here: %v", err)
	}
	code, stdout, _ := runTest(t, args, "")
	if code != 4 || strings.Contains(stdout, `"valid"`) {
		t.Fatalf("exit=%d, want 4 and no report; stdout=%q", code, stdout)
	}
}
