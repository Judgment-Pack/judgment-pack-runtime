package emit

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit/emittest"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// host runs mode's helper as the emitter of a manifest with changes, over the
// test registry.
func host(t *testing.T, root string, mode []string, changes map[string]any, opts Options) *Report {
	t.Helper()
	m := mustManifest(t, manifestDoc(mode, changes))
	opts.Root = root
	if opts.Timeout == 0 {
		opts.Timeout = 30 * time.Second
	}
	return Emit(context.Background(), m, nil, Inputs{Registry: testRegistry(t)}, opts)
}

// symlink makes a link, skipping the test where links cannot be made.
func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symbolic links cannot be made here: %v", err)
		}
		t.Fatal(err)
	}
}

func reportJSON(t *testing.T, report *Report) string {
	t.Helper()
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestEmitWritesTheResponseAndDeletesStaleOwnedFiles(t *testing.T) {
	root := testRepo(t)
	writeFiles(t, root, map[string]string{
		"gen/stale.sql": "old\n", "gen/x": "a stale file where a directory goes\n",
		"gen/sub/same.txt": "same\n", "src/kept.go": "package src\n",
	})
	response := responseJSON(t,
		map[string]any{"path": "gen/a.txt", "text": "alpha\n"},
		map[string]any{"path": "gen/sub/same.txt", "text": "same\n"},
		map[string]any{"path": "gen/tool.bin", "base64": "AAEC", "mode": "0755"},
		map[string]any{"path": "gen/x/y.txt", "text": "below\n"},
	)
	report := echoHost(t, root, response, false, nil)
	if report.Outcome != OutcomeWritten || report.Exit() != 0 || len(report.Diagnostics) != 0 {
		t.Fatalf("report: %s", reportJSON(t, report))
	}
	if want := []string{"gen/a.txt", "gen/sub/same.txt", "gen/tool.bin", "gen/x/y.txt"}; !slices.Equal(report.Written, want) {
		t.Fatalf("written %v, want %v", report.Written, want)
	}
	if want := []string{"gen/stale.sql", "gen/x"}; !slices.Equal(report.Deleted, want) {
		t.Fatalf("deleted %v, want %v", report.Deleted, want)
	}
	for path, want := range map[string]string{"gen/a.txt": "alpha\n", "gen/tool.bin": "\x00\x01\x02", "gen/x/y.txt": "below\n", "src/kept.go": "package src\n"} {
		if got, _, ok := onDisk(t, root, path); !ok || got != want {
			t.Errorf("%s holds %q, want %q", path, got, want)
		}
	}
	if _, mode, _ := onDisk(t, root, "gen/tool.bin"); modesRecorded && mode != 0o755 {
		t.Errorf("gen/tool.bin has mode %v", mode)
	}
	if _, _, ok := onDisk(t, root, "gen/stale.sql"); ok {
		t.Error("the stale file is still there")
	}
	leftovers, _ := filepath.Glob(filepath.Join(root, "gen", ".jpack-emit-*"))
	if len(leftovers) > 0 {
		t.Errorf("temporary files left: %v", leftovers)
	}

	// The same response again is current: check finds it identical, and
	// writing again deletes nothing.
	if again := echoHost(t, root, response, true, nil); again.Outcome != OutcomeIdentical || again.Exit() != 0 {
		t.Fatalf("check after write: %s", reportJSON(t, again))
	}
	if again := echoHost(t, root, response, false, nil); again.Outcome != OutcomeWritten || len(again.Deleted) != 0 {
		t.Fatalf("second write: %s", reportJSON(t, again))
	}
}

func TestEmitCheckReportsEachDifferenceAndWritesNothing(t *testing.T) {
	root := testRepo(t)
	writeFiles(t, root, map[string]string{
		"gen/changed": "on-disk-secret-text\n", "gen/extra": "extra-file-text\n",
		"gen/mode": "same\n", "gen/same": "same\n",
	})
	if err := os.Chmod(filepath.Join(root, "gen", "mode"), 0o755); err != nil {
		t.Fatal(err)
	}
	response := responseJSON(t,
		map[string]any{"path": "gen/changed", "text": "response-secret-text\n"},
		map[string]any{"path": "gen/missing", "text": "missing-file-text\n"},
		map[string]any{"path": "gen/mode", "text": "same\n"},
		map[string]any{"path": "gen/same", "text": "same\n"},
	)
	report := echoHost(t, root, response, true, nil)
	want := []Difference{{"gen/changed", DifferenceChanged}, {"gen/extra", DifferenceExtra}, {"gen/missing", DifferenceMissing}, {"gen/mode", DifferenceMode}}
	if !modesRecorded {
		want = slices.Delete(want, 3, 4)
	}
	if report.Outcome != OutcomeDifferent || report.Exit() != ExitDifferent || !slices.Equal(report.Differences, want) {
		t.Fatalf("report: %s", reportJSON(t, report))
	}
	text := reportJSON(t, report)
	for _, content := range []string{"secret-text", "extra-file-text", "missing-file-text"} {
		if strings.Contains(text, content) {
			t.Errorf("the report holds a file's contents (%s): %s", content, text)
		}
	}
	if len(report.Written) != 0 || len(report.Deleted) != 0 {
		t.Fatalf("check wrote or deleted: %s", text)
	}
	if got, _, _ := onDisk(t, root, "gen/changed"); got != "on-disk-secret-text\n" {
		t.Errorf("check changed gen/changed: %q", got)
	}
	if _, _, ok := onDisk(t, root, "gen/missing"); ok {
		t.Error("check wrote gen/missing")
	}
	if _, _, ok := onDisk(t, root, "gen/extra"); !ok {
		t.Error("check deleted gen/extra")
	}

	// A longer file on disk differs too, read only to the response's length.
	writeFiles(t, root, map[string]string{"gen/changed": "response-secret-text\nand more\n"})
	if report := echoHost(t, root, responseJSON(t, map[string]any{"path": "gen/changed", "text": "response-secret-text\n"}), true, map[string]any{"owns": []any{"gen/changed"}}); !slices.Equal(report.Differences, []Difference{{"gen/changed", DifferenceChanged}}) {
		t.Fatalf("a longer file: %s", reportJSON(t, report))
	}
}

func TestEmitRefusesALinkAtOrAboveAnOwnedPath(t *testing.T) {
	cases := []struct {
		name, link, target, write string
	}{
		{"at the path", "gen/a", "../outside/a", "gen/a"},
		{"above the path", "gen", "outside", "gen/a"},
		{"between", "gen/sub", "../outside", "gen/sub/a"},
		{"owned and not written", "gen/old", "../outside/a", "gen/new"},
	}
	for _, c := range cases {
		for _, check := range []bool{false, true} {
			t.Run(c.name+map[bool]string{true: " check", false: " write"}[check], func(t *testing.T) {
				root := testRepo(t)
				writeFiles(t, root, map[string]string{"outside/a": "kept\n"})
				if c.link != "gen" {
					writeFiles(t, root, map[string]string{"gen/stale": "stale\n"})
				}
				symlink(t, c.target, filepath.Join(root, filepath.FromSlash(c.link)))
				report := echoHost(t, root, responseJSON(t, map[string]any{"path": c.write, "text": "new\n"}), check, nil)
				if report.Exit() != ExitNotDone || !slices.Contains(codes(report.Diagnostics), "EMIT-LINK@"+c.link) {
					t.Fatalf("report: %s", reportJSON(t, report))
				}
				if got, _, _ := onDisk(t, root, "outside/a"); got != "kept\n" {
					t.Errorf("the link's target was changed: %q", got)
				}
				if entries, err := os.ReadDir(filepath.Join(root, "outside")); err != nil || len(entries) != 1 {
					t.Errorf("a file was written through the link: %v %v", entries, err)
				}
				if c.link != "gen" {
					if _, _, ok := onDisk(t, root, "gen/stale"); !ok {
						t.Error("a stale file was deleted in a refused run")
					}
				}
				if len(report.Written) != 0 || len(report.Deleted) != 0 {
					t.Errorf("a refused run wrote or deleted: %s", reportJSON(t, report))
				}
			})
		}
	}
}

func TestEmitValidatesEveryResponseBeforeWriting(t *testing.T) {
	root := testRepo(t)
	writeFiles(t, root, map[string]string{"gen/stale": "stale\n"})
	// The first file is good; the second is not owned.
	report := echoHost(t, root, responseJSON(t, map[string]any{"path": "gen/a", "text": "a\n"}, map[string]any{"path": "src/b", "text": "b\n"}), false, nil)
	if report.Exit() != ExitNotDone || !slices.Equal(codes(report.Diagnostics), []string{"EMIT-RESPONSE@/files/1/path"}) {
		t.Fatalf("report: %s", reportJSON(t, report))
	}
	if _, _, ok := onDisk(t, root, "gen/a"); ok {
		t.Error("a file of a refused response was written")
	}
	if _, _, ok := onDisk(t, root, "gen/stale"); !ok {
		t.Error("a stale file was deleted for a refused response")
	}
	// Each environment's run is valid alone, but both give gen/a: the second
	// is refused, and nothing of the first is written.
	report = echoHost(t, root, responseJSON(t, map[string]any{"path": "gen/a", "text": "a\n"}), false, map[string]any{"environments": "each"})
	if report.Exit() != ExitNotDone || !slices.Equal(codes(report.Diagnostics), []string{"EMIT-RESPONSE@/files/0/path"}) || report.Diagnostics[0].Location.Environment != "prod" {
		t.Fatalf("report: %s", reportJSON(t, report))
	}
	if _, _, ok := onDisk(t, root, "gen/a"); ok {
		t.Error("a file of a refused run was written")
	}
}

func TestEmitRefusesAPathThatIsADirectoryOfAnother(t *testing.T) {
	for _, owns := range []string{"gen/**", "gen/a/**"} {
		for _, check := range []bool{false, true} {
			root := testRepo(t)
			writeFiles(t, root, map[string]string{"gen/a/stale": "stale\n"})
			response := responseJSON(t, map[string]any{"path": "gen/a", "text": "a\n"}, map[string]any{"path": "gen/a/b", "text": "b\n"})
			report := echoHost(t, root, response, check, map[string]any{"owns": []any{owns}})
			if report.Exit() != ExitNotDone || !slices.Equal(codes(report.Diagnostics), []string{"EMIT-RESPONSE@/files/1/path"}) {
				t.Fatalf("owns %s, check %v: %s", owns, check, reportJSON(t, report))
			}
			if got, _, ok := onDisk(t, root, "gen/a/stale"); !ok || got != "stale\n" {
				t.Errorf("owns %s, check %v: the stale file was changed", owns, check)
			}
			if _, _, ok := onDisk(t, root, "gen/a/b"); ok {
				t.Errorf("owns %s, check %v: a file of a refused response was written", owns, check)
			}
		}
	}
	// Each run alone is valid, but one gives a directory of the other's path.
	root := testRepo(t)
	writeFiles(t, root, map[string]string{
		"fixtures/dev.json":  responseJSON(t, map[string]any{"path": "gen/a", "text": "a\n"}),
		"fixtures/prod.json": responseJSON(t, map[string]any{"path": "gen/a/b", "text": "b\n"}),
	})
	report := host(t, root, []string{"echo-env", "fixtures"}, nil, Options{})
	if report.Exit() != ExitNotDone || !slices.Equal(codes(report.Diagnostics), []string{"EMIT-RESPONSE@/files/0/path"}) || report.Diagnostics[0].Location.Environment != "prod" {
		t.Fatalf("two runs: %s", reportJSON(t, report))
	}
	if _, _, ok := onDisk(t, root, "gen/a"); ok {
		t.Error("a file of a refused run was written")
	}
}

func TestEmitNeverOwnsGitsDirectory(t *testing.T) {
	gitFiles := map[string]string{
		"gen/.git/HEAD": "ref: refs/heads/main\n", "gen/sub/.GIT/config": "[core]\n", "gen/module/.git": "gitdir: ../.git/modules/m\n",
	}
	for _, check := range []bool{false, true} {
		root := testRepo(t)
		writeFiles(t, root, gitFiles)
		writeFiles(t, root, map[string]string{"gen/stale": "stale\n", "gen/.gitignore": "*.tmp\n"})
		response := responseJSON(t, map[string]any{"path": "gen/.github/a", "text": "a\n"}, map[string]any{"path": "gen/.gitignore", "text": "*.tmp\n"})
		report := echoHost(t, root, response, check, nil)
		if len(report.Diagnostics) != 0 {
			t.Fatalf("check %v: %s", check, reportJSON(t, report))
		}
		if check {
			want := []Difference{{"gen/.github/a", DifferenceMissing}, {"gen/stale", DifferenceExtra}}
			if !slices.Equal(report.Differences, want) {
				t.Fatalf("differences %v, want %v", report.Differences, want)
			}
		} else if !slices.Equal(report.Deleted, []string{"gen/stale"}) {
			t.Fatalf("deleted %v", report.Deleted)
		}
		for path, want := range gitFiles {
			if got, _, ok := onDisk(t, root, path); !ok || got != want {
				t.Errorf("check %v: %s was changed", check, path)
			}
		}
	}
	root := testRepo(t)
	writeFiles(t, root, gitFiles)
	for _, path := range []string{"gen/.git/HEAD", "gen/.Git"} {
		report := echoHost(t, root, responseJSON(t, map[string]any{"path": path, "text": "x\n"}), false, nil)
		if !slices.Equal(codes(report.Diagnostics), []string{"EMIT-RESPONSE@/files/0/path"}) {
			t.Fatalf("%s: %s", path, reportJSON(t, report))
		}
	}
	if got, _, _ := onDisk(t, root, "gen/.git/HEAD"); got != gitFiles["gen/.git/HEAD"] {
		t.Error("a file in git's directory was written")
	}
}

func TestEmitGivesTheEmitterOnlyItsEnvironment(t *testing.T) {
	t.Setenv("JPACK_EMIT_TEST_PARENT_ONLY", "parent-value-text")
	t.Setenv("EMIT_TEST_DECLARED", "declared-value-text")
	root := testRepo(t)
	report := host(t, root, []string{"env", "gen/env.txt"}, map[string]any{"environments": "all", "env": []any{"EMIT_TEST_DECLARED", "EMIT_TEST_NEVER_SET"}}, Options{})
	if report.Outcome != OutcomeWritten || !slices.Equal(codes(report.Diagnostics), []string{"EMIT-ENV-UNSET@/env/1"}) || report.Diagnostics[0].Location.Variable != "EMIT_TEST_NEVER_SET" {
		t.Fatalf("report: %s", reportJSON(t, report))
	}
	if text := reportJSON(t, report); strings.Contains(text, "value-text") {
		t.Fatalf("the report holds an env value: %s", text)
	}
	text, _, _ := onDisk(t, root, "gen/env.txt")
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		name, value, _ := strings.Cut(line, "=")
		if runtime.GOOS == "windows" && strings.EqualFold(name, "SYSTEMROOT") {
			continue // Go adds it on Windows, where programs need it
		}
		got[name] = value
	}
	names := make([]string, 0, len(got))
	for name := range got {
		names = append(names, name)
	}
	slices.Sort(names)
	if want := []string{"EMIT_TEST_DECLARED", "HOME", "LANG", "PATH"}; !slices.Equal(names, want) {
		t.Fatalf("the emitter's environment has %v, want %v", names, want)
	}
	if got["EMIT_TEST_DECLARED"] != "declared-value-text" || got["LANG"] != "C.UTF-8" || got["PATH"] != os.Getenv("PATH") {
		t.Fatalf("environment: %v", got)
	}
	if home := got["HOME"]; home == os.Getenv("HOME") || !filepath.IsAbs(home) {
		t.Fatalf("HOME is %q", home)
	} else if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("the emitter's HOME was not removed: %v", err)
	}
}

