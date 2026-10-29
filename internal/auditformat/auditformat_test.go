package auditformat

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// The committed catalog, change records and status agree with the code.
func TestCommittedFormatsAgree(t *testing.T) {
	generated, writers := Generate()
	if problems := Check(read(t, CatalogFile), read(t, ChangesFile), read(t, StatusFile), generated, writers); len(problems) != 0 {
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
	got := entry("f", "l", reflect.TypeOf(handwrittenShape{}))
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
	v1 := entry("trail.jsonl", "line", reflect.TypeOf(syntheticV1{}))
	v2 := entry("trail.jsonl", "line", reflect.TypeOf(syntheticV2{}))
	baseline := `{"file":"trail.jsonl","line":"line","sha256":"` + v1.SHA256 + `","formatVersion":"1","decision":"baseline","reason":"first"}`
	addition := `{"file":"trail.jsonl","line":"line","sha256":"` + v2.SHA256 + `","formatVersion":"1","decision":"compatible-addition","reason":"optional member"}`
	newVersion := `{"file":"trail.jsonl","line":"line","sha256":"` + v2.SHA256 + `","formatVersion":"2","decision":"new-format-version","readingRule":"version 1 lines lack extra","reason":"new member"}`
	status := func(writes, baseline, released string) string {
		return `{"statusVersion":1,"files":[{"file":"trail.jsonl","writes":"` + writes + `","baseline":"` + baseline + `","released":"` + released + `"}]}`
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
		{"no status", v1, baseline, `{"statusVersion":1,"files":[]}`, "1", false},
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
