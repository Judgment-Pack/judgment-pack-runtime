package conformance

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit"
)

// The request mode answers each environment's request as a file: its files
// hold the registry digest, as an emitter's header may (RFC 0036 §4), and
// every secret reference whole.
var requestMode = []string{"request", "gen/req"}

func TestUpdateThenEveryCheckPasses(t *testing.T) {
	f := newFixture(t)
	m := f.manifest(requestMode, nil)
	f.registryVector("orders", nil)
	f.registryVector("refused", map[string]string{"expected-failure.json": `{"code":"EMIT-OPTIONS"}`, "options.json": `{"unknown":true}`})

	updated := f.run(m, true)
	if updated.Outcome != OutcomePass || updated.Exit() != ExitPass {
		t.Fatalf("update: %s", dump(updated))
	}
	if row := rowOf(t, updated, "orders", checkGolden); row.Reason != reasonUpdated {
		t.Fatalf("update row: %+v", row)
	}
	for _, environment := range []string{"dev", "prod"} {
		data, err := os.ReadFile(filepath.Join(f.vectors, "orders", "expected", "gen", "req", environment, "request.json"))
		if err != nil || !strings.Contains(string(data), `"registry":{"digest":"sha256:`) {
			t.Fatalf("expected/ for %s: %q %v", environment, data, err)
		}
	}
	// --update never touches an expected-failure vector.
	if _, err := os.Lstat(filepath.Join(f.vectors, "refused", "expected")); !os.IsNotExist(err) {
		t.Fatalf("update wrote an expected-failure vector's expected/: %v", err)
	}

	report := f.run(m, false)
	if report.Outcome != OutcomePass || report.Exit() != ExitPass || report.HarnessLevel != DefaultLevel || report.HarnessReportVersion != "1.0.0" {
		t.Fatalf("run: %s", dump(report))
	}
	wantRows(t, report, "orders", StatusPass, StatusPass, StatusPass, StatusPass, StatusPass, StatusPass)
	wantRows(t, report, "refused", StatusPass, StatusSkip, StatusSkip, StatusSkip, StatusSkip, StatusSkip)
	if row := rowOf(t, report, "refused", checkKindFiltering); row.Reason != reasonRefused {
		t.Fatalf("refused vector's check 4: %+v", row)
	}
	if got, want := len(report.Rows), 2*6+len(hostiles); got != want {
		t.Fatalf("rows: %d, want %d", got, want)
	}
	data, err := report.Canonical()
	if err != nil || strings.Contains(string(data), "retention") || strings.Contains(string(data), "orders-queue") {
		t.Fatalf("the report holds a request value: %s %v", data, err)
	}
}

func TestGoldenDifferencesAndExpectedFailures(t *testing.T) {
	f := newFixture(t)
	m := f.manifest(requestMode, nil)
	f.registryVector("orders", nil)
	if report := f.run(m, true); report.Outcome != OutcomePass {
		t.Fatalf("update: %s", dump(report))
	}
	expected := filepath.Join(f.vectors, "orders", "expected", "gen", "req")
	write(t, expected, map[string]string{"dev/request.json": "edited\n", "extra.txt": "x\n"})
	if err := os.Remove(filepath.Join(expected, "prod", "request.json")); err != nil {
		t.Fatal(err)
	}
	f.registryVector("wrong-code", map[string]string{"expected-failure.json": `{"code":"x-not-reported"}`, "options.json": `{"unknown":true}`})
	f.registryVector("succeeds", map[string]string{"expected-failure.json": `{"code":"EMIT-PROCESS"}`})

	report := f.run(m, false)
	if report.Outcome != OutcomeFail || report.Exit() != ExitFail {
		t.Fatalf("outcome: %s", dump(report))
	}
	row := rowOf(t, report, "orders", checkGolden)
	want := []emit.Difference{
		{Path: "gen/req/dev/request.json", Difference: emit.DifferenceChanged},
		{Path: "gen/req/extra.txt", Difference: emit.DifferenceExtra},
		{Path: "gen/req/prod/request.json", Difference: emit.DifferenceMissing},
	}
	if row.Status != StatusFail || row.Reason != reasonDifferent || !slices.Equal(row.Differences, want) {
		t.Fatalf("golden row: %+v", row)
	}
	if row := rowOf(t, report, "wrong-code", checkGolden); row.Status != StatusFail || row.Reason != reasonWrongFailure || row.Expected != "x-not-reported" || len(row.Diagnostics) == 0 || row.Diagnostics[0].Code != "EMIT-OPTIONS" {
		t.Fatalf("wrong code: %+v", row)
	}
	if row := rowOf(t, report, "succeeds", checkGolden); row.Status != StatusFail || row.Reason != reasonSucceeded {
		t.Fatalf("a run that succeeds: %+v", row)
	}
}

