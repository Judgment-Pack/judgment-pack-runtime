package record_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/sdk/record"
)

// The golden records under testdata are written by hand: the plain one is
// the hand-written Core 0.3.0 record of package sdk's tests on the main line,
// whose bytes this change keeps, and the keyed one holds HMAC-SHA-256 values
// computed outside this module over the hand-written canonical texts below,
// under testKey.
const (
	goldenFacts    = `{"amount":"1250"}`
	goldenEvidence = `{"invoice":"present"}`
	keyedFacts     = `{"amount":"1250","member":"M-0042"}`
	keyedEvidence  = `{"invoice":"present"}`
	testKeyID      = "k-7f3a9c"
)

// testKey is recognisable, so a test can search every output for it.
var testKey = []byte("RECOGNISABLE-TEST-KEY-7f3a9c-do-not-use")

func golden(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return bytes.TrimRight(data, "\r\n")
}

func sha(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// hmacWith is the caller's keyed-digest function over one key: the key stays
// in this closure.
func hmacWith(key []byte) record.KeyedDigestFunc {
	return func(algorithm, keyID string, canonical []byte) ([]byte, error) {
		if algorithm != record.HMACSHA256 || keyID != testKeyID {
			return nil, errors.New("no key for that algorithm and key identifier")
		}
		mac := hmac.New(sha256.New, key)
		mac.Write(canonical)
		return mac.Sum(nil), nil
	}
}

// Read reads both golden records: each in its own form, with JSON the bytes
// read and Digest their digest. The keyed record has no plain digest in any
// field.
func TestReadReadsBothForms(t *testing.T) {
	plainBytes, keyedBytes := golden(t, "record-1.0.0-plain.json"), golden(t, "record-2.0.0-keyed.json")
	plain, err := record.Read(plainBytes)
	if err != nil {
		t.Fatal(err)
	}
	if plain.Form() != record.FormPlain || plain.Keyed != nil || plain.FactsDigest != sha(goldenFacts) || plain.EvidenceDigest != sha(goldenEvidence) ||
		plain.Pack.SpecVersion != "0.3.0" || plain.Evaluator != (record.Evaluator{RuntimeVersion: "v0.45.0", SpecVersion: "0.3.0"}) ||
		len(plain.Binding) != 2 || !bytes.Equal(plain.JSON(), plainBytes) || plain.Digest() != sha(string(plainBytes)) {
		t.Fatalf("plain record read as %#v", plain)
	}
	keyed, err := record.Read(keyedBytes)
	if err != nil {
		t.Fatal(err)
	}
	wantFacts, wantEvidence := hmacOf(keyedFacts), hmacOf(keyedEvidence)
	if keyed.Form() != record.FormKeyed || keyed.FactsDigest != "" || keyed.EvidenceDigest != "" || keyed.Keyed == nil ||
		keyed.Keyed.Algorithm != record.HMACSHA256 || keyed.Keyed.KeyID != testKeyID ||
		!bytes.Equal(keyed.Keyed.Facts, wantFacts) || !bytes.Equal(keyed.Keyed.Evidence, wantEvidence) ||
		!bytes.Equal(keyed.JSON(), keyedBytes) || keyed.Digest() != sha(string(keyedBytes)) {
		t.Fatalf("keyed record read as %#v", keyed)
	}
	if record.RecordVersion != "1.0.0" || record.KeyedRecordVersion != "2.0.0" {
		t.Fatalf("record versions %s and %s", record.RecordVersion, record.KeyedRecordVersion)
	}
}

func hmacOf(text string) []byte {
	mac := hmac.New(sha256.New, testKey)
	mac.Write([]byte(text))
	return mac.Sum(nil)
}

// edit replaces one exact substring of a golden record, which must occur.
func edit(t *testing.T, data []byte, old, new string) []byte {
	t.Helper()
	if bytes.Count(data, []byte(old)) != 1 {
		t.Fatalf("%q is not in the golden record once", old)
	}
	return bytes.Replace(data, []byte(old), []byte(new), 1)
}

// A keyed record is never read as a plain one, nor the reverse: each version
// admits exactly its own members, so a record that carries the other form's
// digest member, or the other form's version, or a version this reader does
// not know, is refused, never read with the member passed over. Every other
// member is held to its shape. No error repeats the bytes.
func TestAKeyedRecordIsNeverReadAsAPlainOneNorTheReverse(t *testing.T) {
	plain, keyed := golden(t, "record-1.0.0-plain.json"), golden(t, "record-2.0.0-keyed.json")
	keyedMember := `"keyedDigests":{"algorithm":"hmac-sha256","evidence":"` + hex.EncodeToString(hmacOf(keyedEvidence)) + `","facts":"` + hex.EncodeToString(hmacOf(keyedFacts)) + `","keyId":"k-7f3a9c"},`
	plainFacts := `"factsDigest":"` + sha(goldenFacts) + `",`
	rows := []struct {
		name string
		data []byte
	}{
		{"a keyed record under the plain version", edit(t, keyed, `"recordVersion":"2.0.0"`, `"recordVersion":"1.0.0"`)},
		{"a plain record under the keyed version", edit(t, plain, `"recordVersion":"1.0.0"`, `"recordVersion":"2.0.0"`)},
		{"a keyed record that also holds a factsDigest", edit(t, keyed, `"keyedDigests":`, plainFacts+`"keyedDigests":`)},
		{"a keyed record that also holds an evidenceDigest", edit(t, keyed, `"keyedDigests":`, `"evidenceDigest":"`+sha(keyedEvidence)+`","keyedDigests":`)},
		{"a plain record that also holds keyedDigests", edit(t, plain, `"pack":`, keyedMember+`"pack":`)},
		{"a plain record with keyedDigests in place of factsDigest", edit(t, plain, plainFacts, keyedMember)},
		{"a plain record without its evidenceDigest", edit(t, plain, `"evidenceDigest":"`+sha(goldenEvidence)+`",`, "")},
		{"a later minor version", edit(t, plain, `"recordVersion":"1.0.0"`, `"recordVersion":"1.1.0"`)},
		{"a later patch version", edit(t, keyed, `"recordVersion":"2.0.0"`, `"recordVersion":"2.0.1"`)},
		{"a later major version", edit(t, keyed, `"recordVersion":"2.0.0"`, `"recordVersion":"3.0.0"`)},
		{"no version", edit(t, plain, `,"recordVersion":"1.0.0"`, "")},
		{"an unknown member", edit(t, plain, `"pack":`, `"note":"x","pack":`)},
		{"another algorithm", edit(t, keyed, `"hmac-sha256"`, `"hmac-sha512"`)},
		{"the algorithm in upper case", edit(t, keyed, `"hmac-sha256"`, `"HMAC-SHA256"`)},
		{"an empty key identifier", edit(t, keyed, `"keyId":"k-7f3a9c"`, `"keyId":""`)},
		{"a key identifier of 129 characters", edit(t, keyed, `"keyId":"k-7f3a9c"`, `"keyId":"`+strings.Repeat("k", 129)+`"`)},
		{"a key identifier with a space", edit(t, keyed, `"keyId":"k-7f3a9c"`, `"keyId":"k 7f3a9c"`)},
		{"a key identifier outside ASCII", edit(t, keyed, `"keyId":"k-7f3a9c"`, `"keyId":"k-é7f3a9c"`)},
		{"a keyed digest one digit short", edit(t, keyed, `"facts":"`+hex.EncodeToString(hmacOf(keyedFacts)), `"facts":"`+hex.EncodeToString(hmacOf(keyedFacts))[1:])},
		{"a keyed digest in upper case", edit(t, keyed, hex.EncodeToString(hmacOf(keyedFacts)), strings.ToUpper(hex.EncodeToString(hmacOf(keyedFacts))))},
		{"a keyed digest in the plain digest's form", edit(t, keyed, `"facts":"`, `"facts":"sha256:`)},
		{"keyedDigests without its keyId", edit(t, keyed, `,"keyId":"k-7f3a9c"`, "")},
		{"keyedDigests with a key member", edit(t, keyed, `"keyId":"k-7f3a9c"`, `"key":"52454347","keyId":"k-7f3a9c"`)},
		{"a plain digest in another form", edit(t, plain, `"factsDigest":"sha256:`, `"factsDigest":"sha512:`)},
		{"an empty binding", edit(t, keyed, `{"settings":"sha256:abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd"}`, `{}`)},
		{"a malformed binding name", edit(t, keyed, `"settings"`, `"Settings"`)},
		{"a pack without its digest", edit(t, keyed, `"pack":{"digest":"sha256:c86a61e383471b0f3ed0d348ef00637b5ea12c60711460782b968333461a5c46",`, `"pack":{`)},
		{"an evaluator with another member", edit(t, keyed, `"evaluator":{`, `"evaluator":{"build":"x",`)},
		{"white space", edit(t, keyed, `"recordVersion":`, `"recordVersion": `)},
		{"a disposition that is not an object", edit(t, keyed, `{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"review","reasons":[]}`, `"outcome"`)},
		{"not JSON", []byte(`{"recordVersion":"2.0.0"`)},
		{"empty", nil},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			read, err := record.Read(row.data)
			if !errors.Is(err, record.ErrRecord) || read.JSON() != nil || read.Form() != "" {
				t.Fatalf("read %s as %#v, error %v", row.data, read, err)
			}
			for _, fragment := range []string{"7f3a9c", "abcdef", "M-0042", hex.EncodeToString(hmacOf(keyedFacts))[:16]} {
				if strings.Contains(err.Error(), fragment) {
					t.Fatalf("error %q repeats the record", err)
				}
			}
		})
	}
	// The bounds themselves are admitted.
	for _, id := range []string{strings.Repeat("K", record.MaxKeyIDLength), "a", "AZaz09._-:/"} {
		if _, err := record.Read(edit(t, keyed, `"keyId":"k-7f3a9c"`, `"keyId":"`+id+`"`)); err != nil {
			t.Fatalf("key identifier %q: %v", id, err)
		}
	}
}

