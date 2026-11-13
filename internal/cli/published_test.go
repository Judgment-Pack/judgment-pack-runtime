package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/published"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/published/gitfixture"
)

const (
	publishedPackA = `{"specVersion":"0.5.0","id":"expense","version":"1.0.0","outcomes":[{"id":"approve"}]}`
	publishedPackB = `{"specVersion":"0.5.0","id":"expense","version":"1.0.0","outcomes":[{"id":"deny"}]}`
)

func sha256Of(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// taggedHistory builds one tagged commit per map, each the child of the one
// before, and points HEAD at the last.
func taggedHistory(t *testing.T, tags []string, snapshots ...map[string]string) *gitfixture.Repo {
	t.Helper()
	repo := gitfixture.New(t)
	parent := ""
	for index, files := range snapshots {
		var commit string
		if parent == "" {
			commit = repo.Snapshot(files)
		} else {
			commit = repo.Snapshot(files, parent)
		}
		repo.AnnotatedTag(tags[index], commit)
		parent = commit
	}
	repo.Head(parent)
	return repo
}

type publishedEnvelope struct {
	OutputVersion         string `json:"outputVersion"`
	Command               string `json:"command"`
	Status                string `json:"status"`
	PublishedCheckVersion string `json:"publishedCheckVersion"`
	Repository            struct {
		Head    string `json:"head"`
		Shallow bool   `json:"shallow"`
	} `json:"repository"`
	Tags struct {
		Total int `json:"total"`
		Read  int `json:"read"`
	} `json:"tags"`
	Identities      int                    `json:"identities"`
	FindingCount    int                    `json:"findingCount"`
	Findings        []published.Finding    `json:"findings"`
	UnreadableCount int                    `json:"unreadableCount"`
	Unreadable      []published.Unreadable `json:"unreadable"`
	Manifest        *struct {
		NewCount  int    `json:"newCount"`
		WrittenTo string `json:"writtenTo"`
	} `json:"manifest"`
	Diagnostics []struct {
		Code string `json:"code"`
	} `json:"diagnostics"`
}

func checkPublishedJSON(t *testing.T, wantCode int, args ...string) publishedEnvelope {
	t.Helper()
	code, stdout, stderr := runTest(t, append([]string{"experimental", "check-published", "--format", "json"}, args...), "")
	if code != wantCode {
		t.Fatalf("exit %d, want %d: %s %s", code, wantCode, stdout, stderr)
	}
	var envelope publishedEnvelope
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("not one JSON value: %v: %q", err, stdout)
	}
	return envelope
}