func TestExpectedFailureOfTheEmitter(t *testing.T) {
	f := newFixture(t)
	m := f.manifest([]string{"exit", "3"}, nil)
	f.registryVector("exits", map[string]string{"expected-failure.json": `{"code":"EMIT-PROCESS"}`})
	report := f.run(m, true)
	if report.Outcome != OutcomePass {
		t.Fatalf("outcome: %s", dump(report))
	}
	wantRows(t, report, "exits", StatusPass, StatusPass, StatusSkip, StatusPass, StatusSkip, StatusPass)
	if row := rowOf(t, report, "exits", checkPathSafety); row.Reason != reasonNoResponse {
		t.Fatalf("check 3: %+v", row)
	}
	if row := rowOf(t, report, "exits", checkReferences); row.Reason != reasonNoFiles {
		t.Fatalf("check 5: %+v", row)
	}
}

func TestDeterminismFailsOnTheTimeZone(t *testing.T) {
	f := newFixture(t)
	m := f.manifest([]string{"tz", "gen/tz.txt"}, map[string]any{"environments": "all"})
	f.registryVector("tz", nil)
	report := f.run(m, true)
	row := rowOf(t, report, "tz", checkDeterminism)
	if row.Status != StatusFail || row.Reason != reasonNondeterminism || !slices.Equal(row.Runs, []int{2, 3}) || report.Exit() != ExitFail {
		t.Fatalf("determinism: %+v\n%s", row, dump(report))
	}
	// Check 1 compares run 1, which has no TZ.
	data, err := os.ReadFile(filepath.Join(f.vectors, "tz", "expected", "gen", "tz.txt"))
	if err != nil || string(data) != "TZ=\n" {
		t.Fatalf("run 1's file: %q %v", data, err)
	}
	if strings.Contains(dump(report), "Kiritimati") {
		t.Fatal("the report holds an environment value")
	}
}

// A HOME the harness cannot make for run 2 or 3 stops the run (exit 2); it
// never reads as nondeterminism.
func TestDeterminismHomeFaultIsNotRun(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for name, temp := range map[string]func(string, string) (string, error){
		"no temporary directory": func(string, string) (string, error) { return "", errors.New("no space") },
		"no HOME below it":       func(string, string) (string, error) { return file, nil },
	} {
		t.Run(name, func(t *testing.T) {
			saved := makeTemp
			makeTemp = temp
			defer func() { makeTemp = saved }()
			f := newFixture(t)
			f.registryVector("orders", nil)
			report := f.run(f.manifest(requestMode, nil), true)
			if report.Outcome != OutcomeNotRun || report.Exit() != ExitNotRun || len(report.Diagnostics) != 1 {
				t.Fatalf("outcome: %s", dump(report))
			}
			if d := report.Diagnostics[0]; d.Code != "EMIT-HARNESS-BOUND" || d.Location.Path != "orders" {
				t.Fatalf("diagnostic: %+v", d)
			}
			for _, row := range report.Rows {
				if row.Status == StatusFail {
					t.Fatalf("a row failed: %+v", row)
				}
			}
		})
	}
}

func TestReferences(t *testing.T) {
	cases := map[string]struct {
		mode   []string
		status string
	}{
		"whole as text":    {[]string{"reference", "gen/ref.txt", "0", "text"}, StatusPass},
		"whole as base64":  {[]string{"reference", "gen/ref.bin", "0", "base64"}, StatusPass},
		"cut as text":      {[]string{"reference", "gen/ref.txt", "40", "text"}, StatusFail},
		"cut as base64":    {[]string{"reference", "gen/ref.bin", "40", "base64"}, StatusFail},
		"32 of it as text": {[]string{"reference", "gen/ref.txt", "32", "text"}, StatusFail},
		"31 of it as text": {[]string{"reference", "gen/ref.txt", "31", "text"}, StatusPass},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			m := f.manifest(c.mode, map[string]any{"environments": "all"})
			f.registryVector("ref", nil)
			report := f.run(m, true)
			row := rowOf(t, report, "ref", checkReferences)
			if row.Status != c.status {
				t.Fatalf("check 5: %+v", row)
			}
			if c.status == StatusFail {
				want := Reference{File: c.mode[1], Entry: row.References[0].Entry, Pointer: "/password"}
				if len(row.References) != 1 || row.References[0] != want || row.References[0].Entry.Kind != "queue" || row.References[0].Entry.ID != "orders" {
					t.Fatalf("references: %+v", row.References)
				}
				if strings.Contains(dump(report), "orders-queue") {
					t.Fatal("the report holds a reference")
				}
			}
		})
	}
}

func TestReferencesSkips(t *testing.T) {
	f := newFixture(t)
	facts := f.manifest(requestMode, map[string]any{"input": "factsContracts", "environments": nil})
	f.registryVector("facts", nil)
	report := f.run(facts, true)
	wantRows(t, report, "facts", StatusPass, StatusPass, StatusPass, StatusPass, StatusSkip, StatusPass)
	if row := rowOf(t, report, "facts", checkReferences); row.Reason != reasonFacts {
		t.Fatalf("facts: %+v", row)
	}
}

func TestReferencesBound(t *testing.T) {
	f := newFixture(t)
	m := f.manifest([]string{"reference", "gen/ref.txt", "0", "text"}, map[string]any{"environments": "all"})
	f.registryVector("ref", nil)
	saved := maxReferenceWork
	maxReferenceWork = 10
	defer func() { maxReferenceWork = saved }()
	report := f.run(m, true)
	if report.Outcome != OutcomeNotRun || report.Exit() != ExitNotRun || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "EMIT-HARNESS-BOUND" {
		t.Fatalf("bound: %s", dump(report))
	}
}