// frozenV1 is the reader of record version 1.0.0 as ADR-0079 and
// VERSIONING.md documented it before the keyed form existed, frozen here: it
// admits a recordVersion whose major part is 1, passes over a member it does
// not know (the minor rule), and needs factsDigest and evidenceDigest, each
// "sha256:" and 64 lower-case hexadecimal digits. ignoreVersion is the most
// lenient such reader, one that does not look at recordVersion at all.
func frozenV1(data []byte, ignoreVersion bool) (facts, evidence string, err error) {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(data, &members); err != nil {
		return "", "", err
	}
	var version string
	if err := json.Unmarshal(members["recordVersion"], &version); err != nil && !ignoreVersion {
		return "", "", err
	}
	if !ignoreVersion && !strings.HasPrefix(version, "1.") {
		return "", "", fmt.Errorf("record version %q is not of major version 1", version)
	}
	for name, target := range map[string]*string{"factsDigest": &facts, "evidenceDigest": &evidence} {
		raw, given := members[name]
		if !given {
			return "", "", fmt.Errorf("no %s", name)
		}
		if err := json.Unmarshal(raw, target); err != nil || len(*target) != 71 || !strings.HasPrefix(*target, "sha256:") {
			return "", "", fmt.Errorf("%s is not a sha256 digest", name)
		}
	}
	return facts, evidence, nil
}

