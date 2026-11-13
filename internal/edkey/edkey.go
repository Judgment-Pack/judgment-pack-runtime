// Package edkey holds an Ed25519 public key a verifier is given to more than
// its length (#216). It reaches no file, environment or network, so the
// signature checks of the audit trail (internal/audit) and of a pack
// (internal/packsig, and through it the public Go API) apply the one rule.
//
// crypto/ed25519 verifies under any 32 bytes that decode to a point, and it
// decodes them leniently: a y of p or more is read as y minus p, and an x of 0
// with its sign bit set as x = 0, so the key it uses is not the one written.
// Under a point whose order divides 8, the curve's cofactor, it accepts a
// signature anyone can make without a private key: R a point of small order
// and s zero verify for most messages. The all-zero key, a likely placeholder,
// is such a point. A verifier that trusted one would count as signed bytes
// anyone wrote, so every public key this runtime reads is held to Check. A
// public key a seed derives is the base point times a nonzero scalar below its
// order, which is never refused.
package edkey

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/big"
)

// The refusals of a public key. Each reads after "is not one: ".
var (
	// ErrForm is a public key that is not 64 hexadecimal characters, 32
	// bytes.
	ErrForm = errors.New("a public key is 64 hexadecimal characters")
	// ErrNotCanonical is an encoding a verifier reads as another: its y is p
	// or more, or its x is 0 and its sign bit is set.
	ErrNotCanonical = errors.New("it is not the canonical encoding of an Ed25519 point: its y is 2^255-19 or more, or its x is 0 and its sign bit is set")
	// ErrSmallOrder is a point whose order divides 8.
	ErrSmallOrder = errors.New("it is an Ed25519 point of small order, under which anyone can make a signature without a private key")
	// ErrNotOnCurve is 32 bytes that encode no point of the curve, under
	// which nothing verifies.
	ErrNotOnCurve = errors.New("it is not a point of the Ed25519 curve, so nothing verifies under it")
)

// The field and the curve of Ed25519 (RFC 8032 §5.1): -x² + y² = 1 + d·x²·y²
// over the integers modulo p = 2^255 - 19, with d = -121665/121666.
var (
	fieldPrime = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(19))
	curveD     = func() *big.Int {
		d := new(big.Int).ModInverse(big.NewInt(121666), fieldPrime)
		d.Mul(d, big.NewInt(-121665))
		return d.Mod(d, fieldPrime)
	}()
)

// smallOrderPoints are the canonical encodings of the eight points of the
// curve whose order divides 8: the identity, the point of order 2, the two of
// order 4 and the four of order 8. Every other encoding of these points is not
// canonical, and refused as that first, so these eight are all there is to
// compare.
var smallOrderPoints = func() [][]byte {
	points := [][]byte{}
	for _, encoded := range []string{
		"0100000000000000000000000000000000000000000000000000000000000000",
		"ecffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff7f",
		"0000000000000000000000000000000000000000000000000000000000000000",
		"0000000000000000000000000000000000000000000000000000000000000080",
		"26e8958fc2b227b045c3f489f2ef98f0d5dfac05d3c63339b13802886d53fc05",
		"26e8958fc2b227b045c3f489f2ef98f0d5dfac05d3c63339b13802886d53fc85",
		"c7176a703d4dd84fba3c0b760d10670f2a2053fa2c39ccc64ec7fd7792ac037a",
		"c7176a703d4dd84fba3c0b760d10670f2a2053fa2c39ccc64ec7fd7792ac03fa",
	} {
		point, _ := hex.DecodeString(encoded)
		points = append(points, point)
	}
	return points
}()

// Check says why 32 bytes are no public key a verifier may trust, or nil:
// they must be the canonical encoding (RFC 8032 §5.1.2) of a point of the
// curve whose order does not divide 8. The encoding is read as written,
// before anything reduces it: y is its low 255 bits, little-endian, and its
// top bit is the sign of x.
func Check(public []byte) error {
	if len(public) != ed25519.PublicKeySize {
		return ErrForm
	}
	bigEndian := make([]byte, len(public))
	for index, b := range public {
		bigEndian[len(public)-1-index] = b
	}
	sign := bigEndian[0] >> 7
	bigEndian[0] &= 0x7f
	y := new(big.Int).SetBytes(bigEndian)
	if y.Cmp(fieldPrime) >= 0 {
		return ErrNotCanonical
	}
	// x² = (y² - 1) / (d·y² + 1), so x is 0 exactly when y² is 1, and then
	// the sign bit must be clear. The divisor is never 0: d is no square.
	ySquared := new(big.Int).Mul(y, y)
	numerator := new(big.Int).Sub(ySquared, big.NewInt(1))
	numerator.Mod(numerator, fieldPrime)
	if numerator.Sign() == 0 && sign == 1 {
		return ErrNotCanonical
	}
	for _, point := range smallOrderPoints {
		if bytes.Equal(public, point) {
			return ErrSmallOrder
		}
	}
	divisor := ySquared.Mul(ySquared, curveD)
	divisor.Add(divisor, big.NewInt(1)).Mod(divisor, fieldPrime)
	xSquared := new(big.Int).ModInverse(divisor, fieldPrime)
	xSquared.Mul(xSquared, numerator).Mod(xSquared, fieldPrime)
	if big.Jacobi(xSquared, fieldPrime) < 0 {
		return ErrNotOnCurve
	}
	return nil
}

// ID is a public key's keyId in the gateway's form: the first 32 lowercase
// hexadecimal characters of the SHA-256 of its 32 raw bytes.
func ID(public []byte) string {
	sum := sha256.Sum256(public)
	return hex.EncodeToString(sum[:])[:32]
}