func TestStrayWrites(t *testing.T) {
	cases := map[string]struct {
		mode []string
		want []StrayWrite
	}{
		"create in a new directory":      {[]string{"stray", "create", "gen2/new.txt"}, []StrayWrite{{"EMIT-STRAY-WRITE", ".", StrayCreated}}},
		"create in a directory that was": {[]string{"stray", "create", "docs/inner/new.txt"}, []StrayWrite{{"EMIT-STRAY-WRITE", "docs", StrayCreated}}},
		"change":                         {[]string{"stray", "change", "README"}, []StrayWrite{{"EMIT-STRAY-WRITE", "README", StrayChanged}}},
		"remove":                         {[]string{"stray", "remove", "README"}, []StrayWrite{{"EMIT-STRAY-WRITE", "README", StrayRemoved}}},
		// The copy holds no .git, so one the emitter makes is a creation.
		"create in .git":         {[]string{"stray", "create", ".git/hooks/post-checkout"}, []StrayWrite{{"EMIT-STRAY-WRITE", ".", StrayCreated}}},
		"create in a .GIT below": {[]string{"stray", "create", "docs/.GIT/x"}, []StrayWrite{{"EMIT-STRAY-WRITE", "docs", StrayCreated}}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			write(t, f.root, map[string]string{"README": "readme\n", "docs/a.md": "a\n"})
			if err := os.MkdirAll(filepath.Join(f.root, ".git"), 0o755); err != nil {
				t.Fatal(err)
			}
			m := f.manifest(c.mode, map[string]any{"environments": "all"})
			// Two vectors: the second runs in a fresh copy, so a removal
			// succeeds again and is reported again.
			f.registryVector("a", nil)
			f.registryVector("b", nil)
			report := f.run(m, true)
			for _, vector := range []string{"a", "b"} {
				row := rowOf(t, report, vector, checkStrayWrites)
				if row.Status != StatusFail || row.Reason != reasonStray || !slices.Equal(row.StrayWrites, c.want) {
					t.Fatalf("%s: %+v", vector, row)
				}
				if golden := rowOf(t, report, vector, checkGolden); golden.Status != StatusPass {
					t.Fatalf("%s check 1: %+v", vector, golden)
				}
			}
			if data, err := os.ReadFile(filepath.Join(f.root, "README")); err != nil || string(data) != "readme\n" {
				t.Fatalf("the repository itself changed: %q %v", data, err)
			}
			if _, err := os.Lstat(filepath.Join(f.root, "gen2")); !os.IsNotExist(err) {
				t.Fatalf("the repository itself changed: %v", err)
			}
		})
	}
}

func TestStrayWritesCopyBound(t *testing.T) {
	f := newFixture(t)
	m := f.manifest(requestMode, nil)
	f.registryVector("orders", nil)
	saved := maxCopyEntries
	maxCopyEntries = 1
	defer func() { maxCopyEntries = saved }()
	report := f.run(m, true)
	if report.Outcome != OutcomeNotRun || len(report.Rows) != 0 || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "EMIT-HARNESS-BOUND" {
		t.Fatalf("copy bound: %s", dump(report))
	}
	if _, err := os.Lstat(filepath.Join(f.vectors, "orders", "expected")); !os.IsNotExist(err) {
		t.Fatalf("a run that could not start wrote expected/: %v", err)
	}
}

func TestCopyPassesOverGit(t *testing.T) {
	source, target := t.TempDir(), t.TempDir()
	write(t, source, map[string]string{".git/config": "x\n", "a/.GIT/config": "x\n", "a/b.txt": "b\n"})
	states, err := copyTree(source, target)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := states["a/b.txt"]; !ok || len(states) != 2 {
		t.Fatalf("copied: %v", states)
	}
	for _, path := range []string{".git", "a/.GIT"} {
		if _, err := os.Lstat(filepath.Join(target, path)); !os.IsNotExist(err) {
			t.Fatalf("%s was copied: %v", path, err)
		}
	}
	// The repository's own .git is not in the copy, so the copy holds none
	// unless the emitter made one, at any depth, and each is a creation.
	if changes := diffTree(target, states); len(changes) != 0 {
		t.Fatalf("changes: %+v", changes)
	}
	write(t, target, map[string]string{".git/new": "x\n", "a/.Git": "x\n"})
	want := []StrayWrite{{"EMIT-STRAY-WRITE", ".", StrayCreated}, {"EMIT-STRAY-WRITE", "a", StrayCreated}}
	if changes := diffTree(target, states); !slices.Equal(changes, want) {
		t.Fatalf("changes: %+v", changes)
	}
}

// symlinks makes links below dir, by slash path, or skips the test where
// links cannot be made.
func symlinks(t *testing.T, dir string, links map[string]string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("making a symbolic link needs a privilege on Windows")
	}
	for name, target := range links {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, full); err != nil {
			t.Fatal(err)
		}
	}
}