// A 1.0.0 reader reads the plain golden record as it always did, and refuses
// a keyed record cleanly: by its major version, and, for a reader that does
// not look at the version, because no factsDigest or evidenceDigest is there
// to read. It never finds a keyed digest where it looks for a plain one.
func TestAOneZeroZeroReaderRefusesAKeyedRecord(t *testing.T) {
	plain, keyed := golden(t, "record-1.0.0-plain.json"), golden(t, "record-2.0.0-keyed.json")
	for _, ignore := range []bool{false, true} {
		facts, evidence, err := frozenV1(plain, ignore)
		if err != nil || facts != sha(goldenFacts) || evidence != sha(goldenEvidence) {
			t.Fatalf("the 1.0.0 reader on the plain record: %s %s %v", facts, evidence, err)
		}
		facts, evidence, err = frozenV1(keyed, ignore)
		if err == nil || facts != "" || evidence != "" {
			t.Fatalf("the 1.0.0 reader (ignoring the version: %t) read a keyed record: %s %s", ignore, facts, evidence)
		}
	}
	if _, _, err := frozenV1(keyed, false); err == nil || !strings.Contains(err.Error(), "major version 1") {
		t.Fatalf("the 1.0.0 reader refused the keyed record for another reason: %v", err)
	}
}

// CheckDocuments compares the documents a caller holds with the record's
// digests. On a plain record it needs no key and never calls the function;
// on a keyed record the right key gives a match, a wrong key or other
// documents a mismatch, and no function "not checkable without the key" for
// both documents, never a match and never a mismatch. It reads the record's
// bytes, not its fields.
func TestCheckDocuments(t *testing.T) {
	plain, err := record.Read(golden(t, "record-1.0.0-plain.json"))
	if err != nil {
		t.Fatal(err)
	}
	keyed, err := record.Read(golden(t, "record-2.0.0-keyed.json"))
	if err != nil {
		t.Fatal(err)
	}
	never := func(string, string, []byte) ([]byte, error) {
		t.Fatal("the keyed-digest function was called for a plain record")
		return nil, nil
	}
	rows := []struct {
		name            string
		record          record.DecisionRecord
		facts, evidence string
		omitEvidence    bool
		keyed           record.KeyedDigestFunc
		want            record.DocumentCheck
	}{
		{"plain, the documents spelled another way", plain, "{ \"amount\" : \"12\\u0035\\u0030\" }", `{"invoice": "present"}`, false, never, record.DocumentCheck{Form: record.FormPlain, Facts: record.Match, Evidence: record.Match}},
		{"plain, other facts", plain, `{"amount":"1251"}`, goldenEvidence, false, nil, record.DocumentCheck{Form: record.FormPlain, Facts: record.Mismatch, Evidence: record.Match}},
		{"plain, evidence omitted", plain, goldenFacts, "", true, nil, record.DocumentCheck{Form: record.FormPlain, Facts: record.Match, Evidence: record.Mismatch}},
		{"keyed, the right key", keyed, "{\"member\":\"M-0042\",\n\"amount\":\"1250\"}", keyedEvidence, false, hmacWith(testKey), record.DocumentCheck{Form: record.FormKeyed, Facts: record.Match, Evidence: record.Match}},
		{"keyed, a wrong key", keyed, keyedFacts, keyedEvidence, false, hmacWith([]byte("another-key")), record.DocumentCheck{Form: record.FormKeyed, Facts: record.Mismatch, Evidence: record.Mismatch}},
		{"keyed, the right key and other facts", keyed, `{"amount":"1250","member":"M-0043"}`, keyedEvidence, false, hmacWith(testKey), record.DocumentCheck{Form: record.FormKeyed, Facts: record.Mismatch, Evidence: record.Match}},
		{"keyed, no key", keyed, keyedFacts, keyedEvidence, false, nil, record.DocumentCheck{Form: record.FormKeyed, Facts: record.NotCheckableWithoutKey, Evidence: record.NotCheckableWithoutKey}},
		{"keyed, no key and other facts", keyed, `{"other":true}`, "", true, nil, record.DocumentCheck{Form: record.FormKeyed, Facts: record.NotCheckableWithoutKey, Evidence: record.NotCheckableWithoutKey}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			var evidence []byte
			if !row.omitEvidence {
				evidence = []byte(row.evidence)
			}
			got, err := row.record.CheckDocuments([]byte(row.facts), evidence, row.keyed)
			if err != nil || got != row.want {
				t.Fatalf("check = %#v, %v; want %#v", got, err, row.want)
			}
		})
	}
	// The fields are the caller's copy; the check reads the bytes.
	edited := keyed
	edited.Keyed = &record.KeyedDigests{Algorithm: record.HMACSHA256, KeyID: testKeyID, Facts: hmacOf(`{"other":true}`), Evidence: hmacOf("{}")}
	edited.FactsDigest = sha(`{"other":true}`)
	if got, err := edited.CheckDocuments([]byte(`{"other":true}`), nil, hmacWith(testKey)); err != nil || got.Facts != record.Mismatch {
		t.Fatalf("an edited field moved the check: %#v, %v", got, err)
	}
	if _, err := (record.DecisionRecord{FactsDigest: sha(goldenFacts)}).CheckDocuments([]byte(goldenFacts), nil, nil); err == nil || errors.Is(err, record.ErrDocument) {
		t.Fatalf("a struct literal was checked: %v", err)
	}
	if _, err := keyed.CheckDocuments([]byte(`{"a":1,"a":2}`), nil, nil); !errors.Is(err, record.ErrDocument) {
		t.Fatalf("a refused document: %v", err)
	}
	short := func(string, string, []byte) ([]byte, error) { return make([]byte, 31), nil }
	if _, err := keyed.CheckDocuments([]byte(keyedFacts), nil, short); !errors.Is(err, record.ErrKeyedDigestFunc) {
		t.Fatalf("a function returning 31 bytes: %v", err)
	}
}