func TestCheckPublishedReportsAChangedIdentityInJSON(t *testing.T) {
	repo := taggedHistory(t, []string{"v1", "v2"}, map[string]string{"expense.json": publishedPackA}, map[string]string{"expense.json": publishedPackB})
	envelope := checkPublishedJSON(t, 1, "--repo", repo.Dir)
	if envelope.OutputVersion != "2" || envelope.Command != "experimental check-published" || envelope.Status != "invalid" ||
		envelope.PublishedCheckVersion != published.CheckVersion || envelope.Repository.Shallow || len(envelope.Repository.Head) != 40 ||
		envelope.Tags.Total != 2 || envelope.Tags.Read != 2 || envelope.Identities != 1 || envelope.FindingCount != 1 || len(envelope.Findings) != 1 {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
	finding := envelope.Findings[0]
	if finding.Code != "changed" || finding.Kind != "pack" || finding.ID != "expense" || finding.Version != "1.0.0" ||
		finding.Published.Ref != "refs/tags/v1" || finding.Published.Digest != sha256Of(publishedPackA) || finding.Published.Path != "expense.json" ||
		finding.Found == nil || finding.Found.Ref != "refs/tags/v2" || finding.Found.Digest != sha256Of(publishedPackB) {
		t.Fatalf("unexpected finding: %#v", finding)
	}
	if envelope.Manifest != nil {
		t.Fatal("no manifest member without --manifest or --write-manifest")
	}

	clean := taggedHistory(t, []string{"v1", "v2"}, map[string]string{"expense.json": publishedPackA}, map[string]string{"moved/expense.json": publishedPackA})
	if envelope := checkPublishedJSON(t, 0, "--repo", clean.Dir); envelope.Status != "valid" || envelope.FindingCount != 0 || envelope.Findings == nil || envelope.Unreadable == nil {
		t.Fatalf("an unchanged identity is valid, with empty lists: %#v", envelope)
	}
}

func TestCheckPublishedAnUnreadableDocumentIsIncompleteNotClean(t *testing.T) {
	repo := taggedHistory(t, []string{"v1"}, map[string]string{"expense.json": publishedPackA, "broken.json": `{"specVersion":`})
	envelope := checkPublishedJSON(t, 1, "--repo", repo.Dir)
	if envelope.Status != "incomplete" || envelope.UnreadableCount != 1 || envelope.Unreadable[0].Path != "broken.json" || envelope.Unreadable[0].Ref != "refs/tags/v1" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
	code, stdout, _ := runTest(t, []string{"experimental", "check-published", "--repo", repo.Dir}, "")
	if code != 1 || !strings.HasPrefix(stdout, "incomplete: ") || !strings.Contains(stdout, `unreadable at tag "refs/tags/v1", "broken.json"`) {
		t.Fatalf("exit %d: %s", code, stdout)
	}
}

func TestCheckPublishedHumanOutputQuotesEveryRepositoryName(t *testing.T) {
	repo := gitfixture.New(t)
	odd := "pack\x1b[31m\nname.json"
	first := repo.Snapshot(map[string]string{odd: publishedPackA})
	repo.Tag("-x", first)
	second := repo.Snapshot(map[string]string{odd: publishedPackB}, first)
	repo.Tag("--upload-pack=x", second)
	repo.Head(second)
	code, stdout, stderr := runTest(t, []string{"experimental", "check-published", "--repo", repo.Dir}, "")
	if code != 1 || stderr != "" {
		t.Fatalf("exit %d: %s %s", code, stdout, stderr)
	}
	if strings.ContainsAny(stdout, "\x1b") || strings.Contains(stdout, "\nname.json") {
		t.Fatalf("a name from the repository reached the terminal raw: %q", stdout)
	}
	for _, want := range []string{
		"invalid: 1 finding(s)\n",
		`- changed: pack "expense" version "1.0.0"`,
		`published: tag "refs/tags/-x", "pack\u001b[31m\nname.json", ` + sha256Of(publishedPackA),
		`found:     tag "refs/tags/--upload-pack=x", "pack\u001b[31m\nname.json", ` + sha256Of(publishedPackB),
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("missing %q in:\n%s", want, stdout)
		}
	}
}