func TestEmitRunsTheEmitterInTheRepositoryRoot(t *testing.T) {
	root := testRepo(t)
	report := host(t, root, []string{"cwd", "gen/cwd.txt"}, map[string]any{"environments": "all"}, Options{})
	if got, _, _ := onDisk(t, root, "gen/cwd.txt"); report.Outcome != OutcomeWritten || got != root+"\n" {
		t.Fatalf("working directory %q, want %q: %s", got, root, reportJSON(t, report))
	}
}

func TestEmitRequestHoldsOnlyTheKindsRead(t *testing.T) {
	root := testRepo(t)
	report := host(t, root, []string{"request", "gen/req"}, map[string]any{"env": []any{"EMIT_TEST_NEVER_SET"}}, Options{})
	if report.Outcome != OutcomeWritten || len(report.Runs) != 2 || !slices.Equal(codes(report.Diagnostics), []string{"EMIT-ENV-UNSET@/env/0"}) {
		t.Fatalf("report: %s", reportJSON(t, report))
	}
	for i, environment := range []string{"dev", "prod"} {
		text, _, _ := onDisk(t, root, "gen/req/"+environment+"/request.json")
		request := strings.TrimSuffix(text, "\n")
		if run := report.Runs[i]; run.Environment != environment || run.RequestDigest != digestOf([]byte(request)) || run.RequestVersion != "1.0.0" {
			t.Fatalf("run %d: %+v", i, run)
		}
		for _, absent := range []string{"topic", "events", "partitions"} {
			if strings.Contains(request, absent) {
				t.Fatalf("the %s request holds %q, of a kind not read: %s", environment, absent, request)
			}
		}
		var parsed struct {
			Version  string         `json:"emitRequestVersion"`
			Emitter  Identity       `json:"emitter"`
			Options  map[string]any `json:"options"`
			Resolved []struct {
				Environment string `json:"environment"`
			} `json:"resolved"`
		}
		if err := json.Unmarshal([]byte(request), &parsed); err != nil {
			t.Fatal(err)
		}
		if parsed.Version != "1.0.0" || parsed.Emitter.ID != "https://example.invalid/emitters/test" || len(parsed.Options) != 0 ||
			len(parsed.Resolved) != 1 || parsed.Resolved[0].Environment != environment {
			t.Fatalf("the %s request: %s", environment, request)
		}
		retention := map[string]string{"dev": `"retention":3`, "prod": `"retention":14`}[environment]
		if !strings.Contains(request, retention) {
			t.Fatalf("the %s request lacks %s: %s", environment, retention, request)
		}
	}

	all := host(t, root, []string{"request", "gen/req"}, map[string]any{"environments": "all"}, Options{})
	text, _, _ := onDisk(t, root, "gen/req/request.json")
	if all.Outcome != OutcomeWritten || len(all.Runs) != 1 || strings.Contains(text, "topic") || strings.Count(text, `"environment":`) != 2 {
		t.Fatalf("all environments: %s\n%s", reportJSON(t, all), text)
	}
	if !slices.Equal(all.Deleted, []string{"gen/req/dev/request.json", "gen/req/prod/request.json"}) {
		t.Fatalf("deleted %v", all.Deleted)
	}
}