// A link whose absolute target is in the repository points into the copy:
// a write through it is a stray write of the copy, and never reaches the
// repository itself.
func TestStrayWriteThroughALinkIntoTheRepository(t *testing.T) {
	f := newFixture(t)
	write(t, f.root, map[string]string{"real/a.txt": "a\n"})
	symlinks(t, f.root, map[string]string{"cache": filepath.Join(f.root, "real")})
	m := f.manifest([]string{"stray", "create", "cache/written.txt"}, map[string]any{"environments": "all"})
	f.registryVector("a", nil)
	report := f.run(m, true)
	row := rowOf(t, report, "a", checkStrayWrites)
	if want := []StrayWrite{{"EMIT-STRAY-WRITE", "real", StrayCreated}}; row.Status != StatusFail || !slices.Equal(row.StrayWrites, want) {
		t.Fatalf("check 6: %+v", row)
	}
	if _, err := os.Lstat(filepath.Join(f.root, "real", "written.txt")); !os.IsNotExist(err) {
		t.Fatalf("the write reached the repository itself: %v", err)
	}
}

// A link that leads out of the repository refuses the run, before the
// emitter runs: a write through it would leave the copy.
func TestALinkOutOfTheRepositoryIsRefused(t *testing.T) {
	outside := t.TempDir()
	cases := map[string]struct {
		links map[string]string
		path  string
	}{
		"absolute":                     {map[string]string{"out": outside}, "out"},
		"absolute, by a .. of its own": {map[string]string{"out": "ROOT/docs/../.."}, "out"},
		"relative":                     {map[string]string{"docs/out": "../.."}, "docs/out"},
		"through a link to the root":   {map[string]string{"r": ".", "s": "r/../x"}, "s"},
		"absolute, through a link":     {map[string]string{"r": ".", "s": "ROOT/r/../x"}, "s"},
		"a loop":                       {map[string]string{"loop": "loop/x"}, "loop"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			write(t, f.root, map[string]string{"docs/a.md": "a\n"})
			links := map[string]string{}
			for link, target := range c.links {
				links[link] = strings.Replace(target, "ROOT", f.root, 1)
			}
			symlinks(t, f.root, links)
			m := f.manifest([]string{"stray", "create", "out/written.txt"}, map[string]any{"environments": "all"})
			f.registryVector("a", nil)
			report := f.run(m, true)
			if report.Outcome != OutcomeNotRun || report.Exit() != ExitNotRun || len(report.Rows) != 0 || len(report.Diagnostics) != 1 {
				t.Fatalf("report: %s", dump(report))
			}
			if d := report.Diagnostics[0]; d.Code != "EMIT-HARNESS-BOUND" || d.Location.Document != emit.DocumentRepository || d.Location.Path != c.path {
				t.Fatalf("diagnostic: %+v", d)
			}
			if _, err := os.Lstat(filepath.Join(outside, "written.txt")); !os.IsNotExist(err) {
				t.Fatalf("the emitter wrote outside the copy: %v", err)
			}
		})
	}
}

// A link that cannot be made in the copy, other than for want of Windows'
// privilege, refuses the run naming it, and the emitter never runs: in a
// fallback it would run in the repository itself and write its marker
// there. Here the
// link's absolute target is in the repository, and moved into a copy whose
// path is longer, past the system's limit on a link's target. A link that
// leads out is refused even when it comes after that one in walk order.
func TestALinkThatCannotBeMadeIsRefused(t *testing.T) {
	limit := 4096
	if runtime.GOOS == "darwin" {
		limit = 1024
	}
	cases := map[string]struct {
		out  bool
		code string
		path string
	}{
		"alone":                        {false, "EMIT-PATH-UNREADABLE", "a-long"},
		"before a link that leads out": {true, "EMIT-HARNESS-BOUND", "z-out"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			outside := t.TempDir()
			// The copy is made below TMPDIR: a long one makes its path longer
			// than the repository's.
			long := filepath.Join(t.TempDir(), strings.Repeat("d", 200), strings.Repeat("e", 200), strings.Repeat("f", 200))
			if err := os.MkdirAll(long, 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("TMPDIR", long)
			target := f.root
			for len(target) < limit-96 {
				target += string(filepath.Separator) + strings.Repeat("p", min(200, limit-96-len(target)-1))
			}
			links := map[string]string{"a-long": target}
			if c.out {
				links["z-out"] = outside
			}
			symlinks(t, f.root, links)
			m := f.manifest([]string{"stray", "create", "ran.txt"}, map[string]any{"environments": "all"})
			f.registryVector("a", nil)
			report := f.run(m, true)
			if report.Outcome != OutcomeNotRun || report.Exit() != ExitNotRun || len(report.Rows) != 0 || len(report.Diagnostics) != 1 {
				t.Fatalf("report: %s", dump(report))
			}
			if d := report.Diagnostics[0]; d.Code != c.code || d.Location.Document != emit.DocumentRepository || d.Location.Path != c.path {
				t.Fatalf("diagnostic: %+v", d)
			}
			if _, err := os.Lstat(filepath.Join(f.root, "ran.txt")); !os.IsNotExist(err) {
				t.Fatalf("the emitter ran: %v", err)
			}
		})
	}
}

