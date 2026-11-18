package federation

import (
	"bytes"
	"encoding/hex"
	"path/filepath"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/regsig"
)

var signatureSeeds = []string{
	"locks/single-42.lock.json.sig.json",
	"signatures/billing.json",
	"vectors/verify-signature-version/lock.sig.json",
	"vectors/verify-signature-member/lock.sig.json",
	"vectors/verify-signature-key-small-order/lock.sig.json",
	"vectors/verify-lock-signature-s-plus-l/lock.sig.json",
	"vectors/verify-signature-over-bound/lock.sig.json",
}

// FuzzSignatureDocument reads a fuzzed document as either kind: one read
// with no failure has exactly its kind's five members, and its JCS bytes are
// what a signer writes for them; one with a failure has no identity; and a
// key identifier is named only for an object read.
func FuzzSignatureDocument(f *testing.F) {
	for _, name := range signatureSeeds {
		f.Add(testdataFile(f, name))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		for _, kind := range []regsig.Kind{regsig.Lock, regsig.Source} {
			read, problems := readSignature(registry.Document{Data: data}, kind)
			if len(problems) == 0 {
				if read == nil || len(read.root) != 5 || read.identity == "" || read.keyID == "" {
					t.Fatalf("read with no failure: %+v", read)
				}
				canonical, err := jcs.EncodeDocument(read.root)
				if err != nil {
					t.Fatal(err)
				}
				public := mustHex(t, read.root["publicKey"].(string))
				signature := mustHex(t, read.root["signature"].(string))
				written, err := kind.Encode(public, read.root[kind.DigestMember].(string), signature)
				if err != nil || !bytes.Equal(written, append(canonical, '\n')) {
					t.Fatalf("read %s, written %s (%v)", canonical, written, err)
				}
				continue
			}
			if read != nil && read.identity != "" {
				t.Fatal("a refused document has an identity")
			}
			if read == nil && (len(problems) != 1 || problems[0].code != "LOCK-SIG-DOCUMENT") {
				t.Fatalf("unread with %+v", problems)
			}
		}
	})
}

// FuzzRecord reads a fuzzed record file: one it accepts is written back as
// the same record.
func FuzzRecord(f *testing.F) {
	good, _ := encodeRecord(recordID, Record{42, digestA})
	f.Add(good)
	f.Add([]byte(`{"digest":"sha256:","formatVersion":"1.0.0","registryId":"x","serial":1e3}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		record, reason := decodeRecord(data, recordID)
		if record == nil {
			if reason == "" {
				t.Fatal("refused with no reason")
			}
			return
		}
		if record.Serial < 1 || record.Serial > MaxSerial || !isDigest(record.Digest) {
			t.Fatalf("accepted %+v", record)
		}
		written, err := encodeRecord(recordID, *record)
		if err != nil {
			t.Fatal(err)
		}
		if again, _ := decodeRecord(written, recordID); again == nil || *again != *record {
			t.Fatalf("%+v written as %s", record, written)
		}
	})
}

// FuzzVerify verifies the assembled row's lock with a fuzzed lock signature
// and a fuzzed billing signature: a verification accepts only the documents
// the rows signed, member for member, and a refused one writes no record.
func FuzzVerify(f *testing.F) {
	lockSignature := testdataFile(f, "locks/assembled-7.lock.json.sig.json")
	billing := testdataFile(f, "signatures/billing.json")
	f.Add(lockSignature, billing)
	f.Add(testdataFile(f, "vectors/verify-lock-signature-s-plus-l/lock.sig.json"), billing)
	f.Add(lockSignature, testdataFile(f, "signatures/billing-wrong-key.json"))
	lock := registry.Document{Data: testdataFile(f, "locks/assembled-7.lock.json")}
	index := registry.Document{Data: testdataFile(f, "assembled/two-sources/index.json")}
	standard := registry.Document{Data: testdataFile(f, "signatures/standard.json")}
	id, require := "https://example.invalid/registries/platform", true
	signers := &Signers{Key: "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a", Repositories: map[string]string{"https://example.invalid/scm/billing": "3d4017c3e843895a92b70aa74d1b7ebc9c982ccf2ec4968cc0cd55f12af4660c"}}
	same := func(a, b []byte) bool {
		x, _ := readSignature(registry.Document{Data: a}, regsig.Lock)
		y, _ := readSignature(registry.Document{Data: b}, regsig.Lock)
		if x == nil || y == nil {
			return false
		}
		p, _ := jcs.EncodeDocument(x.root)
		q, _ := jcs.EncodeDocument(y.root)
		return bytes.Equal(p, q)
	}
	sameSource := func(a, b []byte) bool {
		x, _ := readSignature(registry.Document{Data: a}, regsig.Source)
		y, _ := readSignature(registry.Document{Data: b}, regsig.Source)
		return x != nil && y != nil && x.identity != "" && x.identity == y.identity
	}
	f.Fuzz(func(t *testing.T, fuzzedLock, fuzzedBilling []byte) {
		dir := filepath.Join(t.TempDir(), "records")
		floor := int64(1)
		out, err := Verify(VerifyInput{
			Lock:             lock,
			LockSignatures:   []GivenDocument{Held(registry.Document{Data: fuzzedLock})},
			SourceSignatures: []GivenDocument{Held(registry.Document{Data: fuzzedBilling}), Held(standard)},
			Index:            &index,
			Signers:          signers,
			Expect:           Expect{RegistryID: &id, RequireIndex: &require},
			Floor:            &floor,
			Records:          NewRecordStore(dir),
		})
		if err != nil {
			t.Fatal(err)
		}
		record, problem := NewRecordStore(dir).read(id)
		if problem != nil {
			t.Fatal(problem)
		}
		if !out.Report.Valid {
			if record != nil {
				t.Fatal("a refused lock is recorded")
			}
			return
		}
		if !same(fuzzedLock, lockSignature) || !sameSource(fuzzedBilling, billing) {
			t.Fatalf("accepted\n%q\n%q", fuzzedLock, fuzzedBilling)
		}
		if record == nil || record.Serial != 7 {
			t.Fatalf("recorded %+v", record)
		}
	})
}

func mustHex(t *testing.T, text string) []byte {
	t.Helper()
	out, err := hex.DecodeString(text)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