func TestEmitRefusesAKindPinOutsideTheManifestsReads(t *testing.T) {
	root := testRepo(t)
	for name, c := range map[string]struct {
		read map[string]any
		want string
	}{
		"version out of range": {map[string]any{"kind": "queue", "id": "https://example.invalid/kinds/queue", "versions": "^2.0.0"}, "EMIT-KIND-RANGE@/kinds/0"},
		"another kind id":      {map[string]any{"kind": "queue", "id": "https://example.invalid/kinds/other-queue", "versions": "^1.0.0"}, "EMIT-KIND-ID@/kinds/0"},
	} {
		t.Run(name, func(t *testing.T) {
			report := host(t, root, []string{"exit", "0"}, map[string]any{"reads": []any{c.read}}, Options{})
			if report.Exit() != ExitNotDone || !slices.Equal(codes(report.Diagnostics), []string{c.want}) || len(report.Runs) != 0 {
				t.Fatalf("report: %s", reportJSON(t, report))
			}
		})
	}
}

func TestEmitChecksOptionsAndRequestVersions(t *testing.T) {
	root := testRepo(t)
	schema := map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"schemaName": map[string]any{"type": "string"}}}
	run := func(options any, changes map[string]any) *Report {
		m := mustManifest(t, manifestDoc([]string{"request", "gen/req"}, changes))
		return Emit(context.Background(), m, nil, Inputs{Registry: testRegistry(t), Options: options}, Options{Root: root, Timeout: 30 * time.Second})
	}
	if report := run(map[string]any{"schemaName": json.Number("5")}, map[string]any{"options": schema}); !slices.Equal(codes(report.Diagnostics), []string{"EMIT-OPTIONS@/schemaName"}) || len(report.Runs) != 0 {
		t.Fatalf("options of the wrong type: %s", reportJSON(t, report))
	}
	if report := run(map[string]any{"other": true}, nil); !slices.Equal(codes(report.Diagnostics), []string{"EMIT-OPTIONS@/other"}) {
		t.Fatalf("options with no schema: %s", reportJSON(t, report))
	}
	report := run(map[string]any{"schemaName": "billing"}, map[string]any{"options": schema, "environments": "all"})
	if text, _, _ := onDisk(t, root, "gen/req/request.json"); report.Outcome != OutcomeWritten || !strings.Contains(text, `"options":{"schemaName":"billing"}`) {
		t.Fatalf("valid options: %s\n%s", reportJSON(t, report), text)
	}
	if report := run(nil, map[string]any{"requestVersions": []any{"2.0.0"}}); !slices.Equal(codes(report.Diagnostics), []string{"EMIT-REQUEST-VERSION@/requestVersions"}) {
		t.Fatalf("an unsupported request version: %s", reportJSON(t, report))
	}
	if report := run(nil, map[string]any{"requestVersions": []any{"1.0.0", "1.4.0"}, "environments": "all"}); report.Outcome != OutcomeWritten || report.Runs[0].RequestVersion != "1.0.0" {
		t.Fatalf("one supported request version of two: %s", reportJSON(t, report))
	}
}

