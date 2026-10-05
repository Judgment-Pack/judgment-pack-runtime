package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/timestamp/tsatest"
)

// saveFixture is a project with one chained record, a witness that signed
// its checkpoint, and a file for every input audit verify reads, each one it
// can read: the base arguments verify with a witness key.
type saveFixture struct {
	config, trail, sidecar, stamps string
	witness                        *cliWitness
	key, statements, head, resume  string
	publicKey, revoked             string
	roots, crls, expect            string
	signatures, stampsFile         string
}

func newSaveFixture(t *testing.T) *saveFixture {
	t.Helper()
	configPath, trail := recordedProject(t, 1)
	checkpoints := trailCheckpoints(t, configPath)
	w := newCLIWitness("save")
	w.sign("checkpoint", checkpoints[0])
	f := &saveFixture{config: configPath, trail: trail, witness: w, key: w.keyFile(t)}
	f.statements = w.file(t, "statements.jsonl", 0, 1)
	f.head = w.file(t, "head.jsonl", 0, 1)
	f.resume = writeDocument(t, "resume.json", `{"continuationVersion":"1","last":`+w.lines[0]+`,"latestCheckpoint":`+w.lines[0]+"}\n")
	f.sidecar = filepath.Join(filepath.Dir(trail), audit.SidecarName)
	f.stamps = filepath.Join(filepath.Dir(trail), audit.StampsName)
	for _, beside := range []string{f.sidecar, f.stamps} {
		if err := os.WriteFile(beside, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	other := newCLIWitness("revoked")
	f.publicKey = writeDocument(t, "public.pub", hex.EncodeToString(newCLIWitness("public").public)+"\n")
	f.revoked = writeDocument(t, "revoked.jsonl", `{"from":1,"publicKey":"`+hex.EncodeToString(other.public)+`"}`+"\n")
	tsa, err := tsatest.New(tsatest.Options{})
	if err != nil {
		t.Fatal(err)
	}
	crl, err := tsa.CRL(time.Now().Add(time.Hour), time.Time{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.roots = writeDocument(t, "roots.pem", string(tsa.RootPEM()))
	f.crls = writeDocument(t, "tsa.crl", string(crl))
	f.expect = writeDocument(t, "held.jsonl", checkpoints[0]+"\n")
	f.signatures = writeDocument(t, "signatures.jsonl", "")
	f.stampsFile = writeDocument(t, "stamps.jsonl", "")
	return f
}

// digestOf is a file's SHA-256, or what kept it from being read, as a short
// string to compare.
func digestOf(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return "unreadable: " + err.Error()
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// leftovers is the temporary files a save could leave in a directory.
func leftovers(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	left := []string{}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			left = append(left, entry.Name())
		}
	}
	return left
}

// spellings are the ways a test names one file as --witness-save: as given,
// by another spelling of its path, by a symbolic link to it and by a hard link
// to it. A spelling the platform cannot make is left out.
func spellings(t *testing.T, path string) map[string]string {
	t.Helper()
	dir, name := filepath.Split(path)
	named := map[string]string{
		"the same spelling": path,
		"another spelling":  dir + "." + string(filepath.Separator) + name,
	}
	links := t.TempDir()
	if err := os.Symlink(path, filepath.Join(links, "symbolic")); err == nil {
		named["a symbolic link"] = filepath.Join(links, "symbolic")
	} else if runtime.GOOS != "windows" {
		t.Fatal(err)
	}
	if err := os.Link(path, filepath.Join(links, "hard")); err == nil {
		named["a hard link"] = filepath.Join(links, "hard")
	} else {
		t.Fatal(err)
	}
	return named
}

// saveRefused runs audit verify with args and --witness-save destination, and
// says how the run disagrees with a refusal of the destination, for why when
// it is not "", that leaves input as it was and leaves no temporary file, or
// "". A verification that found nothing answers with the refusal, exit 3; one
// that found something reports it, exit 1, with the refusal noted beside it.
func saveRefused(t *testing.T, args []string, destination, input, why string) string {
	t.Helper()
	before := digestOf(t, input)
	code, stdout, _ := runTest(t, append(append([]string{"audit", "verify", "--format", "json"}, args...), "--witness-save", destination), "")
	var output result.AuditVerification
	noted := code == result.ExitInvalid && json.Unmarshal([]byte(stdout), &output) == nil && output.FindingsTotal > 0 &&
		output.Witness != nil && strings.Contains(output.Witness.SaveRefused, why) && strings.HasSuffix(output.Witness.SaveRefused, "; nothing was saved")
	refused := code == result.ExitInvocation && strings.Contains(stdout, `"JPS-INVOCATION-AUDIT-WITNESS-SAVE"`) && strings.Contains(stdout, why) &&
		strings.Contains(stdout, "The verification itself found nothing, and nothing was saved.")
	switch {
	case !noted && !refused:
		return fmt.Sprintf("exit=%d %s", code, first(stdout, 300))
	case digestOf(t, input) != before:
		return "the input changed"
	case len(leftovers(t, filepath.Dir(destination))) > 0:
		return fmt.Sprintf("left %v", leftovers(t, filepath.Dir(destination)))
	}
	return ""
}

// Every flag of audit verify is either an input, which --witness-save may not
// name, or not a file at all. A flag added later fails here until it is
// classified, and an input it reads that the App does not record fails the
// refusal below.
func TestAuditVerifyNeverSavesOverAnInput(t *testing.T) {
	f := newSaveFixture(t)
	base := []string{"--config", f.config, "--witness-key", f.key}
	inputs := map[string]struct {
		args  []string
		input string
	}{
		"trail":          {[]string{"--trail", f.trail, "--witness-key", f.key}, f.trail},
		"config":         {base, f.config},
		"expect":         {append(append([]string{}, base...), "--expect", f.expect), f.expect},
		"public-key":     {append(append([]string{}, base...), "--public-key", f.publicKey), f.publicKey},
		"revoked":        {append(append([]string{}, base...), "--public-key", f.publicKey, "--revoked", f.revoked), f.revoked},
		"signatures":     {append(append([]string{}, base...), "--public-key", f.publicKey, "--signatures", f.signatures), f.signatures},
		"tsa-roots":      {append(append([]string{}, base...), "--tsa-roots", f.roots), f.roots},
		"tsa-crls":       {append(append([]string{}, base...), "--tsa-roots", f.roots, "--tsa-crls", f.crls), f.crls},
		"stamps":         {append(append([]string{}, base...), "--tsa-roots", f.roots, "--stamps", f.stampsFile), f.stampsFile},
		"witness-key":    {base, f.key},
		"witness":        {append(append([]string{}, base...), "--witness", f.statements), f.statements},
		"witness-head":   {append(append([]string{}, base...), "--witness-head", f.head), f.head},
		"witness-resume": {append(append([]string{}, base...), "--witness-resume", f.resume, "--witness", f.resume), f.resume},
	}
	notFiles := map[string]string{
		"format":                        "an output format",
		"require-checkpoint-through":    "a sequence",
		"require-signed-through":        "a sequence",
		"require-stamped-through":       "a sequence",
		"require-countersigned-through": "a sequence",
		"tsa-policy":                    "an object identifier",
		"witness-save":                  "the destination itself",
	}
	flags := []string{}
	(&App{}).auditVerifyCommand().Flags().VisitAll(func(flag *pflag.Flag) { flags = append(flags, flag.Name) })
	sort.Strings(flags)
	if len(flags) != len(inputs)+len(notFiles) {
		t.Fatalf("audit verify has %d flags, and %d are classified: %v", len(flags), len(inputs)+len(notFiles), flags)
	}
	for _, name := range flags {
		_, input := inputs[name]
		_, other := notFiles[name]
		if input == other {
			t.Fatalf("--%s is not classified as an input or as no file", name)
		}
	}
	// Every input a flag names is recorded by the App as it is opened, and so
	// is every file read without a flag naming it: that record, and nothing
	// named here, is what the destination is held to.
	recordedBy := func(stdin *os.File, args []string, input string) bool {
		t.Helper()
		app := &App{in: strings.NewReader(""), out: &bytes.Buffer{}, errOut: &bytes.Buffer{}}
		if stdin != nil {
			app.in = stdin
		}
		command := app.auditVerifyCommand()
		command.SetArgs(append([]string{"--format", "json"}, args...))
		command.SetOut(&bytes.Buffer{})
		command.SetErr(&bytes.Buffer{})
		_ = command.Execute()
		info, err := os.Stat(input)
		if err != nil {
			t.Fatal(err)
		}
		for _, read := range app.inputs {
			if os.SameFile(read.info, info) {
				return true
			}
		}
		return false
	}
	for name, each := range inputs {
		if !recordedBy(nil, each.args, each.input) {
			t.Errorf("--%s: the App did not record the file it read", name)
		}
	}
	for _, each := range []struct {
		name  string
		args  []string
		input string
	}{
		{"the project's trail", base, f.trail},
		{"the project's sidecar", append(append([]string{}, base...), "--public-key", f.publicKey), f.sidecar},
		{"the project's stamps file", append(append([]string{}, base...), "--tsa-roots", f.roots), f.stamps},
		{"the sidecar beside a named trail", []string{"--trail", f.trail, "--witness-key", f.key, "--public-key", f.publicKey}, f.sidecar},
		{"the stamps file beside a named trail", []string{"--trail", f.trail, "--witness-key", f.key, "--tsa-roots", f.roots}, f.stamps},
	} {
		if !recordedBy(nil, each.args, each.input) {
			t.Errorf("%s: the App did not record the file it read", each.name)
		}
	}
	held, err := os.Open(f.expect)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if !recordedBy(held, append(append([]string{}, base...), "--expect", "-"), f.expect) {
		t.Error("an --expect read from standard input that is a file: the App did not record it")
	}
	for name, each := range inputs {
		for spelling, destination := range spellings(t, each.input) {
			if disagreement := saveRefused(t, each.args, destination, each.input, ""); disagreement != "" {
				t.Errorf("--%s, %s: %s", name, spelling, disagreement)
			}
		}
	}
	// The files read without a flag naming them: the project's trail, and the
	// files beside the project's trail and a named trail.
	for _, each := range []struct {
		name  string
		args  []string
		input string
	}{
		{"the project's trail", base, f.trail},
		{"the project's sidecar", append(append([]string{}, base...), "--public-key", f.publicKey), f.sidecar},
		{"the project's stamps file", append(append([]string{}, base...), "--tsa-roots", f.roots), f.stamps},
		{"the sidecar beside a named trail", []string{"--trail", f.trail, "--witness-key", f.key, "--public-key", f.publicKey}, f.sidecar},
		{"the stamps file beside a named trail", []string{"--trail", f.trail, "--witness-key", f.key, "--tsa-roots", f.roots}, f.stamps},
	} {
		for spelling, destination := range spellings(t, each.input) {
			if disagreement := saveRefused(t, each.args, destination, each.input, ""); disagreement != "" {
				t.Errorf("%s, %s: %s", each.name, spelling, disagreement)
			}
		}
	}
	// An input that holds a continuation is refused for being an input, since
	// a continuation is what a save may otherwise replace.
	continuation, err := os.ReadFile(f.resume)
	if err != nil {
		t.Fatal(err)
	}
	holding := func(name string) string { return writeDocument(t, name, string(continuation)) }
	trail, signatures, stampsFile, statements, head := holding("trail.jsonl"), holding("signatures.jsonl"), holding("stamps.jsonl"), holding("statements.jsonl"), holding("head.jsonl")
	for _, each := range []struct {
		name  string
		args  []string
		input string
	}{
		{"--trail", []string{"--trail", trail, "--witness-key", f.key}, trail},
		{"--signatures", append(append([]string{}, base...), "--public-key", f.publicKey, "--signatures", signatures), signatures},
		{"--stamps", append(append([]string{}, base...), "--tsa-roots", f.roots, "--stamps", stampsFile), stampsFile},
		{"--witness", append(append([]string{}, base...), "--witness", statements), statements},
		{"--witness-head", append(append([]string{}, base...), "--witness-head", head), head},
		{"--witness-resume, given as --witness too", append(append([]string{}, base...), "--witness-resume", f.resume, "--witness", f.resume), f.resume},
	} {
		for spelling, destination := range spellings(t, each.input) {
			if disagreement := saveRefused(t, each.args, destination, each.input, "which this verification reads"); disagreement != "" {
				t.Errorf("%s holding a continuation, %s: %s", each.name, spelling, disagreement)
			}
		}
	}
	for _, beside := range []string{f.sidecar, f.stamps} {
		if err := os.WriteFile(beside, continuation, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, each := range []struct {
		name  string
		args  []string
		input string
	}{
		{"the project's sidecar", append(append([]string{}, base...), "--public-key", f.publicKey), f.sidecar},
		{"the project's stamps file", append(append([]string{}, base...), "--tsa-roots", f.roots), f.stamps},
		{"the sidecar beside a named trail", []string{"--trail", f.trail, "--witness-key", f.key, "--public-key", f.publicKey}, f.sidecar},
	} {
		if disagreement := saveRefused(t, each.args, each.input, each.input, "which this verification reads"); disagreement != "" {
			t.Errorf("%s holding a continuation: %s", each.name, disagreement)
		}
	}

	// An input read from standard input, when standard input is a file.
	stdin, err := os.Open(f.expect)
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	before := digestOf(t, f.expect)
	var stdout, stderr bytes.Buffer
	code := Run(append(append([]string{"audit", "verify", "--format", "json"}, base...), "--witness", f.statements, "--expect", "-", "--witness-save", f.expect), stdin, &stdout, &stderr)
	if code != result.ExitInvocation || !strings.Contains(stdout.String(), `"JPS-INVOCATION-AUDIT-WITNESS-SAVE"`) || digestOf(t, f.expect) != before {
		t.Fatalf("standard input from the destination: exit=%d %s", code, first(stdout.String(), 300))
	}
}

// The continuation --witness-resume read is the one input --witness-save may
// name: a reading advances it. A file that is anything but a continuation is
// left as it is, and a destination that is not a regular file is refused; a
// save after any finding writes nothing, and none leaves a temporary file.
func TestAuditVerifySavesOnlyOverAContinuation(t *testing.T) {
	f := newSaveFixture(t)
	base := []string{"--config", f.config, "--witness-key", f.key}

	// From index 0 to a saved continuation, then advanced in place.
	saved := filepath.Join(t.TempDir(), "continuation.json")
	if code, output := verification(t, append(append([]string{}, base...), "--witness", f.statements, "--witness-save", saved)...); code != 0 || !output.Witness.ContinuationSaved {
		t.Fatalf("a first save: exit=%d %+v", code, output.Findings)
	}
	facts := writeDocument(t, "facts.json", hardFailFacts)
	if code, _, stderr := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake", "--config", f.config, "--facts", facts}, ""); code != 0 {
		t.Fatalf("exit=%d %q", code, stderr)
	}
	f.witness.sign("checkpoint", trailCheckpoints(t, f.config)[1])
	next := f.witness.file(t, "next.jsonl", 1, 2)
	code, output := verification(t, append(append([]string{}, base...), "--witness-resume", saved, "--witness", next, "--witness-save", saved)...)
	advanced, err := os.ReadFile(saved)
	if code != 0 || !output.Witness.ContinuationSaved || err != nil ||
		string(advanced) != `{"continuationVersion":"1","last":`+f.witness.lines[1]+`,"latestCheckpoint":`+f.witness.lines[1]+"}\n" {
		t.Fatalf("resume and save one file: exit=%d %+v %q", code, output.Findings, advanced)
	}
	if left := leftovers(t, filepath.Dir(saved)); len(left) > 0 {
		t.Fatalf("left %v", left)
	}
	// A continuation that is no input is replaced.
	other := writeDocument(t, "other.json", string(advanced))
	if code, output := verification(t, append(append([]string{}, base...), "--witness", f.statements, "--witness-save", other)...); code != 0 || !output.Witness.ContinuationSaved ||
		digestOf(t, other) == digestOf(t, saved) {
		t.Fatalf("another continuation: exit=%d %+v", code, output.Findings)
	}

	// Anything else is refused and left as it is.
	notes := writeDocument(t, "notes.txt", "not a continuation\n")
	if disagreement := saveRefused(t, append(append([]string{}, base...), "--witness", f.statements), notes, notes, "holds something other than a continuation"); disagreement != "" {
		t.Fatalf("a file that is not a continuation: %s", disagreement)
	}
	pack := filepath.Join(filepath.Dir(f.config), "packs", "intake-0.1.0.pack.json")
	if disagreement := saveRefused(t, append(append([]string{}, base...), "--witness", f.statements), pack, pack, "holds something other than a continuation"); disagreement != "" {
		t.Fatalf("a pack, which this verification does not read: %s", disagreement)
	}
	directory := t.TempDir()
	if disagreement := saveRefused(t, append(append([]string{}, base...), "--witness", f.statements), directory, f.statements, "is not a regular file"); disagreement != "" {
		t.Fatalf("a directory: %s", disagreement)
	}
	links := t.TempDir()
	linked := filepath.Join(links, "linked.json")
	if err := os.Symlink(other, linked); err == nil {
		if disagreement := saveRefused(t, append(append([]string{}, base...), "--witness", f.statements), linked, other, "is a symbolic link"); disagreement != "" {
			t.Fatalf("a symbolic link to a continuation: %s", disagreement)
		}
		dangling := filepath.Join(links, "dangling.json")
		if err := os.Symlink(filepath.Join(links, "absent.json"), dangling); err != nil {
			t.Fatal(err)
		}
		if disagreement := saveRefused(t, append(append([]string{}, base...), "--witness", f.statements), dangling, f.statements, "is a symbolic link"); disagreement != "" {
			t.Fatalf("a symbolic link to nothing: %s", disagreement)
		}
		if _, err := os.Lstat(filepath.Join(links, "absent.json")); !os.IsNotExist(err) {
			t.Fatal("a save was written through a link to nothing")
		}
	} else if runtime.GOOS != "windows" {
		t.Fatal(err)
	}
	if fifo := makeFIFO(t); fifo != "" {
		if disagreement := saveRefused(t, append(append([]string{}, base...), "--witness", f.statements), fifo, f.statements, "is not a regular file"); disagreement != "" {
			t.Fatalf("a FIFO: %s", disagreement)
		}
	}
	if disagreement := saveRefused(t, append(append([]string{}, base...), "--witness", f.statements), t.TempDir()+string(filepath.Separator), f.statements, "names no file"); disagreement != "" {
		t.Fatalf("a path that names no file: %s", disagreement)
	}
	code, stdout, _ := runTest(t, append(append([]string{"audit", "verify", "--format", "json"}, base...), "--witness", f.statements, "--witness-save", filepath.Join(t.TempDir(), "absent", "continuation.json")), "")
	if code != result.ExitIO || !strings.Contains(stdout, `"JPS-AUDIT-WITNESS-SAVE"`) {
		t.Fatalf("a directory that is not there: exit=%d %s", code, first(stdout, 300))
	}

	// A save after any finding writes nothing.
	empty := t.TempDir()
	for _, failing := range [][]string{
		{"--witness", f.statements, "--require-countersigned-through", "9"},
		{"--witness", f.witness.file(t, "late.jsonl", 1, 2)},
		{"--witness", f.statements, "--expect", writeDocument(t, "wrong.jsonl", strings.Replace(trailCheckpoints(t, f.config)[0], `"sequence":1`, `"sequence":7`, 1)+"\n")},
	} {
		destination := filepath.Join(empty, "continuation.json")
		code, output := verification(t, append(append(append([]string{}, base...), failing...), "--witness-save", destination)...)
		if code != result.ExitInvalid || output.Witness.ContinuationSaved {
			t.Fatalf("%v: exit=%d", failing, code)
		}
		if entries, err := os.ReadDir(empty); err != nil || len(entries) != 0 {
			t.Fatalf("%v: a save after a finding wrote %d file(s)", failing, len(entries))
		}
	}
}

// The file a save would replace is looked up through the directory held as
// well as by its path, so a path that names another file by the time it is
// checked, as when a directory on it was re-pointed after it was opened, does
// not hide an input in the directory the continuation would be written to.
func TestTheDestinationIsLookedUpInTheDirectoryHeld(t *testing.T) {
	held := t.TempDir()
	input := filepath.Join(held, "statements.jsonl")
	if err := os.WriteFile(input, []byte("a statement\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	app := &App{in: strings.NewReader(""), out: &bytes.Buffer{}, errOut: &bytes.Buffer{}}
	file, err := app.openInput(input, "a --witness file")
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	root, err := fssecure.OpenRoot(held)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	elsewhere := filepath.Join(t.TempDir(), "statements.jsonl")
	target := &continuationTarget{dir: root, name: "statements.jsonl", path: elsewhere}
	if refused := app.checkContinuationTarget(target); refused == nil || !strings.Contains(refused.why, "is a --witness file, which this verification reads") {
		t.Fatalf("an input in the directory held, its path naming nothing: %+v", refused)
	}
}

// A refused destination never hides what the verification found. With a
// finding, the report is what it is without --witness-save, exit 1, and the
// refusal is noted beside it; with none, the refusal is the answer, exit 3,
// and says the verification found nothing. Nothing is saved in either, and an
// acceptable destination is written only when nothing was found.
func TestARefusedSaveNeverHidesAFinding(t *testing.T) {
	f := newSaveFixture(t)
	notJSON := writeDocument(t, "bad.jsonl", "not JSON\n")
	finding := []string{"--trail", notJSON, "--witness-key", f.key, "--witness", f.statements}
	clean := []string{"--config", f.config, "--witness-key", f.key, "--witness", f.statements}
	code, without := verification(t, finding...)
	if code != result.ExitInvalid || len(without.Findings) == 0 || without.Findings[0].Name != audit.FindingWitnessTrailMismatch {
		t.Fatalf("the finding without --witness-save: exit=%d %+v", code, without.Findings)
	}
	// A continuation spelled with a space, so the one a save writes differs
	// from it byte for byte.
	acceptable := func() string {
		return writeDocument(t, "continuation.json", strings.Replace(string(readFileBytes(t, f.resume)), `{"continuationVersion"`, `{ "continuationVersion"`, 1))
	}
	refused := func() string { return writeDocument(t, "keep.txt", "keep me\n") }
	const why = "holds something other than a continuation"
	for _, each := range []struct {
		name        string
		args        []string
		destination func() string
		exit        int
		saved       bool
	}{
		{"a finding, the destination acceptable", finding, acceptable, result.ExitInvalid, false},
		{"a finding, the destination refused", finding, refused, result.ExitInvalid, false},
		{"no finding, the destination acceptable", clean, acceptable, 0, true},
		{"no finding, the destination refused", clean, refused, result.ExitInvocation, false},
	} {
		t.Run(each.name, func(t *testing.T) {
			destination := each.destination()
			before := digestOf(t, destination)
			args := append(append([]string{"audit", "verify", "--format", "json"}, each.args...), "--witness-save", destination)
			code, stdout, _ := runTest(t, args, "")
			if code != each.exit || (digestOf(t, destination) != before) != each.saved || len(leftovers(t, filepath.Dir(destination))) != 0 {
				t.Fatalf("exit=%d, saved=%v: %s", code, digestOf(t, destination) != before, first(stdout, 400))
			}
			var output result.AuditVerification
			switch each.exit {
			case result.ExitInvalid:
				if err := json.Unmarshal([]byte(stdout), &output); err != nil || output.FindingsTotal != without.FindingsTotal ||
					output.Findings[0] != without.Findings[0] || output.Witness.ContinuationSaved {
					t.Fatalf("the finding is not reported as it is without --witness-save: %v %+v", err, output.Findings)
				}
				noted := output.Witness.SaveRefused
				if strings.HasSuffix(each.name, "refused") != (noted != "") || (noted != "" && !strings.Contains(noted, why)) {
					t.Fatalf("the refusal noted: %q", noted)
				}
				if strings.Join(output.DoesNotEstablish, "|") != strings.Join(without.DoesNotEstablish, "|") {
					t.Fatal("the sentences differ from the report without --witness-save")
				}
			case result.ExitInvocation:
				if !strings.Contains(stdout, `"JPS-INVOCATION-AUDIT-WITNESS-SAVE"`) || !strings.Contains(stdout, why) ||
					!strings.Contains(stdout, "The verification itself found nothing, and nothing was saved.") {
					t.Fatalf("the refusal: %s", first(stdout, 400))
				}
			default:
				if err := json.Unmarshal([]byte(stdout), &output); err != nil || !output.Witness.ContinuationSaved || output.Witness.SaveRefused != "" {
					t.Fatalf("the save: %v %+v", err, output.Witness)
				}
			}
		})
	}
	// The human report of a finding with a refused destination: the finding,
	// and the refusal noted.
	destination := refused()
	before := digestOf(t, destination)
	code, human, _ := runTest(t, append(append([]string{"audit", "verify"}, finding...), "--witness-save", destination), "")
	if code != result.ExitInvalid || !strings.Contains(human, "- witness-trail-mismatch (line 0): ") ||
		!strings.Contains(human, "note: --witness-save "+destination+" "+why) || digestOf(t, destination) != before {
		t.Fatalf("human: exit=%d %s", code, first(human, 800))
	}
	// A destination refused before the inputs are read, by its directory or
	// its name, waits for the verification too.
	for _, each := range []struct {
		destination, code string
	}{
		{filepath.Join(t.TempDir(), "absent", "continuation.json"), "is in a directory that could not be opened"},
		{t.TempDir() + string(filepath.Separator), "names no file"},
	} {
		code, output := verification(t, append(append([]string{}, finding...), "--witness-save", each.destination)...)
		if code != result.ExitInvalid || output.FindingsTotal == 0 || !strings.Contains(output.Witness.SaveRefused, each.code) {
			t.Fatalf("%s: exit=%d %+v", each.destination, code, output.Witness)
		}
	}
}

// readFileBytes is a file's bytes.
func readFileBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
