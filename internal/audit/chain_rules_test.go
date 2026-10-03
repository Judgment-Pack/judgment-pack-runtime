package audit

import (
	"bytes"
	"crypto/x509"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/timestamp/tsatest"
)

// What the guide's "Record signatures, exactly" states about the trail's own
// lines (#218): which line is a chained record, which findings are the
// chain's checks that a signed coverage stops at, and where a finding about
// the sidecar is placed.

// chainedAt is a chained record of the guide's test vector's trail at line
// sequence, following previous.
func chainedAt(sequence int64, previous string) []byte {
	return fmt.Appendf(nil, `{"recordVersion":"1","trail":"%s","sequence":%d,"previous":"%s","kind":"evaluation"}`, vectorTrail, sequence, previous)
}

// A line is a chained record only when it is one JSON object, no member
// given twice, with trail, sequence and previous each of its form. A line
// with one of them missing or of another form is unchained, which is no
// failed check: the next chained line links over it to the whole file
// before it, and as the last line nothing commits to it. Linked as if it
// were chained, the next line fails previous-mismatch.
func TestAChainMemberOfAnotherFormLeavesTheLineUnchained(t *testing.T) {
	first := chainedAt(1, Digest(nil))
	good := string(chainedAt(2, Digest(first)))
	for name, middle := range map[string]string{
		"a sequence written as text":  strings.Replace(good, `"sequence":2`, `"sequence":"2"`, 1),
		"a sequence written as 2.0":   strings.Replace(good, `"sequence":2`, `"sequence":2.0`, 1),
		"a sequence of zero":          strings.Replace(good, `"sequence":2`, `"sequence":0`, 1),
		"a sequence at 2^53-1":        strings.Replace(good, `"sequence":2`, `"sequence":9007199254740991`, 1),
		"a trail in upper case":       strings.Replace(good, vectorTrail, strings.ToUpper(vectorTrail), 1),
		"a trail one character short": strings.Replace(good, vectorTrail, vectorTrail[:31], 1),
		"a previous without sha256:":  strings.Replace(good, `"previous":"sha256:`, `"previous":"`, 1),
		"no previous":                 strings.Replace(good, `"previous":"`+Digest(first)+`",`, ``, 1),
		"another member given twice":  strings.Replace(good, `"kind":"evaluation"`, `"kind":"evaluation","kind":"evaluation"`, 1),
		"a chain member given twice":  strings.Replace(good, `"kind":"evaluation"`, `"kind":"evaluation","sequence":2`, 1),
		"not one JSON object":         good + ` {}`,
	} {
		if middle == good {
			t.Fatalf("%s: the edit did not apply", name)
		}
		lines := [][]byte{first, []byte(middle)}
		after := chainedAt(3, Digest(joinLines(lines)))
		chain := verifyBytes(t, joinLines(append(slices.Clone(lines), after)), nil)
		if chain.Status != "valid" || len(chain.Findings) != 0 || chain.Coverage.Chained != 2 || chain.Coverage.Unchained != 1 {
			t.Fatalf("%s: the line is unchained and no failed check: %s %v %+v", name, chain.Status, findingNames(chain), chain.Coverage)
		}
		linked := chainedAt(3, Digest([]byte(middle)))
		if chain := verifyBytes(t, joinLines(append(slices.Clone(lines), linked)), nil); !slices.Equal(findingNames(chain), []string{"previous-mismatch@3"}) {
			t.Fatalf("%s: linked as if it were chained: %v", name, findingNames(chain))
		}
		if chain := verifyBytes(t, joinLines(lines), nil); chain.Status != "valid" || chain.Coverage.Uncovered != 1 {
			t.Fatalf("%s: as the last line: %s %+v", name, chain.Status, chain.Coverage)
		}
	}
	// An escape in a member's name is the character it spells: the line is
	// chained, and the next line links to it.
	escaped := strings.Replace(good, `"trail":`, `"tr`+"BSu0061"+`il":`, 1)
	escaped = strings.Replace(escaped, "BS", string(rune(92)), 1)
	lines := [][]byte{first, []byte(escaped), chainedAt(3, Digest([]byte(escaped)))}
	if chain := verifyBytes(t, joinLines(lines), nil); chain.Status != "valid" || chain.Coverage.Chained != 3 {
		t.Fatalf("an escaped member name: %s %v %+v", chain.Status, findingNames(chain), chain.Coverage)
	}
}

