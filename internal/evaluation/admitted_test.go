package evaluation

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

func admittedFixture(t *testing.T) []byte {
	t.Helper()
	pack, err := os.ReadFile(filepath.Join("testdata", "data-request-intake-triage.json"))
	if err != nil {
		t.Fatal(err)
	}
	return pack
}

func admittedEngine(t *testing.T) *Engine {
	t.Helper()
	validator, err := validation.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	return NewEngine(validator)
}

// The admission key is canonical and injective: two spellings of one
// capability set share an admission, and no two different inputs can share a
// key — the collision family a control character or a marker suffix used to
// open (issue #78's review, finding 1).
func TestAdmissionKeysAreCanonicalAndCollisionFree(t *testing.T) {
	same := [][2]Options{
		{{SupportedExtensions: []string{"a", "a", "b"}}, {SupportedExtensions: []string{"b", "a"}}},
		{{SupportedExtensions: nil}, {SupportedExtensions: []string{}}},
	}
	for _, pair := range same {
		if admissionKey(pair[0]) != admissionKey(pair[1]) {
			t.Fatalf("one set, two keys: %q vs %q", admissionKey(pair[0]), admissionKey(pair[1]))
		}
	}
	different := [][2]Options{
		{{SupportedExtensions: []string{"com.example.review-policy", "junk"}},
			{SupportedExtensions: []string{"com.example.review-policy\x00junk"}}},
		{{SupportedExtensions: []string{"x"}, RFC0008Quantifiers: true},
			{SupportedExtensions: []string{"x", "\x01rfc0008"}}},
		{{RFC0008Quantifiers: true}, {SupportedExtensions: []string{"\x01rfc0008"}}},
	}
	for _, pair := range different {
		if admissionKey(pair[0]) == admissionKey(pair[1]) {
			t.Fatalf("two inputs, one key: %q", admissionKey(pair[0]))
		}
	}
}

// A returned failure is the caller's own copy: mutating it cannot poison the
// admission the next row replays.
func TestACachedFailureCannotBePoisoned(t *testing.T) {
	engine := admittedEngine(t)
	admitted := engine.AdmitPack([]byte(`{"not": "a pack"}`))
	_, first := engine.EvaluateAdmitted(admitted, []byte(`{}`), nil, Options{Command: "test"})
	if first == nil {
		t.Fatal("a non-conformant pack must refuse")
	}
	class := first.Class
	first.Class = "poisoned"
	_, second := engine.EvaluateAdmitted(admitted, []byte(`{}`), nil, Options{Command: "test"})
	if second == nil || second.Class != class {
		t.Fatalf("the second row must see the pristine failure: %+v", second)
	}
}

// Admits keeps its documented semantics — the raw byte limit stays with
// EvaluateWith: a conformant pack padded past the byte limit with whitespace
// still Admits, while EvaluateWith refuses it at the limit.
func TestAdmitsLeavesTheByteLimitToEvaluate(t *testing.T) {
	pack := admittedFixture(t)
	padded := append(bytes.Clone(pack), bytes.Repeat([]byte(" "), int(carrier.HardMaxBytes))...)
	engine := admittedEngine(t)
	if !engine.Admits(padded, nil) {
		t.Fatal("Admits applies conformance, not the raw byte limit")
	}
	if _, failure := engine.EvaluateWith(padded, []byte(`{"request":{}}`), nil, Options{Command: "test"}); failure == nil || failure.Class != result.ClassPackNotConformant {
		t.Fatalf("EvaluateWith holds the byte limit: %+v", failure)
	}
}

// The memo is bounded: distinct capability sets beyond the cap are admitted
// without being retained, so an engineered matrix cannot pin unbounded
// decoded roots — and every set, cached or not, evaluates identically.
func TestTheAdmissionMemoIsBounded(t *testing.T) {
	engine := admittedEngine(t)
	admitted := engine.AdmitPack(admittedFixture(t))
	facts := []byte(`{"request":{"type":"data-access","completeness":"complete","appropriateness":"hard-fail","embargoedInformationToUnauthorizedRecipients":false}}`)
	evidence := []byte(`{"intake-form":"present","sponsor-endorsement":"present"}`)
	for i := 0; i < maxAdmissions+8; i++ {
		options := Options{Command: "test", SupportedExtensions: []string{"com.example.cap-" + strings.Repeat("x", i%7) + string(rune('a'+i%26))}}
		evaluated, failure := engine.EvaluateAdmitted(admitted, facts, evidence, options)
		if failure != nil {
			t.Fatalf("set %d refused: %+v", i, failure)
		}
		if evaluated.Disposition.Kind != "outcome" {
			t.Fatalf("set %d evaluated differently: %+v", i, evaluated.Disposition)
		}
	}
	if len(admitted.admissions) > maxAdmissions {
		t.Fatalf("the memo retained %d admissions", len(admitted.admissions))
	}
}
