package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// These tests are about packs covers (ADR-0072): whether a reviewed-set lock
// covers one document's exact bytes, with no project opened. Every lock here
// is written by hand, its digests computed in the test from the bytes it pins,
// so nothing the command reports is compared with something the code under
// test produced. The pack bytes are not a pack the evaluator would admit: the
// command compares bytes and parses no pack, and a fixture that happened to
// validate would hide a reader that started to.

const (
	coversPack   = "{\"pack\":\"reviewed bytes\"}\n"
	coversGraph  = "{\"graph\":\"reviewed bytes\"}\n"
	coversConfig = `{"configVersion":"2","packs":{"intake":{"path":"packs/intake.json"}},"graphs":{"flow":{"path":"graphs/flow.json"}}}`
)

// sha is one document's digest in the form the lock writes, computed here and
// not by the package under test.
func sha(document string) string {
	sum := sha256.Sum256([]byte(document))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// quoted is one string as the payload encodes it.
func quoted(t *testing.T, value string) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// handLock is a lock document written by hand: the configuration digest it
// pins, and one pack and one graph entry.
func handLock(configDigest, packPath, packDigest, graphPath, graphDigest string) string {
	return `{"lockVersion":"1","config":{"digest":"` + configDigest + `"},` +
		`"packs":{"intake":{"path":"` + packPath + `","digest":"` + packDigest + `"}},` +
		`"graphs":{"flow":{"path":"` + graphPath + `","digest":"` + graphDigest + `"}}}`
}

// coversFiles is one directory holding a configuration, the documents it
// declares, and a hand-written lock over them, plus copies of the documents
// outside it, which is where the caller this command serves keeps them.
type coversFiles struct {
	dir, config, lock, pack, graph string
	lockText                       string
}

func coversFixture(t *testing.T) coversFiles {
	t.Helper()
	dir := t.TempDir()
	files := coversFiles{
		dir:      dir,
		config:   filepath.Join(dir, "project", "jpack.json"),
		lock:     filepath.Join(dir, "project", "jpack.lock.json"),
		pack:     filepath.Join(dir, "frozen", "intake.json"),
		graph:    filepath.Join(dir, "frozen", "flow.json"),
		lockText: handLock(sha(coversConfig), "packs/intake.json", sha(coversPack), "graphs/flow.json", sha(coversGraph)),
	}
	for path, body := range map[string]string{
		files.config: coversConfig,
		files.lock:   files.lockText,
		files.pack:   coversPack,
		files.graph:  coversGraph,
		filepath.Join(dir, "project", "packs", "intake.json"): coversPack,
		filepath.Join(dir, "project", "graphs", "flow.json"):  coversGraph,
	} {
		writeCoversFile(t, path, body)
	}
	return files
}

func writeCoversFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// coversPayload is the exact JSON line packs covers writes, assembled by hand.
// digestMember is packDigest or graphDigest; entry and configPath are omitted
// when empty, and findings is the inside of the array.
func coversPayload(t *testing.T, status, lockPath, lockText, configDigest, configPath, id, digestMember, digest, entry, findings string) string {
	t.Helper()
	config := ""
	if configPath != "" {
		config = `,"configPath":` + quoted(t, configPath)
	}
	if entry != "" {
		entry = `,"entry":` + entry
	}
	return `{"outputVersion":"2","tool":{"name":"jpack","version":"` + result.CLIVersion + `"},"command":"packs covers",` +
		`"status":"` + status + `","kind":"non-normative-runtime-convention","lockPath":` + quoted(t, lockPath) +
		`,"lockDigest":"` + sha(lockText) + `","lockVersion":"1","configDigest":"` + configDigest + `"` + config +
		`,"id":"` + id + `","` + digestMember + `":"` + digest + `"` + entry + `,"findings":[` + findings + `]}` + "\n"
}

// refusal is the exact operational envelope one refusal writes.
func refusal(t *testing.T, status, code, message string) string {
	t.Helper()
	return `{"outputVersion":"2","tool":{"name":"jpack","version":"` + result.CLIVersion + `"},"command":"packs covers","status":"` + status +
		`","diagnostics":[{"code":"` + code + `","codeStability":"provisional","layer":"operation","severity":"error","instancePath":"","message":` +
		quoted(t, message) + `}]}` + "\n"
}

func packEntry(path, digest string) string {
	return `{"kind":"pack","id":"intake","path":"` + path + `","digest":"` + digest + `"}`
}

func finding(t *testing.T, name, kind, id, path, detail string) string {
	t.Helper()
	text := `{"name":"` + name + `"`
	if kind != "" {
		text += `,"kind":"` + kind + `"`
	}
	if id != "" {
		text += `,"id":"` + id + `"`
	}
	if path != "" {
		text += `,"path":` + quoted(t, path)
	}
	return text + `,"detail":` + quoted(t, detail) + `}`
}

func runCovers(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	return runTest(t, append([]string{"packs", "covers"}, args...), "")
}

// A lock covers bytes when its entry for the id pins exactly them, and the
// report says so in both formats, exit 0.
func TestPacksCoversReportsBytesItsLockCovers(t *testing.T) {
	files := coversFixture(t)

	code, stdout, stderr := runCovers(t, "--lock", files.lock, "--id", "intake", "--pack", files.pack, "--format", "json")
	want := coversPayload(t, "valid", files.lock, files.lockText, sha(coversConfig), "", "intake", "packDigest", sha(coversPack),
		packEntry("packs/intake.json", sha(coversPack)), "")
	if code != 0 || stderr != "" || stdout != want {
		t.Fatalf("exit=%d stderr=%q\n got %s\nwant %s", code, stderr, stdout, want)
	}

	code, stdout, stderr = runCovers(t, "--lock", files.lock, "--id", "intake", "--pack", files.pack)
	wantHuman := "covered: the reviewed set's entry for the pack intake pins these exact bytes\n" +
		"- bytes: " + sha(coversPack) + "\n" +
		"- entry: " + sha(coversPack) + " (packs/intake.json)\n" +
		"- configuration: " + sha(coversConfig) + ", as the lock pins it\n" +
		files.lock + " (lockVersion 1, " + sha(files.lockText) + ") · non-normative-runtime-convention\n"
	if code != 0 || stderr != "" || stdout != wantHuman {
		t.Fatalf("exit=%d stderr=%q\n got %q\nwant %q", code, stderr, stdout, wantHuman)
	}

	// The configuration the lock pins, declaring the id at the entry's path,
	// adds nothing to report and is named in the payload as compared.
	code, stdout, stderr = runCovers(t, "--lock", files.lock, "--id", "intake", "--pack", files.pack, "--config", files.config, "--format", "json")
	want = coversPayload(t, "valid", files.lock, files.lockText, sha(coversConfig), files.config, "intake", "packDigest", sha(coversPack),
		packEntry("packs/intake.json", sha(coversPack)), "")
	if code != 0 || stderr != "" || stdout != want {
		t.Fatalf("exit=%d stderr=%q\n got %s\nwant %s", code, stderr, stdout, want)
	}
	code, stdout, _ = runCovers(t, "--lock", files.lock, "--id", "intake", "--pack", files.pack, "--config", files.config)
	if code != 0 || !strings.Contains(stdout, "- configuration: "+sha(coversConfig)+", as the lock pins it, compared with "+files.config+"\n") {
		t.Fatalf("exit=%d stdout=%q", code, stdout)
	}
}

// Each way bytes can fail to be covered is its own named finding, with packs
// verify's name and, where the condition is the same one, its words; two at
// once are two findings, in packs verify's order. Every one exits 1.
func TestPacksCoversNamesEachWayBytesAreNotCovered(t *testing.T) {
	const (
		drifted       = "{\"pack\":\"edited bytes\"}\n"
		renamedConfig = `{"configVersion":"2","packs":{"intake":{"path":"packs/renamed.json"}},"graphs":{"flow":{"path":"graphs/flow.json"}}}`
		otherConfig   = `{"configVersion":"2","packs":{"other":{"path":"packs/other.json"}},"graphs":{"flow":{"path":"graphs/flow.json"}}}`
	)
	driftDetail := "The pack document's bytes differ from the reviewed set."
	configDetail := "The configuration's own bytes differ from the reviewed set. Adding, removing, or re-pointing a declared pack is an amendment."
	mismatchDetail := `The reviewed set records this pack at "packs/intake.json", and the configuration declares it at "packs/renamed.json".`
	undeclaredDetail := "The reviewed set names this pack and the configuration no longer declares it."

	for _, c := range []struct {
		name       string
		lock       string // the lock's text; the fixture's when empty
		pack       string // the bytes handed over; the reviewed ones when empty
		id         string
		config     string // a configuration to write and pass; none when empty
		configPins string // the configuration digest the lock pins
		entry      string
		findings   func(configPath string) string
	}{
		{
			name: "document-drift", pack: drifted, id: "intake", configPins: sha(coversConfig),
			entry: packEntry("packs/intake.json", sha(coversPack)),
			findings: func(string) string {
				return finding(t, "document-drift", "pack", "intake", "packs/intake.json", driftDetail)
			},
		},
		{
			name: "lock-entry-missing", id: "absent", configPins: sha(coversConfig),
			findings: func(string) string {
				return finding(t, "lock-entry-missing", "pack", "absent", "", "The reviewed set does not name this pack.")
			},
		},
		{
			// The configuration declares the id and the lock does not: packs
			// verify's own words for the same condition. Its bytes are not the
			// ones the lock pins either, which is a second finding.
			name: "lock-entry-missing for an id the configuration declares", id: "other", config: otherConfig, configPins: sha(coversConfig),
			findings: func(configPath string) string {
				return finding(t, "config-drift", "", "", configPath, configDetail) + "," +
					finding(t, "lock-entry-missing", "pack", "other", "packs/other.json", "The configuration declares this pack and the reviewed set does not name it.")
			},
		},
		{
			name: "config-drift", id: "intake", config: coversConfig + "\n", configPins: sha(coversConfig),
			entry: packEntry("packs/intake.json", sha(coversPack)),
			findings: func(configPath string) string {
				return finding(t, "config-drift", "", "", configPath, configDetail)
			},
		},
		{
			// A lock pinning the renamed configuration's own bytes, whose entry
			// still records the old path: the digest of the configuration matches
			// and only the path differs.
			name: "path-mismatch", id: "intake", config: renamedConfig, configPins: sha(renamedConfig),
			lock:  handLock(sha(renamedConfig), "packs/intake.json", sha(coversPack), "graphs/flow.json", sha(coversGraph)),
			entry: packEntry("packs/intake.json", sha(coversPack)),
			findings: func(string) string {
				return finding(t, "path-mismatch", "pack", "intake", "packs/renamed.json", mismatchDetail)
			},
		},
		{
			name: "locked-but-undeclared", id: "intake", config: otherConfig, configPins: sha(otherConfig),
			lock:  handLock(sha(otherConfig), "packs/intake.json", sha(coversPack), "graphs/flow.json", sha(coversGraph)),
			entry: packEntry("packs/intake.json", sha(coversPack)),
			findings: func(string) string {
				return finding(t, "locked-but-undeclared", "pack", "intake", "packs/intake.json", undeclaredDetail)
			},
		},
		{
			name: "document-drift and config-drift together", pack: drifted, id: "intake", config: coversConfig + "\n", configPins: sha(coversConfig),
			entry: packEntry("packs/intake.json", sha(coversPack)),
			findings: func(configPath string) string {
				return finding(t, "config-drift", "", "", configPath, configDetail) + "," +
					finding(t, "document-drift", "pack", "intake", "packs/intake.json", driftDetail)
			},
		},
		{
			name: "config-drift, path-mismatch and document-drift together", pack: drifted, id: "intake", config: renamedConfig, configPins: sha(coversConfig),
			entry: packEntry("packs/intake.json", sha(coversPack)),
			findings: func(configPath string) string {
				return finding(t, "config-drift", "", "", configPath, configDetail) + "," +
					finding(t, "path-mismatch", "pack", "intake", "packs/renamed.json", mismatchDetail) + "," +
					finding(t, "document-drift", "pack", "intake", "packs/renamed.json", driftDetail)
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			files := coversFixture(t)
			lockText := files.lockText
			if c.lock != "" {
				lockText = c.lock
				writeCoversFile(t, files.lock, lockText)
			}
			pack := coversPack
			if c.pack != "" {
				pack = c.pack
				writeCoversFile(t, files.pack, pack)
			}
			args := []string{"--lock", files.lock, "--id", c.id, "--pack", files.pack, "--format", "json"}
			configPath := ""
			if c.config != "" {
				configPath = filepath.Join(files.dir, "compared", "jpack.json")
				writeCoversFile(t, configPath, c.config)
				args = append(args, "--config", configPath)
			}
			code, stdout, stderr := runCovers(t, args...)
			want := coversPayload(t, "invalid", files.lock, lockText, c.configPins, configPath, c.id, "packDigest", sha(pack), c.entry, c.findings(configPath))
			if code != result.ExitInvalid || stderr != "" || stdout != want {
				t.Fatalf("exit=%d stderr=%q\n got %s\nwant %s", code, stderr, stdout, want)
			}
		})
	}

	// The human form of two findings at once: each on its own line, in order,
	// with the steer packs verify gives.
	files := coversFixture(t)
	writeCoversFile(t, files.pack, drifted)
	configPath := filepath.Join(files.dir, "compared", "jpack.json")
	writeCoversFile(t, configPath, coversConfig+"\n")
	code, stdout, stderr := runCovers(t, "--lock", files.lock, "--id", "intake", "--pack", files.pack, "--config", configPath)
	want := "invalid: the reviewed set does not cover these bytes of the pack intake (2 difference(s))\n" +
		"- bytes: " + sha(drifted) + "\n" +
		"- entry: " + sha(coversPack) + " (packs/intake.json)\n" +
		"- configuration: " + sha(coversConfig) + ", as the lock pins it, compared with " + configPath + "\n" +
		"- config-drift: " + configPath + " — " + configDetail + "\n" +
		"- document-drift: intake (packs/intake.json) — " + driftDetail + "\n" +
		"Run jpack packs lock to declare the amendment, or restore the reviewed bytes.\n" +
		files.lock + " (lockVersion 1, " + sha(files.lockText) + ") · non-normative-runtime-convention\n"
	if code != result.ExitInvalid || stderr != "" || stdout != want {
		t.Fatalf("exit=%d stderr=%q\n got %q\nwant %q", code, stderr, stdout, want)
	}

	// An id the lock does not name, in human form: no entry line, and the
	// finding names the id alone because there is no path to name.
	code, stdout, _ = runCovers(t, "--lock", files.lock, "--id", "absent", "--pack", files.pack)
	want = "invalid: the reviewed set does not cover these bytes of the pack absent (1 difference(s))\n" +
		"- bytes: " + sha(drifted) + "\n" +
		"- configuration: " + sha(coversConfig) + ", as the lock pins it\n" +
		"- lock-entry-missing: absent — The reviewed set does not name this pack.\n" +
		"Run jpack packs lock to declare the amendment, or restore the reviewed bytes.\n" +
		files.lock + " (lockVersion 1, " + sha(files.lockText) + ") · non-normative-runtime-convention\n"
	if code != result.ExitInvalid || stdout != want {
		t.Fatalf("exit=%d\n got %q\nwant %q", code, stdout, want)
	}
}

// A graph is the same question against the lock's graphs, and a pack entry of
// the same id is never what a graph is compared with.
func TestPacksCoversAnswersForAGraphFromTheGraphEntries(t *testing.T) {
	files := coversFixture(t)
	graphEntry := `{"kind":"graph","id":"flow","path":"graphs/flow.json","digest":"` + sha(coversGraph) + `"}`

	code, stdout, stderr := runCovers(t, "--lock", files.lock, "--graph-id", "flow", "--graph", files.graph, "--config", files.config, "--format", "json")
	want := coversPayload(t, "valid", files.lock, files.lockText, sha(coversConfig), files.config, "flow", "graphDigest", sha(coversGraph), graphEntry, "")
	if code != 0 || stderr != "" || stdout != want {
		t.Fatalf("exit=%d stderr=%q\n got %s\nwant %s", code, stderr, stdout, want)
	}
	code, stdout, _ = runCovers(t, "--lock", files.lock, "--graph-id", "flow", "--graph", files.graph)
	if code != 0 || !strings.HasPrefix(stdout, "covered: the reviewed set's entry for the graph flow pins these exact bytes\n- bytes: "+sha(coversGraph)+"\n") {
		t.Fatalf("exit=%d stdout=%q", code, stdout)
	}

	// The pack's bytes handed over as the graph are not the graph.
	code, stdout, _ = runCovers(t, "--lock", files.lock, "--graph-id", "flow", "--graph", files.pack, "--format", "json")
	want = coversPayload(t, "invalid", files.lock, files.lockText, sha(coversConfig), "", "flow", "graphDigest", sha(coversPack), graphEntry,
		finding(t, "document-drift", "graph", "flow", "graphs/flow.json", "The graph document's bytes differ from the reviewed set."))
	if code != result.ExitInvalid || stdout != want {
		t.Fatalf("exit=%d\n got %s\nwant %s", code, stdout, want)
	}

	// A lock whose pack and graph share an id: each kind answers from its own
	// entries, so the graph's bytes are not covered by the pack entry.
	shared := `{"lockVersion":"1","config":{"digest":"` + sha(coversConfig) + `"},` +
		`"packs":{"flow":{"path":"packs/flow.json","digest":"` + sha(coversPack) + `"}},` +
		`"graphs":{"flow":{"path":"graphs/flow.json","digest":"` + sha(coversGraph) + `"}}}`
	writeCoversFile(t, files.lock, shared)
	code, stdout, _ = runCovers(t, "--lock", files.lock, "--graph-id", "flow", "--graph", files.pack, "--format", "json")
	want = coversPayload(t, "invalid", files.lock, shared, sha(coversConfig), "", "flow", "graphDigest", sha(coversPack), graphEntry,
		finding(t, "document-drift", "graph", "flow", "graphs/flow.json", "The graph document's bytes differ from the reviewed set."))
	if code != result.ExitInvalid || stdout != want {
		t.Fatalf("exit=%d\n got %s\nwant %s", code, stdout, want)
	}
	code, _, _ = runCovers(t, "--lock", files.lock, "--id", "flow", "--pack", files.pack)
	if code != 0 {
		t.Fatalf("the pack entry of the same id covers the pack's bytes: exit=%d", code)
	}
}

// A lock, document or configuration that cannot be read, or a lock that is not
// the shape its generator writes, is a refusal: never a finding, never
// "covered", and with the code the other readers of those files give.
func TestPacksCoversRefusesWhatItCannotRead(t *testing.T) {
	type refused struct {
		code     int
		status   string
		diagCode string
		message  string
	}
	for _, c := range []struct {
		name  string
		setup func(t *testing.T, files coversFiles) []string
		want  func(files coversFiles) refused
	}{
		{
			// The doubled period is the shared reader's existing text: the
			// carrier's sentence ends in one and the lock's adds another. It is
			// pinned as it is because packs verify and every deciding surface
			// give the same bytes.
			name: "a lock that is not one JSON value",
			setup: func(t *testing.T, files coversFiles) []string {
				writeCoversFile(t, files.lock, files.lockText+`{}`)
				return []string{"--lock", files.lock, "--id", "intake", "--pack", files.pack}
			},
			want: func(files coversFiles) refused {
				return refused{result.ExitInvalid, "error", "JPS-LOCK-JSON", "The reviewed-set lock " + files.lock +
					" is not acceptable JSON: Input contains trailing data after the JSON value.. It is generated: run jpack packs lock to write it again."}
			},
		},
		{
			// The strict decode every other reader of the file applies: a
			// duplicate member is a defect, not a last-one-wins.
			name: "a lock with a duplicate member",
			setup: func(t *testing.T, files coversFiles) []string {
				writeCoversFile(t, files.lock, strings.Replace(files.lockText, `"lockVersion":"1"`, `"lockVersion":"1","lockVersion":"1"`, 1))
				return []string{"--lock", files.lock, "--id", "intake", "--pack", files.pack}
			},
			want: func(files coversFiles) refused {
				return refused{result.ExitInvalid, "error", "JPS-LOCK-JSON", "The reviewed-set lock " + files.lock +
					" is not acceptable JSON: Object member name is duplicated.. It is generated: run jpack packs lock to write it again."}
			},
		},
		{
			name: "a lock that is not an object",
			setup: func(t *testing.T, files coversFiles) []string {
				writeCoversFile(t, files.lock, `["lockVersion"]`)
				return []string{"--lock", files.lock, "--id", "intake", "--pack", files.pack}
			},
			want: func(files coversFiles) refused {
				return refused{result.ExitInvalid, "error", "JPS-LOCK-SHAPE", "The reviewed-set lock " + files.lock +
					" must be a JSON object. It is generated: run jpack packs lock to write it again."}
			},
		},
		{
			name: "a lock with an entry with no digest",
			setup: func(t *testing.T, files coversFiles) []string {
				writeCoversFile(t, files.lock, `{"lockVersion":"1","config":{"digest":"`+sha(coversConfig)+`"},"packs":{"intake":{"path":"packs/intake.json"}}}`)
				return []string{"--lock", files.lock, "--id", "intake", "--pack", files.pack}
			},
			want: func(files coversFiles) refused {
				return refused{result.ExitInvalid, "error", "JPS-LOCK-SHAPE", "The reviewed-set lock " + files.lock +
					` declares the pack "intake" with no usable digest. It is generated: run jpack packs lock to write it again.`}
			},
		},
		{
			name: "a lock from a later toolchain",
			setup: func(t *testing.T, files coversFiles) []string {
				writeCoversFile(t, files.lock, `{"lockVersion":"2"}`)
				return []string{"--lock", files.lock, "--id", "intake", "--pack", files.pack}
			},
			want: func(files coversFiles) refused {
				return refused{result.ExitUnsupported, "unsupported", "JPS-LOCK-VERSION", "The reviewed-set lock " + files.lock +
					` declares lockVersion "2", which this runtime does not read. It reads: 1.`}
			},
		},
		{
			name: "a lock past the byte limit",
			setup: func(t *testing.T, files coversFiles) []string {
				writeCoversFile(t, files.lock, files.lockText+strings.Repeat(" ", int(project.MaxLockBytes)+1-len(files.lockText)))
				return []string{"--lock", files.lock, "--id", "intake", "--pack", files.pack}
			},
			want: func(files coversFiles) refused {
				return refused{result.ExitIO, "error", "JPS-RESOURCE-LOCK-BYTE-LIMIT", "The reviewed-set lock " + files.lock +
					" exceeds the 1048576-byte limit every reader of this file applies; nothing was compared."}
			},
		},
		{
			name: "a lock that is not there",
			setup: func(t *testing.T, files coversFiles) []string {
				return []string{"--lock", filepath.Join(files.dir, "absent.lock.json"), "--id", "intake", "--pack", files.pack}
			},
			want: func(files coversFiles) refused {
				return refused{result.ExitIO, "error", "JPS-LOCK-READ", "The reviewed-set lock " + filepath.Join(files.dir, "absent.lock.json") +
					" could not be read as one bounded regular file; nothing was compared."}
			},
		},
		{
			name: "a pack that is a directory",
			setup: func(t *testing.T, files coversFiles) []string {
				return []string{"--lock", files.lock, "--id", "intake", "--pack", filepath.Dir(files.pack)}
			},
			want: func(files coversFiles) refused {
				return refused{result.ExitIO, "error", "JPS-INPUT-READ", "The --pack file " + filepath.Dir(files.pack) +
					" could not be read as one bounded regular file; nothing was compared."}
			},
		},
		{
			name: "a pack that is not there",
			setup: func(t *testing.T, files coversFiles) []string {
				return []string{"--lock", files.lock, "--id", "intake", "--pack", filepath.Join(files.dir, "absent.json")}
			},
			want: func(files coversFiles) refused {
				return refused{result.ExitIO, "error", "JPS-INPUT-READ", "The --pack file " + filepath.Join(files.dir, "absent.json") +
					" could not be read as one bounded regular file; nothing was compared."}
			},
		},
		{
			name: "a graph past the byte limit",
			setup: func(t *testing.T, files coversFiles) []string {
				writeCoversFile(t, files.graph, strings.Repeat(" ", 1<<20+1))
				return []string{"--lock", files.lock, "--graph-id", "flow", "--graph", files.graph}
			},
			want: func(files coversFiles) refused {
				return refused{result.ExitIO, "error", "JPS-RESOURCE-INPUT-BYTE-LIMIT", "The --graph file " + files.graph +
					" exceeds the 1048576-byte limit a project reads a declared graph within; nothing was compared."}
			},
		},
		{
			name: "a pack past the byte limit",
			setup: func(t *testing.T, files coversFiles) []string {
				writeCoversFile(t, files.pack, strings.Repeat(" ", 10<<20+1))
				return []string{"--lock", files.lock, "--id", "intake", "--pack", files.pack}
			},
			want: func(files coversFiles) refused {
				return refused{result.ExitIO, "error", "JPS-RESOURCE-INPUT-BYTE-LIMIT", "The --pack file " + files.pack +
					" exceeds the 10485760-byte limit a project reads a declared pack within; nothing was compared."}
			},
		},
		{
			name: "a configuration that is not there",
			setup: func(t *testing.T, files coversFiles) []string {
				return []string{"--lock", files.lock, "--id", "intake", "--pack", files.pack, "--config", filepath.Join(files.dir, "absent.json")}
			},
			want: func(files coversFiles) refused {
				return refused{result.ExitIO, "error", "JPS-PROJECT-CONFIG-READ", "The --config file " + filepath.Join(files.dir, "absent.json") +
					" could not be read as one bounded regular file; nothing was compared."}
			},
		},
		{
			// The project reader's own refusal, code and words: one reader of
			// jpack.json, not a second one here.
			name: "a configuration that is not one JSON value",
			setup: func(t *testing.T, files coversFiles) []string {
				writeCoversFile(t, files.config, coversConfig+`{}`)
				return []string{"--lock", files.lock, "--id", "intake", "--pack", files.pack, "--config", files.config}
			},
			want: func(files coversFiles) refused {
				return refused{result.ExitInvocation, "error", "JPS-PROJECT-CONFIG-JSON", "The project configuration " + files.config +
					" is not acceptable JSON: Input contains trailing data after the JSON value."}
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			files := coversFixture(t)
			args := c.setup(t, files)
			want := c.want(files)
			code, stdout, stderr := runCovers(t, append(args, "--format", "json")...)
			wantJSON := refusal(t, want.status, want.diagCode, want.message)
			if code != want.code || stderr != "" || stdout != wantJSON {
				t.Fatalf("exit=%d stderr=%q\n got %s\nwant %s", code, stderr, stdout, wantJSON)
			}
			code, stdout, stderr = runCovers(t, args...)
			if want.status == "unsupported" {
				if code != want.code || stderr != "" || stdout != "unsupported: "+want.message+"\n" {
					t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
				}
				return
			}
			if code != want.code || stdout != "" || stderr != "error: "+want.message+"\n" {
				t.Fatalf("exit=%d stdout=%q\n got %q\nwant %q", code, stdout, stderr, "error: "+want.message+"\n")
			}
		})
	}
}

// A final symbolic link is not followed, for the pack, the graph or the lock:
// the bytes compared are the bytes of the file named, or nothing is compared.
func TestPacksCoversDoesNotFollowAFinalSymbolicLink(t *testing.T) {
	files := coversFixture(t)
	packLink := filepath.Join(files.dir, "frozen", "link.json")
	if err := os.Symlink(files.pack, packLink); err != nil {
		t.Skipf("symbolic links are unavailable here: %v", err)
	}
	lockLink := filepath.Join(files.dir, "link.lock.json")
	if err := os.Symlink(files.lock, lockLink); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := runCovers(t, "--lock", files.lock, "--id", "intake", "--pack", packLink, "--format", "json")
	want := refusal(t, "error", "JPS-INPUT-READ", "The --pack file "+packLink+" could not be read as one bounded regular file; nothing was compared.")
	if code != result.ExitIO || stdout != want {
		t.Fatalf("exit=%d\n got %s\nwant %s", code, stdout, want)
	}
	code, stdout, _ = runCovers(t, "--lock", files.lock, "--graph-id", "flow", "--graph", packLink, "--format", "json")
	want = refusal(t, "error", "JPS-INPUT-READ", "The --graph file "+packLink+" could not be read as one bounded regular file; nothing was compared.")
	if code != result.ExitIO || stdout != want {
		t.Fatalf("exit=%d\n got %s\nwant %s", code, stdout, want)
	}
	code, stdout, _ = runCovers(t, "--lock", lockLink, "--id", "intake", "--pack", files.pack, "--format", "json")
	want = refusal(t, "error", "JPS-LOCK-READ", "The reviewed-set lock "+lockLink+" could not be read as one bounded regular file; nothing was compared.")
	if code != result.ExitIO || stdout != want {
		t.Fatalf("exit=%d\n got %s\nwant %s", code, stdout, want)
	}
}

// A run names one document, by its id and the file holding its bytes, and a
// lock; anything else is an invocation error, never a guess at what was meant.
func TestPacksCoversRefusesABadInvocation(t *testing.T) {
	files := coversFixture(t)
	for _, c := range []struct {
		name    string
		args    []string
		code    string
		message string
	}{
		{"no lock", []string{"--id", "intake", "--pack", files.pack}, "JPS-INVOCATION-COVERS",
			"--lock names the reviewed-set lock to compare against, and is required."},
		{"no document", []string{"--lock", files.lock}, "JPS-INVOCATION-COVERS",
			"Name the document to check: --id and --pack for a pack, or --graph-id and --graph for a graph."},
		{"an id with no pack", []string{"--lock", files.lock, "--id", "intake"}, "JPS-INVOCATION-COVERS",
			"Name the document to check: --id and --pack for a pack, or --graph-id and --graph for a graph."},
		{"a pack with no id", []string{"--lock", files.lock, "--pack", files.pack}, "JPS-INVOCATION-COVERS",
			"Name the document to check: --id and --pack for a pack, or --graph-id and --graph for a graph."},
		{"a pack and a graph", []string{"--lock", files.lock, "--id", "intake", "--graph-id", "flow", "--pack", files.pack, "--graph", files.graph}, "JPS-INVOCATION-COVERS",
			"--id names a pack and --graph-id a graph; name one document per run."},
		{"an id with a graph file", []string{"--lock", files.lock, "--id", "intake", "--pack", files.pack, "--graph", files.graph}, "JPS-INVOCATION-COVERS",
			"--graph goes with --graph-id; a pack's bytes are given with --pack."},
		{"a graph id with a pack file", []string{"--lock", files.lock, "--graph-id", "flow", "--graph", files.graph, "--pack", files.pack}, "JPS-INVOCATION-COVERS",
			"--pack goes with --id; a graph's bytes are given with --graph."},
		{"the pack from standard input", []string{"--lock", files.lock, "--id", "intake", "--pack", "-"}, "JPS-INVOCATION-INPUT",
			"--pack takes a file path, not -: packs covers reads no standard input."},
		{"the lock from standard input", []string{"--lock", "-", "--id", "intake", "--pack", files.pack}, "JPS-INVOCATION-INPUT",
			"--lock takes a file path, not -: packs covers reads no standard input."},
		{"the configuration from standard input", []string{"--lock", files.lock, "--id", "intake", "--pack", files.pack, "--config", "-"}, "JPS-INVOCATION-INPUT",
			"--config takes a file path, not -: packs covers reads no standard input."},
		{"a lock at a URL", []string{"--lock", "https://example.invalid/jpack.lock.json", "--id", "intake", "--pack", files.pack}, "JPS-INVOCATION-INPUT",
			"URL and remote filesystem inputs are not supported for --lock; use a local file."},
	} {
		t.Run(c.name, func(t *testing.T) {
			code, stdout, stderr := runCovers(t, append(c.args, "--format", "json")...)
			want := refusal(t, "error", c.code, c.message)
			if code != result.ExitInvocation || stderr != "" || stdout != want {
				t.Fatalf("exit=%d stderr=%q\n got %s\nwant %s", code, stderr, stdout, want)
			}
		})
	}
	code, stdout, _ := runCovers(t, "--lock", files.lock, "--id", "intake", "--pack", files.pack, "--format", "text")
	if code != result.ExitInvocation || stdout != "" {
		t.Fatalf("an unknown format is refused: exit=%d stdout=%q", code, stdout)
	}
}

// The command opens no project: run in a directory whose jpack.json and lock
// are both broken, with $JPACK_CONFIG naming a broken file too, it answers
// exactly as it does anywhere else. packs verify in the same place refuses,
// which is what shows the place is broken for anything that opens a project.
func TestPacksCoversOpensNoProject(t *testing.T) {
	files := coversFixture(t)
	want := coversPayload(t, "valid", files.lock, files.lockText, sha(coversConfig), "", "intake", "packDigest", sha(coversPack),
		packEntry("packs/intake.json", sha(coversPack)), "")

	broken := t.TempDir()
	writeCoversFile(t, filepath.Join(broken, "jpack.json"), `{broken`)
	writeCoversFile(t, filepath.Join(broken, "jpack.lock.json"), `{broken`)
	t.Chdir(broken)
	t.Setenv(project.ConfigEnv, filepath.Join(broken, "jpack.json"))

	if code, _, stderr := runTest(t, []string{"packs", "verify"}, ""); code == 0 || !strings.Contains(stderr, "jpack.json") {
		t.Fatalf("packs verify must refuse this directory: exit=%d stderr=%q", code, stderr)
	}
	code, stdout, stderr := runCovers(t, "--lock", files.lock, "--id", "intake", "--pack", files.pack, "--format", "json")
	if code != 0 || stderr != "" || stdout != want {
		t.Fatalf("exit=%d stderr=%q\n got %s\nwant %s", code, stderr, stdout, want)
	}
	// Relative names resolve against the working directory, as every other
	// file argument does, and still nothing there but the named files is read.
	writeCoversFile(t, filepath.Join(broken, "frozen.json"), coversPack)
	code, stdout, _ = runCovers(t, "--lock", files.lock, "--id", "intake", "--pack", "frozen.json", "--format", "json")
	if code != 0 || stdout != want {
		t.Fatalf("exit=%d\n got %s\nwant %s", code, stdout, want)
	}
}

// The lock packs lock writes is a lock packs covers reads: the generator and
// this reader agree on the bytes a project's own pack and graph have.
func TestPacksCoversReadsTheLockPacksLockWrites(t *testing.T) {
	files := coversFixture(t)
	if err := os.Remove(files.lock); err != nil {
		t.Fatal(err)
	}
	mustLock(t, files.config)
	if code, stdout, _ := runCovers(t, "--lock", files.lock, "--id", "intake", "--pack", files.pack, "--config", files.config); code != 0 {
		t.Fatalf("exit=%d stdout=%q", code, stdout)
	}
	if code, stdout, _ := runCovers(t, "--lock", files.lock, "--graph-id", "flow", "--graph", files.graph, "--config", files.config); code != 0 {
		t.Fatalf("exit=%d stdout=%q", code, stdout)
	}
	writeCoversFile(t, files.pack, coversPack+" ")
	if code, stdout, _ := runCovers(t, "--lock", files.lock, "--id", "intake", "--pack", files.pack); code != result.ExitInvalid || !strings.Contains(stdout, "- document-drift: intake (packs/intake.json)") {
		t.Fatalf("exit=%d stdout=%q", code, stdout)
	}
}

// refusalOf is the exact operational envelope one refusal of another command
// writes.
func refusalOf(t *testing.T, command, status, code, message string) string {
	t.Helper()
	return strings.Replace(refusal(t, status, code, message), `"command":"packs covers"`, `"command":`+quoted(t, command), 1)
}

// A lock that carries a member packs lock does not write is refused by every
// reader, here and at the deciding gate, with the same code and words. The
// member here is a second spelling of "packs" under Unicode case folding,
// holding an entry for bytes the exact member does not pin: read loosely, it
// would be the entry compared.
func TestALockMemberPacksLockDoesNotWriteIsRefusedByEveryReader(t *testing.T) {
	const longS = "\u017f" // LATIN SMALL LETTER LONG S, which folds to "s"
	const tail = " that packs lock does not write. It is generated: run jpack packs lock to write it again."
	edited := coversPack + " "
	files := coversFixture(t)
	variant := strings.TrimSuffix(files.lockText, "}") + `,"pack` + longS + `":{"intake":{"path":"packs/intake.json","digest":"` + sha(edited) + `"}}}`
	writeCoversFile(t, files.lock, variant)
	writeCoversFile(t, files.pack, edited)
	writeCoversFile(t, filepath.Join(files.dir, "project", "packs", "intake.json"), edited)
	message := "The reviewed-set lock " + files.lock + ` has a member "pack` + longS + `"` + tail

	code, stdout, stderr := runCovers(t, "--lock", files.lock, "--id", "intake", "--pack", files.pack, "--format", "json")
	if want := refusal(t, "error", "JPS-LOCK-SHAPE", message); code != result.ExitInvalid || stderr != "" || stdout != want {
		t.Fatalf("packs covers: exit=%d stderr=%q\n got %s\nwant %s", code, stderr, stdout, want)
	}
	code, stdout, stderr = runTest(t, []string{"packs", "verify", "--config", files.config, "--format", "json"}, "")
	if want := refusalOf(t, "packs verify", "error", "JPS-LOCK-SHAPE", message); code != result.ExitInvalid || stderr != "" || stdout != want {
		t.Fatalf("packs verify: exit=%d stderr=%q\n got %s\nwant %s", code, stderr, stdout, want)
	}

	// The deciding gate: the lock packs lock wrote, a drifted pack, and a
	// second spelling of "packs" naming the drifted bytes. Nothing is
	// evaluated and nothing is recorded.
	configPath := auditProject(t)
	mustLock(t, configPath)
	packPath := filepath.Join(filepath.Dir(configPath), "packs", "intake-0.1.0.pack.json")
	body, err := os.ReadFile(packPath)
	if err != nil {
		t.Fatal(err)
	}
	drifted := append(body, '\n')
	if err := os.WriteFile(packPath, drifted, 0o600); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(lockPath(configPath))
	if err != nil {
		t.Fatal(err)
	}
	spelled := strings.TrimSuffix(string(written), "}\n") + `,"Packs":{"intake":{"path":"packs/intake-0.1.0.pack.json","digest":"` + sha(string(drifted)) + `"}}}` + "\n"
	if err := os.WriteFile(lockPath(configPath), []byte(spelled), 0o600); err != nil {
		t.Fatal(err)
	}
	facts := writeDocument(t, "facts.json", hardFailFacts)
	code, stdout, stderr = runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake", "--config", configPath, "--facts", facts, "--format", "json"}, "")
	want := refusalOf(t, "experimental evaluate", "error", "JPS-LOCK-SHAPE", "The reviewed-set lock "+lockPath(configPath)+` has a member "Packs"`+tail)
	if code != result.ExitInvalid || stderr != "" || stdout != want {
		t.Fatalf("experimental evaluate: exit=%d stderr=%q\n got %s\nwant %s", code, stderr, stdout, want)
	}
	noAuditTrail(t, configPath)
}

// A FIFO named as the lock is refused at once rather than waited on, and a
// configuration named through a final symbolic link is refused as every other
// file argument here is.
func TestPacksCoversRefusesAFIFOAndALinkedConfiguration(t *testing.T) {
	files := coversFixture(t)
	t.Run("a FIFO as --lock", func(t *testing.T) {
		fifo := makeFIFO(t)
		if fifo == "" {
			t.Skip("FIFOs are unavailable here")
		}
		type run struct {
			code           int
			stdout, stderr string
		}
		done := make(chan run, 1)
		go func() {
			var stdout, stderr bytes.Buffer
			code := Run([]string{"packs", "covers", "--lock", fifo, "--id", "intake", "--pack", files.pack, "--format", "json"}, strings.NewReader(""), &stdout, &stderr)
			done <- run{code, stdout.String(), stderr.String()}
		}()
		select {
		case got := <-done:
			want := refusal(t, "error", "JPS-LOCK-READ", "The reviewed-set lock "+fifo+" could not be read as one bounded regular file; nothing was compared.")
			if got.code != result.ExitIO || got.stderr != "" || got.stdout != want {
				t.Fatalf("exit=%d stderr=%q\n got %s\nwant %s", got.code, got.stderr, got.stdout, want)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("packs covers waited on a FIFO named as the lock")
		}
	})
	t.Run("a final symbolic link as --config", func(t *testing.T) {
		link := filepath.Join(files.dir, "project", "linked.json")
		if err := os.Symlink(files.config, link); err != nil {
			t.Skipf("symbolic links are unavailable here: %v", err)
		}
		code, stdout, stderr := runCovers(t, "--lock", files.lock, "--id", "intake", "--pack", files.pack, "--config", link, "--format", "json")
		want := refusal(t, "error", "JPS-PROJECT-CONFIG-READ", "The --config file "+link+" could not be read as one bounded regular file; nothing was compared.")
		if code != result.ExitIO || stderr != "" || stdout != want {
			t.Fatalf("exit=%d stderr=%q\n got %s\nwant %s", code, stderr, stdout, want)
		}
	})
}
