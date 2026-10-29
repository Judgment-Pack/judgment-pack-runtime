package auditformat

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
)

// The committed catalog, change records and status agree with the code.
func TestCommittedFormatsAgree(t *testing.T) {
	generated, _ := Generate()
	if problems := CheckFiles(read(t, CatalogFile), read(t, ChangesFile), read(t, StatusFile)); len(problems) != 0 {
		t.Fatal(problems)
	}
	if len(generated.Lines) != 5 {
		t.Fatalf("persisted line types: %d", len(generated.Lines))
	}
}

func read(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

type customString string

func (customString) MarshalJSON() ([]byte, error) { return []byte(`""`), nil }

type promotedMembers struct {
	Promoted string `json:"promoted"`
}

// handwrittenShape exercises each normalization rule once.
type handwrittenShape struct {
	A string `json:"a"`
	B int64  `json:"b,omitempty"`
	C int    `json:"c,string"`
	D bool   `json:"d-e"`
	F *struct {
		G string `json:"g"`
	} `json:"f"`
	H      []string           `json:"h"`
	I      map[string]float64 `json:"i"`
	J      json.RawMessage    `json:"j"`
	K      []byte             `json:"k"`
	L      customString       `json:"l"`
	Hidden string             `json:"-"`
	hidden string
	promotedMembers
	M any `json:"m"`
}

// The expected text and digest are written by hand; the digest was computed
// outside this code from the text.
func TestNormalizeHandwrittenShape(t *testing.T) {
	const want = `{a:string,b?:integer,c~:integer,"d-e":boolean,f:{g:string}|null,h:[string]|null,i:{string:number}|null,j:json,k:base64|null,l:customstring,promoted:string,m:any}`
	got := entry("f", "l", reflect.TypeOf(handwrittenShape{}), nil)
	if got.Type != want {
		t.Fatalf("normalized:\n got %s\nwant %s", got.Type, want)
	}
	if got.SHA256 != "sha256:93884f1a0c8f9f11c275fe0df93b9fc24d98db29c4c2d884bc641e7db74b7e94" {
		t.Fatal("digest", got.SHA256)
	}
	renamed := struct {
		Renamed string `json:"a"`
	}{}
	if Normalize(reflect.TypeOf(renamed)) != "{a:string}" {
		t.Fatal("a Go field rename changed the encoded shape")
	}
}

// Changing a member of any persisted line without a change record fails, even
// after the catalog is regenerated.
func TestChangedMemberWithoutRecordFails(t *testing.T) {
	baseline, writers := Generate()
	for i, line := range baseline.Lines {
		changed, _ := Generate()
		changed.Lines[i].Type = strings.TrimSuffix(line.Type, "}") + ",added?:string}"
		sum := sha256.Sum256([]byte(changed.Lines[i].Type))
		changed.Lines[i].SHA256 = "sha256:" + hex.EncodeToString(sum[:])
		problems := Check(Encode(changed), read(t, ChangesFile), read(t, StatusFile), changed, writers)
		if len(problems) != 1 || !strings.Contains(problems[0].Error(), line.File+" "+line.Line+": digest") {
			t.Fatalf("%s %s: %v", line.File, line.Line, problems)
		}
		// The stale catalog is refused as well when it is not regenerated.
		if problems := Check(read(t, CatalogFile), read(t, ChangesFile), read(t, StatusFile), changed, writers); len(problems) != 2 {
			t.Fatalf("%s %s stale catalog: %v", line.File, line.Line, problems)
		}
	}
}

type syntheticV1 struct {
	Kind string `json:"kind"`
}
type syntheticV2 struct {
	Kind  string `json:"kind"`
	Extra string `json:"extra,omitempty"`
}

func TestCheckDecisionsAndStatus(t *testing.T) {
	v1 := entry("trail.jsonl", "line", reflect.TypeOf(syntheticV1{}), nil)
	v2 := entry("trail.jsonl", "line", reflect.TypeOf(syntheticV2{}), nil)
	baseline := `{"file":"trail.jsonl","line":"line","sha256":"` + v1.SHA256 + `","formatVersion":"1","decision":"baseline","reason":"first"}`
	addition := `{"file":"trail.jsonl","line":"line","sha256":"` + v2.SHA256 + `","formatVersion":"1","decision":"compatible-addition","reason":"optional member"}`
	newVersion := `{"file":"trail.jsonl","line":"line","sha256":"` + v2.SHA256 + `","formatVersion":"2","decision":"new-format-version","readingRule":"version 1 lines lack extra","reason":"new member"}`
	status := func(writes, baseline, released string) string {
		return `{"statusVersion":1,"releasedChanges":{"count":0,"sha256":"sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},"files":[{"file":"trail.jsonl","writes":"` + writes + `","baseline":"` + baseline + `","released":"` + released + `"}]}`
	}
	for _, tc := range []struct {
		name    string
		entry   Entry
		changes string
		status  string
		writer  string
		ok      bool
	}{
		{"baseline", v1, baseline, status("1", "1", "1"), "1", true},
		{"no record for change", v2, baseline, status("1", "1", "1"), "1", false},
		{"compatible addition", v2, baseline + "," + addition, status("1", "1", "1"), "1", true},
		{"new version", v2, baseline + "," + newVersion, status("2", "1", "1"), "2", true},
		{"new version without reading rule", v2, baseline + "," + strings.Replace(newVersion, `"readingRule":"version 1 lines lack extra",`, "", 1), status("2", "1", "1"), "2", false},
		{"new version skipping one", v2, baseline + "," + strings.Replace(newVersion, `"formatVersion":"2"`, `"formatVersion":"3"`, 1), status("3", "1", "1"), "3", false},
		{"new version, writer constant behind", v2, baseline + "," + newVersion, status("2", "1", "1"), "1", false},
		{"addition changing version", v2, baseline + "," + strings.Replace(addition, `"formatVersion":"1"`, `"formatVersion":"2"`, 1), status("2", "1", "1"), "2", false},
		{"no reason", v2, baseline + "," + strings.Replace(addition, `"optional member"`, `" "`, 1), status("1", "1", "1"), "1", false},
		{"unknown decision", v2, baseline + "," + strings.Replace(addition, "compatible-addition", "rename", 1), status("1", "1", "1"), "1", false},
		{"second baseline", v2, baseline + "," + strings.Replace(addition, "compatible-addition", "baseline", 1), status("1", "1", "1"), "1", false},
		{"first record not a baseline", v2, addition, status("1", "1", "1"), "1", false},
		{"repeated digest", v1, baseline + "," + strings.Replace(addition, v2.SHA256, v1.SHA256, 1), status("1", "1", "1"), "1", false},
		{"unknown line", v1, baseline + "," + strings.Replace(baseline, `"line":"line"`, `"line":"other"`, 1), status("1", "1", "1"), "1", false},
		{"unknown member", v1, strings.Replace(baseline, `"reason"`, `"note":"x","reason"`, 1), status("1", "1", "1"), "1", false},
		{"writer ahead of change records", v1, baseline, status("2", "1", "1"), "2", false},
		{"status contradicts writer", v1, baseline, status("2", "1", "1"), "1", false},
		{"baseline after released", v1, baseline, status("1", "2", "1"), "1", false},
		{"released after writes", v2, baseline + "," + newVersion, status("2", "1", "3"), "2", false},
		{"version not canonical", v1, baseline, status("01", "1", "1"), "1", false},
		{"no status", v1, baseline, `{"statusVersion":1,"releasedChanges":{"count":0,"sha256":"sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},"files":[]}`, "1", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			generated := Catalog{CatalogVersion: 1, Normalization: Normalization, Lines: []Entry{tc.entry}}
			problems := Check(Encode(generated), []byte(`{"changes":[`+tc.changes+`]}`), []byte(tc.status), generated, map[string]string{"trail.jsonl": tc.writer})
			if (len(problems) == 0) != tc.ok {
				t.Fatal(problems)
			}
		})
	}
}