// The key is never in a record or in any error or text this package writes:
// a keyed-digest function whose own error carries the key fails the check
// with an error that does not repeat it, and from which the caller's error is
// still reachable. Neither is the key identifier repeated by an error.
func TestTheKeyIsInNoOutputOfTheReader(t *testing.T) {
	keyed, err := record.Read(golden(t, "record-2.0.0-keyed.json"))
	if err != nil {
		t.Fatal(err)
	}
	leaking := fmt.Errorf("no such key; tried %s (%x)", testKey, testKey)
	failing := func(string, string, []byte) ([]byte, error) { return nil, leaking }
	var outputs []string
	_, err = keyed.CheckDocuments([]byte(keyedFacts), []byte(keyedEvidence), failing)
	if !errors.Is(err, record.ErrKeyedDigestFunc) || !errors.Is(err, leaking) {
		t.Fatalf("a failing function: %v", err)
	}
	outputs = append(outputs, err.Error(), fmt.Sprintf("%v %+v %#v", err, err, err))
	for _, key := range []record.KeyedDigestFunc{hmacWith(testKey), hmacWith([]byte("another-key")), nil} {
		check, err := keyed.CheckDocuments([]byte(keyedFacts), []byte(keyedEvidence), key)
		if err != nil {
			t.Fatal(err)
		}
		outputs = append(outputs, fmt.Sprintf("%#v", check))
	}
	marshalled, err := json.Marshal(keyed)
	if err != nil {
		t.Fatal(err)
	}
	outputs = append(outputs, string(keyed.JSON()), string(marshalled), fmt.Sprintf("%#v", keyed))
	if _, err := record.Read(edit(t, golden(t, "record-2.0.0-keyed.json"), `"keyId":"k-7f3a9c"`, `"keyId":"k 7f3a9c"`)); err != nil {
		outputs = append(outputs, err.Error())
	}
	searchForKey(t, outputs, "k 7f3a9c")
}

