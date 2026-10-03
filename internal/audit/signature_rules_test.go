package audit

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	mathrand "math/rand/v2"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The rules a second implementation of record signatures needs (#216), held
// against this runtime: the public keys a verifier refuses, and how a sidecar
// line is read. The guide states each one ("Record signatures, exactly").

// The curve's arithmetic for these tests, written apart from the code under
// test: a point is decoded by RFC 8032 §5.1.3 with math/big's square root,
// and points are added by the complete affine addition law of §5.1.4.
var (
	testPrime = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(19))
	testD     = new(big.Int).Mod(new(big.Int).Mul(big.NewInt(-121665), new(big.Int).ModInverse(big.NewInt(121666), testPrime)), testPrime)
)

type testPoint struct{ x, y *big.Int }

// decodeTestPoint decodes a canonical encoding, or answers false for one that
// is not canonical or is no point.
func decodeTestPoint(encoded []byte) (testPoint, bool) {
	reversed := slices.Clone(encoded)
	slices.Reverse(reversed)
	sign := reversed[0] >> 7
	reversed[0] &= 0x7f
	y := new(big.Int).SetBytes(reversed)
	if y.Cmp(testPrime) >= 0 {
		return testPoint{}, false
	}
	yy := new(big.Int).Mul(y, y)
	u := new(big.Int).Sub(yy, big.NewInt(1))
	v := new(big.Int).Add(new(big.Int).Mul(testD, yy), big.NewInt(1))
	xx := new(big.Int).Mul(u, new(big.Int).ModInverse(v.Mod(v, testPrime), testPrime))
	x := new(big.Int).ModSqrt(xx.Mod(xx, testPrime), testPrime)
	if x == nil || (x.Sign() == 0 && sign == 1) {
		return testPoint{}, false
	}
	if x.Bit(0) != uint(sign) {
		x.Sub(testPrime, x)
	}
	return testPoint{x, y}, true
}

func (a testPoint) plus(b testPoint) testPoint {
	t := new(big.Int).Mul(a.x, b.x)
	t.Mul(t, a.y).Mul(t, b.y).Mul(t, testD).Mod(t, testPrime)
	x := new(big.Int).Add(new(big.Int).Mul(a.x, b.y), new(big.Int).Mul(a.y, b.x))
	y := new(big.Int).Add(new(big.Int).Mul(a.y, b.y), new(big.Int).Mul(a.x, b.x))
	xDivisor := new(big.Int).Add(big.NewInt(1), t)
	yDivisor := new(big.Int).Sub(big.NewInt(1), t)
	x.Mul(x, new(big.Int).ModInverse(xDivisor.Mod(xDivisor, testPrime), testPrime))
	y.Mul(y, new(big.Int).ModInverse(yDivisor.Mod(yDivisor, testPrime), testPrime))
	return testPoint{x.Mod(x, testPrime), y.Mod(y, testPrime)}
}

func (a testPoint) isIdentity() bool { return a.x.Sign() == 0 && a.y.Cmp(big.NewInt(1)) == 0 }

// encodeY is the 32-byte encoding of y with the sign bit given, which may be
// no point, or not canonical, on purpose.
func encodeY(y *big.Int, sign byte) []byte {
	encoded := y.FillBytes(make([]byte, 32))
	slices.Reverse(encoded)
	encoded[31] |= sign << 7
	return encoded
}