func TestEmitReportsAnEmitterThatFails(t *testing.T) {
	root := testRepo(t)
	// A program named as a name is never looked for in the working
	// directory, though one is there.
	if err := emittest.Link(root, filepath.Base(emitterCommand)); err != nil {
		t.Fatal(err)
	}
	all := map[string]any{"environments": "all"}
	cases := map[string]struct {
		mode    []string
		changes map[string]any
		opts    Options
		want    string
	}{
		"non-zero exit":  {[]string{"exit", "3"}, all, Options{}, "EMIT-PROCESS@/command"},
		"empty response": {[]string{"exit", "0"}, all, Options{}, "EMIT-RESPONSE@"},
		// A name is looked up on PATH only, here an empty directory.
		"name not on PATH":    {nil, map[string]any{"environments": "all", "command": []any{"jpack-emit-test-no-such-program"}}, Options{Environ: []string{"PATH=" + t.TempDir()}}, "EMIT-PROCESS@/command/0"},
		"relative path":       {nil, map[string]any{"environments": "all", "command": []any{"bin/missing"}}, Options{}, "EMIT-PROCESS@/command/0"},
		"relative to the cwd": {nil, map[string]any{"environments": "all", "command": []any{"emitter"}}, Options{Environ: []string{"PATH=" + t.TempDir()}}, "EMIT-PROCESS@/command/0"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			report := host(t, root, c.mode, c.changes, c.opts)
			if report.Exit() != ExitNotDone || !slices.Equal(codes(report.Diagnostics), []string{c.want}) {
				t.Fatalf("report: %s", reportJSON(t, report))
			}
		})
	}

	t.Run("a link out of the root", func(t *testing.T) {
		outside := testRepo(t)
		symlink(t, filepath.Join(outside, filepath.FromSlash(emitterCommand)), filepath.Join(root, "bin", "linked"))
		report := host(t, root, nil, map[string]any{"environments": "all", "command": []any{"bin/linked", "exit", "0"}}, Options{})
		if !slices.Equal(codes(report.Diagnostics), []string{"EMIT-PROCESS@/command/0"}) {
			t.Fatalf("report: %s", reportJSON(t, report))
		}
	})

	t.Run("standard error", func(t *testing.T) {
		quiet := host(t, root, []string{"stderr", "100000"}, all, Options{})
		var passed bytes.Buffer
		loud := host(t, root, []string{"stderr", "100000"}, all, Options{Stderr: &passed})
		if quiet.Outcome != OutcomeWritten || quiet.Runs[0].StderrBytes != 100000 || loud.Runs[0].StderrBytes != 100000 || passed.Len() != MaxStderrBytes {
			t.Fatalf("quiet %s, loud %s, passed %d bytes", reportJSON(t, quiet), reportJSON(t, loud), passed.Len())
		}
	})
}

