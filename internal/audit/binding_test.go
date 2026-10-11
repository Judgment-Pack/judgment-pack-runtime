package audit

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// runnerKind is the scheme a companion names its binding by; any kind in the
// grammar would do.
const runnerKind = "judgment-pack-runner/preparation/1"

// hex64 is a well-formed digest's hexadecimal part.
var hex64 = strings.Repeat("0123456789abcdef", 4)

// A binding document is held to its shape and to nothing else (ADR-0050):
// exactly kind and digest, each of its grammar, and every other document is
// refused with the sentence that names what is wrong and no value of it.
func TestParseBindingHoldsTheShapeAndNothingElse(t *testing.T) {
	binding, err := ParseBinding([]byte(`{"kind":"` + runnerKind + `","digest":"sha256:` + hex64 + `"}`))
	if err != nil || binding.Kind != runnerKind || binding.Digest != "sha256:"+hex64 {
		t.Fatalf("as given: %+v %v", binding, err)
	}
	// Member order, JSON's own whitespace and a JSON escape do not matter: a
	// string is its value.
	binding, err = ParseBinding([]byte(" \t\r\n{ \"digest\" : \"sha256:" + hex64 + "\",\n \"kind\": \"judgment-pack-runner\\/preparation\\u002f1\" }\n"))
	if err != nil || binding.Kind != runnerKind || binding.Digest != "sha256:"+hex64 {
		t.Fatalf("reordered, spaced and escaped: %+v %v", binding, err)
	}
	if binding, err := ParseBinding([]byte(`{"kind":"` + strings.Repeat("a", 128) + `","digest":"sha256:` + hex64 + `"}`)); err != nil || len(binding.Kind) != 128 {
		t.Fatalf("a kind of 128 characters: %+v %v", binding, err)
	}
	if binding, err := ParseBinding([]byte(`{"kind":"a.b_c/d-0","digest":"sha256:` + hex64 + `"}`)); err != nil || binding.Kind != "a.b_c/d-0" {
		t.Fatalf("every character class of the grammar: %+v %v", binding, err)
	}
	valid := `{"kind":"k","digest":"sha256:` + hex64 + `"}`
	for _, tc := range []struct {
		name, document string
		want           error
	}{
		{"empty", ``, errBindingNotObject},
		{"whitespace", `   `, errBindingNotObject},
		{"not JSON", `{`, errBindingNotObject},
		{"null", `null`, errBindingNotObject},
		{"an array", `[` + valid + `]`, errBindingNotObject},
		{"a string", `"sha256:` + hex64 + `"`, errBindingNotObject},
		{"two texts", valid + ` {}`, errBindingNotObject},
		{"a form feed before the object", "\f" + valid, errBindingNotObject},
		{"a member twice", `{"kind":"k","kind":"k","digest":"sha256:` + hex64 + `"}`, errBindingNotObject},
		{"no member", `{}`, errBindingMembers},
		{"no digest", `{"kind":"k"}`, errBindingMembers},
		{"no kind", `{"digest":"sha256:` + hex64 + `"}`, errBindingMembers},
		{"an extra member", `{"kind":"k","digest":"sha256:` + hex64 + `","note":"x"}`, errBindingMembers},
		{"a member by another case", `{"Kind":"k","digest":"sha256:` + hex64 + `"}`, errBindingMembers},
		{"an empty kind", `{"kind":"","digest":"sha256:` + hex64 + `"}`, errBindingKind},
		{"a kind of 129 characters", `{"kind":"` + strings.Repeat("a", 129) + `","digest":"sha256:` + hex64 + `"}`, errBindingKind},
		{"a kind in uppercase", `{"kind":"Runner","digest":"sha256:` + hex64 + `"}`, errBindingKind},
		{"a kind with a space", `{"kind":"a b","digest":"sha256:` + hex64 + `"}`, errBindingKind},
		{"a kind with a colon", `{"kind":"a:b","digest":"sha256:` + hex64 + `"}`, errBindingKind},
		{"a kind with a newline at its end", `{"kind":"a\n","digest":"sha256:` + hex64 + `"}`, errBindingKind},
		{"a kind with a lone surrogate", `{"kind":"a\ud800","digest":"sha256:` + hex64 + `"}`, errBindingKind},
		{"a kind not a string", `{"kind":1,"digest":"sha256:` + hex64 + `"}`, errBindingKind},
		{"a null kind", `{"kind":null,"digest":"sha256:` + hex64 + `"}`, errBindingKind},
		{"a digest without its prefix", `{"kind":"k","digest":"` + hex64 + `"}`, errBindingDigest},
		{"a digest of another algorithm", `{"kind":"k","digest":"sha512:` + hex64 + `"}`, errBindingDigest},
		{"a digest in uppercase hex", `{"kind":"k","digest":"sha256:` + strings.ToUpper(hex64) + `"}`, errBindingDigest},
		{"a digest of 63 hex", `{"kind":"k","digest":"sha256:` + hex64[1:] + `"}`, errBindingDigest},
		{"a digest of 65 hex", `{"kind":"k","digest":"sha256:` + hex64 + `0"}`, errBindingDigest},
		{"a digest with a newline at its end", `{"kind":"k","digest":"sha256:` + hex64 + `\n"}`, errBindingDigest},
		{"a digest not a string", `{"kind":"k","digest":7}`, errBindingDigest},
	} {
		if binding, err := ParseBinding([]byte(tc.document)); !errors.Is(err, tc.want) {
			t.Errorf("%s: %+v, %v; want %v", tc.name, binding, err, tc.want)
		}
	}
	// The bound: a valid document padded to exactly 1024 bytes reads, and
	// one byte over is refused for its size before it is parsed.
	atLimit := append([]byte(valid), bytes.Repeat([]byte(" "), MaxBindingBytes-len(valid))...)
	if binding, err := ParseBinding(atLimit); err != nil || binding.Kind != "k" {
		t.Fatalf("a document of exactly the bound reads: %+v %v", binding, err)
	}
	if _, err := ParseBinding(append(atLimit, ' ')); !errors.Is(err, ErrBindingTooLarge) {
		t.Fatalf("a document one byte over the bound: %v", err)
	}
	// Each refusal is a fixed sentence, and none repeats the document.
	for _, err := range []error{ErrBindingTooLarge, errBindingNotObject, errBindingMembers, errBindingKind, errBindingDigest} {
		if strings.Contains(err.Error(), "Runner") || strings.Contains(err.Error(), hex64) {
			t.Fatalf("a refusal names a value: %v", err)
		}
	}
}

