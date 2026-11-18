package federation_test

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/federation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// The RFC 8032 §7.1 TEST 1 and TEST 2 seeds, which signed the rows: public
// keys d75a9801… and 3d4017c3….
const (
	seedTest1 = "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60"
	seedTest2 = "4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb"
)

func signerOf(t testing.TB, seed string) *audit.Signer {
	t.Helper()
	raw, err := hex.DecodeString(seed)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := audit.NewSignerFromSeed(raw)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

// A signer's lock and source signature documents are the rows' own, byte
// for byte: five members in code-point order, no white space, one final line
// feed, over §10.3's messages (Ed25519 is deterministic, so one key and one
// message make one signature).
func TestSignersWriteTheRowsSignatureDocuments(t *testing.T) {
	for _, lock := range []string{"locks/single-42.lock.json", "locks/single-43.lock.json", "locks/assembled-7.lock.json"} {
		document, err := signerOf(t, seedTest1).SignRegistryLock(federation.LockDigest(readRowFile(t, lock)))
		if err != nil {
			t.Fatal(err)
		}
		if want := readRowFile(t, lock+".sig.json"); !bytes.Equal(document, want) {
			t.Errorf("%s: lock signature\n got %s\nwant %s", lock, document, want)
		}
	}
	// Each repository's statement is built from its own documents at its
	// commit, the assembly row's inputs, with no index.
	in := rowInput(t, rowByID(t, "assemble-two-sources"))
	for name, signed := range map[string]struct{ seed, file string }{
		"standard": {seedTest1, "signatures/standard.json"},
		"billing":  {seedTest2, "signatures/billing.json"},
	} {
		statement, report, err := federation.StatementOf(in, name)
		if err != nil || statement == nil || !report.Valid {
			t.Fatalf("%s: %v %+v", name, err, report)
		}
		document, err := signerOf(t, signed.seed).SignRegistrySource(statement.Digest)
		if err != nil {
			t.Fatal(err)
		}
		if want := readRowFile(t, signed.file); !bytes.Equal(document, want) {
			t.Errorf("%s: source signature\n got %s\nwant %s", name, document, want)
		}
	}
}

// A signer refuses a digest not of its form, and signs nothing.
func TestSignerRefusesADigestNotOfItsForm(t *testing.T) {
	signer := signerOf(t, seedTest1)
	for _, digest := range []string{"", "sha256:" + string(bytes.Repeat([]byte("A"), 64)), "17daddbd0d76af8847e788444afc5db180a97c3aafe1167cc7aad184a72360fd"} {
		if document, err := signer.SignRegistryLock(digest); err == nil || document != nil {
			t.Errorf("lock signed over %q", digest)
		}
		if document, err := signer.SignRegistrySource(digest); err == nil || document != nil {
			t.Errorf("source signed over %q", digest)
		}
	}
}

// The statement of a source the manifest does not name, or not given, is an
// input error; one whose path is not given is refused with a report, and no
// statement.
func TestStatementOfRefuses(t *testing.T) {
	in := rowInput(t, rowByID(t, "assemble-two-sources"))
	for _, name := range []string{"", "ledger"} {
		var input *federation.InputError
		if _, _, err := federation.StatementOf(in, name); !errors.As(err, &input) {
			t.Errorf("%q: %v", name, err)
		}
	}
	billing := in.Sources["billing"]
	files := map[string]registry.Document{}
	for path, document := range billing.Files {
		files[path] = document
	}
	for path := range files {
		delete(files, path)
		break
	}
	in.Sources["billing"] = federation.SourceInput{Commit: billing.Commit, Files: files}
	statement, report, err := federation.StatementOf(in, "billing")
	if err != nil || statement != nil || report.Valid || report.Diagnostics[0].Code != "FED-SOURCE-MISSING" {
		t.Fatalf("%v %v %+v", statement, err, report)
	}
	in.Sources["billing"] = federation.SourceInput{Commit: "abc", Files: billing.Files}
	if statement, report, _ := federation.StatementOf(in, "billing"); statement != nil || report.Valid || report.Diagnostics[0].Code != "FED-COMMIT" {
		t.Fatalf("an abbreviated commit: %+v", report)
	}
}
