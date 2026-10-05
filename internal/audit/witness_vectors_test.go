package audit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// witnessVectorSource is the gateway commit testdata/witness/ was copied from
// (gateway ADR-0013, PR 1 of 6). witness.lock.json names it too.
const witnessVectorSource = "c916ee933dff0b72ebf7741c1ce8022487bfb807"

// witnessVectorCount is how many vectors the gateway's corpus/README.md states
// for corpus/witness/ at that commit.
const witnessVectorCount = 54

// witnessLock is testdata/witness.lock.json: where the vectors came from, and
// each file's size and SHA-256.
type witnessLock struct {
	FormatVersion int `json:"formatVersion"`
	Source        struct {
		Repository string `json:"repository"`
		Kind       string `json:"kind"`
		BaseCommit string `json:"baseCommit"`
		Path       string `json:"path"`
	} `json:"source"`
	Files []struct {
		Path   string `json:"path"`
		Bytes  int64  `json:"bytes"`
		SHA256 string `json:"sha256"`
	} `json:"files"`
}

// The vectors are the gateway's frozen data, copied verbatim: the directory
// holds exactly the files the lock names, each of the size and digest it
// states, and the lock names the gateway commit they came from. A file added,
// removed or edited without the lock fails here.
func TestTheWitnessVectorsAreTheGatewaysCopy(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "witness.lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	var lock witnessLock
	if err := json.Unmarshal(raw, &lock); err != nil {
		t.Fatal(err)
	}
	if lock.FormatVersion != 1 || lock.Source.Repository != "https://github.com/Judgment-Pack/judgment-pack-gateway" ||
		lock.Source.Kind != "immutable-git-ref" || lock.Source.BaseCommit != witnessVectorSource || lock.Source.Path != "corpus/witness" {
		t.Fatalf("the lock names another source: %+v", lock.Source)
	}
	entries, err := os.ReadDir(filepath.Join("testdata", "witness"))
	if err != nil {
		t.Fatal(err)
	}
	held := []string{}
	for _, entry := range entries {
		held = append(held, entry.Name())
	}
	sort.Strings(held)
	if len(held) != len(lock.Files) || len(held) != witnessVectorCount {
		t.Fatalf("the directory holds %d files and the lock names %d; the gateway states %d", len(held), len(lock.Files), witnessVectorCount)
	}
	for index, file := range lock.Files {
		if held[index] != file.Path {
			t.Fatalf("file %d is %q, and the lock names %q", index, held[index], file.Path)
		}
		data, err := os.ReadFile(filepath.Join("testdata", "witness", file.Path))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		if int64(len(data)) != file.Bytes || hex.EncodeToString(sum[:]) != file.SHA256 {
			t.Fatalf("%s is %d bytes with digest %x, and the lock states %d bytes and %s", file.Path, len(data), sum, file.Bytes, file.SHA256)
		}
	}
}

// witnessVector is one of the gateway's vectors (its corpus/README.md): the
// trail being verified, the keys supplied in order, the statements files, a
// head file when there is one, and the answer expected.
type witnessVector struct {
	Name     string          `json:"name"`
	Family   string          `json:"family"`
	Note     string          `json:"note"`
	Trail    string          `json:"trail"`
	Keys     []string        `json:"keys"`
	Witness  []witnessFile   `json:"witness"`
	Head     *witnessFile    `json:"head"`
	Expected witnessExpected `json:"expected"`
}

// witnessFile is a file's bytes: a string, its text exactly, or
// {"parts": [{"text", "times"}]}, each part's text repeated times times, in
// order, the form that states a file at the byte bound without landing it.
type witnessFile struct {
	parts []witnessPart
}

type witnessPart struct {
	Text  string `json:"text"`
	Times int    `json:"times"`
}

