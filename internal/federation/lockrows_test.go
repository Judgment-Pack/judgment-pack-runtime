package federation_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/federation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// pendingCodes are the codes of §12.2 that only the lock signature verifier
// (§11), which is not built yet, raises; codes.go lists them under its
// pending heading, which scripts/check-mutation-table.py reads.
var pendingCodes = []string{"LOCK-SERIAL-ROLLBACK", "LOCK-SERIAL-CONFLICT", "LOCK-SERIAL-STATE"}

// lockRow is a lock row's members beyond those of an assembly (the rows'
// README, Operations).
type lockRow struct {
	Registry         string   `json:"registry"`
	Kinds            []string `json:"kinds"`
	SourceSignatures []string `json:"sourceSignatures"`
	Signers          *struct {
		Key          string            `json:"key"`
		Repositories map[string]string `json:"repositories"`
	} `json:"signers"`
	Previous    string       `json:"previous"`
	FirstSerial *int64       `json:"firstSerial"`
	Emitters    []rowEmitter `json:"emitters"`
	Lock        string       `json:"lock"`
	Expect      struct {
		RegistryID   *string `json:"registryId"`
		RequireIndex *bool   `json:"requireIndex"`
		Environment  *string `json:"environment"`
	} `json:"expect"`
	Resolved string   `json:"resolved"`
	File     *rowFile `json:"file"`
}

type rowEmitter struct {
	Source   string    `json:"source"`
	Manifest string    `json:"manifest"`
	Path     string    `json:"path"`
	Check    *int      `json:"check"`
	Files    []rowFile `json:"files"`
}

type rowFile struct {
	Source string `json:"source"`
	Path   string `json:"path"`
	Mode   string `json:"mode"`
	File   string `json:"file"`
}

func rowDocument(t testing.TB, name string) registry.Document {
	return registry.Document{Data: readRowFile(t, name)}
}

func rowEmitters(t testing.TB, r row) []federation.Emitter {
	var out []federation.Emitter
	for _, e := range r.Emitters {
		files := []federation.EmitterFile{}
		for _, f := range e.Files {
			files = append(files, federation.EmitterFile{Path: f.Path, Mode: f.Mode, Digest: federation.FileDigest(readRowFile(t, f.File))})
		}
		out = append(out, federation.Emitter{Source: e.Source, Path: e.Path, Manifest: rowDocument(t, e.Manifest), Files: files, Check: e.Check})
	}
	return out
}

func rowExpect(r row) federation.Expect {
	return federation.Expect{RegistryID: r.Expect.RegistryID, RequireIndex: r.Expect.RequireIndex, Environment: r.Expect.Environment}
}

// manifestSourceNames are a federation manifest's source names in its
// order, the order in which a writer row lists its source signatures.
func manifestSourceNames(t testing.TB, name string) []string {
	var manifest struct {
		Sources []struct {
			Name string `json:"name"`
		} `json:"sources"`
	}
	if err := json.Unmarshal(readRowFile(t, name), &manifest); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range manifest.Sources {
		names = append(names, s.Name)
	}
	return names
}

// writeInput is a writer row's inputs. A row lists one source signature
// per source, in the manifest's order; the writer is given them by source
// name.
func writeInput(t testing.TB, r row) federation.WriteInput {
	in := federation.WriteInput{FirstSerial: r.FirstSerial, Emitters: rowEmitters(t, r)}
	if r.Federation != "" {
		assembly := rowInput(t, r)
		in.Assembly = &assembly
		names := manifestSourceNames(t, r.Federation)
		if len(names) != len(r.SourceSignatures) {
			t.Fatalf("%s: %d source signatures for %d sources", r.ID, len(r.SourceSignatures), len(names))
		}
		in.SourceSignatures = map[string]registry.Document{}
		for i, file := range r.SourceSignatures {
			in.SourceSignatures[names[i]] = rowDocument(t, file)
		}
		in.Signers = &federation.Signers{Key: r.Signers.Key, Repositories: r.Signers.Repositories}
	} else {
		document := rowDocument(t, r.Registry)
		in.Registry = &document
		for _, file := range r.Kinds {
			in.Kinds = append(in.Kinds, rowDocument(t, file))
		}
	}
	if r.Previous != "" {
		previous := rowDocument(t, r.Previous)
		in.Previous = &previous
	}
	return in
}

// expectedRefusal is a writer row's refused codes, sorted, or nil for a
// row that writes a lock.
func expectedRefusal(t testing.TB, r row) []string {
	var refused struct {
		Refused []string `json:"refused"`
	}
	if err := json.Unmarshal(readRowFile(t, r.Expected), &refused); err != nil {
		t.Fatal(err)
	}
	slices.Sort(refused.Refused)
	return refused.Refused
}

