package terraformfile_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/terraformfile"
)

// The Spec's vectors for the terraform-file reading 1.0.0, copied byte for
// byte from the Spec revision testdata/provenance.json names. The Spec
// proposes them with the companion text: until a terraform-file tag releases
// them they are no conformance corpus and back no claim, and this runtime is
// their one reader, by the same maintainer as the vectors.
const vectorsDir = "testdata/1.0.0"

// The revision the vectors are pinned to: the Spec commit that merges the pull
// request that adds them, until the terraform-file-v1.0.0 tag is published.
const vectorsRevision = "8354daaeed00f7887c494a1aa4d37f9e6a7a489e"

type generateRule struct {
	Template string `json:"template"`
	Replace  string `json:"replace"`
	With     struct {
		Repeat string `json:"repeat"`
		Times  int    `json:"times"`
		Quoted bool   `json:"quoted"`
	} `json:"with"`
}

type vectorRow struct {
	ID          string          `json:"id"`
	Group       string          `json:"group"`
	Operation   string          `json:"operation"`
	FormatTable string          `json:"formatTable"`
	Declaration string          `json:"declaration"`
	Input       string          `json:"input"`
	Generate    json.RawMessage `json:"generate"`
	Expected    string          `json:"expected"`
}

type vectorManifest struct {
	Companion string      `json:"companion"`
	Reading   string      `json:"reading"`
	Status    string      `json:"status"`
	Vectors   []vectorRow `json:"vectors"`
}

func readVectorManifest(t *testing.T) vectorManifest {
	t.Helper()
	var m vectorManifest
	decodeVectorFile(t, "manifest.json", &m)
	if m.Companion != "terraform-file" || m.Reading != "1.0.0" || m.Status != "proposed" {
		t.Fatalf("the manifest is for %s %s, %s", m.Companion, m.Reading, m.Status)
	}
	return m
}

