package edkey

import (
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

// The public key of RFC 8032's first test vector, and its identifier: the
// first 32 hexadecimal characters of its SHA-256.
const (
	vectorPublicKey = "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a"
	vectorKeyID     = "21fe31dfa154a261626bf854046fd227"
)

// Each encoding is written little-endian, as Ed25519 writes a point: y in the
// low 255 bits and the sign of x in the top bit.
func TestCheck(t *testing.T) {
	zeros := strings.Repeat("00", 31)
	cases := []struct {
		name, key string
		want      error
	}{
		{"the vector's key", vectorPublicKey, nil},
		{"another point of the curve, y = 3", "03" + zeros, nil},
		{"no bytes", "", ErrForm},
		{"31 bytes", vectorPublicKey[:62], ErrForm},
		{"33 bytes", vectorPublicKey + "00", ErrForm},
		{"y = 2^255-19, the field's prime", "ed" + strings.Repeat("ff", 30) + "7f", ErrNotCanonical},
		{"y = 2^255-1", strings.Repeat("ff", 31) + "7f", ErrNotCanonical},
		{"y = 1 with the sign bit set, so x = -0", "01" + strings.Repeat("00", 30) + "80", ErrNotCanonical},
		{"the identity, y = 1", "01" + zeros, ErrSmallOrder},
		{"y = 0, a point of order 4", "00" + zeros, ErrSmallOrder},
		{"y = -1, the point of order 2", "ec" + strings.Repeat("ff", 30) + "7f", ErrSmallOrder},
		{"y = 2, off the curve", "02" + zeros, ErrNotOnCurve},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			key, err := hex.DecodeString(c.key)
			if err != nil {
				t.Fatal(err)
			}
			if got := Check(key); !errors.Is(got, c.want) {
				t.Fatalf("Check = %v, want %v", got, c.want)
			}
		})
	}
}

// Every point of small order the list holds is a canonical encoding, and is
// refused as a point of small order.
func TestCheckRefusesEveryListedSmallOrderPoint(t *testing.T) {
	if len(smallOrderPoints) != 8 {
		t.Fatalf("%d points of small order are listed", len(smallOrderPoints))
	}
	for _, point := range smallOrderPoints {
		if err := Check(point); !errors.Is(err, ErrSmallOrder) {
			t.Fatalf("%x: %v", point, err)
		}
	}
}

func TestID(t *testing.T) {
	key, _ := hex.DecodeString(vectorPublicKey)
	if got := ID(key); got != vectorKeyID {
		t.Fatalf("ID = %s, want %s", got, vectorKeyID)
	}
}