// expectedCodes are the codes a row's expected output holds: a writer's
// refused codes, or a report's diagnostics' codes.
func expectedCodes(t testing.TB, r row) []string {
	if r.Class == "lock-writer" {
		return expectedRefusal(t, r)
	}
	var expected federation.Report
	if err := json.Unmarshal(readRowFile(t, r.Expected), &expected); err != nil {
		t.Fatal(err)
	}
	var codes []string
	for _, d := range expected.Diagnostics {
		codes = append(codes, d.Code)
	}
	return codes
}

func refusalCodes(t testing.TB, err error) []string {
	var refusal *federation.Refusal
	if !errors.As(err, &refusal) {
		t.Fatalf("want a refusal, got %v", err)
	}
	return refusal.Codes
}

// runLockWriter compares a written lock byte for byte, and a refusal by its
// codes alone (§2.1 gives writing a lock no report).
func runLockWriter(t *testing.T, r row) {
	lock, err := federation.WriteLock(writeInput(t, r))
	if want := expectedRefusal(t, r); want != nil {
		if got := refusalCodes(t, err); !slices.Equal(got, want) {
			t.Errorf("%s: refused %v, want %v", r.ID, got, want)
		}
		if lock != nil {
			t.Errorf("%s: a refused lock was written", r.ID)
		}
		return
	}
	if err != nil {
		t.Fatalf("%s: %v", r.ID, err)
	}
	if want := readRowFile(t, r.Expected); !bytes.Equal(lock, want) {
		t.Errorf("%s: lock\n got %s\nwant %s", r.ID, lock, want)
	}
}

func runLockChecker(t *testing.T, r row) {
	var report federation.Report
	var err error
	switch r.Operation {
	case "check-lock":
		in := federation.CheckInput{Lock: rowDocument(t, r.Lock), Registry: rowDocument(t, r.Registry), Emitters: rowEmitters(t, r), Expect: rowExpect(r)}
		for _, file := range r.Kinds {
			in.Kinds = append(in.Kinds, rowDocument(t, file))
		}
		if r.Index != "" {
			index := rowDocument(t, r.Index)
			in.Index = &index
		}
		report, err = federation.CheckLock(in)
	case "coverage":
		in := federation.CoverageInput{Lock: rowDocument(t, r.Lock), Expect: rowExpect(r)}
		if r.Resolved != "" {
			resolved := rowDocument(t, r.Resolved)
			in.Resolved = &resolved
		}
		if r.File != nil {
			in.File = &federation.CoveredFile{Source: r.File.Source, Path: r.File.Path, Mode: r.File.Mode, Digest: federation.FileDigest(readRowFile(t, r.File.File))}
		}
		report, err = federation.Coverage(in)
	default:
		t.Fatalf("%s: operation %q", r.ID, r.Operation)
	}
	if err != nil {
		t.Fatalf("%s: %v", r.ID, err)
	}
	compareReport(t, r, report)
}

