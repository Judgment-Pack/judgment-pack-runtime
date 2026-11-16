package jcs

import (
	"encoding/json"
	"math"
	"testing"
)

// The number vectors are RFC 8785 Appendix B's, by IEEE 754 bit pattern.
func TestFormatNumberRFC8785AppendixB(t *testing.T) {
	cases := []struct {
		bits uint64
		want string
	}{
		{0x0000000000000000, "0"},
		{0x8000000000000000, "0"},
		{0x0000000000000001, "5e-324"},
		{0x8000000000000001, "-5e-324"},
		{0x7fefffffffffffff, "1.7976931348623157e+308"},
		{0xffefffffffffffff, "-1.7976931348623157e+308"},
		{0x4340000000000000, "9007199254740992"},
		{0xc340000000000000, "-9007199254740992"},
		{0x4430000000000000, "295147905179352830000"},
		{0x44b52d02c7e14af5, "9.999999999999997e+22"},
		{0x44b52d02c7e14af6, "1e+23"},
		{0x44b52d02c7e14af7, "1.0000000000000001e+23"},
		{0x444b1ae4d6e2ef4e, "999999999999999700000"},
		{0x444b1ae4d6e2ef4f, "999999999999999900000"},
		{0x444b1ae4d6e2ef50, "1e+21"},
		{0x3eb0c6f7a0b5ed8c, "9.999999999999997e-7"},
		{0x3eb0c6f7a0b5ed8d, "0.000001"},
		{0x41b3de4355555553, "333333333.3333332"},
		{0x41b3de4355555554, "333333333.33333325"},
		{0x41b3de4355555555, "333333333.3333333"},
		{0x41b3de4355555556, "333333333.3333334"},
		{0x41b3de4355555557, "333333333.33333343"},
		{0xbecbf647612f3696, "-0.0000033333333333333333"},
		{0x43143ff3c1cb0959, "1424953923781206.2"},
	}
	for _, c := range cases {
		got, err := FormatNumber(math.Float64frombits(c.bits))
		if err != nil || got != c.want {
			t.Errorf("%016x: got %q, %v; want %q", c.bits, got, err, c.want)
		}
	}
	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := FormatNumber(bad); err == nil {
			t.Errorf("%v: want a refusal", bad)
		}
	}
}

func TestEncodeDocumentOrdersAndAdmitsNumbersAndNull(t *testing.T) {
	value := map[string]any{
		"b":          []any{json.Number("1.50"), nil, true, "x"},
		"a":          json.Number("-0"),
		"\U0001F600": "after",
		"�":          "before",
	}
	got, err := EncodeDocument(value)
	if err != nil {
		t.Fatal(err)
	}
	// U+FFFD sorts after the surrogate pair of U+1F600 in UTF-16 code units.
	want := `{"a":0,"b":[1.5,null,true,"x"],"😀":"after","�":"before"}`
	if string(got) != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
	if _, err := EncodeDocument(json.Number("1e400")); err == nil {
		t.Fatal("an out-of-range number must be refused")
	}
	if _, err := Encode(nil); err == nil {
		t.Fatal("Encode must keep refusing null")
	}
}