// Released change records are append-only: rewriting one in place, reordering
// or removing them fails; appending after them passes.
func TestReleasedChangeRecordsAreAppendOnly(t *testing.T) {
	generated, writers := Generate()
	var changes Changes
	if err := json.Unmarshal(read(t, ChangesFile), &changes); err != nil {
		t.Fatal(err)
	}
	encode := func(c Changes) []byte { raw, _ := json.Marshal(c); return raw }
	// A member added to the record line, admitted by an appended record.
	changed, _ := Generate()
	changed.Lines[0].Type = strings.TrimSuffix(changed.Lines[0].Type, "}") + ",added?:string}"
	sum := sha256.Sum256([]byte(changed.Lines[0].Type))
	changed.Lines[0].SHA256 = "sha256:" + hex.EncodeToString(sum[:])
	appended := Changes{append(append([]Change(nil), changes.Changes...), Change{File: changed.Lines[0].File, Line: changed.Lines[0].Line, SHA256: changed.Lines[0].SHA256, FormatVersion: "1", Decision: DecisionCompatibleAddition, Reason: "optional member"})}
	if problems := Check(Encode(changed), encode(appended), read(t, StatusFile), changed, writers); len(problems) != 0 {
		t.Fatal("append refused:", problems)
	}
	// The same change admitted by rewriting the released baseline in place.
	rewritten := Changes{append([]Change(nil), changes.Changes...)}
	rewritten.Changes[0].SHA256 = changed.Lines[0].SHA256
	problems := Check(Encode(changed), encode(rewritten), read(t, StatusFile), changed, writers)
	if len(problems) != 1 || !strings.Contains(problems[0].Error(), "released change records") {
		t.Fatal("in-place rewrite of a released record:", problems)
	}
	swapped := Changes{append([]Change(nil), changes.Changes...)}
	swapped.Changes[0], swapped.Changes[1] = swapped.Changes[1], swapped.Changes[0]
	if problems := Check(Encode(generated), encode(swapped), read(t, StatusFile), generated, writers); len(problems) != 1 {
		t.Fatal("reordered released records:", problems)
	}
	if problems := Check(Encode(generated), encode(Changes{changes.Changes[:4]}), read(t, StatusFile), generated, writers); len(problems) == 0 {
		t.Fatal("removed released record accepted")
	}
}