func rowByID(t *testing.T, id string) row {
	t.Helper()
	m := readRows(t)
	for _, r := range m.Vectors {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no row %s", id)
	return row{}
}

// editJSON decodes a document, edits it and returns its JCS bytes.
func editJSON(t testing.TB, data []byte, edit func(map[string]any)) []byte {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	edit(value)
	out, err := jcs.EncodeDocument(value)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func editSignature(t testing.TB, in *federation.WriteInput, edit func(map[string]any)) {
	in.SourceSignatures["billing"] = registry.Document{Data: editJSON(t, in.SourceSignatures["billing"].Data, edit)}
}

// TestWriterSignerSet holds the writer to §11's rules for the signer set
// and the items' repositories, each LOCK-EXPECTED.
func TestWriterSignerSet(t *testing.T) {
	r := rowByID(t, "write-assembled")
	const billing = "https://example.invalid/scm/billing"
	for name, edit := range map[string]func(in *federation.WriteInput){
		"no map":                func(in *federation.WriteInput) { in.Signers.Repositories = nil },
		"repository not mapped": func(in *federation.WriteInput) { in.Signers.Repositories = map[string]string{} },
		"standard key twice":    func(in *federation.WriteInput) { in.Signers.Repositories[billing] = in.Signers.Key },
		"standard repository in": func(in *federation.WriteInput) {
			in.Signers.Repositories["https://example.invalid/scm/platform"] = otherKey
		},
	} {
		t.Run(name, func(t *testing.T) {
			in := writeInput(t, r)
			in.Signers.Repositories = map[string]string{billing: in.Signers.Repositories[billing]}
			edit(&in)
			_, err := federation.WriteLock(in)
			if got := refusalCodes(t, err); !slices.Contains(got, "LOCK-EXPECTED") {
				t.Errorf("refused %v, want LOCK-EXPECTED", got)
			}
		})
	}
}

// TestWriterSerial holds the writer's serial to §8: one above the
// previous lock's, up to 2^53 - 1; or the first serial given, with no
// default.
func TestWriterSerial(t *testing.T) {
	r := rowByID(t, "write-next-serial")
	withPrevious := func(serial string) federation.WriteInput {
		in := writeInput(t, r)
		previous := registry.Document{Data: editJSON(t, in.Previous.Data, func(v map[string]any) { v["serial"] = json.Number(serial) })}
		in.Previous = &previous
		return in
	}
	serialOf := func(t *testing.T, lock []byte) string {
		var v struct {
			Serial json.Number `json:"serial"`
		}
		if err := json.Unmarshal(lock, &v); err != nil {
			t.Fatal(err)
		}
		return v.Serial.String()
	}
	max := strconv.FormatInt(federation.MaxSerial, 10)
	t.Run("the last serial is written", func(t *testing.T) {
		lock, err := federation.WriteLock(withPrevious(strconv.FormatInt(federation.MaxSerial-1, 10)))
		if err != nil || serialOf(t, lock) != max {
			t.Fatalf("got %s, %v; want serial %s", lock, err, max)
		}
	})
	t.Run("no serial after the last", func(t *testing.T) {
		_, err := federation.WriteLock(withPrevious(max))
		if got := refusalCodes(t, err); !slices.Equal(got, []string{"LOCK-FORM"}) {
			t.Fatalf("refused %v", got)
		}
	})
	t.Run("a previous lock not of its form", func(t *testing.T) {
		_, err := federation.WriteLock(withPrevious("0"))
		if got := refusalCodes(t, err); !slices.Equal(got, []string{"LOCK-FORM"}) {
			t.Fatalf("refused %v", got)
		}
	})
	for _, first := range []int64{0, -1, federation.MaxSerial + 1} {
		t.Run("first serial "+strconv.FormatInt(first, 10), func(t *testing.T) {
			in := writeInput(t, r)
			in.Previous, in.FirstSerial = nil, &first
			_, err := federation.WriteLock(in)
			if got := refusalCodes(t, err); !slices.Equal(got, []string{"LOCK-FORM"}) {
				t.Fatalf("refused %v", got)
			}
		})
	}
	t.Run("first serial at the last", func(t *testing.T) {
		in := writeInput(t, r)
		var last int64 = federation.MaxSerial
		in.Previous, in.FirstSerial = nil, &last
		lock, err := federation.WriteLock(in)
		if err != nil || serialOf(t, lock) != max {
			t.Fatalf("got %s, %v", lock, err)
		}
	})
}

// TestWriterInputs holds the inputs the text gives a writer no lock for to
// an input error, never a default: above all, no previous lock and no first
// serial.
func TestWriterInputs(t *testing.T) {
	single, assembled := rowByID(t, "write-next-serial"), rowByID(t, "write-assembled-emitters")
	one := 1
	for name, in := range map[string]func() federation.WriteInput{
		"no serial": func() federation.WriteInput {
			in := writeInput(t, single)
			in.Previous = nil
			return in
		},
		"both serials": func() federation.WriteInput {
			in := writeInput(t, single)
			in.FirstSerial = &one64
			return in
		},
		"registry and assembly": func() federation.WriteInput {
			in := writeInput(t, single)
			in.Assembly = writeInput(t, assembled).Assembly
			return in
		},
		"neither registry nor assembly": func() federation.WriteInput {
			in := writeInput(t, single)
			in.Registry = nil
			return in
		},
		"signatures for a single registry": func() federation.WriteInput {
			in := writeInput(t, single)
			in.SourceSignatures = map[string]registry.Document{}
			return in
		},
		"no check outcome": func() federation.WriteInput {
			in := writeInput(t, assembled)
			in.Emitters[0].Check = nil
			return in
		},
		"no source in an assembly": func() federation.WriteInput {
			in := writeInput(t, assembled)
			in.Emitters[0].Source = ""
			return in
		},
		"unknown source": func() federation.WriteInput {
			in := writeInput(t, assembled)
			in.Emitters[0].Source = "ledger"
			return in
		},
		"an emitter twice": func() federation.WriteInput {
			in := writeInput(t, assembled)
			in.Emitters = append(in.Emitters, in.Emitters[0])
			in.Emitters[1].Check = &one
			return in
		},
	} {
		t.Run(name, func(t *testing.T) {
			lock, err := federation.WriteLock(in())
			var input *federation.InputError
			if !errors.As(err, &input) || lock != nil {
				t.Fatalf("got %s, %v; want an input error", lock, err)
			}
		})
	}
}

var one64 int64 = 1

// otherKey is a public key the trust rule admits that the signer set does
// not name: RFC 8032's third test key, the public half alone.
const otherKey = "fc51cd8e6218a1a38da47ed00230f0580816ed13ba3303ac5deb911548908025"
