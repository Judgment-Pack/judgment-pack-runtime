package view_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/lock"
	"github.com/Judgment-Pack/judgment-pack-runtime/sdk"
	"github.com/Judgment-Pack/judgment-pack-runtime/sdk/internal/viewhook"
	pv "github.com/Judgment-Pack/judgment-pack-runtime/sdk/view"
)

// updateViewGoldens is the one way the view goldens under
// testdata/golden are written:
//
//	go test ./sdk/view -run TestViewGoldens -update-view-goldens
//
// A golden is never edited by hand. It is read, in review, as the record of
// what the view says about one pack; the hand-written expectations in this
// file and in view_markdown_test.go are what hold the view to the format
// docs/building-with-packs.md describes.
var updateViewGoldens = flag.Bool("update-view-goldens", false, "rewrite the pack view goldens under testdata/golden")

// viewInput is one pack the view must describe.
type viewInput struct {
	// name is the input's place under testdata/golden, without an
	// extension: version, then fixtures, cases or evaluation, then the file.
	name    string
	version string
	pack    []byte
}

// viewVersions are the specification versions Load admits.
var viewVersions = []string{artifacts.EvaluatorDraftVersion, artifacts.CoreVersion}

// viewInputs is every pack the view is held to: the purpose-built fixtures
// under testdata/packs, every conformance case of both admitted versions
// that Load admits — the valid cases, and the one whose only finding is an
// unsupported required extension, which Load admits and Evaluate refuses —
// and every pack of both evaluation corpora.
func viewInputs(t *testing.T) []viewInput {
	t.Helper()
	var inputs []viewInput
	for _, version := range viewVersions {
		fixtures, err := filepath.Glob(filepath.Join("testdata", "packs", version, "*.json"))
		if err != nil || len(fixtures) == 0 {
			t.Fatalf("no view fixtures for %s: %v", version, err)
		}
		for _, fixture := range fixtures {
			data, err := os.ReadFile(fixture)
			if err != nil {
				t.Fatal(err)
			}
			inputs = append(inputs, viewInput{name: path.Join(version, "fixtures", strings.TrimSuffix(filepath.Base(fixture), ".json")), version: version, pack: data})
		}
		set, err := artifacts.Load(version)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range conformanceCases(t, set) {
			if !row.admitted() {
				continue
			}
			data, err := set.Case(row.Path)
			if err != nil {
				t.Fatal(err)
			}
			inputs = append(inputs, viewInput{name: path.Join(version, "cases", strings.TrimSuffix(row.Path, ".json")), version: version, pack: data})
		}
		manifestBytes, err := set.EvaluationManifest()
		if err != nil {
			t.Fatal(err)
		}
		var manifest struct {
			Cases []struct {
				Pack string `json:"pack"`
			} `json:"cases"`
		}
		if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, row := range manifest.Cases {
			if seen[row.Pack] {
				continue
			}
			seen[row.Pack] = true
			data, err := set.EvaluationPack(row.Pack)
			if err != nil {
				t.Fatal(err)
			}
			inputs = append(inputs, viewInput{name: path.Join(version, "evaluation", strings.TrimSuffix(row.Pack, ".json")), version: version, pack: data})
		}
	}
	return inputs
}

type conformanceCase struct {
	ID                 string `json:"id"`
	Path               string `json:"path"`
	ExpectedResult     string `json:"expectedResult"`
	ExpectedDiagnostic struct {
		Code string `json:"code"`
	} `json:"expectedDiagnostic"`
}

// admitted reports whether Load admits the case's pack without supported
// extensions: a valid document, or one whose only finding is a required
// extension the caller does not support.
func (c conformanceCase) admitted() bool {
	return c.ExpectedResult == "valid" || (c.ExpectedResult == "unsupported" && c.ExpectedDiagnostic.Code == "JPS-CAPABILITY-REQUIRED-EXTENSION")
}