// Links that stay in the repository are copied: a relative target as
// written, an absolute one moved into the copy, a dangling one too.
func TestCopyKeepsLinksThatStayIn(t *testing.T) {
	source, target := t.TempDir(), t.TempDir()
	write(t, source, map[string]string{"real/a.txt": "a\n"})
	symlinks(t, source, map[string]string{"self": ".", "rel": "real/a.txt", "dangling": "real/missing", "docs/up": "../real",
		"abs": filepath.Join(source, "real"), "root": source})
	states, err := copyTree(source, target)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"self": ".", "rel": "real/a.txt", "dangling": "real/missing", "docs/up": "../real",
		"abs": filepath.Join(target, "real"), "root": target}
	for name, link := range want {
		got, err := os.Readlink(filepath.Join(target, filepath.FromSlash(name)))
		if err != nil || got != link || states[name].target != link {
			t.Fatalf("%s: %q (%q) %v, want %q", name, got, states[name].target, err, link)
		}
	}
	if changes := diffTree(target, states); len(changes) != 0 {
		t.Fatalf("changes: %+v", changes)
	}
}

func TestPathSafetyOfAVector(t *testing.T) {
	f := newFixture(t)
	write(t, f.root, map[string]string{"answers/outside.json": `{"emitResponseVersion":"1.0.0","files":[{"path":"src/a","text":"x\n"}],"diagnostics":[]}` + "\n"})
	m := f.manifest([]string{"echo", "answers/outside.json"}, map[string]any{"environments": "all"})
	f.registryVector("outside", nil)
	f.registryVector("outside-expected", map[string]string{"expected-failure.json": `{"code":"EMIT-RESPONSE"}`})
	report := f.run(m, true)
	row := rowOf(t, report, "outside", checkPathSafety)
	if row.Status != StatusFail || row.Reason != reasonUnsafe || len(row.Diagnostics) != 1 || row.Diagnostics[0].Location.Pointer != "/files/0/path" {
		t.Fatalf("unsafe: %+v", row)
	}
	if row := rowOf(t, report, "outside-expected", checkPathSafety); row.Status != StatusPass || row.Reason != reasonRefusedAsMeant {
		t.Fatalf("expected: %+v", row)
	}
	if row := rowOf(t, report, "outside-expected", checkGolden); row.Status != StatusPass {
		t.Fatalf("expected golden: %+v", row)
	}
}

func TestHostileResponsesAreRefused(t *testing.T) {
	rows := hostileRows()
	if len(rows) != len(hostiles) {
		t.Fatalf("rows: %d", len(rows))
	}
	for _, row := range rows {
		if row.Status != StatusPass || row.Check != checkPathSafety || !strings.HasPrefix(row.Vector, "hostile/") {
			t.Errorf("%s: %+v", row.Vector, row)
		}
	}
}

// The controls of two hostiles: the fixed request resolves a location that
// names its entry, so the unresolvable location is refused for what it
// names, and a name that is not git's short name is accepted, so GIT~1 is
// refused as git's directory.
func TestHostileControls(t *testing.T) {
	if len(hostiles) != 29 {
		t.Fatalf("hostiles: %d; the ADR and CHANGELOG give the count", len(hostiles))
	}
	for name, data := range map[string][]byte{
		"a location that resolves": hostileDiagnostic(`{"document":"value","entry":{"id":"orders","kind":"queue"},"environment":"dev","pointer":"/retention"}`)(),
		"not git's short name":     hostileResponse(hostileFile("gen/GIT~2/config"))(),
	} {
		if response, findings := emit.ParseResponse(data, hostileRequestVersion, []byte(hostileRequest), hostileManifest()); response == nil || len(findings) != 0 {
			t.Errorf("%s: %+v", name, findings)
		}
	}
}

func TestHostileRowFailsWhenTheHostAccepts(t *testing.T) {
	saved := hostiles
	defer func() { hostiles = saved }()
	hostiles = []hostile{
		{"benign", "/files/0/path", hostileResponse(hostileFile("gen/a"))},
		{"elsewhere", "/files/0/text", hostileResponse(hostileFile("gen/../a"))},
	}
	rows := hostileRows()
	if rows[0].Status != StatusFail || rows[0].Reason != reasonHostileAccept {
		t.Fatalf("benign: %+v", rows[0])
	}
	if rows[1].Status != StatusFail || rows[1].Reason != reasonHostilePlace || len(rows[1].Diagnostics) == 0 {
		t.Fatalf("elsewhere: %+v", rows[1])
	}
}