// The signed coverage stops at the chain's checks and only at them: a
// sequence-mismatch, previous-mismatch, trail-mismatch, discontinuity-malformed,
// discontinuity-mismatch or line-too-long at line 2 keeps it from reaching a
// record at or after line 2, however signed. Damage a valid discontinuity
// names, a held checkpoint that fails, and an incomplete last line do not.
func TestTheSignedCoverageStopsOnlyAtTheChainsChecks(t *testing.T) {
	signer := signerOf(t, vectorSeed1)
	sign := func(lines [][]byte, sequences ...int) []byte {
		sidecar := []byte{}
		for _, sequence := range sequences {
			signed, err := signer.SignRecordLine(lines[sequence-1])
			if err != nil {
				t.Fatal(err)
			}
			sidecar = append(sidecar, signed...)
		}
		return sidecar
	}
	discontinuity := func(sequence int64, previous string, names int64, damaged []byte) []byte {
		line, err := encodeJSONLine(discontinuityRecord{
			RecordVersion: RecordVersion, Trail: vectorTrail, Sequence: sequence, Previous: previous,
			Run: "repair0000000000", At: "2026-10-03T00:00:00Z", Kind: KindDiscontinuity, Surface: "audit repair",
			Tool:          Tool{Name: "jpack", Version: "test"},
			Discontinuity: discontinuity{Reason: ReasonIncompleteLastLine, Line: names, Bytes: int64(len(damaged)), Digest: Digest(damaged)},
		})
		if err != nil {
			t.Fatal(err)
		}
		return bytes.TrimSuffix(line, []byte("\n"))
	}
	first := chainedAt(1, Digest(nil))
	three := func(second []byte) [][]byte { return [][]byte{first, second, chainedAt(3, Digest(second))} }
	// A first record a discontinuity names as damaged, with a sequence and
	// a previous that would each fail: excused, neither failure is reported.
	damagedFirst := chainedAt(7, Digest([]byte("x")))
	other := strings.Repeat("ab", 16)
	otherTrail := []byte(strings.Replace(string(chainedAt(2, Digest(first))), vectorTrail, other, 1))
	cases := []struct {
		name     string
		lines    [][]byte
		signed   []int
		findings []string
		through  int64
		bound    int64
	}{
		{"a sequence-mismatch", three(chainedAt(5, Digest(first))), []int{1, 3}, []string{"sequence-mismatch@2"}, 1, 0},
		{"a previous-mismatch", three(chainedAt(2, Digest([]byte("x")))), []int{1, 2, 3}, []string{"previous-mismatch@2"}, 1, 0},
		{"a trail-mismatch", [][]byte{first, otherTrail, []byte(strings.Replace(string(chainedAt(3, Digest(otherTrail))), vectorTrail, other, 1))}, []int{1, 2, 3}, []string{"trail-mismatch@2"}, 1, 0},
		{"a discontinuity-malformed", three(discontinuity(2, Digest(first), 5, first)), []int{1, 2, 3}, []string{"discontinuity-malformed@2"}, 1, 0},
		{"a discontinuity-mismatch", three(discontinuity(2, Digest(nil), 1, []byte("other"))), []int{2, 3}, []string{"discontinuity-mismatch@2"}, 0, 0},
		{"a line-too-long", [][]byte{first, bytes.Repeat([]byte("x"), 400), chainedAt(3, Digest([]byte("ignored")))}, []int{1, 3}, []string{"line-too-long@2"}, 1, 300},
		{"damage a discontinuity names", three(discontinuity(2, Digest(nil), 1, damagedFirst)), []int{2, 3}, []string{}, 3, 0},
	}
	cases[len(cases)-1].lines[0] = damagedFirst
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.bound > 0 {
				lowerLineBound(t, c.bound)
			}
			chain := verifySigned(t, joinLines(c.lines), sign(c.lines, c.signed...), SignatureOptions{Keys: keys(signer)})
			through := chain.Coverage.Signed.Through
			if !slices.Equal(findingNames(chain), c.findings) || through != c.through || chain.Coverage.SignedRecords != int64(len(c.signed)) {
				t.Fatalf("findings %v, signed %d through %d (%s)", findingNames(chain), chain.Coverage.SignedRecords, through, chain.Coverage.Signed.Status)
			}
		})
	}
	// A held checkpoint that fails at line 2, and an incomplete last line,
	// are findings, and neither is a check of the chain.
	lines := memoryTrail(3)
	sidecar := sign(lines, 1, 2, 3)
	options := SignatureOptions{Keys: keys(signer), Sidecar: bytes.NewReader(sidecar), SidecarSize: int64(len(sidecar))}
	wrong := result.AuditCheckpoint{CheckpointVersion: result.CheckpointVersion, RecordDigest: Digest([]byte("x")), Sequence: 2, Trail: vectorTrail}
	report := verifyWith(t, joinLines(lines), Options{Held: []result.AuditCheckpoint{wrong}, Signatures: &options})
	if !slices.Equal(findingNames(report.Chain), []string{"checkpoint-record-mismatch@2"}) || report.Chain.Coverage.Signed.Through != 3 {
		t.Fatalf("a held checkpoint that fails: %v through %d", findingNames(report.Chain), report.Chain.Coverage.Signed.Through)
	}
	// A trusted stamp naming a checkpoint at line 2 that the trail does not
	// hold is a finding of the trail, recorded after the signed coverage is
	// decided, and is no check of the chain either.
	tsa := testAuthority(t, tsatest.Options{AccuracySeconds: 1})
	stamps := stampOf(t, tsa, wrong)
	pool := x509.NewCertPool()
	pool.AddCert(tsa.Root)
	stamped := StampOptions{Stamps: bytes.NewReader(stamps), StampsSize: int64(len(stamps))}
	stamped.Verify.Roots = pool
	signed := SignatureOptions{Keys: keys(signer), Sidecar: bytes.NewReader(sidecar), SidecarSize: int64(len(sidecar))}
	report = verifyWith(t, joinLines(lines), Options{Signatures: &signed, Stamps: &stamped})
	if !slices.Equal(findingNames(report.Chain), []string{"stamp-checkpoint-mismatch@2"}) || report.Chain.Coverage.Signed.Through != 3 {
		t.Fatalf("a trusted stamp of a checkpoint the trail does not hold: %v through %d", findingNames(report.Chain), report.Chain.Coverage.Signed.Through)
	}
	torn := append(joinLines(lines), []byte(`{"recordVersion"`)...)
	chain := verifySigned(t, torn, sidecar, SignatureOptions{Keys: keys(signer)})
	if !slices.Equal(findingNames(chain), []string{"incomplete-last-line@4"}) || chain.Coverage.Signed.Through != 3 {
		t.Fatalf("an incomplete last line: %v through %d", findingNames(chain), chain.Coverage.Signed.Through)
	}
}

