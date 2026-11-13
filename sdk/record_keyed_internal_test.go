package sdk

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/sdk/record"
)

// keyedKey is the recognisable synthetic key of the golden keyed record,
// held only by the tests' own keyed-digest code.
var keyedKey = []byte("RECOGNISABLE-TEST-KEY-7f3a9c-do-not-use")

// The HMAC-SHA-256 values under keyedKey of the hand-written canonical texts
// {"amount":"1250","member":"M-0042"}, {"invoice":"present"} and {},
// computed outside this module.
const (
	keyedFactsMAC    = "e7f602ba0b2a3002a6d8c6056e00c0a98f3b198467bb3dd5fc231563e88e1e23"
	keyedEvidenceMAC = "456b73db91e85562c6199148f638c3f7530a415a6a501d0f6c0b84a43eba583b"
	keyedEmptyMAC    = "bfc86cc321a10572353a43fea41e293424347f11becafe25ff333f461d6ca20c"
)

func unhex(t *testing.T, text string) []byte {
	t.Helper()
	data, err := hex.DecodeString(text)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func goldenRecord(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("record", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimRight(string(data), "\r\n")
}

func loadPack(t *testing.T, text string) *Pack {
	t.Helper()
	loaded, err := Load(context.Background(), []byte(text), LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

func hmacOver(key []byte, text []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(text)
	return mac.Sum(nil)
}

// The two golden records, both written by hand: the plain one is the Core
// 0.3.0 record TestTwoDecisionRecordsByHand writes out, with every digest
// spelled, and its bytes are the ones the record had before the keyed form
// existed; the keyed one carries the HMAC-SHA-256 values above, and the plain
// form's inputs give the plain bytes whatever the keyed form does.
func TestTheGoldenRecordsByHand(t *testing.T) {
	withRuntimeVersion(t, "v0.45.0")
	_, plain, err := loadPack(t, recordPack030).EvaluateWithRecord(context.Background(),
		[]byte("{ \"amount\" : \"12\\u0035\\u0030\" }"), []byte(`{"invoice": "present"}`),
		record.Binding{"settings": "sha256:abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd", "judgment-pack-runner/preparation/1": "sha256:1111111111111111111111111111111111111111111111111111111111111111"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(plain.JSON()), goldenRecord(t, "record-1.0.0-plain.json"); got != want {
		t.Fatalf("plain record\n got %s\nwant %s", got, want)
	}
	if plain.Form() != record.FormPlain || plain.Keyed != nil || plain.Digest() != "sha256:4d189398c3a0518f7a688b8c3cd6f0c5807f10c9131632621b5f489dabdb2c8d" {
		t.Fatalf("plain record form %q, keyed %v, digest %s", plain.Form(), plain.Keyed, plain.Digest())
	}

	_, keyed, err := loadPack(t, recordPack050).EvaluateWithKeyedRecord(context.Background(),
		[]byte(`{"member": "M-0042", "amount": "1250"}`), []byte(`{"invoice":"present"}`),
		record.Binding{"settings": "sha256:abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd"},
		record.KeyedDigests{Algorithm: record.HMACSHA256, KeyID: "k-7f3a9c", Facts: unhex(t, keyedFactsMAC), Evidence: unhex(t, keyedEvidenceMAC)})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(keyed.JSON()), goldenRecord(t, "record-2.0.0-keyed.json"); got != want {
		t.Fatalf("keyed record\n got %s\nwant %s", got, want)
	}
	if keyed.Form() != record.FormKeyed || keyed.FactsDigest != "" || keyed.EvidenceDigest != "" || keyed.Keyed == nil ||
		keyed.Keyed.KeyID != "k-7f3a9c" || hex.EncodeToString(keyed.Keyed.Facts) != keyedFactsMAC ||
		keyed.Digest() != "sha256:dd36ae6149d23c826112cdd04f060f2956955f89670c4a0a44ae9b8175909154" {
		t.Fatalf("keyed record fields %#v, digest %s", keyed, keyed.Digest())
	}
	for _, built := range []record.DecisionRecord{plain, keyed} {
		read, err := record.Read(built.JSON())
		if err != nil || !bytes.Equal(read.JSON(), built.JSON()) || read.Digest() != built.Digest() || read.Form() != built.Form() {
			t.Fatalf("Read(%s) = %v", built.JSON(), err)
		}
	}
}

// CanonicalFacts and CanonicalEvidence return the exact bytes a plain record
// digests: for every spelling, the SHA-256 of those bytes is the record's
// factsDigest and evidenceDigest, an omitted evidence document included; and
// a keyed digest the caller computes over the same bytes is the one a
// reader with the key checks as a match.
func TestTheCanonicalBytesAreWhatTheRecordDigests(t *testing.T) {
	pack := loadPack(t, recordPack050)
	for _, documents := range [][2]string{
		{`{"amount":"1250"}`, `{"invoice":"present"}`},
		{"{\n \"n\": [1.50, -0, 1e400, 12345678901234567890],\n \"amount\": \"9\" }", ""},
		{`{"amount":"1250","":"<\u0001","😀":{"b":null,"a":[true,false]}}`, `{ "invoice" : "absent" }`},
	} {
		var evidence []byte
		if documents[1] != "" {
			evidence = []byte(documents[1])
		}
		_, plain, err := pack.EvaluateWithRecord(context.Background(), []byte(documents[0]), evidence, nil)
		if err != nil {
			t.Fatal(err)
		}
		facts, err := record.CanonicalFacts([]byte(documents[0]))
		if err != nil {
			t.Fatal(err)
		}
		canonicalEvidence, err := record.CanonicalEvidence(evidence)
		if err != nil {
			t.Fatal(err)
		}
		if sha(string(facts)) != plain.FactsDigest || sha(string(canonicalEvidence)) != plain.EvidenceDigest {
			t.Fatalf("facts %q: canonical %q and %q are not what the record digests: %s", documents[0], facts, canonicalEvidence, plain.JSON())
		}
		_, keyed, err := pack.EvaluateWithKeyedRecord(context.Background(), []byte(documents[0]), evidence, nil,
			record.KeyedDigests{Algorithm: record.HMACSHA256, KeyID: "k1", Facts: hmacOver(keyedKey, facts), Evidence: hmacOver(keyedKey, canonicalEvidence)})
		if err != nil {
			t.Fatal(err)
		}
		check, err := keyed.CheckDocuments([]byte(documents[0]), evidence, func(_, _ string, canonical []byte) ([]byte, error) {
			return hmacOver(keyedKey, canonical), nil
		})
		if err != nil || check != (record.DocumentCheck{Form: record.FormKeyed, Facts: record.Match, Evidence: record.Match}) {
			t.Fatalf("the keyed record of %q checks as %#v, %v", documents[0], check, err)
		}
		if keyed.Disposition != plain.Disposition || keyed.Pack != plain.Pack || keyed.Evaluator != plain.Evaluator {
			t.Fatalf("the two forms record other evaluations: %s\n%s", plain.JSON(), keyed.JSON())
		}
	}
}

// A keyed record carries no plain digest of either document anywhere: not
// in its bytes, in the prefixed form or as bare hexadecimal digits, not in
// its fields, not in the struct as encoding/json writes it, and not in its
// own digest's preimage; the only members that follow from the facts are the
// keyed digests and the disposition. Its digest is not the plain record's.
func TestAKeyedRecordCarriesNoPlainDigest(t *testing.T) {
	pack := loadPack(t, recordPack050)
	facts, evidence := []byte(`{ "amount": "1250", "member": "M-0042" }`), []byte(`{"invoice": "present"}`)
	_, plain, err := pack.EvaluateWithRecord(context.Background(), facts, evidence, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, keyed, err := pack.EvaluateWithKeyedRecord(context.Background(), facts, evidence, nil,
		record.KeyedDigests{Algorithm: record.HMACSHA256, KeyID: "k-7f3a9c", Facts: unhex(t, keyedFactsMAC), Evidence: unhex(t, keyedEvidenceMAC)})
	if err != nil {
		t.Fatal(err)
	}
	marshalled, err := json.Marshal(keyed)
	if err != nil {
		t.Fatal(err)
	}
	outputs := []string{string(keyed.JSON()), string(marshalled), fmt.Sprintf("%#v", keyed), keyed.FactsDigest, keyed.EvidenceDigest}
	for _, plainDigest := range []string{plain.FactsDigest, plain.EvidenceDigest, sha(string(facts)), sha(string(evidence))} {
		bare := strings.TrimPrefix(plainDigest, "sha256:")
		sum, _ := hex.DecodeString(bare)
		for _, output := range outputs {
			if strings.Contains(output, bare) || strings.Contains(output, base64.StdEncoding.EncodeToString(sum)) {
				t.Fatalf("a keyed record output holds the plain digest %s: %s", plainDigest, output)
			}
		}
	}
	if keyed.Digest() == plain.Digest() || bytes.Contains(keyed.JSON(), []byte(`"factsDigest"`)) || bytes.Contains(keyed.JSON(), []byte(`"evidenceDigest"`)) {
		t.Fatalf("keyed %s\nplain %s", keyed.JSON(), plain.JSON())
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(keyed.JSON(), &members); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(members))
	for name := range members {
		names = append(names, name)
	}
	slices.Sort(names)
	if strings.Join(names, ",") != "disposition,evaluator,keyedDigests,pack,recordVersion" {
		t.Fatalf("keyed record members %v", names)
	}
}

// Keyed digests not of their shape are refused before anything is
// evaluated: with facts the evaluator would refuse, the keyed digests' error
// is the one returned. It wraps record.ErrKeyedDigests, is no *Refusal and
// no internal fault, and repeats no label, key identifier or digest. The
// bounds themselves are admitted.
func TestMalformedKeyedDigestsAreRefusedBeforeEvaluating(t *testing.T) {
	pack := loadPack(t, recordPack030)
	digest := bytes.Repeat([]byte{0x5a}, 32)
	good := record.KeyedDigests{Algorithm: record.HMACSHA256, KeyID: "k-1", Facts: digest, Evidence: digest}
	with := func(change func(*record.KeyedDigests)) record.KeyedDigests {
		keyed := good
		change(&keyed)
		return keyed
	}
	rows := []struct {
		name   string
		keyed  record.KeyedDigests
		reason string
	}{
		{"an unknown algorithm", with(func(k *record.KeyedDigests) { k.Algorithm = "hmac-sha512" }), "the algorithm is not hmac-sha256"},
		{"the algorithm in upper case", with(func(k *record.KeyedDigests) { k.Algorithm = "HMAC-SHA256" }), "the algorithm is not hmac-sha256"},
		{"no algorithm", with(func(k *record.KeyedDigests) { k.Algorithm = "" }), "the algorithm is not hmac-sha256"},
		{"the plain algorithm", with(func(k *record.KeyedDigests) { k.Algorithm = "sha256" }), "the algorithm is not hmac-sha256"},
		{"an empty key identifier", with(func(k *record.KeyedDigests) { k.KeyID = "" }), "the key identifier is not 1 to 128 characters"},
		{"a key identifier of 129 characters", with(func(k *record.KeyedDigests) { k.KeyID = strings.Repeat("q", 129) }), "the key identifier is not 1 to 128 characters"},
		{"a key identifier with a space", with(func(k *record.KeyedDigests) { k.KeyID = "my key" }), "the key identifier is not 1 to 128 characters"},
		{"a key identifier with an at sign", with(func(k *record.KeyedDigests) { k.KeyID = "person@example.invalid" }), "the key identifier is not 1 to 128 characters"},
		{"a key identifier outside ASCII", with(func(k *record.KeyedDigests) { k.KeyID = "clé" }), "the key identifier is not 1 to 128 characters"},
		{"a key identifier with a control character", with(func(k *record.KeyedDigests) { k.KeyID = "k\n1" }), "the key identifier is not 1 to 128 characters"},
		{"no facts digest", with(func(k *record.KeyedDigests) { k.Facts = nil }), "the facts digest is not the size"},
		{"a facts digest of 31 bytes", with(func(k *record.KeyedDigests) { k.Facts = digest[:31] }), "the facts digest is not the size"},
		{"a facts digest of 33 bytes", with(func(k *record.KeyedDigests) { k.Facts = append(bytes.Clone(digest), 0) }), "the facts digest is not the size"},
		{"no evidence digest", with(func(k *record.KeyedDigests) { k.Evidence = nil }), "the evidence digest is not the size"},
		{"an evidence digest of 64 bytes", with(func(k *record.KeyedDigests) { k.Evidence = bytes.Repeat(digest, 2) }), "the evidence digest is not the size"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			evaluated, decided, err := pack.EvaluateWithKeyedRecord(context.Background(), []byte(`{"amount":`), nil, nil, row.keyed)
			var refusal *Refusal
			if !errors.Is(err, record.ErrKeyedDigests) || errors.As(err, &refusal) || errors.Is(err, ErrInternal) || errors.Is(err, record.ErrBinding) {
				t.Fatalf("error %v (%T), want one wrapping record.ErrKeyedDigests", err, err)
			}
			if !strings.Contains(err.Error(), row.reason) {
				t.Fatalf("error %q does not say %q", err, row.reason)
			}
			for _, given := range []string{row.keyed.KeyID, row.keyed.Algorithm, hex.EncodeToString(row.keyed.Facts), hex.EncodeToString(row.keyed.Evidence)} {
				if len(given) > 3 && !strings.Contains(record.HMACSHA256, given) && strings.Contains(err.Error(), given) {
					t.Fatalf("error %q repeats %q", err, given)
				}
			}
			if evaluated != nil || decided.JSON() != nil {
				t.Fatal("refused keyed digests returned an evaluation or a record")
			}
		})
	}
	// A malformed binding is still the binding's error.
	if _, _, err := pack.EvaluateWithKeyedRecord(context.Background(), []byte(`{"amount":`), nil, record.Binding{"Upper": "x"}, good); !errors.Is(err, record.ErrBinding) {
		t.Fatalf("a malformed binding beside keyed digests: %v", err)
	}
	// The bounds are admitted, and facts the evaluator refuses are then
	// Evaluate's refusal.
	for _, id := range []string{strings.Repeat("Q", record.MaxKeyIDLength), "z", "AZaz09._-:/"} {
		keyed := with(func(k *record.KeyedDigests) { k.KeyID = id })
		if _, decided, err := pack.EvaluateWithKeyedRecord(context.Background(), []byte(`{"amount":"1"}`), nil, nil, keyed); err != nil || decided.Keyed.KeyID != id {
			t.Fatalf("key identifier %q: %v", id, err)
		}
	}
	var refusal *Refusal
	if _, _, err := pack.EvaluateWithKeyedRecord(context.Background(), []byte(`{"amount":`), nil, nil, good); !errors.As(err, &refusal) {
		t.Fatalf("facts the evaluator refuses, beside good keyed digests: %v", err)
	}
}

// A keyed record must not carry, beside the keyed digests, the plain digest
// they replace: a keyed digest that is the plain SHA-256 of a document,
// as passed or as canonical text, is refused, and so is a binding digest
// that is one, each in a fixed sentence and never as a fault. The same
// binding on a plain record, which carries the plain digest anyway, is
// admitted as before.
func TestAPlainDigestIsRefusedInAKeyedRecord(t *testing.T) {
	pack := loadPack(t, recordPack030)
	facts, evidence := []byte(`{ "amount" : "1250" }`), []byte(`{"invoice": "present"}`)
	plainOf := func(text []byte) []byte { sum := sha256.Sum256(text); return sum[:] }
	canonicalFacts, canonicalEvidence := []byte(`{"amount":"1250"}`), []byte(`{"invoice":"present"}`)
	mac := bytes.Repeat([]byte{0x33}, 32)
	keyedRows := map[string]record.KeyedDigests{
		"the facts digest is the canonical text's SHA-256":    {Algorithm: record.HMACSHA256, KeyID: "k", Facts: plainOf(canonicalFacts), Evidence: mac},
		"the facts digest is the passed bytes' SHA-256":       {Algorithm: record.HMACSHA256, KeyID: "k", Facts: plainOf(facts), Evidence: mac},
		"the evidence digest is the canonical text's SHA-256": {Algorithm: record.HMACSHA256, KeyID: "k", Facts: mac, Evidence: plainOf(canonicalEvidence)},
		"the evidence digest is the passed bytes' SHA-256":    {Algorithm: record.HMACSHA256, KeyID: "k", Facts: mac, Evidence: plainOf(evidence)},
		"the evidence digest is the facts' SHA-256":           {Algorithm: record.HMACSHA256, KeyID: "k", Facts: mac, Evidence: plainOf(canonicalFacts)},
	}
	for name, keyed := range keyedRows {
		evaluated, decided, err := pack.EvaluateWithKeyedRecord(context.Background(), facts, evidence, nil, keyed)
		if !errors.Is(err, record.ErrKeyedDigests) || errors.Is(err, ErrInternal) || evaluated != nil || decided.JSON() != nil ||
			!strings.Contains(err.Error(), "a keyed digest is the plain SHA-256") {
			t.Fatalf("%s: %v", name, err)
		}
	}
	good := record.KeyedDigests{Algorithm: record.HMACSHA256, KeyID: "k", Facts: mac, Evidence: mac}
	for name, plainDigest := range map[string][]byte{
		"the canonical facts": canonicalFacts, "the passed facts": facts,
		"the canonical evidence": canonicalEvidence, "the passed evidence": evidence,
	} {
		binding := record.Binding{"settings": "sha256:" + hex.EncodeToString(plainOf(plainDigest))}
		evaluated, decided, err := pack.EvaluateWithKeyedRecord(context.Background(), facts, evidence, binding, good)
		if !errors.Is(err, record.ErrBinding) || errors.Is(err, ErrInternal) || evaluated != nil || decided.JSON() != nil ||
			strings.Contains(err.Error(), hex.EncodeToString(plainOf(plainDigest))) {
			t.Fatalf("a binding of %s: %v", name, err)
		}
		if _, plain, err := pack.EvaluateWithRecord(context.Background(), facts, evidence, binding); err != nil || plain.Binding["settings"] != binding["settings"] {
			t.Fatalf("the same binding on a plain record: %v", err)
		}
	}
	// The omitted evidence document's canonical text is {}.
	_, _, err := pack.EvaluateWithKeyedRecord(context.Background(), facts, nil, nil,
		record.KeyedDigests{Algorithm: record.HMACSHA256, KeyID: "k", Facts: mac, Evidence: plainOf([]byte("{}"))})
	if !errors.Is(err, record.ErrKeyedDigests) {
		t.Fatalf("an evidence digest that is the SHA-256 of {}: %v", err)
	}
	if _, decided, err := pack.EvaluateWithKeyedRecord(context.Background(), facts, nil, nil,
		record.KeyedDigests{Algorithm: record.HMACSHA256, KeyID: "k", Facts: mac, Evidence: unhex(t, keyedEmptyMAC)}); err != nil || decided.Form() != record.FormKeyed {
		t.Fatalf("an omitted evidence document, keyed: %v", err)
	}
}

// The key never reaches the record or any error or text: the caller computes
// the digests, the record and every error the build and the check give
// carry none of the key's forms, and a keyed record's typed fields hold only
// copies the caller's edits do not reach.
func TestTheKeyIsInNoOutput(t *testing.T) {
	pack := loadPack(t, recordPack050)
	facts, evidence := []byte(`{"amount":"1250","member":"M-0042"}`), []byte(`{"invoice":"present"}`)
	canonicalFacts, _ := record.CanonicalFacts(facts)
	canonicalEvidence, _ := record.CanonicalEvidence(evidence)
	keyed := record.KeyedDigests{Algorithm: record.HMACSHA256, KeyID: "k-7f3a9c", Facts: hmacOver(keyedKey, canonicalFacts), Evidence: hmacOver(keyedKey, canonicalEvidence)}
	_, decided, err := pack.EvaluateWithKeyedRecord(context.Background(), facts, evidence, nil, keyed)
	if err != nil {
		t.Fatal(err)
	}
	keyed.Facts[0] ^= 0xff
	keyed.KeyID = "edited"
	if hex.EncodeToString(decided.Keyed.Facts) != keyedFactsMAC || decided.Keyed.KeyID != "k-7f3a9c" {
		t.Fatalf("the record's keyed digests follow the caller's: %#v", decided.Keyed)
	}
	marshalled, _ := json.Marshal(decided)
	outputs := []string{string(decided.JSON()), string(marshalled), fmt.Sprintf("%#v", decided)}
	leaking := func(string, string, []byte) ([]byte, error) { return nil, fmt.Errorf("lookup failed for %s", keyedKey) }
	for _, function := range []record.KeyedDigestFunc{leaking, nil, func(_, _ string, canonical []byte) ([]byte, error) {
		return hmacOver([]byte("wrong"), canonical), nil
	}} {
		check, err := decided.CheckDocuments(facts, evidence, function)
		outputs = append(outputs, fmt.Sprintf("%#v %v", check, err))
		if err != nil {
			outputs = append(outputs, err.Error())
		}
	}
	for _, malformed := range []record.KeyedDigests{
		{Algorithm: string(keyedKey), KeyID: "k", Facts: keyed.Facts, Evidence: keyed.Evidence},
		{Algorithm: record.HMACSHA256, KeyID: string(keyedKey) + " ", Facts: keyed.Facts, Evidence: keyed.Evidence},
		{Algorithm: record.HMACSHA256, KeyID: "k", Facts: keyedKey, Evidence: keyed.Evidence},
	} {
		_, _, err := pack.EvaluateWithKeyedRecord(context.Background(), facts, evidence, nil, malformed)
		if err == nil {
			t.Fatal("malformed keyed digests were admitted")
		}
		outputs = append(outputs, err.Error())
	}
	for _, output := range outputs {
		for _, form := range []string{string(keyedKey), hex.EncodeToString(keyedKey), base64.StdEncoding.EncodeToString(keyedKey), "RECOGNISABLE"} {
			if strings.Contains(output, form) {
				t.Fatalf("an output holds the key as %q: %s", form, output)
			}
		}
	}
}