// boundWriter is a chaining writer signing with signer and binding binding.
func boundWriter(t *testing.T, w *Writer, signer *Signer, binding *Binding) *Writer {
	t.Helper()
	writer := NewWriter(w.root, "audit", true)
	writer.SignWith(signer)
	writer.Bind(binding)
	return writer
}

// A binding is recorded exactly as given once it passes its shape, beside the
// citations, in the one canonical spelling a record member has: compact, kind
// then digest, whatever order and spacing the caller wrote. A record written
// without one carries no member. The member is part of the chained line, so
// the record signature and the chain cover it: one byte of a recorded digest
// changed, the binding still well formed, is caught as any altered member is,
// and on the last line by the signature alone.
func TestABindingIsRecordedAsGivenUnderTheChainAndTheSignature(t *testing.T) {
	signer := signerOf(t, vectorSeed1)
	writer, dir := writerAt(t, "audit")
	binding, err := ParseBinding([]byte(" { \"digest\": \"sha256:" + hex64 + "\", \"kind\": \"" + runnerKind + "\" }\n"))
	if err != nil {
		t.Fatal(err)
	}
	cites := []Citation{{SessionID: "s-1", CallIndex: 3, Signature: strings.Repeat("3", 128)}}
	if err := boundWriter(t, writer, signer, binding).Evaluation(evaluated(), Inputs{Facts: []byte(`{"n":0}`)}, cites, []byte(`{}`), nil); err != nil {
		t.Fatal(err)
	}
	appendSigned(t, writer.root, signer, `{"n":1}`)
	if err := boundWriter(t, writer, signer, binding).Evaluation(evaluated(), Inputs{Facts: []byte(`{"n":2}`)}, nil, []byte(`{}`), nil); err != nil {
		t.Fatal(err)
	}
	trail := readTrailFile(t, dir)
	sidecar := readSidecar(t, dir)
	lines := splitLines(trail)
	if len(lines) != 3 {
		t.Fatalf("three records, got %d", len(lines))
	}
	member := `"binding":{"kind":"` + runnerKind + `","digest":"sha256:` + hex64 + `"}`
	beside := `"cites":[{"sessionId":"s-1","callIndex":3,"signature":"` + strings.Repeat("3", 128) + `"}],` + member + `,"disposition":`
	if !bytes.Contains(lines[0], []byte(beside)) || bytes.Count(lines[0], []byte(`"binding"`)) != 1 {
		t.Fatalf("the binding as given, beside the citations: %s", lines[0])
	}
	if bytes.Contains(lines[1], []byte(`"binding"`)) {
		t.Fatalf("an evaluation without a binding records none: %s", lines[1])
	}
	if !bytes.Contains(lines[2], []byte(`"provenance":"bundled"},`+member+`,"disposition":`)) {
		t.Fatalf("the binding as given, without citations: %s", lines[2])
	}
	chain := verifySigned(t, trail, sidecar, SignatureOptions{Keys: keys(signer)})
	if chain.Status != "valid" || chain.FindingsTotal != 0 || chain.Coverage.Signed.Through != 3 || chain.Coverage.SignedRecords != 3 {
		t.Fatalf("a bound trail verifies: %v %+v", findingNames(chain), chain.Coverage)
	}
	// The digest's last character changed to another hexadecimal one: the
	// binding is still well formed, so only the protections every member has
	// can catch it.
	altered := func(line []byte) []byte {
		return bytes.Replace(line, []byte(hex64+`"}`), []byte(hex64[:63]+`e"}`), 1)
	}
	edited := splitLines(trail)
	edited[0] = altered(edited[0])
	if bytes.Equal(edited[0], lines[0]) {
		t.Fatal("the edit changed nothing")
	}
	chain = verifySigned(t, joinLines(edited), sidecar, SignatureOptions{Keys: keys(signer)})
	if !slices.Equal(findingNames(chain), []string{"signature-record-mismatch@1", "previous-mismatch@2"}) || chain.Coverage.Signed.Status != "none" {
		t.Fatalf("an altered binding on the first record: %v %+v", findingNames(chain), chain.Coverage)
	}
	if plain := verifyBytes(t, joinLines(edited), nil); !slices.Equal(findingNames(plain), []string{"previous-mismatch@2"}) {
		t.Fatalf("the chain alone catches it before the last line: %v", findingNames(plain))
	}
	edited = splitLines(trail)
	edited[2] = altered(edited[2])
	chain = verifySigned(t, joinLines(edited), sidecar, SignatureOptions{Keys: keys(signer)})
	if !slices.Equal(findingNames(chain), []string{"signature-record-mismatch@3"}) || chain.Coverage.Signed.Through != 2 {
		t.Fatalf("an altered binding on the last record: %v %+v", findingNames(chain), chain.Coverage)
	}
	if plain := verifyBytes(t, joinLines(edited), nil); plain.Status != "valid" {
		t.Fatalf("on the last line only the signature sees it: %v", findingNames(plain))
	}
}