// A finding about the sidecar is placed at the trail line it is about: a
// record signature's at its sequence, a rotation's at its at, a line out of
// order at the one it names, and signature-missing at the sequence required.
// Its detail names the sidecar line, counted from 1 over every line,
// unreadable ones included.
func TestASidecarFindingIsPlacedAtItsTrailLine(t *testing.T) {
	signer := signerOf(t, vectorSeed1)
	lines := memoryTrail(3)
	signed, err := signer.SignRecordLine(lines[0])
	if err != nil {
		t.Fatal(err)
	}
	signed = bytes.TrimSuffix(signed, []byte("\n"))
	rotation := bytes.TrimSuffix(signer.RotationLine(vectorTrail, 2, signerOf(t, vectorSeed2).public), []byte("\n"))
	beyond := bytes.Replace(signed, []byte(`"sequence":1`), []byte(`"sequence":9`), 1)
	sidecar := joinLines([][]byte{[]byte("not a sidecar line"), editSignature(signed), editSignature(rotation), signed, beyond})
	chain := verifySigned(t, joinLines(lines), sidecar, SignatureOptions{Keys: keys(signer), RequireThrough: 3})
	want := []struct {
		finding string
		line    string
	}{
		{"signature-invalid@1", "sidecar line 2 "},
		{"rotation-invalid@2", "sidecar line 3 "},
		{"sidecar-out-of-order@1", "sidecar line 4 "},
		{"signature-no-record@9", "sidecar line 5 "},
		{"signature-missing@3", "no valid signature covers the records up to sequence 3"},
	}
	names := findingNames(chain)
	if len(names) != len(want) {
		t.Fatalf("findings %v", names)
	}
	for index, expected := range want {
		if names[index] != expected.finding || !strings.HasPrefix(chain.Findings[index].Detail, expected.line) {
			t.Fatalf("finding %d: %s %q, want %s with %q", index, names[index], chain.Findings[index].Detail, expected.finding, expected.line)
		}
	}
	// signature-missing is about a requirement, not a sidecar line: placed at
	// the sequence required, its detail names that sequence and no sidecar
	// line, with a sidecar or with none.
	for _, sidecar := range [][]byte{sidecar, nil} {
		chain := verifySigned(t, joinLines(lines), sidecar, SignatureOptions{Keys: keys(signer), RequireThrough: 3})
		missing := chain.Findings[len(chain.Findings)-1]
		if missing.Name != "signature-missing" || missing.Line != 3 || missing.Detail != "no valid signature covers the records up to sequence 3: they are unsigned" || strings.Contains(missing.Detail, "sidecar line") {
			t.Fatalf("signature-missing: %+v", missing)
		}
	}
}