func TestGivenRequest(t *testing.T) {
	f := newFixture(t)
	m := f.manifest(requestMode, map[string]any{"environments": "all"})
	request := `{"declarations":[],"emitRequestVersion":"1.0.0","emitter":{"id":"https://example.invalid/emitters/test","version":"1.0.0"},"options":{},"resolved":[]}`
	write(t, f.vectors, map[string]string{
		"given/request.json":                  request,
		"wrong-emitter/request.json":          strings.Replace(request, "emitters/test", "emitters/other", 1),
		"wrong-emitter/expected-failure.json": `{"code":"EMIT-REQUEST"}`,
	})
	report := f.run(m, true)
	if report.Outcome != OutcomePass {
		t.Fatalf("outcome: %s", dump(report))
	}
	wantRows(t, report, "given", StatusPass, StatusPass, StatusPass, StatusSkip, StatusSkip, StatusPass)
	if row := rowOf(t, report, "given", checkKindFiltering); row.Reason != reasonNoRegistry {
		t.Fatalf("check 4: %+v", row)
	}
	if row := rowOf(t, report, "given", checkReferences); row.Reason != reasonGivenRequest {
		t.Fatalf("check 5: %+v", row)
	}
	data, err := os.ReadFile(filepath.Join(f.vectors, "given", "expected", "gen", "req", "request.json"))
	if err != nil || string(data) != request+"\n" {
		t.Fatalf("the given request, canonical: %q %v", data, err)
	}
	wantRows(t, report, "wrong-emitter", StatusPass, StatusSkip, StatusSkip, StatusSkip, StatusSkip, StatusSkip)
}

func TestVectorLayoutRefusals(t *testing.T) {
	cases := map[string]struct {
		files   map[string]string
		pointer string
		path    string
	}{
		"an unknown level":           {map[string]string{"harness.json": `{"harnessLevel":"1.1"}`, "v/request.json": "{}", "v/expected/gen/a": "a\n"}, "/harnessLevel", "harness.json"},
		"a harness.json member more": {map[string]string{"harness.json": `{"harnessLevel":"1.0","x":1}`}, "", "harness.json"},
		"expected and a failure":     {map[string]string{"v/request.json": "{}", "v/expected/gen/a": "a\n", "v/expected-failure.json": `{"code":"EMIT-PROCESS"}`}, "", "v"},
		"neither expected nor fail":  {map[string]string{"v/request.json": "{}"}, "", "v/expected"},
		"request and registry":       {map[string]string{"v/request.json": "{}", "v/registry.json": "{}", "v/kinds/a.json": "{}", "v/expected/gen/a": "a\n"}, "", "v"},
		"a registry without kinds":   {map[string]string{"v/registry.json": "{}", "v/expected/gen/a": "a\n"}, "", "v/kinds"},
		"an unknown member":          {map[string]string{"v/request.json": "{}", "v/expected/gen/a": "a\n", "v/notes.txt": "x"}, "", "v/notes.txt"},
		"a failure code not listed":  {map[string]string{"v/request.json": "{}", "v/expected-failure.json": `{"code":"EMIT-NOT-A-CODE"}`}, "/code", "v/expected-failure.json"},
		"a warning's code":           {map[string]string{"v/request.json": "{}", "v/expected-failure.json": `{"code":"EMIT-DEPRECATED"}`}, "/code", "v/expected-failure.json"},
		// The marker ~empty is a zero-byte regular file alone at the root of
		// expected/; anything else that involves it refuses the vector.
		"a marker that is not empty":   {map[string]string{"v/request.json": "{}", "v/expected/~empty": "x"}, "", "v/expected/~empty"},
		"a marker beside a file":       {map[string]string{"v/request.json": "{}", "v/expected/~empty": "", "v/expected/gen/a": "a\n"}, "", "v/expected/~empty"},
		"a marker in a subdirectory":   {map[string]string{"v/request.json": "{}", "v/expected/gen/~empty": ""}, "", "v/expected/gen/~empty"},
		"a marker that is a directory": {map[string]string{"v/request.json": "{}", "v/expected/~empty/a": ""}, "", "v/expected/~empty"},
		// No vectors: the hostile rows alone must not pass the emitter.
		"no vectors": {map[string]string{}, "", ""},
		"only files": {map[string]string{"harness.json": `{"harnessLevel":"1.0"}`, "notes.txt": "x"}, "", ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			write(t, f.vectors, c.files)
			report := f.run(f.manifest(requestMode, nil), false)
			if report.Outcome != OutcomeNotRun || report.Exit() != ExitNotRun || len(report.Rows) != 0 {
				t.Fatalf("outcome: %s", dump(report))
			}
			d := report.Diagnostics[0]
			if d.Code != "EMIT-VECTOR" || d.Location.Document != DocumentVector || d.Location.Pointer != c.pointer || d.Location.Path != c.path {
				t.Fatalf("diagnostic: %+v", report.Diagnostics)
			}
		})
	}
}

