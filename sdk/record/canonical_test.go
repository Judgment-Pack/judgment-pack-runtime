package record_test

import (
	"bufio"
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/sdk/record"
)

// vector is one line of the fenced block under "## Vectors" in
// docs/decision-records.md, written by hand there: the doc is what a second
// implementation reproduces, and this test holds the code to it.
type vector struct {
	Name       string  `json:"name"`
	Input      *string `json:"input"`
	InputHex   string  `json:"inputHex"`
	Canonical  *string `json:"canonical"`
	SHA256     string  `json:"sha256"`
	Refused    string  `json:"refused"`
	KeyHex     string  `json:"keyHex"`
	HMACSHA256 string  `json:"hmacSha256"`
}

func documentedVectors(t *testing.T) []vector {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "decision-records.md"))
	if err != nil {
		t.Fatal(err)
	}
	var vectors []vector
	inSection, inBlock := false, false
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 1<<16), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "## "):
			inSection = line == "## Vectors"
		case inSection && line == "```jsonl":
			inBlock = true
		case inBlock && line == "```":
			inBlock = false
		case inBlock:
			decoder := json.NewDecoder(strings.NewReader(line))
			decoder.DisallowUnknownFields()
			var parsed vector
			if err := decoder.Decode(&parsed); err != nil {
				t.Fatalf("vector line %q: %v", line, err)
			}
			vectors = append(vectors, parsed)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return vectors
}

func (v vector) input(t *testing.T) []byte {
	t.Helper()
	if v.Input != nil {
		return []byte(*v.Input)
	}
	data, err := hex.DecodeString(v.InputHex)
	if err != nil {
		t.Fatalf("%s: %v", v.Name, err)
	}
	return data
}

// Every vector the documentation states holds: an input gives exactly its
// canonical bytes, whose SHA-256 is the one stated, from CanonicalFacts and
// CanonicalEvidence alike, and a canonical text is its own canonical text; a
// refused input is refused for the stated reason, naming the carrier's code;
// a keyed vector's HMAC-SHA-256 is the one stated. At least a dozen of each
// of the first two kinds are stated.
func TestTheDocumentedCanonicalVectors(t *testing.T) {
	canonical, refused, keyed := 0, 0, 0
	for _, v := range documentedVectors(t) {
		t.Run(v.Name, func(t *testing.T) {
			switch {
			case v.HMACSHA256 != "":
				keyed++
				key, err := hex.DecodeString(v.KeyHex)
				if err != nil || v.Canonical == nil {
					t.Fatalf("a keyed vector needs keyHex and canonical: %v", err)
				}
				mac := hmac.New(sha256.New, key)
				mac.Write([]byte(*v.Canonical))
				if got := hex.EncodeToString(mac.Sum(nil)); got != v.HMACSHA256 {
					t.Fatalf("HMAC-SHA-256 = %s, the vector says %s", got, v.HMACSHA256)
				}
				if again, err := record.CanonicalFacts([]byte(*v.Canonical)); err != nil || string(again) != *v.Canonical {
					t.Fatalf("a keyed vector's text is not canonical: %q, %v", again, err)
				}
			case v.Refused != "":
				refused++
				for name, call := range map[string]func([]byte) ([]byte, error){"facts": record.CanonicalFacts, "evidence": record.CanonicalEvidence} {
					got, err := call(v.input(t))
					if !errors.Is(err, record.ErrDocument) || got != nil {
						t.Fatalf("%s: %q, %v; want a refusal", name, got, err)
					}
					if want := v.Refused; want != "empty" && !strings.Contains(err.Error(), "("+want+")") || want == "empty" && !strings.Contains(err.Error(), "is empty") {
						t.Fatalf("%s: error %q does not say %s", name, err, v.Refused)
					}
					if !strings.Contains(err.Error(), "the "+name+" document") {
						t.Fatalf("error %q does not name the %s document", err, name)
					}
				}
			default:
				canonical++
				if v.Canonical == nil {
					t.Fatal("a vector with neither canonical nor refused")
				}
				sum := sha256.Sum256([]byte(*v.Canonical))
				if hex.EncodeToString(sum[:]) != v.SHA256 {
					t.Fatalf("the vector's own SHA-256 is not that of its canonical text")
				}
				for name, call := range map[string]func([]byte) ([]byte, error){"facts": record.CanonicalFacts, "evidence": record.CanonicalEvidence} {
					got, err := call(v.input(t))
					if err != nil || string(got) != *v.Canonical {
						t.Fatalf("%s: canonical text %q, %v; want %q", name, got, err, *v.Canonical)
					}
					again, err := call(got)
					if err != nil || !bytes.Equal(again, got) {
						t.Fatalf("%s: the canonical text is not its own canonical text: %q", name, again)
					}
				}
			}
		})
	}
	if canonical < 12 || refused < 12 || keyed < 1 {
		t.Fatalf("the documentation states %d canonical, %d refused and %d keyed vectors; want at least 12, 12 and 1", canonical, refused, keyed)
	}
}