// A trail line is the bytes before a newline, and only such lines are read:
// bytes after the last newline are an incomplete last line, whatever they
// hold, never a chained record and never a line too long. The bound does not
// count the newline: a line of exactly the bound and its newline is read, and
// one byte more is line-too-long. The bound is lowered here, and held at its
// real value for unterminated bytes one past it.
func TestOnlyANewlineEndsALineAndTheBoundDoesNotCountIt(t *testing.T) {
	first := chainedAt(1, Digest(nil))
	second := chainedAt(2, Digest(first))
	if chain := verifyBytes(t, append(append(slices.Clone(first), '\n'), second...), nil); !slices.Equal(findingNames(chain), []string{"incomplete-last-line@2"}) || chain.Coverage.Chained != 1 {
		t.Fatalf("a chained record without its newline: %v %+v", findingNames(chain), chain.Coverage)
	}
	const bound = 300
	padded := func(length int) []byte {
		return slices.Concat([]byte("{"), bytes.Repeat([]byte(" "), length-len(first)), first[1:])
	}
	t.Run("lowered bound", func(t *testing.T) {
		lowerLineBound(t, bound)
		if chain := verifyBytes(t, joinLines([][]byte{padded(bound)}), nil); len(chain.Findings) != 0 || chain.Coverage.Chained != 1 {
			t.Fatalf("a line of exactly the bound and its newline is read: %v %+v", findingNames(chain), chain.Coverage)
		}
		if chain := verifyBytes(t, joinLines([][]byte{padded(bound + 1)}), nil); !slices.Equal(findingNames(chain), []string{"line-too-long@1"}) {
			t.Fatalf("one byte more: %v", findingNames(chain))
		}
		if chain := verifyBytes(t, padded(bound+1), nil); !slices.Equal(findingNames(chain), []string{"incomplete-last-line@1"}) {
			t.Fatalf("one byte more and no newline: %v", findingNames(chain))
		}
	})
	over := repeated{fill: 'x', count: maxLineBytes + 1}
	report, err := Verify(over, over.size(), Options{})
	if err != nil || !slices.Equal(findingNames(report.Chain), []string{"incomplete-last-line@1"}) {
		t.Fatalf("unterminated bytes one past the real bound: %v %v", findingNames(report.Chain), err)
	}
}

