package audit

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// The seeds of RFC 8032's first two Ed25519 test vectors, and a third, so a
// reader can check every signature here with any Ed25519 implementation.
const (
	vectorSeed1 = "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60"
	vectorSeed2 = "4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb"
)

func signerOf(t *testing.T, seedHex string) *Signer {
	t.Helper()
	seed, err := hex.DecodeString(seedHex)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := NewSignerFromSeed(seed)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

// thirdSigner is a key neither vector uses.
func thirdSigner(t *testing.T) *Signer {
	sum := sha256.Sum256([]byte("a third key"))
	return signerOf(t, hex.EncodeToString(sum[:]))
}

// The fixed test vector the guide states (docs/building-with-packs.md,
// "Record signatures, exactly"): two record lines of one trail, the first
// signed with the first key, a rotation to the second after line 1, and the
// second record signed with the second key. The bytes are compared exactly, so
// the sidecar's format cannot drift from the one another implementation reads,
// and the verification of them is what the guide says it is.
func TestTheRecordSignatureVectorIsFixed(t *testing.T) {
	first, second := signerOf(t, vectorSeed1), signerOf(t, vectorSeed2)
	if first.PublicKey() != "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a" || first.KeyID() != "21fe31dfa154a261626bf854046fd227" ||
		second.PublicKey() != "3d4017c3e843895a92b70aa74d1b7ebc9c982ccf2ec4968cc0cd55f12af4660c" || second.KeyID() != "39f713d0a644253f04529421b9f51b9b" {
		t.Fatalf("keys: %s %s %s %s", first.PublicKey(), first.KeyID(), second.PublicKey(), second.KeyID())
	}
	line1 := []byte(`{"recordVersion":"1","trail":"00112233445566778899aabbccddeeff","sequence":1,"previous":"sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855","kind":"evaluation"}`)
	line2 := []byte(`{"recordVersion":"1","trail":"00112233445566778899aabbccddeeff","sequence":2,"previous":"sha256:9305565078dd0531a50e67ee2d227d4fd0cd0d504541b92801660236a0f0eaed","kind":"evaluation"}`)
	if Digest(line1) != "sha256:9305565078dd0531a50e67ee2d227d4fd0cd0d504541b92801660236a0f0eaed" {
		t.Fatalf("record digest = %s", Digest(line1))
	}
	message := string(RecordMessage("00112233445566778899aabbccddeeff", 1, Digest(line1)))
	if message != `judgment-pack-runtime/record-signature/1:{"record":"sha256:9305565078dd0531a50e67ee2d227d4fd0cd0d504541b92801660236a0f0eaed","sequence":1,"trail":"00112233445566778899aabbccddeeff"}` {
		t.Fatalf("message = %s", message)
	}
	rotationMessage := string(RotationMessage("00112233445566778899aabbccddeeff", 1, second.PublicKey()))
	if rotationMessage != `judgment-pack-runtime/key-rotation/1:{"at":1,"next":"3d4017c3e843895a92b70aa74d1b7ebc9c982ccf2ec4968cc0cd55f12af4660c","trail":"00112233445566778899aabbccddeeff"}` {
		t.Fatalf("rotation message = %s", rotationMessage)
	}
	signature1, err := first.SignRecordLine(line1)
	if err != nil {
		t.Fatal(err)
	}
	rotation := first.RotationLine("00112233445566778899aabbccddeeff", 1, second.public)
	signature2, err := second.SignRecordLine(line2)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`{"keyId":"21fe31dfa154a261626bf854046fd227","kind":"record-signature","record":"sha256:9305565078dd0531a50e67ee2d227d4fd0cd0d504541b92801660236a0f0eaed","sequence":1,"sidecarVersion":"1","signature":"be509a591ce3d1ecc67a3bd2c35c914e001d2737a62ff8759e5be775448e5d5531370372971fdee8ac5b3c7c63fe16c9f83fed9243e114b8e6c02a2156ffbb0b","trail":"00112233445566778899aabbccddeeff"}` + "\n",
		`{"at":1,"keyId":"21fe31dfa154a261626bf854046fd227","kind":"key-rotation","next":"3d4017c3e843895a92b70aa74d1b7ebc9c982ccf2ec4968cc0cd55f12af4660c","sidecarVersion":"1","signature":"d6cf4da642a0f84dbaaf03a88b7afe0d7eded03897301ac80ab26f7248631502ebc482063a0dc37a2234fff2c62f8bff35ae825261e9d0b6d7908495a7f21201","trail":"00112233445566778899aabbccddeeff"}` + "\n",
		`{"keyId":"39f713d0a644253f04529421b9f51b9b","kind":"record-signature","record":"sha256:d927c7b963914116f0f47219b0564b40741ae2b56de1c38ccaf27fd0f0630702","sequence":2,"sidecarVersion":"1","signature":"b28b5765844abecd9ca69b642ae1685597045babda87635b8760da9d747a6dc1c244cbbbb0b2c2d73766855665bad9487f61a373209244f7c41af0bb406d8400","trail":"00112233445566778899aabbccddeeff"}` + "\n",
	}
	for index, got := range [][]byte{signature1, rotation, signature2} {
		if string(got) != want[index] {
			t.Fatalf("sidecar line %d:\n got %s\nwant %s", index+1, got, want[index])
		}
	}
	trail := joinLines([][]byte{line1, line2})
	sidecar := []byte(strings.Join(want, ""))
	chain := verifySigned(t, trail, sidecar, SignatureOptions{Keys: []ed25519.PublicKey{first.public}})
	if chain.Status != "valid" || len(chain.Findings) != 0 || chain.Coverage.Signed.Status != "through" || chain.Coverage.Signed.Through != 2 ||
		chain.Coverage.SignedRecords != 2 || chain.Coverage.UnsignedRecords != 0 ||
		chain.Signatures.Lines != 3 || chain.Signatures.Rotations != 1 || chain.Signatures.FirstKey != first.KeyID() || chain.Signatures.KeyInForce != second.KeyID() {
		t.Fatalf("vector verification: %+v %+v %v", chain.Coverage, chain.Signatures, findingNames(chain))
	}
	// The vector under the second key alone: it signed only record 2, and the
	// first key's lines do not verify under it.
	chain = verifySigned(t, trail, sidecar, SignatureOptions{Keys: []ed25519.PublicKey{second.public}})
	if !slices.Equal(findingNames(chain), []string{"signature-invalid@1", "rotation-invalid@1"}) {
		t.Fatalf("under the second key alone: %v", findingNames(chain))
	}
}

// verifySigned verifies a trail held in memory with its sidecar.
func verifySigned(t *testing.T, trail, sidecar []byte, options SignatureOptions) result.AuditChain {
	t.Helper()
	options.Sidecar, options.SidecarSize = bytes.NewReader(sidecar), int64(len(sidecar))
	report, err := Verify(bytes.NewReader(trail), int64(len(trail)), Options{Signatures: &options})
	if err != nil {
		t.Fatal(err)
	}
	return report.Chain
}

// signedTrail writes n chained records, each its own invocation, through a
// writer signing with signer, and returns the writer's root and directory.
func signedTrail(t *testing.T, signer *Signer, n int) (*fssecure.Root, string) {
	t.Helper()
	writer, dir := writerAt(t, "audit")
	for index := range n {
		appendSigned(t, writer.root, signer, fmt.Sprintf(`{"n":%d}`, index))
	}
	return writer.root, dir
}

// appendSigned appends one record through a new writer signing with signer.
func appendSigned(t *testing.T, root *fssecure.Root, signer *Signer, facts string) {
	t.Helper()
	writer := NewWriter(root, "audit", true)
	writer.SignWith(signer)
	if err := writer.Evaluation(evaluated(), Inputs{Facts: []byte(facts)}, nil, []byte(`{}`), nil); err != nil {
		t.Fatal(err)
	}
}

func readSidecar(t *testing.T, dir string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "audit", SidecarName))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func writeSidecar(t *testing.T, dir string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "audit", SidecarName), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func keys(signers ...*Signer) []ed25519.PublicKey {
	list := []ed25519.PublicKey{}
	for _, signer := range signers {
		list = append(list, signer.public)
	}
	return list
}