type exemplarDisposition struct {
	Kind    string `json:"kind"`
	Handoff struct {
		State  string   `json:"state"`
		Routes []string `json:"routes,omitempty"`
	} `json:"handoff"`
}
type exemplarLine struct {
	Version     string          `json:"version"`
	Token       string          `json:"token" format:"base64"`
	Disposition json.RawMessage `json:"disposition"`
	Inputs      json.RawMessage `json:"inputs"`
}

// Lines a builder writes are held to the declared embedded shape and grammars.
func TestConformExemplars(t *testing.T) {
	embedded := map[string]reflect.Type{"disposition": reflect.TypeOf(exemplarDisposition{})}
	line := func(token, disposition string) []byte {
		return []byte(`{"version":"1","token":"` + token + `","disposition":` + disposition + `,"inputs":{"anything":[1]}}`)
	}
	full := `{"handoff":{"routes":["q"],"state":"requested"},"kind":"unresolved"}`
	plain := `{"handoff":{"state":"none"},"kind":"outcome"}`
	for _, tc := range []struct {
		name  string
		lines [][]byte
		ok    bool
	}{
		{"valid", [][]byte{line("+/8=", full), line("+/8=", plain)}, true},
		{"undeclared disposition member", [][]byte{line("+/8=", strings.Replace(full, `"kind"`, `"extra":1,"kind"`, 1))}, false},
		{"undeclared nested member", [][]byte{line("+/8=", strings.Replace(full, `"state"`, `"extra":1,"state"`, 1))}, false},
		{"declared member never written", [][]byte{line("+/8=", plain)}, false},
		{"required member absent", [][]byte{line("+/8=", full), line("+/8=", `{"handoff":{"state":"none"}}`)}, false},
		{"wrong kind", [][]byte{line("+/8=", strings.Replace(full, `"unresolved"`, `1`, 1))}, false},
		{"raw URL base64 token", [][]byte{line("-_8", full)}, false},
		{"unpadded token", [][]byte{line("+/8", full)}, false},
		{"undeclared top-level member", [][]byte{[]byte(`{"version":"1","token":"+/8=","disposition":` + full + `,"inputs":null,"extra":true}`)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := Conform(tc.lines, reflect.TypeOf(exemplarLine{}), embedded)
			if (err == nil) != tc.ok {
				t.Fatal(err)
			}
		})
	}
	// The embedded shape is part of the fingerprint, not an opaque "json".
	if got := NormalizeLine(reflect.TypeOf(exemplarLine{}), embedded); got != `{version:string,token:string(base64),disposition:{kind:string,handoff:{state:string,routes?:[string]|null}},inputs:json}` {
		t.Fatal(got)
	}
}

// The whole check runs the exemplars: a line format whose encoder writes an
// undeclared member fails it even when its catalog, records and status agree.
func TestCheckFilesRunsExemplars(t *testing.T) {
	formats := []audit.LineFormat{{File: "trail.jsonl", Line: "line", Version: "1", Type: reflect.TypeOf(syntheticV1{}),
		Exemplars: func() ([][]byte, error) { return [][]byte{[]byte(`{"kind":"k","extra":1}`)}, nil }}}
	generated, _ := generate(formats)
	changes := `{"changes":[{"file":"trail.jsonl","line":"line","sha256":"` + generated.Lines[0].SHA256 + `","formatVersion":"1","decision":"baseline","reason":"first"}]}`
	status := `{"statusVersion":1,"releasedChanges":{"count":0,"sha256":"sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},"files":[{"file":"trail.jsonl","writes":"1","baseline":"1","released":"1"}]}`
	problems := checkFiles(Encode(generated), []byte(changes), []byte(status), formats)
	if len(problems) != 1 || !strings.Contains(problems[0].Error(), "extra") {
		t.Fatal(problems)
	}
}
