package startup

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// overBound is a document one byte over MaxResolvedBytes, all spaces past
// the prefix FuzzReadResolved copies in, made once and reused.
var overBound []byte

// FuzzReadResolved holds ReadResolved on any input: it never panics, reads
// one input the same twice, refuses only with one of its sentinels
// (ErrSelector, ErrEnvironment, ErrLimit, ErrDocument), and accepts only a
// document whose resolvedFormatVersion is exactly 1.0.0, resolved for the
// environment named. Any input made one byte over MaxResolvedBytes is
// ErrLimit. A crasher a scheduled run finds is checked in under
// testdata/fuzz/FuzzReadResolved/ by the pull request that fixes it.
func FuzzReadResolved(f *testing.F) {
	replace := func(old, replacement string) string {
		if strings.Count(prodDocument, old) != 1 {
			f.Fatalf("the document holds %q %d times, want once", old, strings.Count(prodDocument, old))
		}
		return strings.Replace(prodDocument, old, replacement, 1)
	}
	for _, environment := range []string{"prod", "dev", "", "PROD", "prod "} {
		f.Add([]byte(prodDocument), environment)
	}
	for _, document := range []string{
		" {\n\t\"resolvedFormatVersion\" : \"1.0.0\", \"registry\": {\"id\": \"r\", \"version\": \"1.0.0\", \"digest\": \"d\"},\r\n" +
			"\"environment\": \"dev\", \"kinds\": [ ], \"entries\": [ ] } \n",
		replace(`"resolvedFormatVersion":"1.0.0"`, `"resolvedFormatVersion":"1.0.1"`),
		replace(`"resolvedFormatVersion":"1.0.0"`, `"resolvedFormatVersion":1`),
		replace(`,"resolvedFormatVersion":"1.0.0"`, ``),
		replace(`"resolvedFormatVersion":"1.0.0"`, `"resolvedFormatVersion":"1.1.0","secretScopes":{}`),
		`[]`, `"1.0.0"`, `null`, `[{"resolvedFormatVersion":"1.0.0"}]`, ``, `{`, strings.Repeat("[", 100),
		replace(`"environment":"prod"`, `"environment":"dev","environment":"prod"`),
		replace(`{"engine":"postgres"}`, `{"a":[{"b":{"c":1,"c":2}}]}`),
		replace(`"environment":"prod"`, `"ENVIRONMENT":"prod"`),
		replace(`"postgres"`, "\"post\xffgres\""),
		replace(`"postgres"}`, `"postgres",}`),
		replace(`"environment":"prod"`, `"environment":"prod","extra":1`),
		replace(`"environment":"prod"`, `"environment":1`),
		replace(`"version":"4.0.0"}`, `"version":"4.0.0","x":""}`),
		replace(`"kinds":[`, `"kinds":["x",`),
		replace(`{"entries":[`, `{"entries":[[],`),
		replace(`,"value":{"engine":"postgres"}`, ``),
		replace(`{"id":"billing","kind":"database"`, `{"id":"zzz","kind":"database"`),
		replace(`"kind":"database","owner":"team-billing"`, `"kind":"zzz","owner":"team-billing"`),
		replace(`{"engine":"postgres"}`, strings.Repeat("[", MaxResolvedDepth-2)+strings.Repeat("]", MaxResolvedDepth-2)),
		replace(`{"engine":"postgres"}`, strings.Repeat("[", MaxResolvedDepth-1)+strings.Repeat("]", MaxResolvedDepth-1)),
	} {
		f.Add([]byte(document), "prod")
	}
	f.Fuzz(func(t *testing.T, data []byte, environment string) {
		resolved, err := ReadResolved(data, environment)
		again, errAgain := ReadResolved(data, environment)
		if !reflect.DeepEqual(resolved, again) || (err == nil) != (errAgain == nil) || err != nil && err.Error() != errAgain.Error() {
			t.Fatalf("two reads differ: %v, %v", err, errAgain)
		}
		if err != nil {
			if resolved != nil {
				t.Fatalf("refused (%v) with a document", err)
			}
			if !errors.Is(err, ErrSelector) && !errors.Is(err, ErrEnvironment) && !errors.Is(err, ErrLimit) && !errors.Is(err, ErrDocument) {
				t.Fatalf("a refusal with no sentinel: %v", err)
			}
		} else if resolved.ResolvedFormatVersion != "1.0.0" || environment == "" || resolved.Environment != environment {
			t.Fatalf("accepted %q for %q", resolved.ResolvedFormatVersion+" "+resolved.Environment, environment)
		}
		if len(data) > 1<<16 {
			return
		}
		if overBound == nil {
			overBound = bytes.Repeat([]byte(" "), MaxResolvedBytes+1)
		}
		copy(overBound, data)
		_, err = ReadResolved(overBound, environment)
		copy(overBound, bytes.Repeat([]byte(" "), len(data)))
		if !errors.Is(err, ErrLimit) {
			t.Fatalf("one byte over the bound: got %v, want ErrLimit", err)
		}
	})
}
