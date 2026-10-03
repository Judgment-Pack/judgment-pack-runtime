package audit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// The grammar the guide's "Record signatures, exactly" states, held to the
// parsers rule by rule (#218): for each stated rule, the inputs just inside
// it, just outside it, and spelled oddly, with the outcome the guide gives.
// A sentence of the grammar the parsers do not bear out fails here.
func TestTheGrammarTheGuideStatesIsTheParsers(t *testing.T) {
	const (
		hex32 = "00112233445566778899aabbccddeeff"
		max   = "9007199254740990" // 2^53-2
		over  = "9007199254740991" // 2^53-1
		bound = "134217728"        // 128 MiB
	)
	hex64 := strings.Repeat("ab", 32)
	esc := string(rune(92)) + "u00"

	// The integers: digits alone, no sign, fraction, exponent or leading zero.
	integer := func(text string) string {
		var n int64
		if decodeInteger(json.RawMessage(text), &n) != nil {
			return "refused"
		}
		return strconv.FormatInt(n, 10)
	}
	// A trail line: chained or unchained, by its members.
	chained := func(line string) string {
		if _, _, ok := chainedLine([]byte(line)); ok {
			return "chained"
		}
		return "unchained"
	}
	record := func(trail, sequence, previous string) string {
		return `{"recordVersion":"1","trail":` + trail + `,"sequence":` + sequence + `,"previous":` + previous + `,"kind":"evaluation"}`
	}
	goodTrail, goodPrevious := `"`+hex32+`"`, `"sha256:`+hex64+`"`
	// A discontinuity member, as the parser reads it.
	shape := func(inner string) string {
		parsed, isDiscontinuity := discontinuityOf(map[string]json.RawMessage{"kind": json.RawMessage(`"discontinuity"`), "discontinuity": json.RawMessage(inner)})
		switch {
		case !isDiscontinuity:
			return "not a discontinuity"
		case parsed.valid:
			return "well formed"
		}
		return "malformed"
	}
	inner := func(reason, line, bytes, digest string) string {
		return `{"reason":` + reason + `,"line":` + line + `,"bytes":` + bytes + `,"digest":` + digest + `}`
	}
	goodReason, goodDigest := `"incomplete-last-line"`, `"sha256:`+hex64+`"`
	// A line a discontinuity names: excused when the discontinuity after it
	// is well formed, not excused when that discontinuity is malformed.
	named := func(predecessor string, member func(line []byte) string) string {
		line := []byte(predecessor)
		second := []byte(`{"recordVersion":"1","trail":"` + vectorTrail + `","sequence":2,"previous":"` + Digest(nil) + `","kind":"discontinuity","discontinuity":` + member(line) + `}`)
		chain := verifyBytes(t, joinLines([][]byte{line, second, chainedAt(3, Digest(second))}), nil)
		for _, name := range findingNames(chain) {
			if name == "discontinuity-malformed@2" {
				return "not excused"
			}
		}
		if chain.Coverage.Damaged == 1 {
			return "excused"
		}
		return fmt.Sprintf("neither: %v", findingNames(chain))
	}
	naming := func(line []byte) string {
		return inner(goodReason, "1", strconv.Itoa(len(line)), `"`+Digest(line)+`"`)
	}
	namingWith := func(lineNumber, length string) func([]byte) string {
		return func(line []byte) string { return inner(goodReason, lineNumber, length, `"`+Digest(line)+`"`) }
	}
	plain := string(chainedAt(1, Digest(nil)))
	// A sidecar line, readable or not.
	signer := signerOf(t, vectorSeed1)
	signedLine, err := signer.SignRecordLine(chainedAt(1, Digest(nil)))
	if err != nil {
		t.Fatal(err)
	}
	sidecarLine := string(bytes.TrimSuffix(signedLine, []byte("\n")))
	rotationLine := string(bytes.TrimSuffix(signer.RotationLine(vectorTrail, 1, signerOf(t, vectorSeed2).public), []byte("\n")))
	sidecar := func(line string) string {
		if parseSidecarLine([]byte(line)).readable {
			return "readable"
		}
		return "unreadable"
	}
	edit := func(line, old, new string) string {
		if !strings.Contains(line, old) {
			t.Fatalf("the edit %q does not apply to %s", old, line)
		}
		return strings.Replace(line, old, new, 1)
	}
	// A trail line against the bound, lowered, with and without its newline.
	padded := func(length int) []byte {
		first := chainedAt(1, Digest(nil))
		return append(append([]byte("{"), bytes.Repeat([]byte(" "), length-len(first))...), first[1:]...)
	}
	trailLine := func(data []byte) string {
		chain := verifyBytes(t, data, nil)
		if len(chain.Findings) > 0 {
			return strings.Join(findingNames(chain), " ")
		}
		return fmt.Sprintf("read, %d chained", chain.Coverage.Chained)
	}

	rows := []struct {
		rule, input string
		got         func() string
		want        string
	}{
		// Integers.
		{"an integer is digits", "1", func() string { return integer("1") }, "1"},
		{"an integer is digits", "0", func() string { return integer("0") }, "0"},
		{"an integer is digits", "2^53-2", func() string { return integer(max) }, max},
		{"no sign", "-0", func() string { return integer("-0") }, "refused"},
		{"no sign", "-1", func() string { return integer("-1") }, "refused"},
		{"no sign", "+1", func() string { return integer("+1") }, "refused"},
		{"no leading zero", "01", func() string { return integer("01") }, "refused"},
		{"no leading zero", "00", func() string { return integer("00") }, "refused"},
		{"no fraction", "1.0", func() string { return integer("1.0") }, "refused"},
		{"no exponent", "1e0", func() string { return integer("1e0") }, "refused"},
		{"no exponent", "1E0", func() string { return integer("1E0") }, "refused"},
		{"a number, not a string", `"1"`, func() string { return integer(`"1"`) }, "refused"},
		{"within 64 bits", "2^64", func() string { return integer("18446744073709551616") }, "refused"},

		// A chained record's members.
		{"sequence from 1", "1", func() string { return chained(record(goodTrail, "1", goodPrevious)) }, "chained"},
		{"sequence from 1", "0", func() string { return chained(record(goodTrail, "0", goodPrevious)) }, "unchained"},
		{"sequence to 2^53-2", "2^53-2", func() string { return chained(record(goodTrail, max, goodPrevious)) }, "chained"},
		{"sequence to 2^53-2", "2^53-1", func() string { return chained(record(goodTrail, over, goodPrevious)) }, "unchained"},
		{"sequence written as an integer", "-0", func() string { return chained(record(goodTrail, "-0", goodPrevious)) }, "unchained"},
		{"sequence written as an integer", `"1"`, func() string { return chained(record(goodTrail, `"1"`, goodPrevious)) }, "unchained"},
		{"trail of 32 lowercase hex", "32", func() string { return chained(record(goodTrail, "1", goodPrevious)) }, "chained"},
		{"trail of 32 lowercase hex", "31", func() string { return chained(record(`"`+hex32[:31]+`"`, "1", goodPrevious)) }, "unchained"},
		{"trail of 32 lowercase hex", "33", func() string { return chained(record(`"`+hex32+`0"`, "1", goodPrevious)) }, "unchained"},
		{"trail of 32 lowercase hex", "upper case", func() string { return chained(record(`"`+strings.ToUpper(hex32[:31])+`F"`, "1", goodPrevious)) }, "unchained"},
		{"strings by their decoded values", "an escaped hex digit", func() string { return chained(record(`"`+esc+`30`+hex32[1:]+`"`, "1", goodPrevious)) }, "chained"},
		{"previous of sha256: and 64 lowercase hex", "sha512:", func() string { return chained(record(goodTrail, "1", `"sha512:`+hex64+`"`)) }, "unchained"},
		{"previous of sha256: and 64 lowercase hex", "63", func() string { return chained(record(goodTrail, "1", `"sha256:`+hex64[:63]+`"`)) }, "unchained"},
		{"previous of sha256: and 64 lowercase hex", "upper case", func() string { return chained(record(goodTrail, "1", `"sha256:`+strings.ToUpper(hex64)+`"`)) }, "unchained"},
		{"no member given twice", "an unrelated member twice", func() string {
			return chained(strings.Replace(record(goodTrail, "1", goodPrevious), `"kind"`, `"run":"a","run":"b","kind"`, 1))
		}, "unchained"},
		{"no member given twice", "a name twice by an escape", func() string {
			return chained(strings.Replace(record(goodTrail, "1", goodPrevious), `"kind"`, `"tr`+esc+`61il":"`+hex32+`","kind"`, 1))
		}, "unchained"},
		{"names by their decoded values", "an escaped name", func() string {
			return chained(strings.Replace(record(goodTrail, "1", goodPrevious), `"trail"`, `"tr`+esc+`61il"`, 1))
		}, "chained"},
		{"one JSON object", "an array", func() string { return chained(`[` + record(goodTrail, "1", goodPrevious) + `]`) }, "unchained"},
		{"one JSON object", "a second value", func() string { return chained(record(goodTrail, "1", goodPrevious) + ` {}`) }, "unchained"},

		// A discontinuity member.
		{"the four members", "well formed", func() string { return shape(inner(goodReason, "1", "0", goodDigest)) }, "well formed"},
		{"exactly four members", "a fifth", func() string {
			return shape(`{"reason":"incomplete-last-line","line":1,"bytes":0,"digest":` + goodDigest + `,"x":1}`)
		}, "malformed"},
		{"exactly four members", "three", func() string { return shape(`{"reason":"incomplete-last-line","line":1,"bytes":0}`) }, "malformed"},
		{"none given twice", "line twice", func() string {
			return shape(`{"reason":"incomplete-last-line","line":1,"line":1,"bytes":0,"digest":` + goodDigest + `}`)
		}, "malformed"},
		{"one JSON object", "an array", func() string { return shape(`[` + inner(goodReason, "1", "0", goodDigest) + `]`) }, "malformed"},
		{"reason incomplete-last-line", "another", func() string { return shape(inner(`"repaired"`, "1", "0", goodDigest)) }, "malformed"},
		{"reason by its decoded value", "escaped", func() string { return shape(inner(`"incomplete`+esc+`2dlast-line"`, "1", "0", goodDigest)) }, "well formed"},
		{"line from 1", "1", func() string { return shape(inner(goodReason, "1", "0", goodDigest)) }, "well formed"},
		{"line from 1", "0", func() string { return shape(inner(goodReason, "0", "0", goodDigest)) }, "malformed"},
		{"line to 2^53-2", "2^53-2", func() string { return shape(inner(goodReason, max, "0", goodDigest)) }, "well formed"},
		{"line to 2^53-2", "2^53-1", func() string { return shape(inner(goodReason, over, "0", goodDigest)) }, "malformed"},
		{"line written as an integer", "-0", func() string { return shape(inner(goodReason, "-0", "0", goodDigest)) }, "malformed"},
		{"bytes from 0", "0", func() string { return shape(inner(goodReason, "1", "0", goodDigest)) }, "well formed"},
		{"bytes written as an integer", "-0", func() string { return shape(inner(goodReason, "1", "-0", goodDigest)) }, "malformed"},
		{"bytes written as an integer", "1.0", func() string { return shape(inner(goodReason, "1", "1.0", goodDigest)) }, "malformed"},
		{"digest of sha256: and 64 lowercase hex", "upper case", func() string { return shape(inner(goodReason, "1", "0", `"sha256:`+strings.ToUpper(hex64)+`"`)) }, "malformed"},
		{"digest of sha256: and 64 lowercase hex", "63", func() string { return shape(inner(goodReason, "1", "0", `"sha256:`+hex64[:63]+`"`)) }, "malformed"},

		// The line a discontinuity names.
		{"line is L-1", "the line before", func() string { return named(plain, naming) }, "excused"},
		{"line is L-1", "2^53-2", func() string { return named(plain, namingWith(max, strconv.Itoa(len(plain)))) }, "not excused"},
		{"bytes to 128 MiB", "128 MiB", func() string { return named(plain, namingWith("1", bound)) }, "excused"},
		{"bytes to 128 MiB", "128 MiB + 1", func() string { return named(plain, namingWith("1", "134217729")) }, "not excused"},
		{"bytes written as an integer", "-0, of an empty line", func() string { return named("", namingWith("1", "-0")) }, "not excused"},
		{"not itself a discontinuity", "a chained one", func() string {
			return named(strings.Replace(plain, `"kind":"evaluation"`, `"kind":"discontinuity"`, 1), naming)
		}, "not excused"},
		{"not itself a discontinuity", "an unchained one", func() string { return named(`{"kind":"discontinuity"}`, naming) }, "not excused"},
		{"not itself a discontinuity", "its kind escaped", func() string { return named(`{"k`+esc+`69nd":"discontinuity"}`, naming) }, "not excused"},
		{"one JSON object with no member given twice", "an unrelated member twice", func() string { return named(`{"kind":"discontinuity","x":1,"x":2}`, naming) }, "excused"},
		{"one JSON object with no member given twice", "kind twice by an escape", func() string {
			return named(`{"kind":"discontinuity","k`+esc+`69nd":"discontinuity"}`, naming)
		}, "excused"},
		{"whatever its kind", "another kind", func() string { return named(`{"kind":"evaluation"}`, naming) }, "excused"},

		// A sidecar line.
		{"seven members of their forms", "the writer's", func() string { return sidecar(sidecarLine) }, "readable"},
		{"seven members of their forms", "a rotation", func() string { return sidecar(rotationLine) }, "readable"},
		{"sequence written as an integer", "-0", func() string { return sidecar(edit(sidecarLine, `"sequence":1`, `"sequence":-0`)) }, "unreadable"},
		{"sequence to 2^53-2", "2^53-1", func() string { return sidecar(edit(sidecarLine, `"sequence":1`, `"sequence":`+over)) }, "unreadable"},
		{"at written as an integer", "-0", func() string { return sidecar(edit(rotationLine, `"at":1`, `"at":-0`)) }, "unreadable"},
		{"keyId of 32 lowercase hex", "31", func() string { return sidecar(edit(sidecarLine, `"keyId":"2`, `"keyId":"`)) }, "unreadable"},
		{"signature of 128 lowercase hex", "upper case", func() string { return sidecar(edit(sidecarLine, `"signature":"b`, `"signature":"B`)) }, "unreadable"},
		{"sidecarVersion 1", "2", func() string { return sidecar(edit(sidecarLine, `"sidecarVersion":"1"`, `"sidecarVersion":"2"`)) }, "unreadable"},
		{"exactly seven members", "an eighth", func() string { return sidecar(edit(sidecarLine, `"trail":`, `"x":1,"trail":`)) }, "unreadable"},
		{"each once", "kind twice by an escape", func() string {
			return sidecar(edit(sidecarLine, `"keyId"`, `"k`+esc+`69nd":"record-signature","keyId"`))
		}, "unreadable"},
		{"by decoded values", "an escaped kind", func() string {
			return sidecar(edit(sidecarLine, `"kind":"record-signature"`, `"kind":"record`+esc+`2dsignature"`))
		}, "readable"},
		{"a sidecar line to 4096 bytes", "4096", func() string { return sidecar("{" + strings.Repeat(" ", 4096-len(sidecarLine)) + sidecarLine[1:]) }, "readable"},
		{"a sidecar line to 4096 bytes", "4097", func() string { return sidecar("{" + strings.Repeat(" ", 4097-len(sidecarLine)) + sidecarLine[1:]) }, "unreadable"},
	}
	for _, row := range rows {
		if got := row.got(); got != row.want {
			t.Errorf("%s, %s: the parser gives %q, the guide %q", row.rule, row.input, got, row.want)
		}
	}

	// The bound and the newline, the bound lowered.
	lowerLineBound(t, 300)
	for _, row := range []struct {
		input string
		data  []byte
		want  string
	}{
		{"exactly the bound and its newline", joinLines([][]byte{padded(300)}), "read, 1 chained"},
		{"one byte more and its newline", joinLines([][]byte{padded(301)}), "line-too-long@1"},
		{"one byte more and no newline", padded(301), "incomplete-last-line@1"},
		{"a chained record and no newline", padded(200), "incomplete-last-line@1"},
	} {
		if got := trailLine(row.data); got != row.want {
			t.Errorf("a line is the bytes before a newline, its bound not counting it: %s gives %q, the guide %q", row.input, got, row.want)
		}
	}
}