// A discontinuity record is well formed in one shape: its discontinuity
// member exactly reason, line, bytes and digest, of their forms, naming the
// line before it, a line no longer than the bound, with a stated length no
// longer than the bound. A well-formed one excuses the line it names, even
// when its length or digest then does not match, which is its own finding.
// One that is not well formed excuses nothing.
func TestADiscontinuityIsWellFormedInOneShape(t *testing.T) {
	damaged := chainedAt(7, Digest([]byte("x")))
	repair := func(names, length int64, digest string) []byte {
		line, err := encodeJSONLine(discontinuityRecord{
			RecordVersion: RecordVersion, Trail: vectorTrail, Sequence: 2, Previous: Digest(nil),
			Run: "repair0000000000", At: "2026-10-03T00:00:00Z", Kind: KindDiscontinuity, Surface: "audit repair",
			Tool:          Tool{Name: "jpack", Version: "test"},
			Discontinuity: discontinuity{Reason: ReasonIncompleteLastLine, Line: names, Bytes: length, Digest: digest},
		})
		if err != nil {
			t.Fatal(err)
		}
		return bytes.TrimSuffix(line, []byte("\n"))
	}
	verify := func(second []byte) result.AuditChain {
		return verifyBytes(t, joinLines([][]byte{damaged, second, chainedAt(3, Digest(second))}), nil)
	}
	good := repair(1, int64(len(damaged)), Digest(damaged))
	if chain := verify(good); len(chain.Findings) != 0 || chain.Status != "segmented" || chain.Coverage.Damaged != 1 {
		t.Fatalf("a well-formed discontinuity excuses the line it names: %v %s %+v", findingNames(chain), chain.Status, chain.Coverage)
	}
	for name, second := range map[string][]byte{
		"another digest": repair(1, int64(len(damaged)), Digest([]byte("other"))),
		"another length": repair(1, int64(len(damaged))+1, Digest(damaged)),
	} {
		if chain := verify(second); !slices.Equal(findingNames(chain), []string{"discontinuity-mismatch@2"}) || chain.Coverage.Damaged != 1 {
			t.Fatalf("%s: the line is excused and the mismatch is the discontinuity's own: %v", name, findingNames(chain))
		}
	}
	text := string(good)
	for name, second := range map[string]string{
		"a fifth member":                strings.Replace(text, `"digest":`, `"extra":1,"digest":`, 1),
		"a member given twice":          strings.Replace(text, `"digest":`, `"line":1,"digest":`, 1),
		"bytes written as 1.0":          strings.Replace(text, fmt.Sprintf(`"bytes":%d`, len(damaged)), fmt.Sprintf(`"bytes":%d.0`, len(damaged)), 1),
		"another reason":                strings.Replace(text, `"reason":"incomplete-last-line"`, `"reason":"other"`, 1),
		"naming another line":           string(repair(5, int64(len(damaged)), Digest(damaged))),
		"a digest in upper case":        strings.Replace(text, Digest(damaged), strings.ToUpper(Digest(damaged)), 1),
		"a length over the bound":       string(repair(1, maxLineBytes+1, Digest(damaged))),
		"a discontinuity not an object": text[:strings.Index(text, `"discontinuity":{`)] + `"discontinuity":"none"}`,
	} {
		if second == text {
			t.Fatalf("%s: the edit did not apply", name)
		}
		chain := verify([]byte(second))
		names := findingNames(chain)
		if !slices.Contains(names, "discontinuity-malformed@2") || !slices.Contains(names, "sequence-mismatch@1") || chain.Coverage.Damaged != 0 {
			t.Fatalf("%s: malformed, and the line it names is not excused: %v", name, names)
		}
	}
}
