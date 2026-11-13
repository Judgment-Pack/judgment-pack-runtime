package baseline

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

const (
	digestA = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	digestB = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	digestC = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	outcome = `{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"approve","reasons":[]}`
	decline = `{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"decline","reasons":["r1"]}`
)

// basePack is one pack's baseline: two rows, one disposition and one error.
func basePack() Pack {
	return Pack{
		ID: "intake", PackID: "urn:pack", PackVersion: "0.1.0", SpecVersion: "0.3.0",
		PackDigest: digestA, MatrixDigest: digestB,
		Rows: []Row{
			{ID: "approve", Status: "passed", Disposition: outcome},
			{ID: "broken", Status: "passed", ErrorClass: "malformed-input", ErrorPhase: "preflight"},
		},
	}
}

// candidateRun is the run that gives exactly basePack's results.
func candidateRun() project.PackRun {
	return project.PackRun{
		ID: "intake", Ran: true, PackID: "urn:pack", PackVersion: "0.1.0", SpecVersion: "0.3.0",
		PackDigest: digestA, MatrixDigest: digestB,
		Rows: []result.EvaluationCorpusCase{
			{ID: "approve", Status: "passed", Expected: outcome, Actual: outcome},
			{ID: "broken", Status: "passed", ExpectedErrorClass: "malformed-input", ActualErrorClass: "malformed-input", ExpectedErrorPhase: "preflight", ActualErrorPhase: "preflight"},
		},
	}
}

func source(form string, packs ...Pack) Source {
	return Source{Form: form, Version: "1", Tool: result.Tool{Name: "jpack", Version: "0.45.0"}, Packs: packs}
}

type wantFinding struct {
	kind    string
	row     string
	members []string
}