// Every vector is laid out before any run, and each is read just before its
// own: a later vector whose layout is wrong refuses the run before the
// emitter runs, and one whose bytes are refused does so only after the
// earlier vector's run, whose rows the report holds and whose expected/
// --update wrote.
func TestVectorsAreReadOneAtATime(t *testing.T) {
	cases := map[string]struct {
		files map[string]string
		ran   bool
		path  string
	}{
		"a later vector of a wrong layout": {map[string]string{"b/request.json": "{}", "b/expected-failure.json": `{"code":"EMIT-PROCESS"}`, "b/notes.txt": "x"}, false, "b/notes.txt"},
		"a later vector read malformed":    {map[string]string{"b/request.json": "{}", "b/expected-failure.json": `{"code":`}, true, "b/expected-failure.json"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			m := f.manifest(requestMode, nil)
			f.registryVector("a", nil)
			write(t, f.vectors, c.files)
			report := f.run(m, true)
			if report.Outcome != OutcomeNotRun || report.Exit() != ExitNotRun {
				t.Fatalf("report: %s", dump(report))
			}
			if d := report.Diagnostics[len(report.Diagnostics)-1]; d.Code != "EMIT-VECTOR" || d.Location.Path != c.path {
				t.Fatalf("diagnostic: %+v", report.Diagnostics)
			}
			_, err := os.Lstat(filepath.Join(f.vectors, "a", "expected"))
			if ran := err == nil; ran != c.ran || (len(report.Rows) == 6) != c.ran {
				t.Fatalf("vector a ran: %v with %d rows, want %v", ran, len(report.Rows), c.ran)
			}
			if c.ran && rowOf(t, report, "a", checkGolden).Reason != reasonUpdated {
				t.Fatalf("vector a: %s", dump(report))
			}
		})
	}
}

// A set whose every vector is refused before the emitter runs is refused:
// its rows, and the hostile rows, would otherwise pass a run that never ran
// the emitter. The manifest's command does not exist.
func TestNoVectorRanTheEmitter(t *testing.T) {
	f := newFixture(t)
	m := f.manifest(nil, map[string]any{"command": []any{"bin/jpack-harness-test-absent"}})
	f.registryVector("refused", map[string]string{"expected-failure.json": `{"code":"EMIT-OPTIONS"}`, "options.json": `{"unknown":true}`})
	for _, update := range []bool{false, true} {
		report := f.run(m, update)
		if report.Outcome != OutcomeNotRun || report.Exit() != ExitNotRun {
			t.Fatalf("update %v: %s", update, dump(report))
		}
		last := report.Diagnostics[len(report.Diagnostics)-1]
		if last.Code != "EMIT-VECTOR" || last.Location.Document != DocumentVector || last.Location.Path != "" {
			t.Fatalf("update %v: %+v", update, report.Diagnostics)
		}
	}
}

// A command that never starts gives EMIT-PROCESS, as a vector may expect:
// that vector passes check 1 without the emitter, so the set is refused as
// one that never ran it.
func TestAnEmitterThatNeverStartedDidNotRun(t *testing.T) {
	f := newFixture(t)
	m := f.manifest(nil, map[string]any{"command": []any{"bin/jpack-harness-test-absent"}})
	f.registryVector("process", map[string]string{"expected-failure.json": `{"code":"EMIT-PROCESS"}`})
	for _, update := range []bool{false, true} {
		report := f.run(m, update)
		if report.Outcome != OutcomeNotRun || report.Exit() != ExitNotRun || len(report.Diagnostics) != 1 {
			t.Fatalf("update %v: %s", update, dump(report))
		}
		if d := report.Diagnostics[0]; d.Code != "EMIT-VECTOR" || d.Location.Document != DocumentVector || d.Location.Path != "" {
			t.Fatalf("update %v: %+v", update, report.Diagnostics)
		}
	}
}

// More of the marker ~empty: beside an empty directory, and a link.
func TestEmptyMarkerRefusals(t *testing.T) {
	cases := map[string]func(t *testing.T, expected string){
		"beside an empty directory": func(t *testing.T, expected string) {
			write(t, expected, map[string]string{"~empty": ""})
			if err := os.Mkdir(filepath.Join(expected, "gen"), 0o755); err != nil {
				t.Fatal(err)
			}
		},
		"a link": func(t *testing.T, expected string) {
			target := filepath.Join(t.TempDir(), "empty")
			write(t, filepath.Dir(target), map[string]string{"empty": ""})
			symlinks(t, expected, map[string]string{"~empty": target})
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			write(t, f.vectors, map[string]string{"v/request.json": "{}"})
			expected := filepath.Join(f.vectors, "v", "expected")
			if err := os.Mkdir(expected, 0o755); err != nil {
				t.Fatal(err)
			}
			build(t, expected)
			report := f.run(f.manifest(requestMode, nil), false)
			if report.Outcome != OutcomeNotRun || len(report.Rows) != 0 {
				t.Fatalf("outcome: %s", dump(report))
			}
			if d := report.Diagnostics[0]; d.Code != "EMIT-VECTOR" || d.Location.Path != "v/expected/~empty" {
				t.Fatalf("diagnostic: %+v", report.Diagnostics)
			}
		})
	}
}