func TestEmitEndsAnEmitterThatPassesTheResponseBound(t *testing.T) {
	defer func(limit int) { maxResponseBytes = limit }(maxResponseBytes)
	maxResponseBytes = 1 << 20
	start := time.Now()
	report := host(t, testRepo(t), []string{"flood"}, map[string]any{"environments": "all"}, Options{Timeout: time.Minute})
	if !slices.Equal(codes(report.Diagnostics), []string{"EMIT-RESPONSE@"}) || report.Diagnostics[0].Location.Document != DocumentResponse {
		t.Fatalf("report: %s", reportJSON(t, report))
	}
	if elapsed := time.Since(start); elapsed > 30*time.Second {
		t.Fatalf("the flood ran %v: it was not ended at the bound", elapsed)
	}
}

func TestEmitRefusesOwnsThatOverlapAnotherManifests(t *testing.T) {
	root := testRepo(t)
	m := mustManifest(t, manifestDoc([]string{"exit", "0"}, nil))
	overlapping := mustManifest(t, manifestDoc([]string{"exit", "0"}, map[string]any{"owns": []any{"docs/**", "gen/db/*.sql"}}))
	report := Emit(context.Background(), m, []*Manifest{overlapping}, Inputs{Registry: testRegistry(t)}, Options{Root: root, Timeout: 30 * time.Second})
	if report.Exit() != ExitNotDone || len(report.Runs) != 0 || !slices.Equal(codes(report.Diagnostics), []string{"EMIT-OWNS-OVERLAP@/owns/0", "EMIT-OWNS-OVERLAP@/owns/1"}) ||
		report.Diagnostics[1].Location.Manifest != 1 {
		t.Fatalf("report: %s", reportJSON(t, report))
	}
	apart := mustManifest(t, manifestDoc([]string{"exit", "0"}, map[string]any{"owns": []any{"docs/**"}}))
	writeFiles(t, root, map[string]string{"fixtures/response.json": responseJSON(t)})
	echo := mustManifest(t, manifestDoc([]string{"echo", "fixtures/response.json"}, map[string]any{"environments": "all"}))
	if report := Emit(context.Background(), echo, []*Manifest{apart}, Inputs{Registry: testRegistry(t)}, Options{Root: root, Timeout: 30 * time.Second}); report.Outcome != OutcomeWritten {
		t.Fatalf("owns apart: %s", reportJSON(t, report))
	}
}