func conformanceCases(t *testing.T, set *artifacts.Set) []conformanceCase {
	t.Helper()
	manifestBytes, err := set.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Cases []conformanceCase `json:"cases"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Cases) == 0 {
		t.Fatal("the conformance manifest has no cases")
	}
	return manifest.Cases
}

func mustView(t *testing.T, pack []byte) (*sdk.Pack, pv.PackView) {
	t.Helper()
	loaded, err := sdk.Load(context.Background(), pack, sdk.LoadOptions{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	view, err := loaded.View()
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	return loaded, view
}

// Every pack Load admits views, and the view is the document: its pack member
// is, byte for byte, the canonical form of the document decoded on its own by
// the carrier, and decodes back to that same value. Since the view's JSON is
// written from the typed fields and not from the document, a member the typed
// view dropped, renamed, emptied or invented fails here for every pack that
// holds it. The view is stable across two calls and across a second Load, its
// identity is the pack's, and building it leaves the admitted document as it
// was.
func TestEveryAdmittedPackViewsFaithfullyAndStably(t *testing.T) {
	inputs := viewInputs(t)
	for _, input := range inputs {
		t.Run(input.name, func(t *testing.T) {
			loaded, view := mustView(t, input.pack)
			root, failure := carrier.Decode(input.pack, carrier.DefaultLimits())
			if failure != nil {
				t.Fatalf("carrier.Decode: %+v", failure)
			}
			document, err := pv.CanonicalText(root)
			if err != nil {
				t.Fatal(err)
			}
			want := `{"digest":"` + loaded.Identity().Digest + `","pack":` + document + `,"viewVersion":"1"}`
			if got := string(view.JSON()); got != want {
				t.Fatalf("the view is not the document:\n got %s\nwant %s", got, want)
			}
			var decoded struct {
				Digest      string `json:"digest"`
				Pack        any    `json:"pack"`
				ViewVersion string `json:"viewVersion"`
			}
			decoder := json.NewDecoder(bytes.NewReader(view.JSON()))
			decoder.UseNumber()
			if err := decoder.Decode(&decoded); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(decoded.Pack, root) || decoded.ViewVersion != pv.ViewVersion || decoded.Digest != loaded.Identity().Digest {
				t.Fatalf("the view's JSON does not decode to the document")
			}
			if sdk.Identity(view.Identity) != loaded.Identity() || view.Identity.SpecVersion != input.version {
				t.Fatalf("view identity %+v, pack identity %+v", view.Identity, loaded.Identity())
			}

			// The builder is handed the admission's own root, which every
			// evaluation of the pack shares; it is read before and after one
			// build, as the builder saw it.
			var before, after string
			previous := viewhook.Build
			viewhook.Build = func(root map[string]any, digest string) (any, string) {
				before, _ = pv.CanonicalText(root)
				built, fault := previous(root, digest)
				after, _ = pv.CanonicalText(root)
				return built, fault
			}
			again, err := loaded.View()
			viewhook.Build = previous
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(again, view) || !bytes.Equal(again.JSON(), view.JSON()) || !bytes.Equal(again.Markdown(), view.Markdown()) {
				t.Fatal("two views of one loaded pack differ")
			}
			_, reloaded := mustView(t, slices.Clone(input.pack))
			if !bytes.Equal(reloaded.JSON(), view.JSON()) || !bytes.Equal(reloaded.Markdown(), view.Markdown()) {
				t.Fatal("the view of a second Load of the same bytes differs")
			}
			if before == "" || after != before {
				t.Fatal("building the view changed the admitted document")
			}
			// The returned bytes are the caller's own.
			view.JSON()[0] = 'X'
			view.Markdown()[0] = 'X'
			if !bytes.Equal(again.JSON(), view.JSON()) || !bytes.Equal(again.Markdown(), view.Markdown()) {
				t.Fatal("editing returned bytes changed the view")
			}
		})
	}
}

// The view of every input is held to its golden, byte for byte, in both
// encodings. The goldens are written only by -update-view-goldens.
func TestViewGoldens(t *testing.T) {
	inputs := viewInputs(t)
	expected := map[string]bool{}
	for _, input := range inputs {
		_, view := mustView(t, input.pack)
		for _, encoding := range []struct {
			extension string
			bytes     []byte
		}{{".json", view.JSON()}, {".md", view.Markdown()}} {
			golden := filepath.Join("testdata", "golden", filepath.FromSlash(input.name)+encoding.extension)
			expected[filepath.ToSlash(golden)] = true
			if *updateViewGoldens {
				if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(golden, encoding.bytes, 0o644); err != nil {
					t.Fatal(err)
				}
				continue
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%s: %v (write the goldens with -update-view-goldens)", input.name, err)
			}
			if !bytes.Equal(encoding.bytes, want) {
				t.Errorf("%s: the view differs from %s", input.name, golden)
			}
		}
	}
	// A golden whose input is gone is reported, never left to drift.
	var present []string
	err := filepath.WalkDir(filepath.Join("testdata", "golden"), func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			present = append(present, filepath.ToSlash(name))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(present)
	for _, name := range present {
		if !expected[name] {
			t.Errorf("%s is a golden no input produces", name)
		}
	}
}

// Every conformance case Load refuses is refused, never viewed: the view is
// only ever of an admitted document.
func TestRefusedCasesAreNotViewed(t *testing.T) {
	for _, version := range viewVersions {
		set, err := artifacts.Load(version)
		if err != nil {
			t.Fatal(err)
		}
		refused, crossVersion := 0, 0
		for _, row := range conformanceCases(t, set) {
			if row.admitted() {
				continue
			}
			data, err := set.Case(row.Path)
			if err != nil {
				t.Fatal(err)
			}
			// A case that declares another version is invalid under this
			// set's schema and is judged by Load under the version it
			// declares; this set's manifest says nothing about that.
			var declared struct {
				SpecVersion string `json:"specVersion"`
			}
			if json.Unmarshal(data, &declared) == nil && declared.SpecVersion != version && slices.Contains(viewVersions, declared.SpecVersion) {
				crossVersion++
				continue
			}
			loaded, err := sdk.Load(context.Background(), data, sdk.LoadOptions{})
			var refusal *sdk.Refusal
			if !errors.As(err, &refusal) || loaded != nil {
				t.Fatalf("%s %s: Load = %v, %v; want a refusal", version, row.ID, loaded, err)
			}
			refused++
		}
		if refused == 0 || crossVersion > 2 {
			t.Fatalf("%s: %d refused cases checked, %d skipped as declaring another version", version, refused, crossVersion)
		}
	}
}

// The digest is over the exact bytes given to Load, in the convention of the
// reviewed-set lock: one trailing newline makes another digest, and editing
// the caller's slice after Load changes nothing.
func TestTheDigestIsOverTheExactBytes(t *testing.T) {
	pack := []byte(viewMinimalPack)
	withNewline := append(slices.Clone(pack), '\n')
	first, err := sdk.Load(context.Background(), pack, sdk.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := sdk.Load(context.Background(), withNewline, sdk.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		loaded *sdk.Pack
		bytes  []byte
	}{{first, pack}, {second, withNewline}} {
		sum := sha256.Sum256(check.bytes)
		want := "sha256:" + hex.EncodeToString(sum[:])
		if got := check.loaded.Identity().Digest; got != want || got != lock.Digest(check.bytes) {
			t.Fatalf("digest %s, want %s (lock convention %s)", got, want, lock.Digest(check.bytes))
		}
	}
	if first.Identity().Digest == second.Identity().Digest {
		t.Fatal("a trailing newline did not change the digest")
	}
	digest := first.Identity().Digest
	pack[len(pack)-2] = ' '
	if first.Identity().Digest != digest {
		t.Fatal("editing the caller's slice after Load changed the digest")
	}
	view, err := first.View()
	if err != nil {
		t.Fatal(err)
	}
	if view.Identity.Digest != digest || !strings.Contains(string(view.Markdown()), "\n- Digest: "+digest+"\n") {
		t.Fatal("the view does not carry the pack's digest")
	}
}

// viewMinimalPack is the smallest pack the view tests need, written by hand.
const viewMinimalPack = `{
  "specVersion": "0.3.0",
  "id": "https://example.invalid/judgment-packs/view-minimal",
  "version": "0.1.0",
  "title": "Minimal",
  "decision": {"intent": "Test the view.", "question": "Is it minimal?"},
  "outcomes": [{"id": "yes", "label": "Yes"}, {"id": "no", "label": "No"}],
  "rules": [{"id": "always", "description": "Always yes.", "when": {"op": "literal", "value": true}, "outcome": "yes", "onUnknown": "ignore"}]
}`

// The whole of one view, in both encodings, written by hand from the format
// docs/building-with-packs.md states: the canonical member order, the absent
// members left out, and the Markdown layout.
func TestOneViewByHand(t *testing.T) {
	loaded, view := mustView(t, []byte(viewMinimalPack))
	digest := loaded.Identity().Digest
	wantJSON := `{"digest":"` + digest + `","pack":{"decision":{"intent":"Test the view.","question":"Is it minimal?"},` +
		`"id":"https://example.invalid/judgment-packs/view-minimal","outcomes":[{"id":"yes","label":"Yes"},{"id":"no","label":"No"}],` +
		`"rules":[{"description":"Always yes.","id":"always","onUnknown":"ignore","outcome":"yes","when":{"op":"literal","value":true}}],` +
		`"specVersion":"0.3.0","title":"Minimal","version":"0.1.0"},"viewVersion":"1"}`
	if got := string(view.JSON()); got != wantJSON {
		t.Fatalf("JSON:\n got %s\nwant %s", got, wantJSON)
	}
	wantMarkdown := "# Minimal\n" +
		"\n" +
		"- Id: https\\:\\/\\/example\\.invalid\\/judgment\\-packs\\/view\\-minimal\n" +
		"- Version: 0\\.1\\.0\n" +
		"- Spec version: 0\\.3\\.0\n" +
		"- Digest: " + digest + "\n" +
		"- View version: 1\n" +
		"\n" +
		"## Decision\n" +
		"\n" +
		"- Intent: Test the view\\.\n" +
		"- Question: Is it minimal\\?\n" +
		"\n" +
		"## Outcomes\n" +
		"\n" +
		"| Id | Label | Description |\n" +
		"| --- | --- | --- |\n" +
		"| yes | Yes | |\n" +
		"| no | No | |\n" +
		"\n" +
		"## Rules\n" +
		"\n" +
		"### Rule 1: always\n" +
		"\n" +
		"- Description: Always yes\\.\n" +
		"- Outcome: yes\n" +
		"- On unknown: ignore\n" +
		"- When:\n" +
		"  - literal true\n"
	if got := string(view.Markdown()); got != wantMarkdown {
		t.Fatalf("Markdown:\n got %q\nwant %q", got, wantMarkdown)
	}
}

func fixture(t *testing.T, version, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "packs", version, name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// Rules and exceptions keep document order, which is not the order of their
// ids, and so does every operand of a compound condition.
func TestRulesKeepDocumentOrder(t *testing.T) {
	_, view := mustView(t, fixture(t, artifacts.EvaluatorDraftVersion, "nested-conditions"))
	ids := []string{}
	for _, rule := range view.Rules {
		ids = append(ids, rule.ID)
	}
	if !slices.Equal(ids, []string{"zeta", "alpha", "mu"}) {
		t.Fatalf("rule order %v", ids)
	}
	markdown := string(view.Markdown())
	first, second, third := strings.Index(markdown, "### Rule 1: zeta\n"), strings.Index(markdown, "### Rule 2: alpha\n"), strings.Index(markdown, "### Rule 3: mu\n")
	if first < 0 || second < first || third < second {
		t.Fatalf("the Markdown does not keep rule order:\n%s", markdown)
	}
	inner := view.Rules[0].When.Condition.Condition.Conditions[0].Conditions[0].Condition.Conditions
	if inner[0].Path != "/z" || inner[1].Path != "/a" {
		t.Fatalf("operand order %q, %q", inner[0].Path, inner[1].Path)
	}
	if !strings.Contains(markdown, "\n              - fact \\\"\\/z\\\" equals \\\"last\\\"\n              - fact \\\"\\/a\\\" equals \\\"first\\\"\n") {
		t.Fatalf("the Markdown does not keep operand order and nesting:\n%s", markdown)
	}
	_, every := mustView(t, fixture(t, artifacts.EvaluatorDraftVersion, "every-member"))
	exceptions := []string{}
	for _, exception := range every.Exceptions {
		exceptions = append(exceptions, exception.ID)
	}
	if !slices.Equal(exceptions, []string{"auditor-hold", "trusted-vendor", "new-vendor"}) {
		t.Fatalf("exception order %v", exceptions)
	}
}

// A member that is present and empty is not an absent one, in the typed
// view, in the JSON and in the Markdown.
func TestAbsentStaysAbsentAndEmptyStaysEmpty(t *testing.T) {
	_, empty := mustView(t, fixture(t, artifacts.EvaluatorDraftVersion, "absent-and-empty"))
	if empty.EvidenceRequirements == nil || len(empty.EvidenceRequirements) != 0 ||
		empty.Sources == nil || len(empty.Sources) != 0 ||
		empty.Exceptions == nil || len(empty.Exceptions) != 0 ||
		empty.Rules[0].EvidenceRequirementRefs == nil || len(empty.Rules[0].EvidenceRequirementRefs) != 0 ||
		empty.Rules[0].SourceRefs == nil || len(empty.Rules[0].SourceRefs) != 0 ||
		empty.Rules[0].Extensions == nil || empty.Decision.Extensions == nil || empty.Outcomes[0].Extensions == nil ||
		empty.Extensions == nil || len(empty.Extensions) != 0 || empty.Outcomes[1].Extensions != nil {
		t.Fatalf("present empty members are not kept: %+v", empty)
	}
	if empty.Metadata == nil || !reflect.DeepEqual(*empty.Metadata, pv.Metadata{}) {
		t.Fatalf("an empty metadata object is %+v", empty.Metadata)
	}
	json := string(empty.JSON())
	for _, member := range []string{`"evidenceRequirements":[]`, `"sources":[]`, `"exceptions":[]`, `"evidenceRequirementRefs":[]`, `"sourceRefs":[]`, `"metadata":{}`, `"extensions":{}`} {
		if !strings.Contains(json, member) {
			t.Errorf("the JSON lacks %s: %s", member, json)
		}
	}
	markdown := string(empty.Markdown())
	for _, line := range []string{
		"## Evidence requirements\n\n*empty*\n",
		"## Sources\n\n*empty*\n",
		"## Exceptions\n\n*empty*\n",
		"## Metadata\n\n*empty*\n",
		"## Extensions\n\n*empty*\n",
		"- Evidence requirement refs: *empty*\n",
		"- Source refs: *empty*\n",
		"- Extensions: *empty*\n",
		"### Extensions of outcome 1: yes\n\n*empty*\n",
	} {
		if !strings.Contains(markdown, line) {
			t.Errorf("the Markdown lacks %q:\n%s", line, markdown)
		}
	}

	_, absent := mustView(t, []byte(viewMinimalPack))
	if absent.EvidenceRequirements != nil || absent.Sources != nil || absent.Exceptions != nil || absent.Applicability != nil ||
		absent.Escalation != nil || absent.Metadata != nil || absent.Extensions != nil || absent.Decision.Extensions != nil ||
		absent.Rules[0].EvidenceRequirementRefs != nil || absent.Rules[0].SourceRefs != nil || absent.Rules[0].Extensions != nil ||
		absent.Description != "" || absent.FallbackOutcome != "" || absent.Identity.Profile != "" {
		t.Fatalf("absent members are not absent: %+v", absent)
	}
	for _, text := range []string{"evidenceRequirements", "sources", "exceptions", "metadata", "extensions", "profile", "Profile", "Evidence requirements", "Sources", "Exceptions", "Metadata", "Extensions", "Fallback", "Escalation", "Applicability"} {
		if strings.Contains(string(absent.JSON()), text) || strings.Contains(string(absent.Markdown()), text) {
			t.Errorf("an absent member shows as %q", text)
		}
	}

	// The schema's default profile is not a statement the document made.
	_, stated := mustView(t, fixture(t, artifacts.CoreVersion, "every-member"))
	if stated.Identity.Profile != "base" || !strings.Contains(string(stated.JSON()), `"profile":"base"`) || !strings.Contains(string(stated.Markdown()), "\n- Profile: base\n") {
		t.Fatal("a stated profile is not carried")
	}
}

// Numbers are carried as the literal tokens the document holds, and decimal
// strings as the strings they are; nothing is normalized.
func TestNumbersAndDecimalStringsAsWritten(t *testing.T) {
	_, view := mustView(t, fixture(t, artifacts.CoreVersion, "numbers-as-written"))
	values := []string{}
	for _, condition := range view.Rules[0].When.Conditions {
		values = append(values, condition.Value)
	}
	want := []string{`1.50`, `1e2`, `1E+2`, `-0`, `0.000`, `123456789012345678901234567890`, `-1.5e-10`, `[1.0,"1.0",1,"1"]`, `"1000.50"`, `"-0.0"`, `"0.10"`, `"1.50"`}
	if !slices.Equal(values, want) {
		t.Fatalf("values %q\nwant   %q", values, want)
	}
	if got := view.Rules[0].Extensions; len(got) != 1 || got[0].Value != `{"high":1E-3,"low":2.500}` {
		t.Fatalf("extension value %+v", got)
	}
	for _, text := range []string{`"value":1.50}`, `"value":1E+2}`, `"value":"-0.0"}`} {
		if !strings.Contains(string(view.JSON()), text) {
			t.Errorf("the JSON lacks %s", text)
		}
	}
	for _, line := range []string{
		"  - fact \\\"\\/a\\\" equals 1\\.50\n",
		"  - fact \\\"\\/c\\\" equals 1E\\+2\n",
		"  - fact \\\"\\/h\\\" in \\[1\\.0\\,\\\"1\\.0\\\"\\,1\\,\\\"1\\\"\\]\n",
		"  - fact \\\"\\/j\\\" less\\-than \\\"\\-0\\.0\\\"\n",
	} {
		if !strings.Contains(string(view.Markdown()), line) {
			t.Errorf("the Markdown lacks %q", line)
		}
	}
}

// Extensions are ordered by the UTF-16 code units of their names, which for a
// supplementary-plane name is not the UTF-8 byte order.
func TestExtensionValuesAreCanonicalJSON(t *testing.T) {
	_, view := mustView(t, fixture(t, artifacts.CoreVersion, "adversarial-strings"))
	got := view.Decision.Extensions
	want := "{\"a\":\"ascii\",\"\u00e9\":\"accent\",\"\U0001F600\":\"emoji\",\"\ufb01\":\"ligature\"}"
	if len(got) != 1 || got[0].Name != "com.example.order" || got[0].Value != want {
		t.Fatalf("extensions %+v\nwant value %s", got, want)
	}
	names := []string{}
	for _, extension := range view.Extensions {
		names = append(names, extension.Name)
	}
	if !slices.Equal(names, []string{"com.example.html", "com.example.nested"}) {
		t.Fatalf("extension names %v", names)
	}
}

// The zero PackView has no encodings, and a copy of a view shares its slices
// with the original while neither copy's cached encodings follow an edit.
func TestTheZeroViewAndCopiesOfAView(t *testing.T) {
	var zero pv.PackView
	if zero.JSON() != nil || zero.Markdown() != nil {
		t.Fatalf("the zero PackView has encodings: %q %q", zero.JSON(), zero.Markdown())
	}
	_, view := mustView(t, []byte(viewMinimalPack))
	before, beforeMarkdown := view.JSON(), view.Markdown()
	copied := view
	copied.Rules[0].ID = "edited"
	if view.Rules[0].ID != "edited" {
		t.Fatal("a copy of a PackView does not share its slices")
	}
	if !bytes.Equal(view.JSON(), before) || !bytes.Equal(copied.JSON(), before) || !bytes.Equal(copied.Markdown(), beforeMarkdown) {
		t.Fatal("an encoding followed an edit of the fields")
	}
}
