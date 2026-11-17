package terraformfile

import (
	"bytes"
	"testing"
	"unicode/utf8"
)

// compareUTF16Decoding is compareUTF16 as it was before it skipped the shared
// bytes: every code point of both names decoded from the first.
func compareUTF16Decoding(a, b []byte) int {
	for len(a) > 0 && len(b) > 0 {
		ra, na := utf8.DecodeRune(a)
		rb, nb := utf8.DecodeRune(b)
		if ra != rb {
			ua, la := utf16Units(ra)
			ub, lb := utf16Units(rb)
			if ua != ub {
				return int(ua) - int(ub)
			}
			return int(la) - int(lb)
		}
		a, b = a[na:], b[nb:]
	}
	return len(a) - len(b)
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

// TestCompareUTF16SkipsSharedBytesWithoutChangingTheOrder holds the shared-
// prefix skip to the order of decoding both names whole, over every pair of
// names of up to three bytes drawn from lead, continuation and ASCII bytes,
// each behind a shared prefix that ends inside a code point and one that does
// not. Malformed names are among them: the order is the decoder's either way.
func TestCompareUTF16SkipsSharedBytesWithoutChangingTheOrder(t *testing.T) {
	alphabet := []byte{'a', 0xc3, 0xa9, 0x80, 0xe2, 0x82, 0xf0, 0x9f, 0xff}
	var names [][]byte
	var grow func(prefix []byte)
	grow = func(prefix []byte) {
		names = append(names, prefix)
		if len(prefix) == 3 {
			return
		}
		for _, c := range alphabet {
			grow(append(bytes.Clone(prefix), c))
		}
	}
	grow(nil)
	for _, shared := range [][]byte{nil, []byte("key-"), []byte("é"), {0xf0, 0x9f, 0x98}, {0xe2, 0x82}} {
		for _, a := range names {
			for _, b := range names {
				x, y := append(bytes.Clone(shared), a...), append(bytes.Clone(shared), b...)
				if got, want := sign(compareUTF16(x, y)), sign(compareUTF16Decoding(x, y)); got != want {
					t.Fatalf("% x vs % x: %d, want %d", x, y, got, want)
				}
			}
		}
	}
	// A supplementary code point after a long shared prefix still sorts by
	// its UTF-16 units: U+FFFF before U+10000 in code points, after it in
	// UTF-16.
	prefix := bytes.Repeat([]byte("é"), 4096)
	if compareUTF16(append(bytes.Clone(prefix), "￿"...), append(bytes.Clone(prefix), "\U00010000"...)) <= 0 {
		t.Error("U+FFFF sorted before U+10000 behind a shared prefix")
	}
}