// The comparison, case by case: what each kind of change to one side is
// reported as, and that nothing else is.
func TestCompare(t *testing.T) {
	for _, test := range []struct {
		name      string
		base      func(*Pack)
		form      string
		candidate func(*project.PackRun)
		noMatrix  []string
		extra     []project.PackRun
		status    string
		bytes     string
		findings  []wantFinding
	}{
		{name: "identical results", status: "same", bytes: "same"},
		{name: "a changed disposition", status: "different", bytes: "same",
			candidate: func(run *project.PackRun) { run.Rows[0].Actual = decline },
			findings:  []wantFinding{{FindingRowDiffers, "approve", []string{"disposition"}}}},
		{name: "a changed error class and phase", status: "different", bytes: "same",
			candidate: func(run *project.PackRun) {
				run.Rows[1].ActualErrorClass, run.Rows[1].ActualErrorPhase = "unsupported-extension", "evaluation"
			},
			findings: []wantFinding{{FindingRowDiffers, "broken", []string{"errorClass", "errorPhase"}}}},
		{name: "an error where a disposition was", status: "different", bytes: "same",
			candidate: func(run *project.PackRun) {
				run.Rows[0].Actual, run.Rows[0].ActualErrorClass, run.Rows[0].Status = "", "malformed-input", "mismatch"
			},
			findings: []wantFinding{{FindingRowDiffers, "approve", []string{"disposition", "errorClass", "status"}}}},
		{name: "a changed status alone", status: "different", bytes: "same",
			candidate: func(run *project.PackRun) { run.Rows[0].Status = "mismatch" },
			findings:  []wantFinding{{FindingRowDiffers, "approve", []string{"status"}}}},
		{name: "a handoff target that appears", status: "different", bytes: "same",
			candidate: func(run *project.PackRun) { run.Rows[0].ActualHandoffTarget = `{"kind":"queue","name":"q"}` },
			findings:  []wantFinding{{FindingRowDiffers, "approve", []string{"handoffTarget"}}}},
		{name: "a row only in the baseline", status: "different", bytes: "same",
			candidate: func(run *project.PackRun) { run.Rows = run.Rows[:1] },
			findings:  []wantFinding{{FindingRowOnlyInBaseline, "broken", nil}}},
		{name: "a row only in the candidate", status: "different", bytes: "same",
			candidate: func(run *project.PackRun) {
				run.Rows = append(run.Rows, result.EvaluationCorpusCase{ID: "added", Status: "passed", Actual: outcome})
			},
			findings: []wantFinding{{FindingRowOnlyInCandidate, "added", nil}}},
		{name: "a pack whose bytes changed", status: "different", bytes: "different",
			candidate: func(run *project.PackRun) { run.PackDigest = digestC },
			findings:  []wantFinding{{FindingPackBytes, "", nil}}},
		{name: "a matrix whose bytes changed", status: "different", bytes: "different",
			candidate: func(run *project.PackRun) { run.MatrixDigest = digestC },
			findings:  []wantFinding{{FindingMatrixBytes, "", nil}}},
		{name: "a baseline from another specVersion", status: "different", bytes: "same",
			base:     func(pack *Pack) { pack.SpecVersion = "0.2.0-draft" },
			findings: []wantFinding{{FindingSpecVersion, "", nil}}},
		{name: "another pack identity", status: "different", bytes: "same",
			candidate: func(run *project.PackRun) { run.PackVersion = "0.2.0" },
			findings:  []wantFinding{{FindingPackIdentity, "", []string{"packVersion"}}}},
		{name: "another pack id", status: "different", bytes: "same",
			candidate: func(run *project.PackRun) { run.PackID = "urn:other" },
			findings:  []wantFinding{{FindingPackIdentity, "", []string{"packId"}}}},
		{name: "a pack the configuration no longer declares", status: "different", bytes: "not-compared",
			candidate: func(run *project.PackRun) { run.ID = "renamed" },
			findings:  []wantFinding{{FindingPackOnlyInBaseline, "", nil}, {FindingPackOnlyInCandidate, "", nil}}},
		{name: "a pack declared with no matrix", status: "different", bytes: "not-compared",
			candidate: func(run *project.PackRun) { run.ID = "other" }, noMatrix: []string{"intake"},
			findings: []wantFinding{{FindingPackOnlyInBaseline, "", nil}, {FindingPackOnlyInCandidate, "", nil}}},
		{name: "a pack only in the candidate", status: "different", bytes: "same",
			extra:    []project.PackRun{{ID: "zeta", Ran: true, Rows: []result.EvaluationCorpusCase{{ID: "r", Status: "passed"}}}},
			findings: []wantFinding{{FindingPackOnlyInCandidate, "", nil}}},
		{name: "a pack whose matrix did not run", status: "different", bytes: "not-compared",
			candidate: func(run *project.PackRun) {
				*run = project.PackRun{ID: "intake", Detail: "The file could not be read."}
			},
			findings: []wantFinding{{FindingPackNotRun, "", nil}}},
		{name: "a report records no digest", status: "same", bytes: "not-recorded", form: FormReport,
			base: func(pack *Pack) { pack.PackDigest, pack.MatrixDigest = "", "" }},
		{name: "a report's rows are still compared", status: "different", bytes: "not-recorded", form: FormReport,
			base:      func(pack *Pack) { pack.PackDigest, pack.MatrixDigest = "", "" },
			candidate: func(run *project.PackRun) { run.Rows[0].Actual = decline },
			findings:  []wantFinding{{FindingRowDiffers, "approve", []string{"disposition"}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			base, run := basePack(), candidateRun()
			if test.base != nil {
				test.base(&base)
			}
			if test.candidate != nil {
				test.candidate(&run)
			}
			form := FormBaseline
			if test.form != "" {
				form = test.form
			}
			comparison, err := Compare([]Source{source(form, base)}, append([]project.PackRun{run}, test.extra...), test.noMatrix)
			if err != nil {
				t.Fatal(err)
			}
			if comparison.Status != test.status {
				t.Fatalf("status = %q, want %q: %+v", comparison.Status, test.status, comparison.Findings)
			}
			got := []wantFinding{}
			for _, finding := range comparison.Findings {
				got = append(got, wantFinding{finding.Kind, finding.Row, finding.Members})
			}
			want := test.findings
			if want == nil {
				want = []wantFinding{}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("findings = %+v, want %+v", got, want)
			}
			if comparison.Summary.Findings != len(want) {
				t.Fatalf("summary counts %d findings, want %d", comparison.Summary.Findings, len(want))
			}
			var intake *result.RevalidatedPack
			for index := range comparison.Packs {
				if comparison.Packs[index].ID == "intake" {
					intake = &comparison.Packs[index]
				}
			}
			if intake == nil {
				t.Fatalf("the baseline's pack is not reported: %+v", comparison.Packs)
			}
			if intake.Bytes != test.bytes {
				t.Fatalf("bytes = %q, want %q", intake.Bytes, test.bytes)
			}
			if test.bytes == "not-recorded" && comparison.Summary.BytesNotRecorded != 1 {
				t.Fatalf("a pack whose bytes were not recorded must be counted: %+v", comparison.Summary)
			}
			if test.status == "same" && comparison.Summary.RowsSame != 2 {
				t.Fatalf("a same result compares every row: %+v", comparison.Summary)
			}
		})
	}
}

// A finding carries both canonical sides, so a reader sees what changed
// without rerunning either runtime.
func TestRowDiffersCarriesBothCanonicalForms(t *testing.T) {
	run := candidateRun()
	run.Rows[0].Actual = decline
	comparison, err := Compare([]Source{source(FormBaseline, basePack())}, []project.PackRun{run}, nil)
	if err != nil {
		t.Fatal(err)
	}
	finding := comparison.Findings[0]
	wantWas := `{"disposition":"{\"handoff\":{\"state\":\"none\"},\"kind\":\"outcome\",\"outcomeId\":\"approve\",\"reasons\":[]}","id":"approve","status":"passed"}`
	wantNow := `{"disposition":"{\"handoff\":{\"state\":\"none\"},\"kind\":\"outcome\",\"outcomeId\":\"decline\",\"reasons\":[\"r1\"]}","id":"approve","status":"passed"}`
	if finding.Baseline != wantWas || finding.Candidate != wantNow {
		t.Fatalf("finding sides =\n%s\n%s", finding.Baseline, finding.Candidate)
	}
}

// A pack two baselines hold is refused rather than chosen between.
func TestComparePackInTwoBaselinesIsRefused(t *testing.T) {
	_, err := Compare([]Source{source(FormBaseline, basePack()), source(FormReport, basePack())}, []project.PackRun{candidateRun()}, nil)
	if err == nil || !strings.Contains(err.Error(), "more than one baseline") {
		t.Fatalf("err = %v", err)
	}
}

// The canonical form is RFC 8785 over the row's members, with the empty
// optional members absent: written out by hand here.
func TestRowCanonicalForm(t *testing.T) {
	for _, test := range []struct {
		row  Row
		want string
	}{
		{Row{ID: "a", Status: "passed"}, `{"id":"a","status":"passed"}`},
		{Row{ID: "b", Status: "mismatch", Disposition: `{"kind":"x"}`, HandoffTarget: `{"kind":"queue","name":"<q>"}`},
			`{"disposition":"{\"kind\":\"x\"}","handoffTarget":"{\"kind\":\"queue\",\"name\":\"<q>\"}","id":"b","status":"mismatch"}`},
		{Row{ID: "c", Status: "passed", ErrorClass: "malformed-input", ErrorPhase: "preflight"},
			`{"errorClass":"malformed-input","errorPhase":"preflight","id":"c","status":"passed"}`},
	} {
		got, err := test.row.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		if got != test.want {
			t.Fatalf("canonical = %s, want %s", got, test.want)
		}
	}
}

// The writer's bytes, written out by hand: the layout is fixed, so the same
// results always write the same file.
func TestEncodeWritesTheDocumentByteForByte(t *testing.T) {
	encoded, err := Encode(result.Tool{Name: "jpack", Version: "0.46.0"}, []Pack{basePack()})
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "baselineVersion": "1",
  "tool": {
    "name": "jpack",
    "version": "0.46.0"
  },
  "packs": [
    {
      "id": "intake",
      "packId": "urn:pack",
      "packVersion": "0.1.0",
      "specVersion": "0.3.0",
      "packDigest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      "matrixDigest": "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
      "rows": [
        {
          "id": "approve",
          "status": "passed",
          "disposition": "{\"handoff\":{\"state\":\"none\"},\"kind\":\"outcome\",\"outcomeId\":\"approve\",\"reasons\":[]}"
        },
        {
          "id": "broken",
          "status": "passed",
          "errorClass": "malformed-input",
          "errorPhase": "preflight"
        }
      ]
    }
  ]
}
`
	if string(encoded) != want {
		t.Fatalf("encoded =\n%s", encoded)
	}
	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.Packs, []Pack{basePack()}) || decoded.Form != FormBaseline || decoded.Tool.Version != "0.46.0" {
		t.Fatalf("round trip = %+v", decoded)
	}
}

// The writer refuses what its own reader would refuse.
func TestEncodeRefusesWhatItCannotReadBack(t *testing.T) {
	for name, change := range map[string]func(*Pack){
		"a digest of another form": func(pack *Pack) { pack.PackDigest = strings.ToUpper(pack.PackDigest) },
		"a row with no status":     func(pack *Pack) { pack.Rows[0].Status = "" },
		"no rows":                  func(pack *Pack) { pack.Rows = nil },
	} {
		t.Run(name, func(t *testing.T) {
			pack := basePack()
			change(&pack)
			if _, err := Encode(result.Tool{Name: "jpack", Version: "0.46.0"}, []Pack{pack}); err == nil {
				t.Fatal("the writer wrote a baseline its reader refuses")
			}
		})
	}
	if _, err := Encode(result.Tool{Name: "jpack", Version: "0.46.0"}, nil); err == nil {
		t.Fatal("the writer wrote a baseline of nothing")
	}
}

// A baseline past MaxBytes is not written: the reader would refuse it, and the
// refusal is the byte limit's, which the command reports as one.
func TestEncodeRefusesABaselinePastTheByteLimit(t *testing.T) {
	pack := basePack()
	pack.Rows = nil
	long := strings.Repeat("x", 9<<20)
	for index := 0; int64(index)*int64(len(long)) <= MaxBytes; index++ {
		pack.Rows = append(pack.Rows, Row{ID: fmt.Sprintf("r%d", index), Status: "passed", Disposition: long})
	}
	if _, err := Encode(result.Tool{Name: "jpack", Version: "0.46.0"}, []Pack{pack}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want the byte limit", err)
	}
}

// A pack holds at most the rows a matrix may declare.
func TestDecodeRefusesMoreRowsThanAMatrixDeclares(t *testing.T) {
	document := func(rows int) []byte {
		var builder strings.Builder
		builder.WriteString(`{"baselineVersion":"1","tool":{"name":"jpack","version":"0.46.0"},"packs":[{"id":"intake","packId":"urn:pack","packVersion":"0.1.0","specVersion":"0.3.0","packDigest":"` + digestA + `","matrixDigest":"` + digestB + `","rows":[`)
		for index := 0; index < rows; index++ {
			if index > 0 {
				builder.WriteString(",")
			}
			fmt.Fprintf(&builder, `{"id":"r%d","status":"passed"}`, index)
		}
		builder.WriteString(`]}]}`)
		return []byte(builder.String())
	}
	if _, err := Decode(document(project.MaxMatrixCases)); err != nil {
		t.Fatalf("a pack of %d rows must be admitted: %v", project.MaxMatrixCases, err)
	}
	_, err := Decode(document(project.MaxMatrixCases + 1))
	if err == nil || !strings.Contains(err.Error(), "more than the 10000 rows") {
		t.Fatalf("err = %v, want the row limit", err)
	}
}

const goodBaseline = `{"baselineVersion":"1","tool":{"name":"jpack","version":"0.46.0"},"packs":[{"id":"intake","packId":"urn:pack","packVersion":"0.1.0","specVersion":"0.3.0","packDigest":"` + digestA + `","matrixDigest":"` + digestB + `","rows":[{"id":"approve","status":"passed","disposition":"{}"}]}]}`

const goodReport = `{"outputVersion":"2","tool":{"name":"jpack","version":"0.45.0"},"command":"packs test","status":"passed","evaluatorSpecVersion":"0.2.0-draft","packs":[{"id":"intake","packId":"urn:pack","packVersion":"0.1.0","matrixPath":"m.json","status":"passed","rows":[{"id":"approve","status":"passed","actual":"{}"}]}]}`

// A baseline is a closed input: each of these is refused, and none as a
// version this runtime does not read.
func TestDecodeRefusesMalformedBaselines(t *testing.T) {
	replace := func(document, old, new string) string {
		if !strings.Contains(document, old) {
			t.Fatalf("fixture has no %q", old)
		}
		return strings.Replace(document, old, new, 1)
	}
	for _, test := range []struct {
		name     string
		document string
		want     string
	}{
		{"not JSON", `{"baselineVersion":"1",`, "not acceptable JSON"},
		{"trailing data", goodBaseline + `{}`, "not acceptable JSON"},
		{"not an object", `[]`, "root is not a JSON object"},
		{"neither version", `{"tool":{}}`, "neither baselineVersion"},
		{"both versions", replace(goodBaseline, `{"baselineVersion":"1",`, `{"baselineVersion":"1","outputVersion":"2",`), "both baselineVersion and outputVersion"},
		{"a version that is not a string", replace(goodBaseline, `"baselineVersion":"1"`, `"baselineVersion":1`), "not a string"},
		{"a duplicate root member", replace(goodBaseline, `"tool":`, `"packs":[],"tool":`), "not acceptable JSON"},
		{"a duplicate row member", replace(goodBaseline, `"status":"passed"`, `"status":"passed","status":"mismatch"`), "not acceptable JSON"},
		{"an unknown root member", replace(goodBaseline, `"tool":`, `"note":"x","tool":`), `member "note" at its root`},
		{"an unknown tool member", replace(goodBaseline, `"name":"jpack"`, `"name":"jpack","build":"x"`), `member "build" in its tool`},
		{"an unknown pack member", replace(goodBaseline, `"id":"intake"`, `"id":"intake","path":"p"`), `member "path" in pack 0`},
		{"an unknown row member", replace(goodBaseline, `"id":"approve"`, `"id":"approve","expected":"{}"`), `member "expected" in row 0`},
		{"a member in another case", replace(goodBaseline, `"disposition":"{}"`, `"Disposition":"{}"`), `member "Disposition" in row 0`},
		{"a missing pack member", replace(goodBaseline, `"specVersion":"0.3.0",`, ``), "has no specVersion"},
		{"a row status of another kind", replace(goodBaseline, `"status":"passed"`, `"status":"skipped"`), "passed or mismatch"},
		{"a row status that is not a string", replace(goodBaseline, `"status":"passed"`, `"status":true`), "status that is not a string"},
		{"an empty optional member", replace(goodBaseline, `"disposition":"{}"`, `"disposition":"","errorClass":"x"`), "empty disposition"},
		{"a digest in upper case", replace(goodBaseline, digestA, "sha256:"+strings.ToUpper(strings.TrimPrefix(digestA, "sha256:"))), "packDigest"},
		{"a digest with a letter past f", replace(goodBaseline, digestA, "sha256:g"+strings.TrimPrefix(digestA, "sha256:a")), "packDigest"},
		{"a digest one digit short", replace(goodBaseline, digestA, digestA[:len(digestA)-1]), "packDigest"},
		{"a digest of another algorithm", replace(goodBaseline, `"matrixDigest":"sha256:`, `"matrixDigest":"sha512:`), "matrixDigest"},
		{"no pack", replace(goodBaseline, goodBaseline[strings.Index(goodBaseline, `[{"id":"intake"`):len(goodBaseline)-1], `[]`), "holds no pack"},
		{"a pack with no rows", replace(goodBaseline, `[{"id":"approve","status":"passed","disposition":"{}"}]`, `[]`), "has no rows"},
		{"a pack twice", replace(goodBaseline, `]}]}`, `]},{"id":"intake","packId":"","packVersion":"","specVersion":"0.3.0","packDigest":"`+digestA+`","matrixDigest":"`+digestB+`","rows":[{"id":"r","status":"passed"}]}]}`), `pack "intake" appears more than once`},
		{"a row twice", replace(goodBaseline, `{"id":"approve","status":"passed","disposition":"{}"}`, `{"id":"approve","status":"passed"},{"id":"approve","status":"passed"}`), `row "approve" appears more than once`},
		{"a report of another command", replace(goodReport, `"command":"packs test"`, `"command":"validate"`), "not of packs test"},
		{"an unknown report member", replace(goodReport, `"status":"passed","evaluatorSpecVersion"`, `"status":"passed","verdict":"x","evaluatorSpecVersion"`), `member "verdict" at its root`},
		{"an unknown report row member", replace(goodReport, `"actual":"{}"`, `"actual":"{}","disposition":"{}"`), `member "disposition" in row 0`},
		{"a report row with no actual", replace(goodReport, `,"actual":"{}"`, ``), "has no actual"},
		{"a report whose matrix did not run", replace(goodReport, `[{"id":"approve","status":"passed","actual":"{}"}]`, `[]`), "did not run under the runtime that wrote it"},
		{"a report with rows and no matrix", replace(goodReport, `"matrixPath":"m.json",`, ``), "declares no matrixPath and has rows"},
		{"a report pack twice", replace(goodReport, `]}]}`, `]},{"id":"intake","packId":"urn:pack","packVersion":"0.1.0","matrixPath":"m.json","status":"passed","rows":[{"id":"r","status":"passed","actual":"{}"}]}]}`), `pack "intake" appears more than once`},
		{"a report with no pack that ran", replace(goodReport, goodReport[strings.Index(goodReport, `[{"id":"intake"`):len(goodReport)-1], `[]`), "holds no pack whose matrix ran"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := Decode([]byte(test.document))
			if err == nil {
				t.Fatal("the baseline was admitted")
			}
			var version *VersionError
			if errors.As(err, &version) {
				t.Fatalf("a malformed baseline was refused as a version: %v", err)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err = %v, want it to say %q", err, test.want)
			}
		})
	}
	if _, err := Decode([]byte(goodBaseline)); err != nil {
		t.Fatalf("the unmodified baseline must be admitted: %v", err)
	}
	if _, err := Decode([]byte(goodReport)); err != nil {
		t.Fatalf("the unmodified report must be admitted: %v", err)
	}
}

// A baseline larger than the reader's bound is refused before it is parsed.
func TestDecodeRefusesAnOversizedBaseline(t *testing.T) {
	oversized := make([]byte, MaxBytes+1)
	if _, err := Decode(oversized); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v", err)
	}
}

// A later version is told it is a later version, before any member of it is
// read: a later document may well carry members this runtime has never seen.
func TestDecodeRefusesAnUnknownVersion(t *testing.T) {
	for name, document := range map[string]string{
		"a later baselineVersion":                strings.Replace(goodBaseline, `"baselineVersion":"1","tool":`, `"baselineVersion":"2","added":true,"tool":`, 1),
		"a packs test report of another version": strings.Replace(goodReport, `"outputVersion":"2"`, `"outputVersion":"3","added":true`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Decode([]byte(document))
			var version *VersionError
			if !errors.As(err, &version) {
				t.Fatalf("err = %v, want a version refusal", err)
			}
		})
	}
}

// The hand-written fixture is a packs test report in the shape v0.45.0
// writes; it is read as a baseline, with its skipped pack left out and no
// digest recorded.
func TestDecodeReadsAV045PacksTestReport(t *testing.T) {
	data := readFixture(t, "packs-test-v0.45.0.json")
	read, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	want := Source{Form: FormReport, Version: "2", Tool: result.Tool{Name: "jpack", Version: "0.45.0"}, Packs: []Pack{{
		ID: "intake", PackID: "https://example.invalid/judgment-packs/data-request-intake-triage", PackVersion: "0.1.0", SpecVersion: "0.2.0-draft",
		Rows: []Row{
			{ID: "hard-fail", Status: "passed", Disposition: `{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"decline-redirect","reasons":[]}`},
			{ID: "undeclared-key", Status: "passed", ErrorClass: "malformed-input", ErrorPhase: "preflight"},
		},
	}}}
	if !reflect.DeepEqual(read, want) {
		t.Fatalf("read = %+v", read)
	}
}

// The report members the reader admits are exactly the members packs test
// writes: a member added to the packs test payload fails here until the
// reader is told what to do with it.
func TestReportMembersArePacksTestPayloadMembers(t *testing.T) {
	for _, test := range []struct {
		name  string
		value any
		known map[string]bool
	}{
		{"result.PackTest", result.PackTest{}, reportMembers},
		{"result.PackTestEntry", result.PackTestEntry{}, reportPackMembers},
		{"result.EvaluationCorpusCase", result.EvaluationCorpusCase{}, reportRowMembers},
	} {
		written := jsonNames(reflect.TypeOf(test.value))
		known := []string{}
		for name := range test.known {
			known = append(known, name)
		}
		sort.Strings(known)
		if !slices.Equal(written, known) {
			t.Fatalf("%s writes %v; the reader admits %v", test.name, written, known)
		}
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func jsonNames(typ reflect.Type) []string {
	names := []string{}
	for index := range typ.NumField() {
		field := typ.Field(index)
		if field.Anonymous {
			names = append(names, jsonNames(field.Type)...)
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name != "" && name != "-" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}