func TestEmitFormsAFactsContractsRequest(t *testing.T) {
	root := testRepo(t)
	report := host(t, root, []string{"request", "gen/req"}, map[string]any{"input": "factsContracts", "environments": nil}, Options{})
	text, _, _ := onDisk(t, root, "gen/req/request.json")
	var parsed struct {
		Contracts []map[string]any `json:"factsContracts"`
		Resolved  any              `json:"resolved"`
	}
	if err := json.Unmarshal([]byte(text), &parsed); err != nil || report.Outcome != OutcomeWritten || len(parsed.Contracts) != 1 || parsed.Resolved != nil {
		t.Fatalf("report %s, request %s, %v", reportJSON(t, report), text, err)
	}
	if strings.Contains(text, "topic") || !strings.Contains(text, "retention") {
		t.Fatalf("the request holds a kind not read, or lacks the one read: %s", text)
	}
}

func TestEmitReportsAnInvalidRegistryAndADeprecatedEmitter(t *testing.T) {
	root := testRepo(t)
	m := mustManifest(t, manifestDoc([]string{"exit", "0"}, nil))
	invalid := registry.Validate([]byte(`{"registryFormatVersion":"1.0.0"}`), nil)
	report := Emit(context.Background(), m, nil, Inputs{Registry: invalid}, Options{Root: root, Timeout: 30 * time.Second})
	if report.Exit() != ExitNotDone || report.RegistryReport == nil || !slices.Equal(codes(report.Diagnostics), []string{"EMIT-REQUEST@"}) {
		t.Fatalf("an invalid registry: %s", reportJSON(t, report))
	}
	writeFiles(t, root, map[string]string{"fixtures/response.json": responseJSON(t)})
	report = echoHost(t, root, responseJSON(t), false, map[string]any{"deprecated": true})
	if report.Outcome != OutcomeWritten || !slices.Equal(codes(report.Diagnostics), []string{"EMIT-DEPRECATED@/deprecated"}) || report.Diagnostics[0].Severity != "warning" {
		t.Fatalf("a deprecated emitter: %s", reportJSON(t, report))
	}
}

func TestEmitHoldsTheReportedDiagnosticsToTheirBound(t *testing.T) {
	defer func(limit int) { maxReported = limit }(maxReported)
	maxReported = 2
	root := testRepo(t)
	warning := `{"severity":"warning","code":"x-note","location":{"document":"value","entry":{"kind":"queue","id":"orders"},"environment":"prod","pointer":"/retention"}}`
	response := `{"emitResponseVersion":"1.0.0","files":[],"diagnostics":[` + warning + "," + warning + "," + warning + `]}`
	report := echoHost(t, root, response, false, nil)
	if report.Outcome != OutcomeWritten || len(report.EmitterDiagnostics) != 2 || !slices.Equal(codes(report.Diagnostics), []string{"EMIT-REPORT-TRUNCATED@/diagnostics"}) {
		t.Fatalf("report: %s", reportJSON(t, report))
	}
}