func decodeVectorFile(t *testing.T, name string, into any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(vectorsDir, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(into); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

// vectorInput is a row's input: its file under vectors/, or the bytes its
// generate rule describes, the template with its one replace text replaced by
// repeat written times times, between double quotes when quoted. A rule has
// those members and no other.
func vectorInput(t *testing.T, row vectorRow) []byte {
	t.Helper()
	if row.Generate == nil {
		data, err := os.ReadFile(filepath.Join(vectorsDir, filepath.FromSlash(row.Input)))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	var rule generateRule
	decoder := json.NewDecoder(bytes.NewReader(row.Generate))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&rule); err != nil {
		t.Fatalf("%s: the generate rule: %v", row.ID, err)
	}
	template, err := os.ReadFile(filepath.Join(vectorsDir, "vectors", filepath.FromSlash(rule.Template)))
	if err != nil {
		t.Fatal(err)
	}
	at := bytes.Index(template, []byte(rule.Replace))
	if row.Input != "" || at < 0 || bytes.Count(template, []byte(rule.Replace)) != 1 || rule.With.Repeat == "" || rule.With.Times < 1 {
		t.Fatalf("%s: the generate rule does not name one replacement of a committed template", row.ID)
	}
	size := len(template) - len(rule.Replace) + len(rule.With.Repeat)*rule.With.Times
	if rule.With.Quoted {
		size += 2
	}
	data := make([]byte, 0, size)
	data = append(data, template[:at]...)
	if rule.With.Quoted {
		data = append(data, '"')
	}
	data = append(data, bytes.Repeat([]byte(rule.With.Repeat), rule.With.Times)...)
	if rule.With.Quoted {
		data = append(data, '"')
	}
	return append(data, template[at+len(rule.Replace):]...)
}

// declaration is a well-formed declaration of §2: document, select and
// maxBytes. A member it does not have, or one of another type, fails decoding.
type declaration struct {
	Document string `json:"document"`
	Select   struct {
		Types   []string `json:"types"`
		Mode    string   `json:"mode"`
		Address *string  `json:"address"`
		Actions []string `json:"actions"`
		Max     int      `json:"max"`
	} `json:"select"`
	MaxBytes int64 `json:"maxBytes"`
}

// vectorSource is the Source a row's declaration names. An absent mode, max
// or maxBytes is the Source's zero value, which reads as the default.
func vectorSource(t *testing.T, row vectorRow) terraformfile.Source {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(vectorsDir, filepath.FromSlash(row.Declaration)))
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var d declaration
	if err := decoder.Decode(&d); err != nil {
		t.Fatalf("%s: the declaration %s is not one this test reads: %v", row.ID, row.Declaration, err)
	}
	return terraformfile.Source{Document: d.Document, MaxBytes: d.MaxBytes, Select: terraformfile.Select{
		Types: d.Select.Types, Mode: d.Select.Mode, Address: d.Select.Address, Actions: d.Select.Actions, Max: d.Select.Max}}
}

func canonical(t *testing.T, value any) string {
	t.Helper()
	encoded, err := jcs.EncodeDocument(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func TestVendoredVectorsKeepTheirUpstreamDigests(t *testing.T) {
	var pin struct {
		Repository string  `json:"repository"`
		Release    *string `json:"release"`
		Revision   string  `json:"revision"`
		Files      map[string]struct {
			Upstream string `json:"upstream"`
			SHA256   string `json:"sha256"`
		} `json:"files"`
	}
	raw, err := os.ReadFile("testdata/provenance.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &pin); err != nil {
		t.Fatal(err)
	}
	if pin.Repository != "https://github.com/Judgment-Pack/judgment-pack-spec" || pin.Release != nil || pin.Revision != vectorsRevision {
		t.Fatalf("the vectors are pinned to %s at %v, %s", pin.Repository, pin.Release, pin.Revision)
	}
	listed := 0
	err = filepath.WalkDir("testdata", func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		name := filepath.ToSlash(path[len("testdata/"):])
		if name == "provenance.json" || name == "README.md" {
			return nil
		}
		file, ok := pin.Files[name]
		if !ok {
			t.Errorf("%s is vendored without a provenance entry", name)
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != file.SHA256 {
			t.Errorf("%s no longer has the bytes of %s at the pinned revision", name, file.Upstream)
		}
		listed++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if listed != len(pin.Files) {
		t.Fatalf("provenance lists %d files and %d are vendored", len(pin.Files), listed)
	}
}

// The table and the codes are written in the reader; they must be the Spec's
// formats.json and codes.json, which the reader never reads.
func TestTheTableAndTheCodesAreTheSpecLists(t *testing.T) {
	var formats struct {
		Companion string `json:"companion"`
		Reading   string `json:"reading"`
		Release   string `json:"release"`
		Table     []struct {
			Document string   `json:"document"`
			Accepted []string `json:"accepted"`
		} `json:"table"`
	}
	decodeVectorFile(t, "formats.json", &formats)
	if formats.Companion != "terraform-file" || formats.Reading != "1.0.0" {
		t.Fatalf("formats.json is for %s %s", formats.Companion, formats.Reading)
	}
	if formats.Release != terraformfile.FormatTable {
		t.Fatalf("the reader's table is %q; formats.json's release is %q", terraformfile.FormatTable, formats.Release)
	}
	var documents []string
	for _, row := range formats.Table {
		documents = append(documents, row.Document)
		if got := terraformfile.AcceptedFormats(row.Document); !slices.Equal(got, row.Accepted) {
			t.Fatalf("the reader accepts %v for %s; formats.json accepts %v", got, row.Document, row.Accepted)
		}
	}
	if !slices.Equal(documents, []string{terraformfile.DocumentPlan, terraformfile.DocumentState}) {
		t.Fatalf("formats.json's documents are %v", documents)
	}

	var codes struct {
		Codes []struct {
			Code     string `json:"code"`
			Severity string `json:"severity"`
			Status   string `json:"status"`
		} `json:"codes"`
	}
	decodeVectorFile(t, "codes.json", &codes)
	var listed []string
	for _, code := range codes.Codes {
		if code.Severity != "error" || code.Status != "active" {
			t.Fatalf("codes.json gives %s as %s, %s; the reader refuses with every code", code.Code, code.Severity, code.Status)
		}
		if terraformfile.Message(code.Code) == "" {
			t.Fatalf("codes.json lists %s, which the reader does not report", code.Code)
		}
		listed = append(listed, code.Code)
	}
	if !slices.Equal(listed, terraformfile.Codes()) {
		t.Fatalf("the reader's codes are %v; codes.json lists %v", terraformfile.Codes(), listed)
	}
}

// Every read and read-stream row through the reader: its one expected result,
// the acquisition record and the acquired value, or the refusal's code with
// the input's sha256 and size. A row whose declaration is malformed reads no
// file, and the reader takes a Source, not a declaration: those rows, and the
// batch's, run through jpack terraform facts in internal/cli.
func TestSpecVectors(t *testing.T) {
	m := readVectorManifest(t)
	accepted, refused, malformed, batch := 0, 0, 0, 0
	for _, row := range m.Vectors {
		if row.FormatTable != terraformfile.FormatTable {
			t.Fatalf("%s: a row of table %q; the reader implements table %q", row.ID, row.FormatTable, terraformfile.FormatTable)
		}
		switch row.Operation {
		case "batch":
			batch++
			continue
		case "read", "read-stream":
		default:
			t.Fatalf("%s: operation %q", row.ID, row.Operation)
		}
		var expected struct {
			Record      json.RawMessage `json:"record"`
			Value       json.RawMessage `json:"value"`
			Declaration string          `json:"declaration"`
			Refusal     *struct {
				Code   string      `json:"code"`
				SHA256 string      `json:"sha256"`
				Bytes  json.Number `json:"bytes"`
				Stream bool        `json:"stream"`
			} `json:"refusal"`
		}
		decodeVectorFile(t, row.Expected, &expected)
		if expected.Declaration != "" {
			if expected.Declaration != "malformed" || expected.Record != nil || expected.Refusal != nil {
				t.Fatalf("%s: expected result %s", row.ID, row.Expected)
			}
			malformed++
			continue
		}
		t.Run(row.ID, func(t *testing.T) {
			data := vectorInput(t, row)
			src := vectorSource(t, row)
			read := terraformfile.Read
			if row.Operation == "read-stream" {
				read = terraformfile.ReadStream
			}
			acquisition, refusal, err := read(bytes.NewReader(data), src)
			if err != nil {
				t.Fatalf("the reader failed: %v", err)
			}
			if expected.Refusal != nil {
				if expected.Record != nil || expected.Value != nil {
					t.Fatalf("expected result %s holds more than one result", row.Expected)
				}
				want := expected.Refusal
				if refusal == nil {
					t.Fatalf("accepted %s; the row expects %s", acquisition.Record(), want.Code)
				}
				if refusal.Code != want.Code || refusal.Stream != want.Stream {
					t.Fatalf("refused %s (stream %v); the row expects %s (stream %v)", refusal.Code, refusal.Stream, want.Code, want.Stream)
				}
				if want.Stream {
					if want.SHA256 != "" || want.Bytes != "" || refusal.SHA256 != "" || refusal.Bytes != 0 {
						t.Fatalf("a stream refusal carries %q and %d; the row %q and %q", refusal.SHA256, refusal.Bytes, want.SHA256, want.Bytes)
					}
				} else if refusal.SHA256 != want.SHA256 || want.Bytes.String() != jsonInt(refusal.Bytes) {
					t.Fatalf("refused with %s and %d bytes; the row expects %s and %s", refusal.SHA256, refusal.Bytes, want.SHA256, want.Bytes)
				}
				refused++
				return
			}
			if refusal != nil {
				t.Fatalf("refused %s; the row expects an acquisition", refusal.Code)
			}
			var record, value any
			if json.Unmarshal(expected.Record, &record) != nil || json.Unmarshal(expected.Value, &value) != nil || record == nil || value == nil {
				t.Fatalf("expected result %s is neither an acquisition nor a refusal", row.Expected)
			}
			if got, want := string(acquisition.Record()), canonical(t, record); got != want {
				t.Fatalf("acquisition record\n got %s\nwant %s", got, want)
			}
			if got, want := string(acquisition.Value()), canonical(t, value); got != want {
				t.Fatalf("acquired value\n got %s\nwant %s", first(got, 2000), first(want, 2000))
			}
			accepted++
		})
	}
	t.Logf("%d rows accepted and %d refused as expected; %d with a malformed declaration and %d batch rows run in internal/cli", accepted, refused, malformed, batch)
	if len(m.Vectors) != 188 || accepted != 70 || refused != 74 || malformed != 36 || batch != 8 {
		t.Fatalf("%d of %d rows accepted, %d refused, %d malformed, %d batch; the manifest at the pinned revision has 188: 70, 74, 36 and 8",
			accepted, len(m.Vectors), refused, malformed, batch)
	}
}

func jsonInt(n int64) string {
	encoded, _ := json.Marshal(n)
	return string(encoded)
}

func first(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