func mustHex(t *testing.T, text string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(text)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

// The eight points whose order divides 8, by their canonical encodings, as
// the guide lists them.
var testSmallOrder = []string{
	"0100000000000000000000000000000000000000000000000000000000000000",
	"ecffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff7f",
	"0000000000000000000000000000000000000000000000000000000000000000",
	"0000000000000000000000000000000000000000000000000000000000000080",
	"26e8958fc2b227b045c3f489f2ef98f0d5dfac05d3c63339b13802886d53fc05",
	"26e8958fc2b227b045c3f489f2ef98f0d5dfac05d3c63339b13802886d53fc85",
	"c7176a703d4dd84fba3c0b760d10670f2a2053fa2c39ccc64ec7fd7792ac037a",
	"c7176a703d4dd84fba3c0b760d10670f2a2053fa2c39ccc64ec7fd7792ac03fa",
}

// forgeUnder makes a signature over message under public without any
// private key: R a point of small order and s zero, which crypto/ed25519
// accepts whenever -[k]A is R. It answers nil when no such R verifies for
// this message.
func forgeUnder(public, message []byte) []byte {
	for _, encoded := range testSmallOrder {
		point, _ := hex.DecodeString(encoded)
		signature := append(point, make([]byte, 32)...)
		if ed25519.Verify(public, message, signature) {
			return signature
		}
	}
	return nil
}

// The eight points of small order are all of them: each is a point of the
// curve whose order divides 8, they are distinct, and the curve has eight
// such points. Under every one, crypto/ed25519 accepts a signature made with
// no private key, and every one is refused.
func TestTheEightKeysOfSmallOrderAreRefused(t *testing.T) {
	seen := map[string]bool{}
	for _, encoded := range testSmallOrder {
		public := mustHex(t, encoded)
		point, ok := decodeTestPoint(public)
		if !ok {
			t.Fatalf("%s is no canonical point", encoded)
		}
		multiple := point
		for range 7 {
			multiple = multiple.plus(point)
		}
		if !multiple.isIdentity() {
			t.Fatalf("%s: eight times it is not the identity", encoded)
		}
		seen[point.x.String()+","+point.y.String()] = true
		forged := 0
		for index := range 64 {
			if forgeUnder(public, []byte(fmt.Sprintf("message %d", index))) != nil {
				forged++
			}
		}
		if forged == 0 {
			t.Fatalf("%s: no forgery verified, so the demonstration shows nothing", encoded)
		}
		t.Logf("%s: %d of 64 messages forged under crypto/ed25519", encoded[:8], forged)
		if err := CheckPublicKey(public); !errors.Is(err, ErrPublicKeySmallOrder) {
			t.Fatalf("%s: %v", encoded, err)
		}
		if _, err := ParsePublicKey([]byte(encoded + "\n")); !errors.Is(err, ErrPublicKeySmallOrder) {
			t.Fatalf("%s as a key file: %v", encoded, err)
		}
	}
	if len(seen) != 8 {
		t.Fatalf("%d distinct points", len(seen))
	}
}

// Every encoding a verifier would read as another is refused, before any
// reduction: y from p to 2^255-1 with either sign bit, and x = 0 with its
// sign bit set. crypto/ed25519 reads y = p as y = 0, the all-zero key's
// point, and the identity with its sign bit set as the identity, and accepts
// forgeries under both.
func TestEncodingsThatAreNotCanonicalAreRefused(t *testing.T) {
	limit := new(big.Int).Lsh(big.NewInt(1), 255)
	count := 0
	for y := new(big.Int).Set(testPrime); y.Cmp(limit) < 0; y.Add(y, big.NewInt(1)) {
		for _, sign := range []byte{0, 1} {
			encoded := encodeY(y, sign)
			if err := CheckPublicKey(encoded); !errors.Is(err, ErrPublicKeyNotCanonical) {
				t.Fatalf("y = p + %s, sign %d: %v", new(big.Int).Sub(y, testPrime), sign, err)
			}
			count++
		}
	}
	if count != 38 {
		t.Fatalf("%d encodings of y at or above p", count)
	}
	for _, y := range []*big.Int{big.NewInt(1), new(big.Int).Sub(testPrime, big.NewInt(1))} {
		if err := CheckPublicKey(encodeY(y, 1)); !errors.Is(err, ErrPublicKeyNotCanonical) {
			t.Fatalf("x = 0 with the sign bit set, y = %s: %v", y, err)
		}
	}
	yIsP := encodeY(testPrime, 0)
	if hex.EncodeToString(yIsP) != "edffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff7f" {
		t.Fatalf("y = p encodes as %x", yIsP)
	}
	for _, public := range [][]byte{yIsP, encodeY(big.NewInt(1), 1)} {
		forged := 0
		for index := range 64 {
			if forgeUnder(public, []byte(fmt.Sprintf("message %d", index))) != nil {
				forged++
			}
		}
		if forged == 0 {
			t.Fatalf("%x: no forgery verified, so crypto/ed25519 did not read it as a point of small order", public)
		}
	}
}

// 32 bytes that encode no point are refused: nothing could verify under them.
func TestAKeyOffTheCurveIsRefused(t *testing.T) {
	found := 0
	for y := int64(2); found < 4; y++ {
		encoded := encodeY(big.NewInt(y), 0)
		if _, ok := decodeTestPoint(encoded); ok {
			if err := CheckPublicKey(encoded); err != nil {
				t.Fatalf("y = %d is a point of the curve: %v", y, err)
			}
			continue
		}
		for _, sign := range []byte{0, 1} {
			if err := CheckPublicKey(encodeY(big.NewInt(y), sign)); !errors.Is(err, ErrPublicKeyNotOnCurve) {
				t.Fatalf("y = %d, sign %d: %v", y, sign, err)
			}
		}
		found++
	}
}

// No key a seed derives is refused, nor a point of the curve whose order does
// not divide 8 though it has a part of small order: no forgery holds under
// such a key without its private key.
func TestEveryKeyASeedDerivesIsAccepted(t *testing.T) {
	for _, seed := range []string{vectorSeed1, vectorSeed2} {
		if err := CheckPublicKey(signerOf(t, seed).public); err != nil {
			t.Fatal(err)
		}
	}
	for range 256 {
		public, _, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if err := CheckPublicKey(public); err != nil {
			t.Fatalf("%x: %v", public, err)
		}
	}
	key, _ := decodeTestPoint(signerOf(t, vectorSeed1).public)
	two, _ := decodeTestPoint(mustHex(t, testSmallOrder[1]))
	mixed := key.plus(two)
	if err := CheckPublicKey(encodeY(mixed.y, byte(mixed.x.Bit(0)))); err != nil {
		t.Fatalf("a point of mixed order: %v", err)
	}
	if err := CheckPublicKey(make([]byte, 31)); !errors.Is(err, ErrPublicKeyForm) {
		t.Fatalf("31 bytes: %v", err)
	}
}

// Every public key the runtime reads is held to the check: a key file, and
// each revocation's. A refused key is named by why, never by its bytes.
func TestEveryPublicKeyReadIsChecked(t *testing.T) {
	refused := map[string]error{
		testSmallOrder[2]: ErrPublicKeySmallOrder,
		"edffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff7f": ErrPublicKeyNotCanonical,
		"0100000000000000000000000000000000000000000000000000000000000080": ErrPublicKeyNotCanonical,
		hex.EncodeToString(encodeY(big.NewInt(2), 0)):                      ErrPublicKeyNotOnCurve,
	}
	if _, ok := decodeTestPoint(encodeY(big.NewInt(2), 0)); ok {
		t.Fatal("y = 2 is a point; pick another off the curve")
	}
	for public, want := range refused {
		if _, err := ParsePublicKey([]byte(public)); !errors.Is(err, want) {
			t.Fatalf("key file %s: %v", public, err)
		}
		_, err := ParseRevocations([]byte(`{"from":1,"publicKey":"` + public + `"}`))
		if err == nil || !strings.Contains(err.Error(), "line 1: a revocation's publicKey is not one: "+want.Error()) || strings.Contains(err.Error(), public) {
			t.Fatalf("revocation of %s: %v", public, err)
		}
	}
}

// memoryTrail is a chained trail of n records held in memory, of the guide's
// test vector's trail.
func memoryTrail(n int) [][]byte {
	lines := [][]byte{}
	previous := Digest(nil)
	for sequence := 1; sequence <= n; sequence++ {
		line := []byte(fmt.Sprintf(`{"recordVersion":"1","trail":"%s","sequence":%d,"previous":"%s","kind":"evaluation"}`, vectorTrail, sequence, previous))
		lines = append(lines, line)
		previous = Digest(line)
	}
	return lines
}

const vectorTrail = "00112233445566778899aabbccddeeff"

// forgedRecordLine is a record-signature line for record under public, its
// signature made without a private key.
func forgedRecordLine(t *testing.T, public ed25519.PublicKey, record []byte) []byte {
	t.Helper()
	trail, sequence, _ := chainedLine(record)
	signature := forgeUnder(public, RecordMessage(trail, sequence, Digest(record)))
	if signature == nil {
		t.Fatalf("no forgery for record %d", sequence)
	}
	return bytes.TrimSuffix(canonicalLine(recordSignatureLine{
		KeyID: KeyID(public), Kind: KindRecordSignature, Record: Digest(record), Sequence: sequence,
		SidecarVersion: SidecarVersion, Signature: hex.EncodeToString(signature), Trail: trail,
	}), []byte("\n"))
}

// A rotation to a key a verifier refuses hands nothing over, even made with
// the key in force: otherwise anyone could sign every record after it. Here
// the key in force rotates to the identity, under which crypto/ed25519
// accepts the forged signature of every record; the verifier keeps the old
// key, so the forgeries are invalid, never signed.
func TestARotationToARefusedKeyHandsNothingOver(t *testing.T) {
	first := signerOf(t, vectorSeed1)
	lines := memoryTrail(3)
	signed, err := first.SignRecordLine(lines[0])
	if err != nil {
		t.Fatal(err)
	}
	identity := ed25519.PublicKey(mustHex(t, testSmallOrder[0]))
	forged := forgedRecordLine(t, identity, lines[1])
	if !ed25519.Verify(identity, RecordMessage(vectorTrail, 2, Digest(lines[1])), parseSidecarLine(forged).signature) {
		t.Fatal("the forgery does not verify under crypto/ed25519, so the demonstration shows nothing")
	}
	sidecar := joinLines([][]byte{
		bytes.TrimSuffix(signed, []byte("\n")),
		bytes.TrimSuffix(first.RotationLine(vectorTrail, 1, identity), []byte("\n")),
		forged,
		forgedRecordLine(t, identity, lines[2]),
	})
	chain := verifySigned(t, joinLines(lines), sidecar, SignatureOptions{Keys: keys(first)})
	if !slices.Equal(findingNames(chain), []string{"rotation-invalid@1", "signature-invalid@2", "signature-invalid@3"}) ||
		chain.Coverage.SignedRecords != 1 || chain.Signatures.Rotations != 0 || chain.Signatures.KeyInForce != first.KeyID() ||
		!strings.Contains(chain.Findings[0].Detail, "not one a verifier accepts: "+ErrPublicKeySmallOrder.Error()) {
		t.Fatalf("%v %+v %+v", findingNames(chain), chain.Coverage, chain.Findings)
	}
	// Every refused key, rotated to, is refused the same way.
	for _, next := range []string{testSmallOrder[2], testSmallOrder[7], "edffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff7f", hex.EncodeToString(encodeY(big.NewInt(2), 0))} {
		rotation := bytes.TrimSuffix(first.RotationLine(vectorTrail, 1, mustHex(t, next)), []byte("\n"))
		chain := verifySigned(t, joinLines(lines), joinLines([][]byte{rotation}), SignatureOptions{Keys: keys(first)})
		if !slices.Equal(findingNames(chain), []string{"rotation-invalid@1"}) || chain.Signatures.KeyInForce != first.KeyID() {
			t.Fatalf("rotation to %s: %v", next, findingNames(chain))
		}
	}
}

// oneRecordSigned is the guide's check of one record without its trail ("One
// record, without the trail"), written from the guide's words, not from the
// verifier. T and S are the record's own trail and sequence, and R the digest
// of its exact bytes. The sidecar's lines, each ended by a newline, are read
// in order: an unreadable one is passed over; each readable one is held to
// step 1 (its place against the last place); a rotation to step 3, T being
// its trail, and when it holds its next key is in force; and the first record
// signature for S, in order, decides by step 2.
func oneRecordSigned(record, sidecar []byte, options SignatureOptions) bool {
	trail, sequence, chained := chainedLine(record)
	if !chained {
		return false
	}
	digest := Digest(record)
	inForce, nextPinned, lastPlace := options.Keys[0], 1, int64(0)
	revoked := func(at int64) bool {
		for _, revocation := range options.Revoked {
			if bytes.Equal(revocation.PublicKey, inForce) && revocation.From <= at {
				return true
			}
		}
		return false
	}
	for {
		end := bytes.IndexByte(sidecar, '\n')
		if end < 0 {
			return false
		}
		item := parseSidecarLine(sidecar[:end])
		sidecar = sidecar[end+1:]
		if !item.readable {
			continue
		}
		place := 2 * item.sequence
		if item.kind == KindKeyRotation {
			place = 2*item.at + 1
		}
		if place < lastPlace || (item.kind == KindRecordSignature && place == lastPlace) {
			continue
		}
		lastPlace = place
		if item.kind == KindKeyRotation {
			if item.trail == trail && item.keyID == KeyID(inForce) &&
				ed25519.Verify(inForce, RotationMessage(item.trail, item.at, hex.EncodeToString(item.next)), item.signature) &&
				!revoked(item.at) && CheckPublicKey(item.next) == nil &&
				(len(options.Keys) == 1 || (nextPinned < len(options.Keys) && bytes.Equal(item.next, options.Keys[nextPinned]))) {
				inForce, nextPinned = item.next, nextPinned+1
			}
			continue
		}
		if item.sequence != sequence {
			continue
		}
		return item.trail == trail && item.record == digest && item.keyID == KeyID(inForce) &&
			ed25519.Verify(inForce, RecordMessage(trail, sequence, digest), item.signature) && !revoked(sequence)
	}
}

// keyIDMember is a sidecar line's keyId, to name another key without
// touching the signature.
var keyIDMember = regexp.MustCompile(`"keyId":"[0-9a-f]{32}"`)

// The guide's check of one record gives, for every record of a trail that
// holds it undamaged, the answer audit verify gives for that record, however
// the sidecar is reordered, cut, repeated, edited or added to: rotations
// moved, made with the wrong key, to a refused key or to an unpinned one;
// records signed twice, by another key or for another record; lines naming
// another key, torn, overlong or of no shape. The records it finds signed are
// the ones the verifier counts, up to the same sequence.
func TestOneRecordIsCheckedAsTheVerifierChecksIt(t *testing.T) {
	first, second, third := signerOf(t, vectorSeed1), signerOf(t, vectorSeed2), thirdSigner(t)
	fourth := signerOf(t, strings.Repeat("0f", 32))
	identity := ed25519.PublicKey(mustHex(t, testSmallOrder[0]))
	const n = 12
	lines := memoryTrail(n)
	trail := joinLines(lines)
	honest := [][]byte{}
	sign := func(signer *Signer, line []byte) []byte {
		signed, err := signer.SignRecordLine(line)
		if err != nil {
			t.Fatal(err)
		}
		return bytes.TrimSuffix(signed, []byte("\n"))
	}
	rotate := func(signer *Signer, trail string, at int64, next ed25519.PublicKey) []byte {
		return bytes.TrimSuffix(signer.RotationLine(trail, at, next), []byte("\n"))
	}
	for index, line := range lines {
		switch {
		case index < 4:
			honest = append(honest, sign(first, line))
		case index < 8:
			honest = append(honest, sign(second, line))
		default:
			honest = append(honest, sign(third, line))
		}
		if index == 3 {
			honest = append(honest, rotate(first, vectorTrail, 4, second.public))
		}
		if index == 7 {
			honest = append(honest, rotate(second, vectorTrail, 8, third.public))
		}
	}
	signers := []*Signer{first, second, third, fourth}
	random := mathrand.New(mathrand.NewPCG(216, 8032))
	trustings := []SignatureOptions{
		{Keys: keys(first)},
		{Keys: keys(first, second, third)},
		{Keys: keys(first, second)},
		{Keys: keys(first), Revoked: []Revocation{{PublicKey: second.public, From: 6}}},
		{Keys: keys(first, second, third), Revoked: []Revocation{{PublicKey: first.public, From: 3}}},
	}
	disagreements := 0
	for round := range 600 {
		sidecar := slices.Clone(honest)
		for range 1 + random.IntN(4) {
			at := random.IntN(len(sidecar) + 1)
			signer := signers[random.IntN(len(signers))]
			line := lines[random.IntN(n)]
			switch random.IntN(11) {
			case 0:
				if len(sidecar) > 0 {
					sidecar = slices.Delete(sidecar, at%len(sidecar), at%len(sidecar)+1)
				}
			case 1:
				if len(sidecar) > 0 {
					sidecar = slices.Insert(sidecar, at, sidecar[random.IntN(len(sidecar))])
				}
			case 2:
				if len(sidecar) > 1 {
					from, to := random.IntN(len(sidecar)), random.IntN(len(sidecar))
					sidecar[from], sidecar[to] = sidecar[to], sidecar[from]
				}
			case 3:
				if len(sidecar) > 0 {
					sidecar[at%len(sidecar)] = editSignature(sidecar[at%len(sidecar)])
				}
			case 4:
				next := signers[random.IntN(len(signers))].public
				sidecar = slices.Insert(sidecar, at, rotate(signer, vectorTrail, int64(1+random.IntN(n+1)), next))
			case 5:
				sidecar = slices.Insert(sidecar, at, rotate(signer, vectorTrail, int64(1+random.IntN(n)), identity), forgedRecordLine(t, identity, line))
			case 6:
				sidecar = slices.Insert(sidecar, at, sign(signer, line))
			case 7:
				edited := bytes.Replace(line, []byte(`"kind":"evaluation"`), []byte(`"kind":"evaluation","x":1`), 1)
				sidecar = slices.Insert(sidecar, at, sign(signer, edited))
			case 8:
				sidecar = slices.Insert(sidecar, at, rotate(signer, strings.Repeat("ab", 16), int64(1+random.IntN(n)), signers[random.IntN(len(signers))].public))
			case 9:
				if len(sidecar) > 0 {
					index := at % len(sidecar)
					sidecar[index] = keyIDMember.ReplaceAll(sidecar[index], []byte(`"keyId":"`+signer.KeyID()+`"`))
				}
			default:
				junk := [][]byte{[]byte(`{"kind":"record-signature"}`), append([]byte("{"), bytes.Repeat([]byte(" "), maxSidecarLineBytes)...), []byte("~")}
				sidecar = slices.Insert(sidecar, at, junk[random.IntN(len(junk))])
			}
		}
		whole := joinLines(sidecar)
		if random.IntN(5) == 0 {
			whole = whole[:len(whole)-1]
		}
		options := trustings[round%len(trustings)]
		chain := verifySigned(t, trail, whole, options)
		count, through := int64(0), int64(0)
		for sequence, record := range lines {
			if oneRecordSigned(record, whole, options) {
				count, through = count+1, int64(sequence+1)
			}
		}
		if chain.Coverage.SignedRecords != count || (count > 0 && chain.Coverage.Signed.Through != through) {
			disagreements++
			t.Errorf("round %d: the verifier signed %d through %d, one record at a time %d through %d; findings %v\n%s",
				round, chain.Coverage.SignedRecords, chain.Coverage.Signed.Through, count, through, findingNames(chain), whole)
			if disagreements > 3 {
				t.FailNow()
			}
		}
	}
}

// One record without its trail takes its trail and sequence from its own
// members and its digest from its exact bytes, as a gateway does: the test
// vector's second record is signed, a record that is not a chained line has
// no signature, and a record re-encoded is another record.
func TestOneRecordTakesItsTrailAndSequenceFromItsOwnMembers(t *testing.T) {
	first, second := signerOf(t, vectorSeed1), signerOf(t, vectorSeed2)
	lines := memoryTrail(2)
	sidecar := vectorSidecar(t, lines)
	options := SignatureOptions{Keys: keys(first)}
	for index, record := range lines {
		if !oneRecordSigned(record, sidecar, options) {
			t.Fatalf("record %d is signed", index+1)
		}
	}
	if oneRecordSigned(lines[1], sidecar, SignatureOptions{Keys: keys(second)}) != true {
		t.Fatal("record 2 is signed under the second key alone")
	}
	respaced := bytes.ReplaceAll(lines[1], []byte(`,"`), []byte(`, "`))
	renumbered := bytes.Replace(lines[1], []byte(`"sequence":2`), []byte(`"sequence":3`), 1)
	unchained := bytes.Replace(lines[1], []byte(`"previous"`), []byte(`"prior"`), 1)
	for name, record := range map[string][]byte{"re-encoded": respaced, "renumbered": renumbered, "unchained": unchained} {
		if oneRecordSigned(record, sidecar, options) {
			t.Fatalf("%s: signed", name)
		}
	}
}

// vectorSidecar is the guide's test vector's three sidecar lines for lines.
func vectorSidecar(t *testing.T, lines [][]byte) []byte {
	t.Helper()
	first, second := signerOf(t, vectorSeed1), signerOf(t, vectorSeed2)
	one, err := first.SignRecordLine(lines[0])
	if err != nil {
		t.Fatal(err)
	}
	two, err := second.SignRecordLine(lines[1])
	if err != nil {
		t.Fatal(err)
	}
	return slices.Concat(one, first.RotationLine(vectorTrail, 1, second.public), two)
}

// Several lines for one sequence: following the key history, the first in
// order decides, and a later one is out of order and signs nothing, however
// valid. The verifier and the check of one record agree.
func TestTheFirstLineForASequenceDecides(t *testing.T) {
	signer := signerOf(t, vectorSeed1)
	lines := memoryTrail(1)
	valid, err := signer.SignRecordLine(lines[0])
	if err != nil {
		t.Fatal(err)
	}
	valid = bytes.TrimSuffix(valid, []byte("\n"))
	options := SignatureOptions{Keys: keys(signer)}
	for name, want := range map[string]struct {
		sidecar  [][]byte
		findings []string
		signed   bool
	}{
		"invalid, then valid":  {[][]byte{editSignature(valid), valid}, []string{"signature-invalid@1", "sidecar-out-of-order@1"}, false},
		"valid, then repeated": {[][]byte{valid, valid}, []string{"sidecar-out-of-order@1"}, true},
	} {
		sidecar := joinLines(want.sidecar)
		chain := verifySigned(t, joinLines(lines), sidecar, options)
		if !slices.Equal(findingNames(chain), want.findings) || (chain.Coverage.SignedRecords == 1) != want.signed || oneRecordSigned(lines[0], sidecar, options) != want.signed {
			t.Fatalf("%s: %v %+v", name, findingNames(chain), chain.Coverage)
		}
	}
}

// "Each rotation before it" is each rotation earlier in the sidecar, which
// step 1 then holds to an at below the record's sequence. A rotation placed
// after the record it precedes is out of order and hands nothing over, and a
// rotation that fails a check hands nothing over: the key in force stays.
func TestARotationCountsWhereItIsInTheSidecar(t *testing.T) {
	first := signerOf(t, vectorSeed1)
	lines := memoryTrail(2)
	vector := splitLines(vectorSidecar(t, lines))
	byFirst, err := first.SignRecordLine(lines[1])
	if err != nil {
		t.Fatal(err)
	}
	byFirst = bytes.TrimSuffix(byFirst, []byte("\n"))
	options := SignatureOptions{Keys: keys(first)}
	for name, want := range map[string]struct {
		sidecar  [][]byte
		findings []string
		signed   bool
	}{
		"moved after the record":             {[][]byte{vector[0], vector[2], vector[1]}, []string{"signature-invalid@2", "sidecar-out-of-order@1"}, false},
		"invalid, next key's record":         {[][]byte{vector[0], editSignature(vector[1]), vector[2]}, []string{"rotation-invalid@1", "signature-invalid@2"}, false},
		"invalid, old key's record":          {[][]byte{vector[0], editSignature(vector[1]), byFirst}, []string{"rotation-invalid@1"}, true},
		"after the record, old key's record": {[][]byte{vector[0], byFirst, vector[1]}, []string{"sidecar-out-of-order@1"}, true},
	} {
		sidecar := joinLines(want.sidecar)
		chain := verifySigned(t, joinLines(lines), sidecar, options)
		if !slices.Equal(findingNames(chain), want.findings) || oneRecordSigned(lines[1], sidecar, options) != want.signed {
			t.Fatalf("%s: %v, record 2 alone %v", name, findingNames(chain), oneRecordSigned(lines[1], sidecar, options))
		}
	}
}

// A record copied out of its trail is checked with its sidecar's lines up to
// and including its own signature line, copied as they are: with them the
// check of one record gives the answer it gives with the whole sidecar, and
// the lines after its own change nothing.
func TestACopiedRecordIsCheckedWithItsSidecarLines(t *testing.T) {
	first := signerOf(t, vectorSeed1)
	lines := memoryTrail(2)
	sidecar := splitLines(vectorSidecar(t, lines))
	options := SignatureOptions{Keys: keys(first)}
	if !oneRecordSigned(lines[1], joinLines(sidecar), options) {
		t.Fatal("record 2 with the whole sidecar")
	}
	if !oneRecordSigned(lines[1], joinLines(append(slices.Clone(sidecar), []byte("{}"), sidecar[0])), options) {
		t.Fatal("lines after its own change nothing")
	}
	if oneRecordSigned(lines[1], joinLines(sidecar[2:]), options) {
		t.Fatal("its signature line alone, without the rotation before it, is under the first key")
	}
	if !oneRecordSigned(lines[0], joinLines(sidecar[:1]), options) {
		t.Fatal("record 1 with its own line")
	}
}

// The 4096-byte bound on a sidecar line does not count its newline: a line
// of 4096 bytes before its newline is read, by the verifier and by the
// writer looking for the key in force, and one of 4097 is not.
func TestTheLineBoundDoesNotCountTheNewline(t *testing.T) {
	signer := signerOf(t, vectorSeed1)
	lines := memoryTrail(1)
	signed, err := signer.SignRecordLine(lines[0])
	if err != nil {
		t.Fatal(err)
	}
	signed = bytes.TrimSuffix(signed, []byte("\n"))
	padded := func(length int) []byte {
		return slices.Concat([]byte("{"), bytes.Repeat([]byte(" "), length-len(signed)), signed[1:])
	}
	for _, length := range []int{maxSidecarLineBytes, maxSidecarLineBytes + 1} {
		line := padded(length)
		if len(line) != length || maxSidecarLineBytes != 4096 {
			t.Fatalf("a line of %d bytes", len(line))
		}
		readable := length <= 4096
		chain := verifySigned(t, joinLines(lines), joinLines([][]byte{line}), SignatureOptions{Keys: keys(signer)})
		if (chain.Coverage.SignedRecords == 1) != readable || (chain.Signatures.Unreadable == 1) == readable {
			t.Fatalf("%d bytes: %+v %+v", length, chain.Coverage, chain.Signatures)
		}
		other := thirdSigner(t).RotationLine(vectorTrail, 1, signerOf(t, vectorSeed2).public)
		sidecar := slices.Concat(other, line, []byte("\n"))
		last, err := lastReadableSidecarLine(bytes.NewReader(sidecar), int64(len(sidecar)))
		if err != nil || (last.kind == KindRecordSignature) != readable {
			t.Fatalf("%d bytes, as the writer reads it: %+v %v", length, last, err)
		}
	}
}

// An integer is written only as one: digits, with no fraction, exponent,
// sign or leading zero. 1.0, 1e0 and 01 are not 1, and a line that spells a
// sequence or an at so is unreadable, as a revocation that spells its from so
// is refused.
func TestAnIntegerIsSpelledOneWay(t *testing.T) {
	first, second := signerOf(t, vectorSeed1), signerOf(t, vectorSeed2)
	lines := memoryTrail(1)
	signed, err := first.SignRecordLine(lines[0])
	if err != nil {
		t.Fatal(err)
	}
	record := bytes.TrimSuffix(signed, []byte("\n"))
	rotation := bytes.TrimSuffix(first.RotationLine(vectorTrail, 1, second.public), []byte("\n"))
	if !parseSidecarLine(record).readable || !parseSidecarLine(rotation).readable {
		t.Fatal("the writer's lines are readable")
	}
	for _, spelling := range []string{"1.0", "1e0", "1E0", "10e-1", "0.1e1", "01", "-1", `"1"`, "1.", "+1"} {
		for _, line := range [][]byte{
			bytes.Replace(record, []byte(`"sequence":1`), []byte(`"sequence":`+spelling), 1),
			bytes.Replace(rotation, []byte(`"at":1`), []byte(`"at":`+spelling), 1),
		} {
			if parseSidecarLine(line).readable {
				t.Fatalf("%s is read as an integer: %s", spelling, line)
			}
		}
		if _, err := ParseRevocations([]byte(`{"from":` + spelling + `,"publicKey":"` + first.PublicKey() + `"}`)); err == nil {
			t.Fatalf("a revocation from %s is read", spelling)
		}
	}
}

// A string, and a member's name, is what JSON decodes it to: an escape is
// the character it spells, so a line written with escapes reads and verifies
// as the line without them, and a name given twice, however spelled, makes
// the line unreadable.
func TestEscapesAreReadAsTheCharactersTheySpell(t *testing.T) {
	signer := signerOf(t, vectorSeed1)
	lines := memoryTrail(1)
	signed, err := signer.SignRecordLine(lines[0])
	if err != nil {
		t.Fatal(err)
	}
	line := bytes.TrimSuffix(signed, []byte("\n"))
	escaped := line
	for _, edit := range [][2]string{
		{`"kind":"record-signature"`, `"k\u0069nd":"record\u002dsignature"`},
		{`"keyId":"21`, `"keyId":"\u00321`},
		{`"record":"sha256:`, `"record":"sha256\u003a`},
		{`"sidecarVersion":"1"`, `"sidecarVersion":"\u0031"`},
		{`"trail":"0011`, `"trail":"\u0030011`},
	} {
		edited := bytes.Replace(escaped, []byte(edit[0]), []byte(edit[1]), 1)
		if bytes.Equal(edited, escaped) {
			t.Fatalf("%s: the edit did not apply", edit[0])
		}
		escaped = edited
	}
	if string(escaped) == string(line) || !parseSidecarLine(escaped).readable {
		t.Fatalf("an escaped line is readable: %s", escaped)
	}
	chain := verifySigned(t, joinLines(lines), joinLines([][]byte{escaped}), SignatureOptions{Keys: keys(signer)})
	if chain.Coverage.SignedRecords != 1 || len(chain.Findings) != 0 {
		t.Fatalf("an escaped line verifies: %v %+v", findingNames(chain), chain.Coverage)
	}
	twice := bytes.Replace(line, []byte(`"keyId"`), []byte(`"k\u0069nd":"record-signature","keyId"`), 1)
	upper := bytes.Replace(line, []byte(`"keyId":"21fe`), []byte(`"keyId":"21\u0046e`), 1)
	for name, edited := range map[string][]byte{"a name twice": twice, "an escape spelling upper case": upper} {
		if bytes.Equal(edited, line) || parseSidecarLine(edited).readable {
			t.Fatalf("%s is unreadable: %s", name, edited)
		}
	}
}