// searchForKey fails when any output holds the test key, in its bytes, its
// hexadecimal or base64 encoding, or extra, which is a value no output
// should repeat.
func searchForKey(t *testing.T, outputs []string, extra string) {
	t.Helper()
	forms := []string{string(testKey), hex.EncodeToString(testKey), strings.ToUpper(hex.EncodeToString(testKey)),
		base64.StdEncoding.EncodeToString(testKey), base64.RawURLEncoding.EncodeToString(testKey), "RECOGNISABLE-TEST-KEY", extra}
	for _, output := range outputs {
		for _, form := range forms {
			if form != "" && strings.Contains(output, form) {
				t.Fatalf("an output holds %q: %s", form, output)
			}
		}
	}
}

// Read holds the members it does not take from the documents to the shapes
// this runtime writes, in both forms: the disposition to a JPS Core §8.3
// disposition of the members, closed vocabularies and presence rules its
// evaluator contract writes, in canonical form, with no draft value member;
// the evaluator's specVersion to a contract this reader knows; and the
// pack's id, version and specVersion to their grammars. What this runtime
// writes is admitted. No error repeats the bytes.
func TestReadHoldsTheDispositionPackAndEvaluatorToTheirShapes(t *testing.T) {
	const disposition = `{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"review","reasons":[]}`
	const id = `"id":"https://example.invalid/judgment-packs/sdk-record"`
	const version = `"version":"1.0.0"}`
	for _, form := range []struct{ name, file, spec, after string }{
		{"plain", "record-1.0.0-plain.json", "0.3.0", "evidenceDigest"},
		{"keyed", "record-2.0.0-keyed.json", "0.5.0", "keyedDigests"},
	} {
		data := golden(t, form.file)
		evaluator := `"specVersion":"` + form.spec + `"},"` + form.after + `"`
		evaluatorAs := func(spec string) [2]string {
			return [2]string{evaluator, `"specVersion":"` + spec + `"},"` + form.after + `"`}
		}
		pack := `"specVersion":"` + form.spec + `","version"`
		packAs := func(spec string) [2]string { return [2]string{pack, `"specVersion":"` + spec + `","version"`} }
		refused := map[string][2]string{
			"an empty disposition":                     {disposition, `{}`},
			"a kind that is a number":                  {disposition, `{"handoff":{"state":"none"},"kind":1,"outcomeId":"review","reasons":[]}`},
			"a kind outside the vocabulary":            {disposition, `{"handoff":{"state":"none"},"kind":"bogus-kind","outcomeId":"review","reasons":[]}`},
			"a draft value member":                     {disposition, `{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"review","reasons":[],"value":{"amount":"1250"}}`},
			"a member of no disposition":               {disposition, `{"handoff":{"state":"none"},"kind":"outcome","note":"x","outcomeId":"review","reasons":[]}`},
			"a member spelled in another case":         {disposition, `{"Kind":"outcome","handoff":{"state":"none"},"outcomeId":"review","reasons":[]}`},
			"no handoff":                               {disposition, `{"kind":"outcome","outcomeId":"review","reasons":[]}`},
			"an outcome without its outcomeId":         {disposition, `{"handoff":{"state":"none"},"kind":"outcome","reasons":[]}`},
			"an empty outcomeId":                       {disposition, `{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"","reasons":[]}`},
			"an outcome with a reason":                 {disposition, `{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"review","reasons":["no-match"]}`},
			"a reason outside the vocabulary":          {disposition, `{"handoff":{"state":"none"},"kind":"unresolved","reasons":["bogus-reason"]}`},
			"reasons out of order":                     {disposition, `{"handoff":{"state":"none"},"kind":"unresolved","reasons":["unknown","conflict"]}`},
			"a reason twice":                           {disposition, `{"handoff":{"state":"none"},"kind":"unresolved","reasons":["unknown","unknown"]}`},
			"a not-applicable result with an outcome":  {disposition, `{"handoff":{"state":"none"},"kind":"not-applicable","outcomeId":"review","reasons":["not-applicable"]}`},
			"a handoff state outside the vocabulary":   {disposition, `{"handoff":{"state":"bogus-state"},"kind":"outcome","outcomeId":"review","reasons":[]}`},
			"a requested handoff with no trigger":      {disposition, `{"handoff":{"state":"requested"},"kind":"unresolved","reasons":["unknown"]}`},
			"an empty triggeredBy":                     {disposition, `{"handoff":{"state":"none","triggeredBy":[]},"kind":"outcome","outcomeId":"review","reasons":[]}`},
			"a trigger that is not a reason":           {disposition, `{"handoff":{"state":"requested","triggeredBy":["conflict"]},"kind":"unresolved","reasons":["unknown"]}`},
			"a handoff member of no handoff":           {disposition, `{"handoff":{"state":"none","target":"x"},"kind":"outcome","outcomeId":"review","reasons":[]}`},
			"a route of a kind outside the vocabulary": {disposition, `{"handoff":{"routes":[{"kind":"bogus-route","name":"desk"}],"state":"requested","triggeredBy":["unknown"]},"kind":"unresolved","reasons":["unknown"]}`},
			"a route with another member":              {disposition, `{"handoff":{"routes":[{"kind":"queue","name":"desk","note":"x"}],"state":"requested","triggeredBy":["unknown"]},"kind":"unresolved","reasons":["unknown"]}`},
			"routes that disagree with the state":      {disposition, `{"handoff":{"routes":[],"state":"requested","triggeredBy":["unknown"]},"kind":"unresolved","reasons":["unknown"]}`},
			"a pack id with a control character":       {id, `"id":"https://example.invalid/\u0001"`},
			"a pack id that is no absolute URI":        {id, `"id":"sdk-record"`},
			"a pack id with a space":                   {id, `"id":"https://example.invalid/sdk record"`},
			"a pack version of two numbers":            {version, `"version":"1.0"}`},
			"a pack version with a leading zero":       {version, `"version":"01.0.0"}`},
			"a pack version with a label":              {version, `"version":"1.0.0-draft"}`},
			"a pack specVersion of no grammar":         packAs("bogus"),
			"a pack specVersion with an empty label":   packAs("0.3.0-"),
			"an evaluator specVersion of no grammar":   evaluatorAs("bogus"),
			"an evaluator contract no reader knows":    evaluatorAs("9.9.9"),
			"the retired draft selector":               evaluatorAs("0.4.0-draft"),
		}
		for row, change := range refused {
			t.Run(form.name+"/"+row, func(t *testing.T) {
				edited := edit(t, data, change[0], change[1])
				read, err := record.Read(edited)
				if !errors.Is(err, record.ErrRecord) || read.JSON() != nil {
					t.Fatalf("read %s as %#v, error %v", edited, read, err)
				}
				if want := because(row); !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q does not say %q", err, want)
				}
				for _, fragment := range []string{"bogus", "review", "desk", "sdk-record", "1250", "v0.45.0"} {
					if strings.Contains(err.Error(), fragment) {
						t.Fatalf("error %q repeats the record", err)
					}
				}
			})
		}
		for row, change := range map[string][2]string{
			"a not-applicable result":              {disposition, `{"handoff":{"state":"none"},"kind":"not-applicable","reasons":["not-applicable"]}`},
			"a requested handoff":                  {disposition, `{"handoff":{"state":"requested","triggeredBy":["unknown"]},"kind":"unresolved","reasons":["conflict","unknown"]}`},
			"a routed handoff":                     {disposition, `{"handoff":{"routes":[{"kind":"queue","name":"desk"}],"state":"requested","triggeredBy":["unknown"]},"kind":"unresolved","reasons":["unknown"]}`},
			"a routed pack that selected no route": {disposition, `{"handoff":{"routes":[],"state":"none"},"kind":"outcome","outcomeId":"review","reasons":[]}`},
			"a pack id of another scheme":          {id, `"id":"urn:example:sdk-record"`},
			"a pack specVersion with a label":      packAs("0.2.0-draft"),
			"the earlier evaluator contract":       evaluatorAs("0.2.0-draft"),
			"the other later evaluator contract":   evaluatorAs(map[string]string{"0.3.0": "0.5.0", "0.5.0": "0.3.0"}[form.spec]),
			"an evaluator with no runtime version": {`"evaluator":{"runtimeVersion":"v0.45.0",`, `"evaluator":{`},
		} {
			t.Run(form.name+"/admitted/"+row, func(t *testing.T) {
				edited := edit(t, data, change[0], change[1])
				if _, err := record.Read(edited); err != nil {
					t.Fatalf("%s: %v", edited, err)
				}
			})
		}
		// A routed disposition is refused under the earlier contract, which
		// writes no routes: the disposition is held to the contract named.
		routed := edit(t, data, disposition, `{"handoff":{"routes":[{"kind":"queue","name":"desk"}],"state":"requested","triggeredBy":["unknown"]},"kind":"unresolved","reasons":["unknown"]}`)
		routed = edit(t, routed, evaluatorAs("0.2.0-draft")[0], evaluatorAs("0.2.0-draft")[1])
		if _, err := record.Read(routed); !errors.Is(err, record.ErrRecord) || !strings.Contains(err.Error(), "its disposition is not") {
			t.Fatalf("%s: a routed disposition under 0.2.0-draft: %v", form.name, err)
		}
		// The draft value member is refused under the earlier contract too,
		// whose disposition type admits it: package sdk never writes it.
		valued := edit(t, data, disposition, `{"handoff":{"state":"none"},"kind":"outcome","outcomeId":"review","reasons":[],"value":{"amount":"1250"}}`)
		valued = edit(t, valued, evaluatorAs("0.2.0-draft")[0], evaluatorAs("0.2.0-draft")[1])
		if _, err := record.Read(valued); !errors.Is(err, record.ErrRecord) || !strings.Contains(err.Error(), "holds a member this runtime never writes") {
			t.Fatalf("%s: a value member under 0.2.0-draft: %v", form.name, err)
		}
	}
}

// because is the part of Read's fixed sentence a refused row of
// TestReadHoldsTheDispositionPackAndEvaluatorToTheirShapes must give.
func because(row string) string {
	switch {
	case row == "a draft value member", row == "a member of no disposition", row == "a member spelled in another case", row == "a handoff member of no handoff":
		return "its disposition holds a member this runtime never writes"
	case strings.Contains(row, "evaluator"), row == "the retired draft selector":
		return "its evaluator is not"
	case strings.Contains(row, "pack"):
		return "its pack is not"
	}
	return "its disposition is not"
}