// audit verify holds a recorded binding to its shape and to nothing else
// (ADR-0050): a binding that is there and malformed is binding-malformed at
// its line, in a fixed sentence naming no value; a well-formed one is not
// mentioned, since nothing here can say what its digest is a digest of.
func TestAMalformedBindingIsAFindingAndAWellFormedOneIsNot(t *testing.T) {
	signer := signerOf(t, vectorSeed1)
	writer, dir := writerAt(t, "audit")
	appendSigned(t, writer.root, signer, `{"n":0}`)
	if err := boundWriter(t, writer, signer, &Binding{Kind: runnerKind, Digest: "sha256:" + hex64}).Evaluation(evaluated(), Inputs{Facts: []byte(`{"n":1}`)}, nil, []byte(`{}`), nil); err != nil {
		t.Fatal(err)
	}
	trail := readTrailFile(t, dir)
	if chain := verifyBytes(t, trail, nil); chain.Status != "valid" || chain.FindingsTotal != 0 {
		t.Fatalf("a well-formed binding is not mentioned: %v", findingNames(chain))
	}
	member := `"binding":{"kind":"` + runnerKind + `","digest":"sha256:` + hex64 + `"}`
	for name, replacement := range map[string]string{
		"null":                     `"binding":null`,
		"a string":                 `"binding":"sha256:` + hex64 + `"`,
		"an array":                 `"binding":[]`,
		"no member":                `"binding":{}`,
		"no digest":                `"binding":{"kind":"` + runnerKind + `"}`,
		"an extra member":          `"binding":{"kind":"` + runnerKind + `","digest":"sha256:` + hex64 + `","x":1}`,
		"a member twice":           `"binding":{"kind":"` + runnerKind + `","kind":"` + runnerKind + `","digest":"sha256:` + hex64 + `"}`,
		"a kind in uppercase":      `"binding":{"kind":"Runner","digest":"sha256:` + hex64 + `"}`,
		"a kind of 129 characters": `"binding":{"kind":"` + strings.Repeat("a", 129) + `","digest":"sha256:` + hex64 + `"}`,
		"a digest of 63 hex":       `"binding":{"kind":"` + runnerKind + `","digest":"sha256:` + hex64[1:] + `"}`,
		"a digest of another form": `"binding":{"kind":"` + runnerKind + `","digest":"` + hex64 + `"}`,
	} {
		lines := splitLines(trail)
		lines[1] = bytes.Replace(lines[1], []byte(member), []byte(replacement), 1)
		chain := verifyBytes(t, joinLines(lines), nil)
		if !slices.Equal(findingNames(chain), []string{"binding-malformed@2"}) || chain.Status != "invalid" ||
			chain.Findings[0].Detail != bindingMalformedDetail {
			t.Errorf("%s: %v %s", name, chain.Findings, chain.Status)
		}
	}
	if strings.Contains(bindingMalformedDetail, hex64) || strings.Contains(bindingMalformedDetail, runnerKind) {
		t.Fatalf("the detail names a value: %s", bindingMalformedDetail)
	}
}