func TestCheckPublishedWritesTheManifestAndChecksAgainstIt(t *testing.T) {
	repo := taggedHistory(t, []string{"v1"}, map[string]string{"expense.json": publishedPackA})
	target := filepath.Join(t.TempDir(), "published.json")
	envelope := checkPublishedJSON(t, 0, "--repo", repo.Dir, "--write-manifest", target)
	if envelope.Manifest == nil || envelope.Manifest.WrittenTo != target {
		t.Fatalf("the written manifest must be reported: %#v", envelope.Manifest)
	}
	written, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"manifestVersion\": \"1\",\n  \"documents\": [\n" +
		"    {\"kind\": \"pack\", \"id\": \"expense\", \"version\": \"1.0.0\", \"digest\": \"" + sha256Of(publishedPackA) + "\", \"firstTag\": \"v1\"}\n" +
		"  ]\n}\n"
	if string(written) != want {
		t.Fatalf("manifest bytes:\n%s", written)
	}
	// Checked and written back in one run, the manifest is the same bytes.
	checkPublishedJSON(t, 0, "--repo", repo.Dir, "--manifest", target, "--write-manifest", target)
	if again, _ := os.ReadFile(target); string(again) != want {
		t.Fatalf("rewritten manifest:\n%s", again)
	}

	// A tampered pin is a finding, and writing keeps the pin as it was.
	tampered := strings.Replace(want, sha256Of(publishedPackA), sha256Of(publishedPackB), 1)
	if err := os.WriteFile(target, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}
	envelope = checkPublishedJSON(t, 1, "--repo", repo.Dir, "--manifest", target, "--write-manifest", target)
	if envelope.FindingCount != 1 || envelope.Findings[0].Code != "differs-from-manifest" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
	if again, _ := os.ReadFile(target); string(again) != tampered {
		t.Fatalf("a run must never move a pin to the bytes it found:\n%s", again)
	}

	// Under --strict an identity the manifest does not pin is a finding.
	empty := filepath.Join(t.TempDir(), "empty.json")
	if err := os.WriteFile(empty, []byte("{\"manifestVersion\": \"1\", \"documents\": []}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if envelope := checkPublishedJSON(t, 0, "--repo", repo.Dir, "--manifest", empty); envelope.Manifest == nil || envelope.Manifest.NewCount != 1 {
		t.Fatalf("a new identity is listed, not a failure: %#v", envelope.Manifest)
	}
	if envelope := checkPublishedJSON(t, 1, "--repo", repo.Dir, "--manifest", empty, "--strict"); envelope.Findings[0].Code != "not-in-manifest" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestCheckPublishedReplacesOnlyAManifest(t *testing.T) {
	repo := taggedHistory(t, []string{"v1"}, map[string]string{"expense.json": publishedPackA})
	dir := t.TempDir()
	other := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(other, []byte("module example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	envelope := checkPublishedJSON(t, 3, "--repo", repo.Dir, "--write-manifest", other)
	if len(envelope.Diagnostics) != 1 || envelope.Diagnostics[0].Code != "JPS-INVOCATION-OUTPUT" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
	if data, _ := os.ReadFile(other); string(data) != "module example\n" {
		t.Fatalf("a file that is not a manifest must be left as it is: %q", data)
	}
	if runtime.GOOS != "windows" {
		link := filepath.Join(dir, "link.json")
		if err := os.Symlink(other, link); err != nil {
			t.Fatal(err)
		}
		if envelope := checkPublishedJSON(t, 3, "--repo", repo.Dir, "--write-manifest", link); envelope.Diagnostics[0].Code != "JPS-INVOCATION-OUTPUT" {
			t.Fatalf("unexpected envelope: %#v", envelope)
		}
	}
	checkPublishedJSON(t, 3, "--repo", repo.Dir, "--write-manifest", "-")
}

func TestCheckPublishedRefusalsAndTheirExitStatuses(t *testing.T) {
	repo := taggedHistory(t, []string{"v1"}, map[string]string{"expense.json": publishedPackA})
	manifest := func(text string) string {
		path := filepath.Join(t.TempDir(), "m.json")
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	for _, row := range []struct {
		name string
		args []string
		exit int
		code string
	}{
		{"tag pattern", []string{"--repo", repo.Dir, "--tags", "a//b"}, 3, "JPS-INVOCATION-PATTERN"},
		{"include pattern", []string{"--repo", repo.Dir, "--include", "a/**b"}, 3, "JPS-INVOCATION-PATTERN"},
		{"exclude pattern", []string{"--repo", repo.Dir, "--exclude", "["}, 3, "JPS-INVOCATION-PATTERN"},
		{"strict alone", []string{"--repo", repo.Dir, "--strict"}, 3, "JPS-INVOCATION-STRICT"},
		{"url", []string{"--repo", "https://example.invalid/r.git"}, 3, "JPS-INVOCATION-INPUT"},
		{"unknown flag", []string{"--repo", repo.Dir, "--unknown"}, 3, "JPS-INVOCATION-ARGUMENTS"},
		{"damaged manifest", []string{"--repo", repo.Dir, "--manifest", manifest(`{"manifestVersion": "1"}`)}, 1, "JPS-PUBLISHED-MANIFEST"},
		{"later manifest", []string{"--repo", repo.Dir, "--manifest", manifest(`{"manifestVersion": "2", "documents": []}`)}, 2, "JPS-PUBLISHED-MANIFEST-VERSION"},
		{"empty manifest", []string{"--repo", repo.Dir, "--manifest", manifest("")}, 1, "JPS-PUBLISHED-MANIFEST"},
		{"missing manifest", []string{"--repo", repo.Dir, "--manifest", filepath.Join(t.TempDir(), "absent.json")}, 4, "JPS-INPUT-READ"},
		{"oversized manifest", []string{"--repo", repo.Dir, "--manifest", manifest(strings.Repeat(" ", int(published.MaxManifestBytes)+1))}, 4, "JPS-RESOURCE-INPUT-BYTE-LIMIT"},
		{"no repository", []string{"--repo", t.TempDir()}, 4, "JPS-PUBLISHED-REPOSITORY"},
	} {
		t.Run(row.name, func(t *testing.T) {
			arguments := append([]string{"experimental", "check-published", "--format", "json"}, row.args...)
			code, stdout, _ := runTest(t, arguments, "")
			var envelope publishedEnvelope
			if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
				t.Fatalf("not one JSON value: %v: %q", err, stdout)
			}
			if code != row.exit || envelope.Command != "experimental check-published" || envelope.Status == "valid" || len(envelope.Diagnostics) != 1 || envelope.Diagnostics[0].Code != row.code {
				t.Fatalf("exit %d: %s", code, stdout)
			}
		})
	}
}

func TestCheckPublishedNamesNonUTF8PathsByteForByte(t *testing.T) {
	if got := asciiName("a\xffb.json"); got != `"a\xffb.json"` {
		t.Fatalf("got %s", got)
	}
	if got := asciiName("caf\u00e9 \"\\ \U0001f600"); got != `"caf\u00e9 \"\\ \ud83d\ude00"` {
		t.Fatalf("got %s", got)
	}
	code, _, stderr := runTest(t, []string{"experimental", "check-published", "--format", "xml"}, "")
	if code != 3 || !strings.Contains(stderr, "--format must be human or json") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
}

func TestCheckPublishedWritesNoManifestFromAShallowClone(t *testing.T) {
	source := taggedHistory(t, []string{"v1", "v2"}, map[string]string{"expense.json": publishedPackA}, map[string]string{"expense.json": publishedPackA, "b.json": "{}"})
	clone := gitfixture.New(t)
	clone.Git("", "-c", "protocol.file.allow=always", "fetch", "-q", "--update-head-ok", "--depth", "1", "file://"+filepath.ToSlash(source.Dir), "+refs/heads/main:refs/heads/main")
	target := filepath.Join(t.TempDir(), "published.json")
	envelope := checkPublishedJSON(t, 4, "--repo", clone.Dir, "--write-manifest", target)
	if envelope.Diagnostics[0].Code != published.FailureShallow {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("nothing may be written from a shallow clone: %v", err)
	}
	// Checked against a manifest a full clone wrote, the shallow clone is
	// read, and still no manifest is written from it.
	full := filepath.Join(t.TempDir(), "full.json")
	checkPublishedJSON(t, 0, "--repo", source.Dir, "--write-manifest", full)
	if envelope := checkPublishedJSON(t, 0, "--repo", clone.Dir, "--manifest", full); !envelope.Repository.Shallow {
		t.Fatalf("a shallow clone is said to be one: %#v", envelope)
	}
	envelope = checkPublishedJSON(t, 4, "--repo", clone.Dir, "--manifest", full, "--write-manifest", target)
	if envelope.Diagnostics[0].Code != published.FailureShallow {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("nothing may be written from a shallow clone: %v", err)
	}
	code, _, stderr := runTest(t, []string{"experimental", "check-published", "--repo", clone.Dir}, "")
	if code != 4 || !strings.Contains(stderr, "shallow clone") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
}
