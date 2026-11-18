package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
)

// The registry 1.1.0 rows (proposed), vendored with internal/federation.
var federationRows = filepath.Join("..", "federation", "testdata", "1.1.0")

type federationRow struct {
	ID         string `json:"id"`
	Class      string `json:"class"`
	Federation string `json:"federation"`
	Sources    map[string]struct {
		Commit string            `json:"commit"`
		Files  map[string]string `json:"files"`
	} `json:"sources"`
	Known struct {
		Indexes []string `json:"indexes"`
		Locks   []string `json:"locks"`
	} `json:"known"`
	Index    string `json:"index"`
	Expected string `json:"expected"`
	Writes   *struct {
		Registry string `json:"registry"`
		Index    string `json:"index"`
	} `json:"writes"`
}

func federationRowsOf(t *testing.T, class string) []federationRow {
	t.Helper()
	var m struct {
		Vectors []federationRow `json:"vectors"`
	}
	if err := json.Unmarshal(federationFile(t, "manifest.json"), &m); err != nil {
		t.Fatal(err)
	}
	var out []federationRow
	for _, r := range m.Vectors {
		if r.Class == class {
			out = append(out, r)
		}
	}
	return out
}

func federationFile(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(federationRows, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func federationPath(t *testing.T, name string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join(federationRows, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

// federationArguments lays a row's sources out as checkouts, each file at
// the path the manifest names, and returns the flags that give them.
func federationArguments(t *testing.T, r federationRow) []string {
	t.Helper()
	root := t.TempDir()
	args := []string{"--federation", federationPath(t, r.Federation)}
	n := 0
	for name, s := range r.Sources {
		dir := filepath.Join(root, fmt.Sprintf("source-%d", n))
		n++
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		for path, file := range s.Files {
			if !filepath.IsLocal(filepath.FromSlash(path)) {
				t.Fatalf("%s: a row file at %q cannot be laid out", r.ID, path)
			}
			target := filepath.Join(dir, filepath.FromSlash(path))
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(target); err == nil {
				t.Fatalf("%s: two row files at %q", r.ID, path)
			}
			if err := os.WriteFile(target, federationFile(t, file), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		args = append(args, "--source", name+"="+s.Commit+"="+dir)
	}
	if len(r.Known.Indexes)+len(r.Known.Locks) == 0 {
		args = append(args, "--no-known")
	}
	for _, file := range r.Known.Indexes {
		args = append(args, "--known-index", federationPath(t, file))
	}
	for _, file := range r.Known.Locks {
		args = append(args, "--known-lock", federationPath(t, file))
	}
	return args
}

// withoutMessages is a report's canonical form with every message removed,
// as the rows compare it; a report jpack wrote has one on every diagnostic.
func withoutMessages(t *testing.T, raw []byte, written bool) ([]byte, bool) {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("a report that is not JSON: %v\n%s", err, raw)
	}
	diagnostics, _ := value["diagnostics"].([]any)
	for _, item := range diagnostics {
		if d, ok := item.(map[string]any); ok {
			if message, _ := d["message"].(string); written && message == "" {
				t.Errorf("a diagnostic with no message: %v", d)
			}
			delete(d, "message")
		}
	}
	out, err := jcs.EncodeDocument(value)
	if err != nil {
		t.Fatal(err)
	}
	valid, _ := value["valid"].(bool)
	return out, valid
}

func checkFederationReport(t *testing.T, r federationRow, code int, stdout, stderr string) {
	t.Helper()
	if stderr != "" {
		t.Errorf("stderr %q", stderr)
	}
	decoder := json.NewDecoder(strings.NewReader(stdout))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("a report that is not JSON: %q", stdout)
	}
	if canonical, err := jcs.EncodeDocument(value); err != nil || string(canonical)+"\n" != stdout {
		t.Errorf("the report is not canonical JSON and one newline: %q", stdout)
	}
	got, _ := withoutMessages(t, []byte(stdout), true)
	want, valid := withoutMessages(t, federationFile(t, r.Expected), false)
	if !bytes.Equal(got, want) {
		t.Errorf("report\n got %s\nwant %s", got, want)
	}
	if wantCode := map[bool]int{true: 0, false: 1}[valid]; code != wantCode {
		t.Errorf("exit %d, want %d", code, wantCode)
	}
}

// TestRegistryAssembleRows runs every assembler row through jpack registry
// assemble, in process: the report, the exit code, and the two files an
// assembly with no error writes and one with an error does not.
func TestRegistryAssembleRows(t *testing.T) {
	rows := federationRowsOf(t, "assembler")
	if len(rows) != 46 {
		t.Fatalf("%d assembler rows, want 46", len(rows))
	}
	for _, r := range rows {
		t.Run(r.ID, func(t *testing.T) {
			out := t.TempDir()
			registryOut, indexOut := filepath.Join(out, "assembled.json"), filepath.Join(out, "index.json")
			args := append([]string{"registry", "assemble"}, federationArguments(t, r)...)
			args = append(args, "--registry-out", registryOut, "--index-out", indexOut)
			code, stdout, stderr := runTest(t, args, "")
			checkFederationReport(t, r, code, stdout, stderr)
			if r.Writes == nil {
				for _, path := range []string{registryOut, indexOut} {
					if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
						t.Errorf("an assembly the row refuses wrote %s", filepath.Base(path))
					}
				}
				return
			}
			for path, file := range map[string]string{registryOut: r.Writes.Registry, indexOut: r.Writes.Index} {
				got, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if want := federationFile(t, file); !bytes.Equal(got, want) {
					t.Errorf("%s\n got %s\nwant %s", filepath.Base(path), got, want)
				}
			}
		})
	}
}

// TestRegistryCheckIndexRows runs every index-checker row through jpack
// registry check-index, in process.
func TestRegistryCheckIndexRows(t *testing.T) {
	rows := federationRowsOf(t, "index-checker")
	if len(rows) != 5 {
		t.Fatalf("%d index-checker rows, want 5", len(rows))
	}
	for _, r := range rows {
		t.Run(r.ID, func(t *testing.T) {
			args := append([]string{"registry", "check-index", "--index", federationPath(t, r.Index)}, federationArguments(t, r)...)
			code, stdout, stderr := runTest(t, args, "")
			checkFederationReport(t, r, code, stdout, stderr)
		})
	}
}

// TestRegistryAssembleInvocation holds the refusals that come before any
// assembly: a usage error exits 3 and an input that cannot be read 4, and
// neither writes a report.
func TestRegistryAssembleInvocation(t *testing.T) {
	r := federationRowsOf(t, "assembler")[0]
	good := federationArguments(t, r)
	manifest := federationPath(t, r.Federation)
	dir := t.TempDir()
	without := func(flag string) []string {
		var out []string
		for i := 0; i < len(good); i++ {
			if good[i] == flag {
				if flag != "--no-known" {
					i++
				}
				continue
			}
			out = append(out, good[i])
		}
		return out
	}
	source := ""
	for i, arg := range good {
		if arg == "--source" {
			source = good[i+1]
			break
		}
	}
	name := strings.SplitN(source, "=", 2)[0]
	cases := []struct {
		label string
		args  []string
		code  int
	}{
		{"no manifest", append([]string{"registry", "assemble"}, without("--federation")...), 3},
		{"no known list", append([]string{"registry", "assemble"}, without("--no-known")...), 3},
		{"known list and none", append([]string{"registry", "assemble", "--known-index", manifest}, good...), 3},
		{"source form", append([]string{"registry", "assemble", "--source", "billing=only"}, good...), 3},
		{"source repeated", append([]string{"registry", "assemble", "--source", source}, good...), 3},
		{"remote manifest", append([]string{"registry", "assemble", "--federation", "https://example.test/f.json"}, without("--federation")...), 3},
		{"remote source", append([]string{"registry", "assemble", "--source", "other=0=https://example.test/x"}, good...), 3},
		{"two stdin inputs", append([]string{"registry", "assemble", "--federation", "-", "--known-lock", "-"}, without("--federation")...), 3},
		{"one output path", append([]string{"registry", "assemble", "--registry-out", filepath.Join(dir, "x.json"), "--index-out", filepath.Join(dir, "x.json")}, good...), 3},
		{"output to stdout", append([]string{"registry", "assemble", "--registry-out", "-"}, good...), 3},
		{"check-index with no index", append([]string{"registry", "check-index"}, good...), 3},
		{"manifest absent", append([]string{"registry", "assemble", "--federation", filepath.Join(dir, "absent.json")}, without("--federation")...), 4},
		{"known index absent", append([]string{"registry", "assemble", "--known-index", filepath.Join(dir, "absent.json")}, without("--no-known")...), 4},
		{"source directory absent", append(append([]string{"registry", "assemble"}, replaceSource(without("--no-known"), source, name+"="+strings.SplitN(source, "=", 3)[1]+"="+filepath.Join(dir, "absent"))...), "--no-known"), 4},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			code, stdout, _ := runTest(t, c.args, "")
			if code != c.code || strings.Contains(stdout, `"valid"`) {
				t.Fatalf("exit=%d, want %d; stdout=%q", code, c.code, stdout)
			}
		})
	}
}

func replaceSource(args []string, from, to string) []string {
	out := append([]string(nil), args...)
	for i := range out {
		if out[i] == from {
			out[i] = to
		}
	}
	return out
}

// TestRegistryAssembleReadsInsideEachSource holds a source's reads to its
// directory: a path that is a symbolic link is a read error, not followed,
// and a file over its bound is given as one, refused by the assembly.
func TestRegistryAssembleReadsInsideEachSource(t *testing.T) {
	r := federationRowsOf(t, "assembler")[0]
	t.Run("link", func(t *testing.T) {
		args := federationArguments(t, r)
		dir, path := firstSourceFile(t, r, args)
		target := filepath.Join(t.TempDir(), "elsewhere.json")
		if err := os.Rename(filepath.Join(dir, filepath.FromSlash(path)), target); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(dir, filepath.FromSlash(path))); err != nil {
			t.Skipf("no symbolic links here: %v", err)
		}
		code, stdout, _ := runTest(t, append([]string{"registry", "assemble"}, args...), "")
		if code != 4 || strings.Contains(stdout, `"valid"`) {
			t.Fatalf("exit=%d stdout=%q", code, stdout)
		}
	})
	t.Run("oversize", func(t *testing.T) {
		args := federationArguments(t, r)
		dir, path := firstSourceFile(t, r, args)
		big := bytes.Repeat([]byte(" "), 8*1024*1024+1)
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(path)), big, 0o600); err != nil {
			t.Fatal(err)
		}
		code, stdout, _ := runTest(t, append([]string{"registry", "assemble"}, args...), "")
		if code != 1 || !strings.Contains(stdout, `"REG-DOC-SIZE"`) {
			t.Fatalf("exit=%d stdout=%q", code, stdout)
		}
	})
}

// firstSourceFile names one file a row lays out: its source's directory
// and its path there.
func firstSourceFile(t *testing.T, r federationRow, args []string) (string, string) {
	t.Helper()
	for i, arg := range args {
		if arg != "--source" {
			continue
		}
		parts := strings.SplitN(args[i+1], "=", 3)
		for path := range r.Sources[parts[0]].Files {
			return parts[2], path
		}
	}
	t.Fatal("the row has no source file")
	return "", ""
}