// An expected/ with neither a regular file nor the marker records no run:
// it refuses the vector, and --update replaces it with the marker, never
// leaving it empty, after which the vector passes.
func TestExpectedWithNoFileAndNoMarker(t *testing.T) {
	cases := map[string][]string{
		"empty":                     nil,
		"only empty subdirectories": {"gen/req", "other"},
	}
	for name, dirs := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			write(t, f.root, map[string]string{"answers/none.json": `{"emitResponseVersion":"1.0.0","files":[],"diagnostics":[]}` + "\n"})
			none := f.manifest([]string{"echo", "answers/none.json"}, map[string]any{"environments": "all"})
			f.registryVector("v", nil)
			expected := filepath.Join(f.vectors, "v", "expected")
			if err := os.Mkdir(expected, 0o755); err != nil {
				t.Fatal(err)
			}
			for _, dir := range dirs {
				if err := os.MkdirAll(filepath.Join(expected, filepath.FromSlash(dir)), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			report := f.run(none, false)
			if report.Outcome != OutcomeNotRun || report.Exit() != ExitNotRun || len(report.Rows) != 0 || len(report.Diagnostics) != 1 {
				t.Fatalf("run: %s", dump(report))
			}
			if d := report.Diagnostics[0]; d.Code != "EMIT-VECTOR" || d.Location.Document != DocumentVector || d.Location.Path != "v/expected" {
				t.Fatalf("diagnostic: %+v", d)
			}
			if report := f.run(none, true); report.Outcome != OutcomePass || rowOf(t, report, "v", checkGolden).Reason != reasonUpdated {
				t.Fatalf("update: %s", dump(report))
			}
			entries, err := os.ReadDir(expected)
			if err != nil || len(entries) != 1 || entries[0].Name() != "~empty" || !entries[0].Type().IsRegular() {
				t.Fatalf("expected/ after update: %v %v", entries, err)
			}
			if report := f.run(none, false); report.Outcome != OutcomePass {
				t.Fatalf("run after update: %s", dump(report))
			}
		})
	}
}

// A run that gives no files is recorded as the marker ~empty, which git can
// track, and read back as no files; a run that gives files replaces it.
func TestEmptyMarkerFromUpdate(t *testing.T) {
	f := newFixture(t)
	write(t, f.root, map[string]string{"answers/none.json": `{"emitResponseVersion":"1.0.0","files":[],"diagnostics":[]}` + "\n"})
	none := f.manifest([]string{"echo", "answers/none.json"}, map[string]any{"environments": "all"})
	f.registryVector("none", nil)
	expected := filepath.Join(f.vectors, "none", "expected")
	// An empty expected/ from before is replaced by the marker.
	if err := os.Mkdir(expected, 0o755); err != nil {
		t.Fatal(err)
	}
	if report := f.run(none, true); report.Outcome != OutcomePass || rowOf(t, report, "none", checkGolden).Reason != reasonUpdated {
		t.Fatalf("update: %s", dump(report))
	}
	entries, err := os.ReadDir(expected)
	if err != nil || len(entries) != 1 || entries[0].Name() != "~empty" || !entries[0].Type().IsRegular() {
		t.Fatalf("expected/: %v %v", entries, err)
	}
	if info, err := entries[0].Info(); err != nil || info.Size() != 0 {
		t.Fatalf("the marker: %v %v", info, err)
	}
	report := f.run(none, false)
	if report.Outcome != OutcomePass {
		t.Fatalf("run: %s", dump(report))
	}
	wantRows(t, report, "none", StatusPass, StatusPass, StatusPass)
	// The marker is no files: a run that gives files differs from it.
	writes := f.manifest(requestMode, nil)
	if row := rowOf(t, f.run(writes, false), "none", checkGolden); row.Status != StatusFail || row.Reason != reasonDifferent || len(row.Differences) != 2 {
		t.Fatalf("golden against the marker: %+v", row)
	}
	if report := f.run(writes, true); report.Outcome != OutcomePass {
		t.Fatalf("update with files: %s", dump(report))
	}
	if _, err := os.Lstat(filepath.Join(expected, "~empty")); !os.IsNotExist(err) {
		t.Fatalf("the marker was kept beside files: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(expected, "gen", "req", "dev", "request.json")); err != nil {
		t.Fatal(err)
	}
}

func TestVectorLinkRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("making a symbolic link needs a privilege on Windows")
	}
	f := newFixture(t)
	outside := t.TempDir()
	write(t, outside, map[string]string{"request.json": "{}", "expected/gen/a": "a\n"})
	if err := os.Symlink(outside, filepath.Join(f.vectors, "linked")); err != nil {
		t.Fatal(err)
	}
	report := f.run(f.manifest(requestMode, nil), true)
	if report.Outcome != OutcomeNotRun || report.Diagnostics[0].Code != "EMIT-VECTOR" || report.Diagnostics[0].Location.Path != "linked" {
		t.Fatalf("link: %s", dump(report))
	}
}

func TestDeprecatedAndEnvUnsetAreReportedOnce(t *testing.T) {
	f := newFixture(t)
	m := f.manifest(requestMode, map[string]any{"deprecated": true, "env": []any{"JPACK_HARNESS_TEST_UNSET"}})
	f.registryVector("a", nil)
	f.registryVector("b", nil)
	report := Run(t.Context(), m, f.vectors, Options{Root: f.root, Environ: []string{"PATH=" + os.Getenv("PATH")}, Update: true})
	var codes []string
	for _, d := range report.Diagnostics {
		codes = append(codes, d.Code)
	}
	if !slices.Equal(codes, []string{"EMIT-DEPRECATED", "EMIT-ENV-UNSET"}) || report.Outcome != OutcomePass {
		t.Fatalf("diagnostics: %s", dump(report))
	}
}