func (f *witnessFile) UnmarshalJSON(raw []byte) error {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		f.parts = []witnessPart{{Text: text, Times: 1}}
		return nil
	}
	var form struct {
		Parts []witnessPart `json:"parts"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&form); err != nil {
		return err
	}
	for _, part := range form.Parts {
		if part.Times < 1 {
			return fmt.Errorf("a part repeated %d times", part.Times)
		}
	}
	f.parts = form.Parts
	return nil
}

// bytes writes the file out in full, so what is read is the file, never the
// description of it.
func (f witnessFile) bytes() []byte {
	size := 0
	for _, part := range f.parts {
		size += len(part.Text) * part.Times
	}
	var out bytes.Buffer
	out.Grow(size)
	for _, part := range f.parts {
		for range part.Times {
			out.WriteString(part.Text)
		}
	}
	return out.Bytes()
}

// witnessExpected is a vector's answer: {"refused"}; {"ok": false,
// "findings"}, compared as a set of names; or, with no finding, what the
// chain read says, every member compared.
type witnessExpected struct {
	Refused          *string  `json:"refused"`
	OK               *bool    `json:"ok"`
	Findings         []string `json:"findings"`
	Reading          *string  `json:"reading"`
	HeadIndex        *int64   `json:"headIndex"`
	HighestIndex     *int64   `json:"highestIndex"`
	LatestCheckpoint *struct {
		Index       int64  `json:"index"`
		Sequence    int64  `json:"sequence"`
		WitnessedAt string `json:"witnessedAt"`
	} `json:"latestCheckpoint"`
	Conflicts []int64 `json:"conflicts"`
	Retired   *bool   `json:"retired"`
}

// readWitnessVectors reads every vector, refusing a member this test does not
// know, a vector not named by its file, and one with no answer.
func readWitnessVectors(t *testing.T) []witnessVector {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", "witness", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	vectors := []witnessVector{}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var vector witnessVector
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&vector); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if vector.Name != strings.TrimSuffix(filepath.Base(path), ".json") || len(vector.Keys) == 0 ||
			(vector.Expected.Refused == nil) == (vector.Expected.OK == nil) {
			t.Fatalf("%s: not a vector this test reads", path)
		}
		vectors = append(vectors, vector)
	}
	return vectors
}

// supplied is what a vector supplies a reading: its keys, decoded, and its
// files written out.
func (vector witnessVector) supplied(t *testing.T) WitnessSupplied {
	t.Helper()
	supplied := WitnessSupplied{}
	for _, key := range vector.Keys {
		raw, err := hex.DecodeString(key)
		if err != nil || len(raw) != 32 {
			t.Fatalf("%s: a key %q is not 32 bytes of hex", vector.Name, key)
		}
		supplied.Keys = append(supplied.Keys, raw)
	}
	for _, file := range vector.Witness {
		supplied.Statements = append(supplied.Statements, file.bytes())
	}
	if vector.Head != nil {
		supplied.Head, supplied.HasHead = vector.Head.bytes(), true
	}
	return supplied
}

// witnessAnswer reads what was supplied as a verification of trail would, and
// gives the answer in the vectors' terms, as short strings: "refused R",
// "findings A|B", or the members of a reading with none.
func witnessAnswer(trail string, supplied WitnessSupplied) string {
	input, err := PrepareWitness(supplied)
	if err != nil {
		var refusal *WitnessRefusal
		if !errors.As(err, &refusal) {
			return "error " + err.Error()
		}
		return "refused " + refusal.Reason
	}
	reading := input.read(trail)
	if !reading.clean {
		return "findings " + strings.Join(findingSet(reading.findings), "|")
	}
	return readingAnswer(reading)
}

// readingAnswer is a clean reading's members, as the vectors state them.
func readingAnswer(reading *witnessReading) string {
	kind, head := "historical", "null"
	if reading.head != nil {
		kind, head = "current", fmt.Sprint(reading.head.index)
	}
	latest := "null"
	if reading.latest != nil {
		latest = fmt.Sprintf("%d/%d/%s", reading.latest.index, reading.latest.checkpoint.Sequence, reading.latest.witnessedAt)
	}
	highest := "null"
	if reading.last != nil {
		highest = fmt.Sprint(reading.last.index)
	}
	return fmt.Sprintf("reading %s head %s highest %s latest %s conflicts %s retired %v", kind, head, highest, latest, shortList(reading.conflicts), reading.retired)
}

// shortList is a list of sequences as a short string: how many, and the first
// eight, so no answer compared or printed grows with the list.
func shortList(sequences []int64) string {
	shown := sequences[:min(len(sequences), 8)]
	return fmt.Sprintf("%d%v", len(sequences), shown)
}

// findingSet is the names of findings, once each, in order.
func findingSet(findings []result.AuditFinding) []string {
	names := map[string]bool{}
	for _, finding := range findings {
		names[finding.Name] = true
	}
	set := []string{}
	for name := range names {
		set = append(set, name)
	}
	sort.Strings(set)
	return set
}

// expectedAnswer is a vector's expected answer in witnessAnswer's terms.
func (vector witnessVector) expectedAnswer() string {
	expected := vector.Expected
	if expected.Refused != nil {
		return "refused " + *expected.Refused
	}
	if !*expected.OK {
		names := append([]string{}, expected.Findings...)
		sort.Strings(names)
		return "findings " + strings.Join(names, "|")
	}
	kind, head := *expected.Reading, "null"
	if expected.HeadIndex != nil {
		head = fmt.Sprint(*expected.HeadIndex)
	}
	latest, highest := "null", "null"
	if expected.LatestCheckpoint != nil {
		latest = fmt.Sprintf("%d/%d/%s", expected.LatestCheckpoint.Index, expected.LatestCheckpoint.Sequence, expected.LatestCheckpoint.WitnessedAt)
	}
	if expected.HighestIndex != nil {
		highest = fmt.Sprint(*expected.HighestIndex)
	}
	conflicts := expected.Conflicts
	if conflicts == nil {
		conflicts = []int64{}
	}
	return fmt.Sprintf("reading %s head %s highest %s latest %s conflicts %s retired %v", kind, head, highest, latest, shortList(conflicts), *expected.Retired)
}

// Every vector of the gateway's corpus is read by this runtime's reader with
// the answer the gateway's specification gives it: each refusal by its
// reason, each reading's findings as a set of names, and every member of a
// reading with none. The large vectors are written out in full first, one at
// a time, and every answer is compared as a short string.
func TestTheGatewaysWitnessVectorsReadAsTheyState(t *testing.T) {
	vectors := readWitnessVectors(t)
	if len(vectors) != witnessVectorCount {
		t.Fatalf("%d vectors, and the gateway states %d", len(vectors), witnessVectorCount)
	}
	families := map[string]int{}
	for _, vector := range vectors {
		families[vector.Family]++
		t.Run(vector.Name, func(t *testing.T) {
			got := witnessAnswer(vector.Trail, vector.supplied(t))
			if want := vector.expectedAnswer(); got != want {
				t.Fatalf("%s\n  expected: %s\n  produced: %s", vector.Note, want, got)
			}
		})
	}
	// The gateway's corpus/README.md states these counts by family.
	stated := map[string]int{"valid": 9, "begins-late": 3, "equivocation": 2, "head": 2, "key": 6, "signature": 8, "trail": 1, "malformed": 6, "chain": 10, "bound": 7}
	for family, count := range stated {
		if families[family] != count {
			t.Errorf("family %s: %d vectors, and the gateway states %d", family, families[family], count)
		}
	}
	if len(families) != len(stated) {
		t.Errorf("%d families, and the gateway states %d", len(families), len(stated))
	}
}