// A malformed binding is a finding about one member of a record, not a
// check of the chain: a trail whose records are all consistent, signed and
// checkpointed is invalid for it, and its coverage is what it would be
// without it. A writer holds no binding to its shape itself -- the surfaces
// do, with ParseBinding, before it gets there -- so one handed a malformed
// binding writes it, and the verifier is what reports it. Unchained records
// are held to the same shape.
func TestAMalformedBindingMovesNoCoverage(t *testing.T) {
	signer := signerOf(t, vectorSeed1)
	writer, dir := writerAt(t, "audit")
	appendSigned(t, writer.root, signer, `{"n":0}`)
	if err := boundWriter(t, writer, signer, &Binding{Kind: "Not In The Grammar", Digest: "sha256:" + hex64}).Evaluation(evaluated(), Inputs{Facts: []byte(`{"n":1}`)}, nil, []byte(`{}`), nil); err != nil {
		t.Fatal(err)
	}
	appendSigned(t, writer.root, signer, `{"n":2}`)
	trail := readTrailFile(t, dir)
	sidecar := readSidecar(t, dir)
	head := splitLines(trail)[2]
	link := readChain(t, head)
	options := SignatureOptions{Keys: keys(signer)}
	options.Sidecar, options.SidecarSize = bytes.NewReader(sidecar), int64(len(sidecar))
	report, err := Verify(bytes.NewReader(trail), int64(len(trail)), Options{
		Signatures: &options,
		Held:       []result.AuditCheckpoint{{CheckpointVersion: result.CheckpointVersion, RecordDigest: Digest(head), Sequence: 3, Trail: link.trail}},
	})
	if err != nil {
		t.Fatal(err)
	}
	chain := report.Chain
	if !slices.Equal(findingNames(chain), []string{"binding-malformed@2"}) || chain.Status != "invalid" {
		t.Fatalf("findings: %v %s", findingNames(chain), chain.Status)
	}
	if chain.Coverage.Signed.Status != "through" || chain.Coverage.Signed.Through != 3 || chain.Coverage.SignedRecords != 3 ||
		chain.Coverage.Checkpointed.Status != "through" || chain.Coverage.Checkpointed.Through != 3 || chain.Coverage.Witnessed != 3 {
		t.Fatalf("the coverage is the chain's and the signatures': %+v", chain.Coverage)
	}
	if !containsString(chain.Establishes, fmt.Sprintf(establishesSigned, 3)) || !containsString(chain.Establishes, fmt.Sprintf(establishesCheckpoint, 3)) {
		t.Fatalf("statements: %q", chain.Establishes)
	}
	// Unchained: a writer with chain off records the binding as well, and the
	// verifier holds it to the same shape.
	plain, plainDir := writerAt(t, "audit")
	unchained := NewWriter(plain.root, "audit", false)
	unchained.Bind(&Binding{Kind: runnerKind, Digest: "sha1:" + hex64[:40]})
	if err := unchained.Evaluation(evaluated(), Inputs{Facts: []byte(`{}`)}, nil, []byte(`{}`), nil); err != nil {
		t.Fatal(err)
	}
	if chain := verifyBytes(t, readTrailFile(t, plainDir), nil); !slices.Equal(findingNames(chain), []string{"binding-malformed@1"}) || chain.Coverage.Chained != 0 {
		t.Fatalf("an unchained record's binding: %v %+v", findingNames(chain), chain.Coverage)
	}
}