// An omitted evidence document is the empty object; a supplied empty one is
// refused, as Evaluate refuses it. The hard byte limit is the evaluation's.
// The caller owns the returned bytes.
func TestCanonicalEvidenceOfAnOmittedDocumentAndTheByteLimit(t *testing.T) {
	if got, err := record.CanonicalEvidence(nil); err != nil || string(got) != "{}" {
		t.Fatalf("omitted evidence: %q, %v", got, err)
	}
	if got, err := record.CanonicalEvidence([]byte{}); !errors.Is(err, record.ErrDocument) || got != nil {
		t.Fatalf("supplied empty evidence: %q, %v", got, err)
	}
	if got, err := record.CanonicalFacts(nil); !errors.Is(err, record.ErrDocument) || got != nil {
		t.Fatalf("nil facts: %q, %v", got, err)
	}
	const limit = 10 * 1024 * 1024
	at := append(append([]byte(`"`), bytes.Repeat([]byte("a"), limit-3)...), '"', ' ')
	over := append(at, ' ')
	if _, err := record.CanonicalFacts(at); err == nil || !strings.Contains(err.Error(), "JPS-RESOURCE-STRING-LIMIT") {
		t.Fatalf("a document at the byte limit reaches the carrier: %v", err)
	}
	if _, err := record.CanonicalFacts(over); !errors.Is(err, record.ErrDocument) || !strings.Contains(err.Error(), "(JPS-RESOURCE-INPUT-BYTE-LIMIT)") {
		t.Fatalf("a document over the byte limit: %v", err)
	}
	got, err := record.CanonicalFacts([]byte(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	got[0] = 'X'
	if again, _ := record.CanonicalFacts([]byte(`{"a":1}`)); string(again) != `{"a":1}` {
		t.Fatalf("editing the returned bytes changed a later call: %q", again)
	}
}

// A refusal repeats nothing of the document: no member name, value or
// character of it.
func TestADocumentRefusalRepeatsNothingOfTheDocument(t *testing.T) {
	const marker = "synthetic-member-7f3a9c"
	for _, document := range []string{
		`{"` + marker + `":1,"` + marker + `":2}`,
		`{"` + marker + `":` + marker + `}`,
		`{"` + marker + `":"\ud800"}`,
		`{"` + marker + `":1} {"` + marker + `":1}`,
		strings.Repeat(`{"`+marker+`":`, 129) + "1" + strings.Repeat("}", 129),
	} {
		_, err := record.CanonicalFacts([]byte(document))
		if !errors.Is(err, record.ErrDocument) {
			t.Fatalf("%.40q: %v", document, err)
		}
		if strings.Contains(err.Error(), marker) || strings.Contains(err.Error(), "7f3a9c") {
			t.Fatalf("the error repeats the document: %q", err)
		}
	}
}