// A writer with a key appends, after each chained record and under the same
// lock, one sidecar line signing it, and the record line itself is the line an
// unsigned writer writes: nothing is added to it. Every record verifies.
func TestASigningWriterSignsEveryChainedRecordAndTouchesNone(t *testing.T) {
	signer := signerOf(t, vectorSeed1)
	writer, dir := writerAt(t, "audit")
	appendSigned(t, writer.root, signer, `{"n":1}`)
	batch := NewWriter(writer.root, "audit", true)
	batch.SignWith(signer)
	record, err := EvaluationRecord(evaluated(), Inputs{Facts: []byte(`{"n":2}`)}, nil, []byte(`{}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := batch.AppendAll([]Record{record, record, record}); err != nil {
		t.Fatal(err)
	}
	trail, lines := trailLines(t, dir, "audit")
	sidecar := readSidecar(t, dir)
	sidecarLines := splitLines(sidecar)
	if len(lines) != 4 || len(sidecarLines) != 4 {
		t.Fatalf("lines = %d, sidecar lines = %d", len(lines), len(sidecarLines))
	}
	for index, line := range lines {
		if bytes.Contains(line, []byte("signature")) || bytes.Contains(line, []byte("keyId")) {
			t.Fatalf("record %d carries a signature member: %s", index+1, line)
		}
		signed, err := signer.SignRecordLine(line)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(append(sidecarLines[index], '\n'), signed) {
			t.Fatalf("sidecar line %d is not the signature of record %d", index+1, index+1)
		}
	}
	checkChain(t, lines)
	chain := verifySigned(t, trail, sidecar, SignatureOptions{Keys: keys(signer)})
	if chain.Status != "valid" || chain.Coverage.Signed.Through != 4 || chain.Coverage.SignedRecords != 4 || chain.Coverage.UnsignedRecords != 0 || chain.Signatures.Unreadable != 0 {
		t.Fatalf("verification: %+v %v", chain.Coverage, findingNames(chain))
	}
	if !containsString(chain.Establishes, fmt.Sprintf(establishesSigned, 4)) ||
		!containsString(chain.DoesNotEstablish, notAgainstOperator) || !containsString(chain.DoesNotEstablish, notAfterTheft) ||
		!containsString(chain.DoesNotEstablish, fmt.Sprintf(notSignedAfter, 4)) || containsString(chain.DoesNotEstablish, notSignedUnchecked) {
		t.Fatalf("statements: %q %q", chain.Establishes, chain.DoesNotEstablish)
	}
	// Without a key nothing is checked, and the report says so.
	unchecked := verifyBytes(t, trail, nil)
	if unchecked.Coverage.Signed.Status != "not-checked" || unchecked.Signatures != nil || unchecked.Coverage.SignedRecords != 0 ||
		!containsString(unchecked.DoesNotEstablish, notSignedUnchecked) || containsString(unchecked.DoesNotEstablish, notAgainstOperator) {
		t.Fatalf("unchecked: %+v %q", unchecked.Coverage, unchecked.DoesNotEstablish)
	}
	// A writer that does not chain signs nothing, and writes no sidecar.
	plain, plainDir := writerAt(t, "audit")
	unchained := NewWriter(plain.root, "audit", false)
	unchained.SignWith(signer)
	if unchained.Signs() {
		t.Fatal("an unchained writer does not sign")
	}
	if err := unchained.Append(record); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(plainDir, "audit", SidecarName)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("an unchained trail has no sidecar: %v", err)
	}
}

// An edit to a signed record fails its signature, the last line's included,
// which the chain alone cannot show: the signature binds the record's exact
// bytes.
func TestAnEditedRecordFailsItsSignature(t *testing.T) {
	signer := signerOf(t, vectorSeed1)
	_, dir := signedTrail(t, signer, 3)
	trail := readTrailFile(t, dir)
	sidecar := readSidecar(t, dir)
	lines := splitLines(trail)
	lines[2] = bytes.Replace(lines[2], []byte(`"n":2`), []byte(`"n":7`), 1)
	chain := verifySigned(t, joinLines(lines), sidecar, SignatureOptions{Keys: keys(signer)})
	if !slices.Equal(findingNames(chain), []string{"signature-record-mismatch@3"}) || chain.Coverage.Signed.Through != 2 || chain.Coverage.SignedRecords != 2 || chain.Status != "invalid" {
		t.Fatalf("an edited last record: %v %+v", findingNames(chain), chain.Coverage)
	}
	// The same check without signatures sees nothing wrong.
	if plain := verifyBytes(t, joinLines(lines), nil); plain.Status != "valid" {
		t.Fatalf("the chain alone does not see a last line edited: %v", findingNames(plain))
	}
	// An edit before the last breaks a link as well, and the coverage stops
	// before it.
	lines = splitLines(trail)
	lines[0] = bytes.Replace(lines[0], []byte(`"n":0`), []byte(`"n":9`), 1)
	chain = verifySigned(t, joinLines(lines), sidecar, SignatureOptions{Keys: keys(signer)})
	if !slices.Equal(findingNames(chain), []string{"signature-record-mismatch@1", "previous-mismatch@2"}) || chain.Coverage.Signed.Status != "none" {
		t.Fatalf("an edited first record: %v %+v", findingNames(chain), chain.Coverage)
	}
	want := "That any uninterrupted prefix from line 1 is authenticated by a signature that was checked; 2 records carry a signature that was checked."
	if !containsString(chain.DoesNotEstablish, want) {
		t.Fatalf("statements: %q", chain.DoesNotEstablish)
	}
}

// An edited signature, or one by another key, does not verify.
func TestAnEditedOrForeignSignatureIsInvalid(t *testing.T) {
	signer := signerOf(t, vectorSeed1)
	_, dir := signedTrail(t, signer, 3)
	trail := readTrailFile(t, dir)
	lines := splitLines(readSidecar(t, dir))
	edited := slices.Clone(lines)
	edited[1] = editSignature(lines[1])
	chain := verifySigned(t, trail, joinLines(edited), SignatureOptions{Keys: keys(signer)})
	if !slices.Equal(findingNames(chain), []string{"signature-invalid@2"}) || chain.Coverage.SignedRecords != 2 || chain.Coverage.Signed.Through != 3 {
		t.Fatalf("an edited signature: %v %+v", findingNames(chain), chain.Coverage)
	}
	// Signed again by another key, under that key's own keyId.
	_, records := trailLines(t, dir, "audit")
	foreign, err := thirdSigner(t).SignRecordLine(records[1])
	if err != nil {
		t.Fatal(err)
	}
	replaced := slices.Clone(lines)
	replaced[1] = bytes.TrimSuffix(foreign, []byte("\n"))
	chain = verifySigned(t, trail, joinLines(replaced), SignatureOptions{Keys: keys(signer)})
	if !slices.Equal(findingNames(chain), []string{"signature-invalid@2"}) {
		t.Fatalf("a foreign signature: %v", findingNames(chain))
	}
	// The signer's own valid signature under another key's keyId is refused:
	// a line names the key it was made with.
	relabelledOwn := slices.Clone(lines)
	relabelledOwn[1] = bytes.Replace(lines[1], []byte(signer.KeyID()), []byte(thirdSigner(t).KeyID()), 1)
	chain = verifySigned(t, trail, joinLines(relabelledOwn), SignatureOptions{Keys: keys(signer)})
	if !slices.Equal(findingNames(chain), []string{"signature-invalid@2"}) {
		t.Fatalf("a valid signature under another keyId: %v", findingNames(chain))
	}
	// Another key's signature under the right keyId does not verify either.
	relabelled := bytes.Replace(replaced[1], []byte(thirdSigner(t).KeyID()), []byte(signer.KeyID()), 1)
	replaced[1] = relabelled
	chain = verifySigned(t, trail, joinLines(replaced), SignatureOptions{Keys: keys(signer)})
	if !slices.Equal(findingNames(chain), []string{"signature-invalid@2"}) {
		t.Fatalf("a foreign signature under the right keyId: %v", findingNames(chain))
	}
}

// Sidecar lines out of order are found, a duplicate among them, and the
// records they leave unchecked stay unsigned.
func TestReorderedOrRepeatedSidecarLinesAreFound(t *testing.T) {
	signer := signerOf(t, vectorSeed1)
	_, dir := signedTrail(t, signer, 3)
	trail := readTrailFile(t, dir)
	lines := splitLines(readSidecar(t, dir))
	swapped := [][]byte{lines[0], lines[2], lines[1]}
	chain := verifySigned(t, trail, joinLines(swapped), SignatureOptions{Keys: keys(signer)})
	if !slices.Equal(findingNames(chain), []string{"sidecar-out-of-order@2"}) || chain.Coverage.SignedRecords != 2 {
		t.Fatalf("swapped: %v %+v", findingNames(chain), chain.Coverage)
	}
	repeated := [][]byte{lines[0], lines[1], lines[1], lines[2]}
	chain = verifySigned(t, trail, joinLines(repeated), SignatureOptions{Keys: keys(signer)})
	if !slices.Equal(findingNames(chain), []string{"sidecar-out-of-order@2"}) || chain.Coverage.SignedRecords != 3 {
		t.Fatalf("repeated: %v %+v", findingNames(chain), chain.Coverage)
	}
}

// A missing signature is not a failure by itself: the record is unsigned, as
// one whose signature could not be written is, and a later signature still
// covers it through the chain. Required coverage that no signature reaches
// is the signature-missing finding.
func TestAMissingSignatureLeavesItsRecordUnsigned(t *testing.T) {
	signer := signerOf(t, vectorSeed1)
	_, dir := signedTrail(t, signer, 3)
	trail := readTrailFile(t, dir)
	lines := splitLines(readSidecar(t, dir))
	middle := [][]byte{lines[0], lines[2]}
	chain := verifySigned(t, trail, joinLines(middle), SignatureOptions{Keys: keys(signer), RequireThrough: 3})
	if chain.Status != "valid" || chain.Coverage.SignedRecords != 2 || chain.Coverage.UnsignedRecords != 1 || chain.Coverage.Signed.Through != 3 || chain.RequiredSigned.Status != "met" {
		t.Fatalf("middle missing: %v %+v %+v", findingNames(chain), chain.Coverage, chain.RequiredSigned)
	}
	last := [][]byte{lines[0], lines[1]}
	chain = verifySigned(t, trail, joinLines(last), SignatureOptions{Keys: keys(signer), RequireThrough: 3})
	if !slices.Equal(findingNames(chain), []string{"signature-missing@3"}) || chain.Coverage.Signed.Through != 2 || chain.RequiredSigned.Status != "unmet" || chain.Status != "invalid" {
		t.Fatalf("last missing, required: %v %+v %+v", findingNames(chain), chain.Coverage, chain.RequiredSigned)
	}
	chain = verifySigned(t, trail, joinLines(last), SignatureOptions{Keys: keys(signer), RequireThrough: 2})
	if chain.Status != "valid" || chain.RequiredSigned.Status != "met" {
		t.Fatalf("last missing, required through 2: %v %+v", findingNames(chain), chain.RequiredSigned)
	}
	chain = verifySigned(t, trail, nil, SignatureOptions{Keys: keys(signer), RequireThrough: 1})
	if !slices.Equal(findingNames(chain), []string{"signature-missing@1"}) || chain.Coverage.Signed.Status != "none" || chain.Signatures.Lines != 0 {
		t.Fatalf("no sidecar: %v %+v", findingNames(chain), chain.Coverage)
	}
}

// A signature moved to another record is for another record: its record
// digest is the first record's. One for another trail, or past the trail's
// end, names no record of this trail; so does one for a line that is not a
// chained record.
func TestASignatureMovedOrNamingNoRecordIsFound(t *testing.T) {
	signer := signerOf(t, vectorSeed1)
	_, dir := signedTrail(t, signer, 3)
	trail := readTrailFile(t, dir)
	lines := splitLines(readSidecar(t, dir))
	moved := slices.Clone(lines)
	moved[2] = bytes.Replace(lines[1], []byte(`"sequence":2`), []byte(`"sequence":3`), 1)
	chain := verifySigned(t, trail, joinLines(moved), SignatureOptions{Keys: keys(signer)})
	if !slices.Equal(findingNames(chain), []string{"signature-record-mismatch@3"}) {
		t.Fatalf("moved: %v", findingNames(chain))
	}
	// The trail cut short keeps the sidecar's signature of what was cut.
	records := splitLines(trail)
	chain = verifySigned(t, joinLines(records[:2]), joinLines(lines), SignatureOptions{Keys: keys(signer)})
	if !slices.Equal(findingNames(chain), []string{"signature-no-record@3"}) || chain.Coverage.Signed.Through != 2 {
		t.Fatalf("cut short: %v %+v", findingNames(chain), chain.Coverage)
	}
	// Another trail's signature, at a sequence this trail has.
	_, otherDir := signedTrail(t, signer, 3)
	other := splitLines(readSidecar(t, otherDir))
	chain = verifySigned(t, trail, joinLines([][]byte{lines[0], other[1], lines[2]}), SignatureOptions{Keys: keys(signer)})
	if !slices.Equal(findingNames(chain), []string{"signature-no-record@2"}) {
		t.Fatalf("another trail's: %v", findingNames(chain))
	}
	// A line that is not a chained record: an unchained line before the chain.
	legacy := append([]byte(`{"recordVersion":"1","note":"before the chain"}`+"\n"), trail...)
	chain = verifySigned(t, legacy, joinLines(lines[:1]), SignatureOptions{Keys: keys(signer)})
	if !containsString(findingNames(chain), "signature-no-record@1") {
		t.Fatalf("an unchained line: %v", findingNames(chain))
	}
}

// A rotation hands signing to the next key after its line: the old key signs
// nothing more, the next signs once the project names it, and the trail
// verifies under the first key alone, following the rotation, and under the
// keys pinned in order. A rotation to a key the verifier did not pin fails,
// and so does every signature of that key.
func TestARotationHandsSigningToTheNextKey(t *testing.T) {
	first, second := signerOf(t, vectorSeed1), signerOf(t, vectorSeed2)
	root, dir := signedTrail(t, first, 2)
	writer := NewWriter(root, "audit", true)
	writer.SignWith(first)
	report, err := writer.Rotate(second)
	if err != nil {
		t.Fatal(err)
	}
	if report.At != 2 || report.From != first.KeyID() || report.Next != second.KeyID() || report.NextPublicKey != second.PublicKey() {
		t.Fatalf("rotation = %+v", report)
	}
	if inForce, err := writer.KeyInForce(); err != nil || inForce {
		t.Fatalf("the old key is no longer in force: %v %v", inForce, err)
	}
	// A trail that ends at its rotation verifies: the rotation follows its
	// last line.
	atRotation := verifySigned(t, readTrailFile(t, dir), readSidecar(t, dir), SignatureOptions{Keys: keys(first, second)})
	if atRotation.Status != "valid" || atRotation.Signatures.Rotations != 1 || atRotation.Signatures.KeyInForce != second.KeyID() {
		t.Fatalf("ending at the rotation: %v %+v", findingNames(atRotation), atRotation.Signatures)
	}
	// The old key, still named somewhere, signs nothing, and the decision is
	// recorded all the same.
	appendSigned(t, root, first, `{"n":"old"}`)
	appendSigned(t, root, second, `{"n":"next"}`)
	appendSigned(t, root, second, `{"n":"next again"}`)
	trail := readTrailFile(t, dir)
	sidecar := readSidecar(t, dir)
	if got := len(splitLines(sidecar)); got != 5 {
		t.Fatalf("sidecar lines = %d, want 2 signatures, a rotation and 2 signatures", got)
	}
	chain := verifySigned(t, trail, sidecar, SignatureOptions{Keys: keys(first)})
	if chain.Status != "valid" || chain.Signatures.Rotations != 1 || chain.Signatures.KeyInForce != second.KeyID() ||
		chain.Coverage.SignedRecords != 4 || chain.Coverage.UnsignedRecords != 1 || chain.Coverage.Signed.Through != 5 {
		t.Fatalf("under the first key: %v %+v %+v", findingNames(chain), chain.Coverage, chain.Signatures)
	}
	chain = verifySigned(t, trail, sidecar, SignatureOptions{Keys: keys(first, second)})
	if chain.Status != "valid" || chain.Signatures.KeysSupplied != 2 {
		t.Fatalf("pinned in order: %v", findingNames(chain))
	}
	chain = verifySigned(t, trail, sidecar, SignatureOptions{Keys: keys(first, thirdSigner(t))})
	if !slices.Equal(findingNames(chain), []string{"rotation-invalid@2", "signature-invalid@4", "signature-invalid@5"}) || chain.Coverage.Signed.Through != 2 {
		t.Fatalf("pinned to another key: %v %+v", findingNames(chain), chain.Coverage)
	}
	// A key the trail has rotated to cannot rotate back to itself, and a
	// rotation needs the key in force.
	if _, err := writer.Rotate(second); !errors.Is(err, ErrKeyNotInForce) {
		t.Fatalf("a rotation from a key not in force: %v", err)
	}
	next := NewWriter(root, "audit", true)
	next.SignWith(second)
	if _, err := next.Rotate(second); !errors.Is(err, ErrRotationSameKey) {
		t.Fatalf("a rotation to the same key: %v", err)
	}
	if _, err := NewWriter(root, "audit", true).Rotate(first); !errors.Is(err, ErrNoSigningKey) {
		t.Fatalf("a rotation without a key: %v", err)
	}
	// A second rotation, to a third key, past the keys a verifier pinned.
	if _, err := next.Rotate(thirdSigner(t)); err != nil {
		t.Fatal(err)
	}
	appendSigned(t, root, thirdSigner(t), `{"n":"third"}`)
	chain = verifySigned(t, readTrailFile(t, dir), readSidecar(t, dir), SignatureOptions{Keys: keys(first, second)})
	if !slices.Equal(findingNames(chain), []string{"rotation-invalid@5", "signature-invalid@6"}) {
		t.Fatalf("a rotation past the keys pinned: %v", findingNames(chain))
	}
	if chain := verifySigned(t, readTrailFile(t, dir), readSidecar(t, dir), SignatureOptions{Keys: keys(first, second, thirdSigner(t))}); chain.Status != "valid" || chain.Signatures.Rotations != 2 {
		t.Fatalf("three keys pinned: %v", findingNames(chain))
	}
}

// A rotation follows the trail's last line, chained or not, and needs a
// chained record before it: there is no trail identity to bind it to
// otherwise.
func TestARotationNeedsAChainedRecordAndFollowsTheLastLine(t *testing.T) {
	first, second := signerOf(t, vectorSeed1), signerOf(t, vectorSeed2)
	writer, dir := writerAt(t, "audit")
	writer.SignWith(first)
	if _, err := writer.Rotate(second); !errors.Is(err, ErrNoTrail) {
		t.Fatalf("no trail: %v", err)
	}
	appendRaw(t, dir, "")
	if _, err := writer.Rotate(second); !errors.Is(err, ErrRotationNoTrail) {
		t.Fatalf("an empty trail: %v", err)
	}
	unchained := NewWriter(writer.root, "audit", false)
	record, err := EvaluationRecord(evaluated(), Inputs{Facts: []byte(`{}`)}, nil, []byte(`{}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := unchained.Append(record); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Rotate(second); !errors.Is(err, ErrRotationNoTrail) {
		t.Fatalf("a trail with no chained record: %v", err)
	}
	appendSigned(t, writer.root, first, `{"n":1}`)
	if err := unchained.Append(record); err != nil {
		t.Fatal(err)
	}
	rotated, err := writer.Rotate(second)
	if err != nil || rotated.At != 3 {
		t.Fatalf("after an unchained last line: %+v %v", rotated, err)
	}
	appendSigned(t, writer.root, second, `{"n":4}`)
	chain := verifySigned(t, readTrailFile(t, dir), readSidecar(t, dir), SignatureOptions{Keys: keys(first, second)})
	if chain.Status != "valid" || chain.Signatures.Rotations != 1 || chain.Coverage.SignedRecords != 2 || chain.Coverage.Signed.Through != 4 {
		t.Fatalf("verification: %v %+v %+v", findingNames(chain), chain.Coverage, chain.Signatures)
	}
	// A torn last line refuses a rotation as it refuses a record.
	appendRaw(t, dir, `{"torn`)
	if _, err := NewWriter(writer.root, "audit", true).Rotate(first); !errors.Is(err, ErrNoSigningKey) {
		t.Fatalf("no key: %v", err)
	}
	next := NewWriter(writer.root, "audit", true)
	next.SignWith(second)
	if _, err := next.Rotate(first); !errors.Is(err, ErrIncompleteLastLine) {
		t.Fatalf("a torn last line: %v", err)
	}
}

// A signature for a line a discontinuity names as damaged names no record,
// even when the damaged line's bytes are a whole chained record.
func TestASignatureForADamagedLineNamesNoRecord(t *testing.T) {
	signer := signerOf(t, vectorSeed1)
	writer, dir := writerAt(t, "audit")
	appendSigned(t, writer.root, signer, `{"n":1}`)
	appendSigned(t, writer.root, signer, `{"n":2}`)
	stripFinalNewline(t, dir)
	repairer := NewWriter(writer.root, "audit", true)
	repairer.SignWith(signer)
	if _, err := repairer.Repair(); err != nil {
		t.Fatal(err)
	}
	chain := verifySigned(t, readTrailFile(t, dir), readSidecar(t, dir), SignatureOptions{Keys: keys(signer)})
	if !slices.Equal(findingNames(chain), []string{"signature-no-record@2"}) || chain.Coverage.SignedRecords != 2 {
		t.Fatalf("a signed damaged line: %v %+v", findingNames(chain), chain.Coverage)
	}
}

// A rotation not signed by the key in force fails and hands nothing over:
// what the key it names signs then fails too. So does a rotation of another
// trail, and one after a line the trail does not have.
func TestARotationNotSignedByTheKeyInForceFails(t *testing.T) {
	first, third := signerOf(t, vectorSeed1), thirdSigner(t)
	root, dir := signedTrail(t, first, 2)
	trail := readTrailFile(t, dir)
	sidecar := readSidecar(t, dir)
	records := splitLines(trail)
	identity := readChain(t, records[0]).trail
	forged := third.RotationLine(identity, 2, third.public)
	chain := verifySigned(t, trail, append(slices.Clone(sidecar), forged...), SignatureOptions{Keys: keys(first)})
	if !slices.Equal(findingNames(chain), []string{"rotation-invalid@2"}) || chain.Signatures.KeyInForce != first.KeyID() || chain.Signatures.Rotations != 0 {
		t.Fatalf("a forged rotation: %v %+v", findingNames(chain), chain.Signatures)
	}
	// The forger then signs a record with the key it named.
	writeSidecar(t, dir, append(slices.Clone(sidecar), forged...))
	appendForged(t, root, dir, third)
	chain = verifySigned(t, readTrailFile(t, dir), readSidecar(t, dir), SignatureOptions{Keys: keys(first)})
	if !slices.Equal(findingNames(chain), []string{"rotation-invalid@2", "signature-invalid@3"}) || chain.Coverage.Signed.Through != 2 {
		t.Fatalf("a forged rotation and its key's signature: %v %+v", findingNames(chain), chain.Coverage)
	}
	// A rotation of another trail, and one past the trail's end.
	elsewhere := first.RotationLine(strings.Repeat("ab", 16), 2, third.public)
	chain = verifySigned(t, trail, append(slices.Clone(sidecar), elsewhere...), SignatureOptions{Keys: keys(first)})
	if !slices.Equal(findingNames(chain), []string{"rotation-invalid@2"}) {
		t.Fatalf("another trail's rotation: %v", findingNames(chain))
	}
	beyond := first.RotationLine(identity, 9, third.public)
	chain = verifySigned(t, trail, append(slices.Clone(sidecar), beyond...), SignatureOptions{Keys: keys(first)})
	if !slices.Equal(findingNames(chain), []string{"signature-no-record@9"}) {
		t.Fatalf("a rotation past the end: %v", findingNames(chain))
	}
}

// A rotation whose only flaw is its signature, under the right keyId, hands
// nothing over: it fails, the key in force stays, and what the key it names
// signs fails too.
func TestARotationWithABadSignatureHandsNothingOver(t *testing.T) {
	first, second := signerOf(t, vectorSeed1), signerOf(t, vectorSeed2)
	root, dir := signedTrail(t, first, 2)
	identity := readChain(t, splitLines(readTrailFile(t, dir))[0]).trail
	rotation := bytes.TrimSuffix(first.RotationLine(identity, 2, second.public), []byte("\n"))
	corrupted := editSignature(rotation)
	if !parseSidecarLine(corrupted).readable || !bytes.Contains(corrupted, []byte(first.KeyID())) {
		t.Fatal("the corrupted rotation keeps its shape and its keyId")
	}
	writeSidecar(t, dir, append(append(readSidecar(t, dir), corrupted...), '\n'))
	appendForged(t, root, dir, second)
	for _, pinned := range [][]ed25519.PublicKey{keys(first), keys(first, second)} {
		chain := verifySigned(t, readTrailFile(t, dir), readSidecar(t, dir), SignatureOptions{Keys: pinned})
		if !slices.Equal(findingNames(chain), []string{"rotation-invalid@2", "signature-invalid@3"}) ||
			chain.Signatures.Rotations != 0 || chain.Signatures.KeyInForce != first.KeyID() || chain.Coverage.Signed.Through != 2 {
			t.Fatalf("%d key(s): %v %+v %+v", len(pinned), findingNames(chain), chain.Signatures, chain.Coverage)
		}
	}
}

// A rotation a failed write left without its newline was never written, as a
// verifier reads it, and the writer reads it so too: the old key stays in
// force and signs, the key it names does not, and ending the torn line keeps
// it unreadable rather than making it a rotation.
func TestATornRotationHandsNothingOver(t *testing.T) {
	first, second := signerOf(t, vectorSeed1), signerOf(t, vectorSeed2)
	root, dir := signedTrail(t, first, 2)
	identity := readChain(t, splitLines(readTrailFile(t, dir))[0]).trail
	rotation := bytes.TrimSuffix(first.RotationLine(identity, 2, second.public), []byte("\n"))
	if item, err := lastReadableSidecarLine(bytes.NewReader(rotation), int64(len(rotation))); err != nil || item.readable {
		t.Fatalf("a sidecar of one line without its newline has no readable line: %+v %v", item, err)
	}
	writeSidecar(t, dir, append(readSidecar(t, dir), rotation...))
	torn := readSidecar(t, dir)
	if item, err := lastReadableSidecarLine(bytes.NewReader(torn), int64(len(torn))); err != nil || item.kind != KindRecordSignature || item.keyID != first.KeyID() {
		t.Fatalf("the writer reads past the torn rotation: %+v %v", item, err)
	}
	chain := verifySigned(t, readTrailFile(t, dir), torn, SignatureOptions{Keys: keys(first)})
	if chain.Status != "valid" || chain.Signatures.Rotations != 0 || chain.Signatures.Unreadable != 1 || chain.Signatures.KeyInForce != first.KeyID() {
		t.Fatalf("the verifier reads the torn rotation as unreadable: %v %+v", findingNames(chain), chain.Signatures)
	}
	next := NewWriter(root, "audit", true)
	next.SignWith(second)
	if inForce, err := next.KeyInForce(); err != nil || inForce {
		t.Fatalf("the key the torn rotation names is not in force: %v %v", inForce, err)
	}
	appendSigned(t, root, second, `{"n":"next"}`)
	if !bytes.Equal(readSidecar(t, dir), torn) {
		t.Fatal("the key the torn rotation names signed")
	}
	appendSigned(t, root, first, `{"n":"old"}`)
	sidecar := readSidecar(t, dir)
	if lines := splitLines(sidecar); len(lines) != 4 || !bytes.Equal(lines[2], append(slices.Clone(rotation), '~')) {
		t.Fatalf("the torn rotation is ended and kept: %q", sidecar)
	}
	chain = verifySigned(t, readTrailFile(t, dir), sidecar, SignatureOptions{Keys: keys(first)})
	if chain.Status != "valid" || chain.Signatures.Rotations != 0 || chain.Signatures.Unreadable != 1 || chain.Coverage.SignedRecords != 3 || chain.Coverage.UnsignedRecords != 1 {
		t.Fatalf("after the old key signed again: %v %+v %+v", findingNames(chain), chain.Coverage, chain.Signatures)
	}
}

// appendForged appends a record and signs it with signer whatever the
// sidecar says is in force, as someone holding that key and the files could.
func appendForged(t *testing.T, root *fssecure.Root, dir string, signer *Signer) {
	t.Helper()
	writeRecords(t, root, `{"n":"forged"}`)
	_, lines := trailLines(t, dir, "audit")
	signed, err := signer.SignRecordLine(lines[len(lines)-1])
	if err != nil {
		t.Fatal(err)
	}
	writeSidecar(t, dir, append(readSidecar(t, dir), signed...))
}

// A key revoked from a sequence signs nothing from there on, record or
// rotation, and a copied key that keeps signing after a rotation is refused
// once the verifier is told so. Pinning the keys refuses a rotation the copied
// key forges to a key of its own before the revocation's sequence.
func TestARevokedKeySignsNothingFromItsSequence(t *testing.T) {
	first, second, third := signerOf(t, vectorSeed1), signerOf(t, vectorSeed2), thirdSigner(t)
	_, dir := signedTrail(t, first, 4)
	trail := readTrailFile(t, dir)
	sidecar := readSidecar(t, dir)
	chain := verifySigned(t, trail, sidecar, SignatureOptions{Keys: keys(first), Revoked: []Revocation{{PublicKey: first.public, From: 3}}})
	if !slices.Equal(findingNames(chain), []string{"signature-key-revoked@3", "signature-key-revoked@4"}) || chain.Coverage.Signed.Through != 2 || chain.Coverage.SignedRecords != 2 {
		t.Fatalf("revoked from 3: %v %+v", findingNames(chain), chain.Coverage)
	}
	// Another key's revocation changes nothing.
	chain = verifySigned(t, trail, sidecar, SignatureOptions{Keys: keys(first), Revoked: []Revocation{{PublicKey: second.public, From: 1}}})
	if chain.Status != "valid" || chain.Signatures.Revocations != 1 {
		t.Fatalf("another key revoked: %v", findingNames(chain))
	}
	// The trail rotates to the second key after line 2; the first key, copied,
	// forges a rotation after line 1 to a key of its own and re-signs.
	root2, dir2 := signedTrail(t, first, 2)
	rotating := NewWriter(root2, "audit", true)
	rotating.SignWith(first)
	if _, err := rotating.Rotate(second); err != nil {
		t.Fatal(err)
	}
	appendSigned(t, root2, second, `{"n":"after"}`)
	genuine := readSidecar(t, dir2)
	trail2 := readTrailFile(t, dir2)
	// The genuine trail under the revocation of the first key from line 3,
	// where it stopped being in force: valid.
	revoked := []Revocation{{PublicKey: first.public, From: 3}}
	if chain := verifySigned(t, trail2, genuine, SignatureOptions{Keys: keys(first), Revoked: revoked}); chain.Status != "valid" {
		t.Fatalf("the genuine trail under the revocation: %v", findingNames(chain))
	}
	// The copied key keeps signing as if no rotation happened: refused by the
	// revocation alone.
	records := splitLines(trail2)
	identity := readChain(t, records[0]).trail
	genuineLines := splitLines(genuine)
	kept := joinLines(genuineLines[:2])
	signed3, err := first.SignRecordLine(records[2])
	if err != nil {
		t.Fatal(err)
	}
	chain = verifySigned(t, trail2, append(kept, signed3...), SignatureOptions{Keys: keys(first), Revoked: revoked})
	if !slices.Equal(findingNames(chain), []string{"signature-key-revoked@3"}) {
		t.Fatalf("the copied key signing on: %v", findingNames(chain))
	}
	// It forges a rotation after line 1, before the revocation's sequence, to
	// a key of its own: the revocation alone does not refuse it, pinning does.
	forgedRotation := first.RotationLine(identity, 1, third.public)
	resigned2, err := third.SignRecordLine(records[1])
	if err != nil {
		t.Fatal(err)
	}
	resigned3, err := third.SignRecordLine(records[2])
	if err != nil {
		t.Fatal(err)
	}
	fork := slices.Concat(append(slices.Clone(genuineLines[0]), '\n'), forgedRotation, resigned2, resigned3)
	if chain := verifySigned(t, trail2, fork, SignatureOptions{Keys: keys(first), Revoked: revoked}); chain.Status != "valid" {
		t.Fatalf("a forged fork before the revocation, keys not pinned: %v", findingNames(chain))
	}
	chain = verifySigned(t, trail2, fork, SignatureOptions{Keys: keys(first, second), Revoked: revoked})
	if !slices.Equal(findingNames(chain), []string{"rotation-invalid@1", "signature-invalid@2", "signature-invalid@3"}) {
		t.Fatalf("a forged fork, keys pinned: %v", findingNames(chain))
	}
	// A rotation made with a revoked key is refused too.
	chain = verifySigned(t, trail2, genuine, SignatureOptions{Keys: keys(first), Revoked: []Revocation{{PublicKey: first.public, From: 2}}})
	if !slices.Equal(findingNames(chain), []string{"signature-key-revoked@2", "signature-key-revoked@2", "signature-invalid@3"}) {
		t.Fatalf("a rotation by a revoked key: %v", findingNames(chain))
	}
}

// A key put in place without a rotation is not the key in force, so it signs
// nothing; the records are written and are unsigned.
func TestAKeyNotInForceSignsNothing(t *testing.T) {
	first := signerOf(t, vectorSeed1)
	root, dir := signedTrail(t, first, 1)
	before := readSidecar(t, dir)
	stranger := NewWriter(root, "audit", true)
	stranger.SignWith(thirdSigner(t))
	if inForce, err := stranger.KeyInForce(); err != nil || inForce {
		t.Fatalf("a key put in place is not in force: %v %v", inForce, err)
	}
	appendSigned(t, root, thirdSigner(t), `{"n":"stranger"}`)
	if after := readSidecar(t, dir); !bytes.Equal(before, after) {
		t.Fatalf("a key not in force wrote to the sidecar: %s", after)
	}
	chain := verifySigned(t, readTrailFile(t, dir), before, SignatureOptions{Keys: keys(first)})
	if chain.Status != "valid" || chain.Coverage.UnsignedRecords != 1 || chain.Coverage.Signed.Through != 1 {
		t.Fatalf("verification: %v %+v", findingNames(chain), chain.Coverage)
	}
	if _, err := NewWriter(root, "audit", true).KeyInForce(); !errors.Is(err, ErrNoSigningKey) {
		t.Fatalf("a writer without a key: %v", err)
	}
}

// A signature that cannot be written leaves the record unsigned and the
// decision recorded: the append reports no failure. A sidecar whose last line
// a failed write left incomplete is ended before the next line, which stays
// whole and verifies; the torn line is counted unreadable, not failed.
func TestASidecarThatCannotBeWrittenLeavesRecordsUnsigned(t *testing.T) {
	signer := signerOf(t, vectorSeed1)
	writer, dir := writerAt(t, "audit")
	if err := os.MkdirAll(filepath.Join(dir, "audit", SidecarName), 0o700); err != nil {
		t.Fatal(err)
	}
	appendSigned(t, writer.root, signer, `{"n":1}`)
	if _, lines := trailLines(t, dir, "audit"); len(lines) != 1 {
		t.Fatalf("the record was written: %d lines", len(lines))
	}
	if err := os.Remove(filepath.Join(dir, "audit", SidecarName)); err != nil {
		t.Fatal(err)
	}
	appendSigned(t, writer.root, signer, `{"n":2}`)
	// A write that did not complete, then the next record.
	torn := readSidecar(t, dir)
	writeSidecar(t, dir, append(slices.Clone(torn), torn[:40]...))
	appendSigned(t, writer.root, signer, `{"n":3}`)
	sidecar := readSidecar(t, dir)
	if lines := splitLines(sidecar); len(lines) != 3 || string(lines[1]) != string(torn[:40])+"~" {
		t.Fatalf("the torn line is kept as a line of its own, ended so it stays unreadable: %q", sidecar)
	}
	chain := verifySigned(t, readTrailFile(t, dir), sidecar, SignatureOptions{Keys: keys(signer)})
	if chain.Status != "valid" || chain.Coverage.SignedRecords != 2 || chain.Coverage.UnsignedRecords != 1 || chain.Signatures.Unreadable != 1 || chain.Coverage.Signed.Through != 3 {
		t.Fatalf("verification: %v %+v %+v", findingNames(chain), chain.Coverage, chain.Signatures)
	}
	// A last line with no newline is unreadable to the verifier, however whole
	// its JSON: it is a write that did not complete.
	chain = verifySigned(t, readTrailFile(t, dir), bytes.TrimSuffix(sidecar, []byte("\n")), SignatureOptions{Keys: keys(signer)})
	if chain.Status != "valid" || chain.Signatures.Unreadable != 2 || chain.Coverage.SignedRecords != 1 {
		t.Fatalf("a whole last line without its newline: %v %+v %+v", findingNames(chain), chain.Coverage, chain.Signatures)
	}
	// A torn last line is unreadable to the verifier too, and an over-long one.
	chain = verifySigned(t, readTrailFile(t, dir), append(slices.Clone(sidecar), bytes.Repeat([]byte("x"), maxSidecarLineBytes+1)...), SignatureOptions{Keys: keys(signer)})
	if chain.Status != "valid" || chain.Signatures.Unreadable != 2 || chain.Signatures.Lines != 4 {
		t.Fatalf("a torn over-long tail: %v %+v", findingNames(chain), chain.Signatures)
	}
}

// A sidecar holding no readable line takes any key, which becomes the first:
// its torn last line is ended, and the new line is whole.
func TestASidecarOfUnreadableLinesTakesAnyKey(t *testing.T) {
	signer := signerOf(t, vectorSeed1)
	writer, dir := writerAt(t, "audit")
	if err := os.MkdirAll(filepath.Join(dir, "audit"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeSidecar(t, dir, []byte("not a line\n{\"torn"))
	appendSigned(t, writer.root, signer, `{"n":1}`)
	sidecar := readSidecar(t, dir)
	if lines := splitLines(sidecar); len(lines) != 3 || string(lines[1]) != `{"torn~` {
		t.Fatalf("sidecar: %q", sidecar)
	}
	chain := verifySigned(t, readTrailFile(t, dir), sidecar, SignatureOptions{Keys: keys(signer)})
	if chain.Status != "valid" || chain.Coverage.SignedRecords != 1 || chain.Signatures.Unreadable != 2 {
		t.Fatalf("verification: %v %+v %+v", findingNames(chain), chain.Coverage, chain.Signatures)
	}
}

// The writer reads the key in force back from the sidecar's end, past lines
// it cannot read, over-long ones included, and across the chunks it reads in.
func TestTheKeyInForceIsReadPastUnreadableLines(t *testing.T) {
	first, second := signerOf(t, vectorSeed1), signerOf(t, vectorSeed2)
	_, dir := signedTrail(t, first, 1)
	line := readSidecar(t, dir)
	for _, tail := range [][]byte{
		nil,
		[]byte("not json\n"),
		append(bytes.Repeat([]byte("y"), maxSidecarLineBytes+10), '\n'),
		append(bytes.Repeat([]byte("z\n"), readChunk), bytes.Repeat([]byte("w"), readChunk+7)...),
	} {
		data := append(slices.Clone(line), tail...)
		item, err := lastReadableSidecarLine(bytes.NewReader(data), int64(len(data)))
		if err != nil || !item.readable || item.keyID != first.KeyID() {
			t.Fatalf("tail of %d bytes: %+v %v", len(tail), item, err)
		}
	}
	// Padding so the readable line straddles a chunk boundary.
	padding := append(bytes.Repeat([]byte("p"), readChunk-len(line)/2), '\n')
	data := append(slices.Clone(padding), line...)
	data = append(data, append(bytes.Repeat([]byte("q"), readChunk-len(line)/2), '\n')...)
	item, err := lastReadableSidecarLine(bytes.NewReader(data), int64(len(data)))
	if err != nil || !item.readable || item.sequence != 1 {
		t.Fatalf("a line across a chunk boundary: %+v %v", item, err)
	}
	if item, _ := lastReadableSidecarLine(bytes.NewReader([]byte("x\ny\n")), 4); item.readable {
		t.Fatal("no readable line is the zero item")
	}
	// The start of an over-long line is not read as a line of its own, even
	// when a chunk boundary falls just after a readable line at its start.
	foreign, err := thirdSigner(t).SignRecordLine(splitLines(readTrailFile(t, dir))[0])
	if err != nil {
		t.Fatal(err)
	}
	crafted := slices.Concat(line, bytes.TrimSuffix(foreign, []byte("\n")), bytes.Repeat([]byte("x"), readChunk-1), []byte("\n"))
	item, err = lastReadableSidecarLine(bytes.NewReader(crafted), int64(len(crafted)))
	if err != nil || !item.readable || item.keyID != first.KeyID() {
		t.Fatalf("an over-long line's start: %+v %v", item, err)
	}
	// A rotation decides by the key it names.
	identity := readChain(t, splitLines(readTrailFile(t, dir))[0]).trail
	rotated := append(slices.Clone(line), first.RotationLine(identity, 1, second.public)...)
	file := filepath.Join(t.TempDir(), "sidecar")
	if err := os.WriteFile(file, rotated, 0o600); err != nil {
		t.Fatal(err)
	}
	opened, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	for signer, want := range map[*Signer]bool{first: false, second: true} {
		inForce, torn, err := keyInForce(opened, signer.public)
		if err != nil || inForce != want || torn {
			t.Fatalf("key %s: in force %v torn %v %v", signer.KeyID(), inForce, torn, err)
		}
	}
}

// A repair's discontinuity record is a chained record, and a signing writer
// signs it.
func TestARepairSignsItsDiscontinuityRecord(t *testing.T) {
	signer := signerOf(t, vectorSeed1)
	writer, dir := writerAt(t, "audit")
	appendSigned(t, writer.root, signer, `{"n":1}`)
	appendRaw(t, dir, `{"recordVersion":"1","trail":"`)
	repairer := NewWriter(writer.root, "audit", true)
	repairer.SignWith(signer)
	repaired, err := repairer.Repair()
	if err != nil {
		t.Fatal(err)
	}
	sidecar := readSidecar(t, dir)
	chain := verifySigned(t, readTrailFile(t, dir), sidecar, SignatureOptions{Keys: keys(signer)})
	if chain.Status != "segmented" || chain.Coverage.SignedRecords != 2 || chain.Coverage.Signed.Through != repaired.Line {
		t.Fatalf("verification: %v %+v", findingNames(chain), chain.Coverage)
	}
}

// Each hand-over changes only the sidecar. Across a torn trail write and its
// repair, the earlier trail and sidecar bytes remain prefixes, the damaged
// bytes remain verbatim, and the two rotations and three record signatures
// are exactly the lines their keys produce.
func TestRotationsAndRepairPreserveEveryHandedOverByte(t *testing.T) {
	first, second, third := signerOf(t, vectorSeed1), signerOf(t, vectorSeed2), thirdSigner(t)
	root, dir := signedTrail(t, first, 1)
	trail1, sidecar1 := readTrailFile(t, dir), readSidecar(t, dir)
	line1 := splitLines(trail1)[0]
	identity := readChain(t, line1).trail

	firstWriter := NewWriter(root, "audit", true)
	firstWriter.SignWith(first)
	if _, err := firstWriter.Rotate(second); err != nil {
		t.Fatal(err)
	}
	if got := readTrailFile(t, dir); !bytes.Equal(got, trail1) {
		t.Fatalf("the first hand-over changed the trail:\n%s", got)
	}
	rotation1 := first.RotationLine(identity, 1, second.public)
	if got, want := readSidecar(t, dir), slices.Concat(sidecar1, rotation1); !bytes.Equal(got, want) {
		t.Fatalf("first hand-over bytes:\ngot  %q\nwant %q", got, want)
	}

	const damage = `{"incomplete":`
	appendRaw(t, dir, damage)
	repairer := NewWriter(root, "audit", true)
	repairer.SignWith(second)
	repaired, err := repairer.Repair()
	if err != nil {
		t.Fatal(err)
	}
	afterRepair := readTrailFile(t, dir)
	if !bytes.HasPrefix(afterRepair, append(slices.Clone(trail1), damage...)) {
		t.Fatalf("repair did not preserve the trail and damaged bytes: %q", afterRepair)
	}
	lines := splitLines(afterRepair)
	if repaired.Line != 3 || repaired.DamagedLine != 2 || len(lines) != 3 || string(lines[1]) != damage {
		t.Fatalf("repair=%+v lines=%q", repaired, lines)
	}
	signedRepair, err := second.SignRecordLine(lines[2])
	if err != nil {
		t.Fatal(err)
	}
	wantThroughRepair := slices.Concat(sidecar1, rotation1, signedRepair)
	if got := readSidecar(t, dir); !bytes.Equal(got, wantThroughRepair) {
		t.Fatalf("repair sidecar bytes:\ngot  %q\nwant %q", got, wantThroughRepair)
	}

	if _, err := repairer.Rotate(third); err != nil {
		t.Fatal(err)
	}
	if got := readTrailFile(t, dir); !bytes.Equal(got, afterRepair) {
		t.Fatalf("the second hand-over changed repaired trail bytes:\n%s", got)
	}
	rotation2 := second.RotationLine(identity, 3, third.public)
	if got, want := readSidecar(t, dir), slices.Concat(wantThroughRepair, rotation2); !bytes.Equal(got, want) {
		t.Fatalf("second hand-over bytes:\ngot  %q\nwant %q", got, want)
	}
	appendSigned(t, root, third, `{"n":"after"}`)
	trail := readTrailFile(t, dir)
	allLines := splitLines(trail)
	signedLast, err := third.SignRecordLine(allLines[3])
	if err != nil {
		t.Fatal(err)
	}
	wantSidecar := slices.Concat(wantThroughRepair, rotation2, signedLast)
	if got := readSidecar(t, dir); !bytes.Equal(got, wantSidecar) {
		t.Fatalf("final sidecar bytes:\ngot  %q\nwant %q", got, wantSidecar)
	}
	chain := verifySigned(t, trail, wantSidecar, SignatureOptions{Keys: keys(first, second, third)})
	if names := findingNames(chain); len(names) != 0 || chain.Status != "segmented" || chain.Coverage.Chained != 3 || chain.Coverage.Damaged != 1 ||
		chain.Coverage.SignedRecords != 3 || chain.Coverage.Signed.Through != 4 || chain.Signatures.Rotations != 2 {
		t.Fatalf("findings=%v status=%s coverage=%+v signatures=%+v", names, chain.Status, chain.Coverage, chain.Signatures)
	}
}

// Writers racing on one trail, each signing with the one key, leave a sidecar
// whose lines follow the trail's exactly: one lock covers both files.
func TestSigningWritersRacingKeepTheSidecarInStep(t *testing.T) {
	signer := signerOf(t, vectorSeed1)
	_, root := writerAt(t, "audit")
	const writers, batches = 4, 16
	var group sync.WaitGroup
	failures := make(chan error, writers*batches)
	for range writers {
		group.Add(1)
		go func() {
			defer group.Done()
			opened, err := fssecure.OpenRoot(root)
			if err != nil {
				failures <- err
				return
			}
			defer opened.Close()
			for batch := range batches {
				failures <- appendSignedBatch(opened, signer, batch%2 == 1)
			}
		}()
	}
	group.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	checkSignedInStep(t, root, signer, writers*batches*2)
}

// appendSignedBatch is appendBatch through a writer that signs.
func appendSignedBatch(root *fssecure.Root, signer *Signer, graph bool) error {
	single, err := EvaluationRecord(evaluated(), Inputs{Facts: []byte(`{"a":"<&>"}`)}, nil, []byte(`{}`), nil)
	if err != nil {
		return err
	}
	writer := NewWriter(root, "audit", true)
	writer.SignWith(signer)
	if !graph {
		return writer.Append(single)
	}
	return writer.AppendAll([]Record{single, single, single})
}

// checkSignedInStep holds a trail and its sidecar to one signature per record,
// in the trail's order, every one verifying.
func checkSignedInStep(t *testing.T, root string, signer *Signer, want int) {
	t.Helper()
	whole, lines := trailLines(t, root, "audit")
	sidecar := readSidecar(t, root)
	if len(lines) != want || len(splitLines(sidecar)) != want {
		t.Fatalf("lines = %d, sidecar lines = %d, want %d", len(lines), len(splitLines(sidecar)), want)
	}
	checkChain(t, lines)
	chain := verifySigned(t, whole, sidecar, SignatureOptions{Keys: keys(signer), RequireThrough: int64(want)})
	if chain.Status != "valid" || chain.Coverage.SignedRecords != int64(want) || chain.Signatures.Unreadable != 0 {
		t.Fatalf("verification: %v %+v %+v", findingNames(chain), chain.Coverage, chain.Signatures)
	}
}

// signHelperDir and signHelperRun mark a copy of this test binary as a
// signing writer, set only by TestSigningWritersInSeveralProcessesKeepTheSidecarInStep.
const (
	signHelperDir = "JPACK_AUDIT_SIGN_HELPER_DIR"
	signHelperRun = "JPACK_AUDIT_SIGN_HELPER_RUN"
)

// The same race across processes.
func TestSigningWritersInSeveralProcessesKeepTheSidecarInStep(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Skipf("this test binary cannot be named: %v", err)
	}
	root := t.TempDir()
	const processes = 4
	commands, outputs := startHelpers(t, self, "^TestSignHelperProcess$", processes, signHelperDir+"="+root, signHelperRun+"=1")
	for index, command := range commands {
		if err := command.Wait(); err != nil {
			t.Fatalf("helper process %d: %v\n%s", index, err, outputs[index])
		}
		if !strings.Contains(outputs[index].String(), "sign helper wrote 12 batches") {
			t.Fatalf("helper process %d did not write: %s", index, outputs[index])
		}
	}
	checkSignedInStep(t, root, signerOf(t, vectorSeed1), processes*12*2)
}

// startHelpers starts n copies of this test binary running one test, with env
// added to their environment.
func startHelpers(t *testing.T, self, run string, n int, env ...string) ([]*exec.Cmd, []*bytes.Buffer) {
	t.Helper()
	commands := make([]*exec.Cmd, 0, n)
	outputs := make([]*bytes.Buffer, 0, n)
	for range n {
		command := exec.Command(self, "-test.run="+run, "-test.count=1")
		command.Env = append(os.Environ(), env...)
		output := &bytes.Buffer{}
		command.Stdout, command.Stderr = output, output
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		commands = append(commands, command)
		outputs = append(outputs, output)
	}
	return commands, outputs
}

// TestSignHelperProcess is the process the test above starts.
func TestSignHelperProcess(t *testing.T) {
	dir := os.Getenv(signHelperDir)
	if dir == "" || os.Getenv(signHelperRun) != "1" {
		t.Skip("run by TestSigningWritersInSeveralProcessesKeepTheSidecarInStep")
	}
	opened, err := fssecure.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	signer := signerOf(t, vectorSeed1)
	for batch := range 12 {
		if err := appendSignedBatch(opened, signer, batch%2 == 1); err != nil {
			t.Fatal(err)
		}
	}
	fmt.Println("sign helper wrote 12 batches")
}

// realTempDir is a fresh directory named by its real path: a signing key is
// named with no symbolic link anywhere in its path, and on some platforms the
// temporary directory is reached through one. It is its owner's alone, as a
// signing key's directory must be: t.TempDir makes it with whatever group and
// other bits the umask leaves, 0775 under umask 0002. The machine's
// directories above it, which the test does not choose, are held as root's
// 0755 for the rest of the test, so nothing here depends on where TMPDIR is.
func realTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(StandInKeyAncestorsForTests(dir))
	return dir
}

// writeKeyFile writes a key file with exactly the mode given.
func writeKeyFile(t *testing.T, path, contents string, mode os.FileMode) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

// A seed file is read in one form only, everywhere: 64 hexadecimal characters
// with nothing else but surrounding whitespace, from a file of at most the
// bound, named by an absolute path. No refusal carries the key's contents.
func TestASeedFileIsReadInItsOneForm(t *testing.T) {
	outside := realTempDir(t)
	good := writeKeyFile(t, filepath.Join(outside, "seed"), vectorSeed1+"\n", 0o600)
	if signer, err := ReadKey(good); err != nil || signer.PublicKey() != signerOf(t, vectorSeed1).PublicKey() {
		t.Fatalf("a good key: %v", err)
	}
	if _, err := ReadKey(writeKeyFile(t, filepath.Join(outside, "spaced"), "  "+strings.ToUpper(vectorSeed1)+"\r\n", 0o600)); err != nil {
		t.Fatalf("surrounding whitespace and upper case are read: %v", err)
	}
	for _, read := range []func(string) (*Signer, error){ReadKey, func(path string) (*Signer, error) { return LoadSigner(path, nil) }} {
		if _, err := read("seed"); !errors.Is(err, ErrKeyNotAbsolute) {
			t.Fatalf("a relative path: %v", err)
		}
	}
	for name, contents := range map[string]string{"short": vectorSeed1[:62], "long": vectorSeed1 + "00", "not hex": strings.Repeat("g", 64), "two": vectorSeed1 + "\n" + vectorSeed1, "empty": ""} {
		_, err := ReadKey(writeKeyFile(t, filepath.Join(outside, "bad-"+strings.ReplaceAll(name, " ", "-")), contents, 0o600))
		if !errors.Is(err, ErrKeyMalformed) {
			t.Fatalf("%s: %v", name, err)
		}
		if contents != "" && strings.Contains(err.Error(), contents) {
			t.Fatalf("%s: the refusal carries the key's contents", name)
		}
	}
	if _, err := ReadKey(writeKeyFile(t, filepath.Join(outside, "large"), vectorSeed1+strings.Repeat(" ", maxKeyFileBytes), 0o600)); !errors.Is(err, ErrKeyMalformed) {
		t.Fatalf("a file larger than a key file: %v", err)
	}
	if _, err := ReadKey(filepath.Join(outside, "absent")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("an absent key: %v", err)
	}
}

// Revocations and public keys are read by their exact shapes.
func TestRevocationsAndPublicKeysAreReadExactly(t *testing.T) {
	public := signerOf(t, vectorSeed1).PublicKey()
	revoked, err := ParseRevocations([]byte(`{"from":3,"publicKey":"` + public + `"}` + "\n\n" + `{"publicKey":"` + public + `","from":1}`))
	if err != nil || len(revoked) != 2 || revoked[0].From != 3 || revoked[1].From != 1 || hex.EncodeToString(revoked[0].PublicKey) != public {
		t.Fatalf("revocations = %+v %v", revoked, err)
	}
	for _, bad := range []string{
		`{"from":0,"publicKey":"` + public + `"}`,
		`{"from":1.0,"publicKey":"` + public + `"}`,
		`{"from":"1","publicKey":"` + public + `"}`,
		`{"from":1,"publicKey":"` + strings.ToUpper(public) + `"}`,
		`{"from":1,"publicKey":"` + public + `","note":"x"}`,
		`{"from":1,"from":2,"publicKey":"` + public + `"}`,
		`[1]`,
	} {
		if _, err := ParseRevocations([]byte(bad)); err == nil {
			t.Fatalf("%s is refused", bad)
		}
	}
	if _, err := ParsePublicKey([]byte(public + "\n")); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{public[:63], public + "0", "", strings.Repeat("x", 64)} {
		if _, err := ParsePublicKey([]byte(bad)); err == nil {
			t.Fatalf("%q is refused", bad)
		}
	}
}

// A rotation line is read by its exact shape too.
func TestARotationLineOfAnotherShapeIsUnreadable(t *testing.T) {
	first, second := signerOf(t, vectorSeed1), signerOf(t, vectorSeed2)
	line := bytes.TrimSuffix(first.RotationLine(strings.Repeat("ab", 16), 1, second.public), []byte("\n"))
	if !parseSidecarLine(line).readable {
		t.Fatal("the writer's rotation is readable")
	}
	for name, edit := range map[string][2]string{
		"at zero":     {`"at":1`, `"at":0`},
		"at fraction": {`"at":1`, `"at":1.5`},
		"next short":  {`"next":"3d`, `"next":"`},
		"next upper":  {`"next":"3d`, `"next":"3D`},
		"kind":        {`"kind":"key-rotation"`, `"kind":"key-rotation-2"`},
	} {
		edited := bytes.Replace(line, []byte(edit[0]), []byte(edit[1]), 1)
		if bytes.Equal(edited, line) || parseSidecarLine(edited).readable {
			t.Fatalf("%s: %s is unreadable", name, edited)
		}
	}
}

// A sidecar line is read by its exact shape: seven members of its kind, each
// once and of its form. A line of another shape is unreadable, not a finding.
func TestASidecarLineOfAnotherShapeIsUnreadable(t *testing.T) {
	signer := signerOf(t, vectorSeed1)
	_, dir := signedTrail(t, signer, 1)
	trail := readTrailFile(t, dir)
	line := bytes.TrimSuffix(readSidecar(t, dir), []byte("\n"))
	if !parseSidecarLine(line).readable {
		t.Fatal("the writer's line is readable")
	}
	spaced := bytes.ReplaceAll(line, []byte(`":`), []byte(`": `))
	if !parseSidecarLine(spaced).readable {
		t.Fatal("JSON whitespace is the same value")
	}
	for name, edit := range map[string][2]string{
		"version":   {`"sidecarVersion":"1"`, `"sidecarVersion":"2"`},
		"kind":      {`"kind":"record-signature"`, `"kind":"other"`},
		"extra":     {`"trail":`, `"x":1,"trail":`},
		"sequence":  {`"sequence":1`, `"sequence":1.0`},
		"zero":      {`"sequence":1`, `"sequence":0`},
		"record":    {`"record":"sha256:`, `"record":"sha512:`},
		"keyId":     {`"keyId":"`, `"keyId":"0`},
		"signature": {`"signature":"`, `"signature":"0`},
		"trail":     {`"trail":"`, `"trail":"0`},
		"missing":   {`,"sidecarVersion":"1"`, ``},
	} {
		edited := bytes.Replace(line, []byte(edit[0]), []byte(edit[1]), 1)
		if bytes.Equal(edited, line) {
			t.Fatalf("%s: the edit did not apply", name)
		}
		if parseSidecarLine(edited).readable {
			t.Fatalf("%s: %s is unreadable", name, edited)
		}
		chain := verifySigned(t, trail, append(edited, '\n'), SignatureOptions{Keys: keys(signer)})
		if chain.Status != "valid" || chain.Signatures.Unreadable != 1 || chain.Coverage.SignedRecords != 0 {
			t.Fatalf("%s: %v %+v", name, findingNames(chain), chain.Signatures)
		}
	}
}

// A finding about the sidecar says nothing about whether the trail is
// consistent, so it voids neither a held checkpoint's coverage nor the
// coverage of a later valid signature.
func TestASidecarFindingVoidsNoCoverage(t *testing.T) {
	signer := signerOf(t, vectorSeed1)
	_, dir := signedTrail(t, signer, 3)
	trail := readTrailFile(t, dir)
	lines := splitLines(readSidecar(t, dir))
	lines[1] = editSignature(lines[1])
	sidecar := joinLines(lines)
	options := SignatureOptions{Keys: keys(signer)}
	options.Sidecar, options.SidecarSize = bytes.NewReader(sidecar), int64(len(sidecar))
	report := verifyWith(t, trail, Options{Held: keptCheckpoints(t, trail, 3), Signatures: &options})
	chain := report.Chain
	if !slices.Equal(findingNames(chain), []string{"signature-invalid@2"}) || chain.Coverage.Checkpointed.Status != "through" || chain.Coverage.Checkpointed.Through != 3 ||
		chain.Coverage.Signed.Through != 3 {
		t.Fatalf("coverage after a sidecar finding: %v %+v", findingNames(chain), chain.Coverage)
	}
}

// editSignature changes the first hexadecimal digit of a sidecar line's
// signature to another hexadecimal digit, so the line keeps its shape.
func editSignature(line []byte) []byte {
	edited := slices.Clone(line)
	at := bytes.Index(edited, []byte(`"signature":"`)) + len(`"signature":"`)
	if edited[at] == '0' {
		edited[at] = '1'
	} else {
		edited[at] = '0'
	}
	return edited
}
