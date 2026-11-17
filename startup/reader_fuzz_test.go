package startup

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// fuzzVariable is the variable FuzzReadSecret binds its reference to.
const fuzzVariable = "JPACK_STARTUP_FUZZ_SECRET"

// FuzzReadSecret holds the reader's handling of a secret reference and of
// the value delivered for it on any input. A crasher a scheduled run finds
// is checked in under testdata/fuzz/FuzzReadSecret/ by the pull request that
// fixes it.
//
// The reference is a key, never parsed (Bindings): it is read only when it
// is byte for byte the bound one, any other is ErrNoBinding itself, which
// carries no reference, and one bound to a variable and to a file is
// ErrTwoBindings before any file is looked at. The value: admitValue never
// panics, reads one value the same twice and leaves it as it was, trims
// exactly one trailing "\n" or "\r\n" and nothing else, and refuses what is
// then empty (ErrEmpty) or over MaxSecretBytes (ErrLimit). ReadEnv applies
// the same rules to the variable.
func FuzzReadSecret(f *testing.F) {
	const bound = "secret:gcp-sm:projects/example-dev/secrets/env-bound/versions/1"
	for _, c := range newlineCases {
		f.Add(bound, []byte(c.raw))
	}
	for _, reference := range []string{
		"secret:gcp-sm:projects/example-dev/secrets/unbound/versions/1", bound + " ", " " + bound,
		"SECRET:GCP-SM:PROJECTS/EXAMPLE-DEV/SECRETS/ENV-BOUND/VERSIONS/1", "", "secret:gcp-sm:projects/example-dev/secrets/env-bound/versions/01",
	} {
		f.Add(reference, []byte("from-the-environment\n"))
	}
	for _, raw := range []string{"", "\n", "\r\n", "\r", "\x00\n", "v\r\r\n"} {
		f.Add(bound, []byte(raw))
	}
	at := strings.Repeat("v", MaxSecretBytes)
	for _, raw := range []string{at, at + "\n", at + "\r\n", at + "v", at + "\n\n", at + "\r\r\n"} {
		f.Add(bound, []byte(raw))
	}
	f.Fuzz(func(t *testing.T, reference string, raw []byte) {
		if len(raw) > MaxSecretBytes+4 {
			return
		}
		held := bytes.Clone(raw)
		secret, err := admitValue(raw)
		again, errAgain := admitValue(raw)
		if !bytes.Equal(raw, held) {
			t.Fatal("admitValue changed the bytes it read")
		}
		if err != errAgain || !bytes.Equal(secret.Reveal(), again.Reveal()) {
			t.Fatal("two reads of one value differ")
		}
		want := raw
		if trimmed, ok := bytes.CutSuffix(raw, []byte("\r\n")); ok {
			want = trimmed
		} else if trimmed, ok := bytes.CutSuffix(raw, []byte("\n")); ok {
			want = trimmed
		}
		switch {
		case len(want) == 0:
			if err != ErrEmpty || !secret.IsZero() {
				t.Fatalf("%q: got %v, want ErrEmpty", raw, err)
			}
		case len(want) > MaxSecretBytes:
			if err != ErrLimit || !secret.IsZero() {
				t.Fatalf("%d bytes: got %v, want ErrLimit", len(raw), err)
			}
		default:
			if err != nil || !bytes.Equal(secret.Reveal(), want) {
				t.Fatalf("%q: got %q (%v), want %q", raw, secret.Reveal(), err, want)
			}
		}

		ctx := context.Background()
		// Both bindings name a path that is never looked at.
		both := Bindings{Env: map[string]string{bound: fuzzVariable}, Files: map[string]string{bound: "never-read"}}
		if _, err := both.ReadSecret(ctx, reference); reference == bound && err != ErrTwoBindings || reference != bound && err != ErrNoBinding {
			t.Fatalf("%q, bound to both: got %v", reference, err)
		}
		// A variable holds no NUL byte, and outside Unix none past 32,767
		// characters.
		if !onUnix || bytes.IndexByte(raw, 0) >= 0 {
			return
		}
		t.Setenv(fuzzVariable, string(raw))
		fromEnv := Bindings{Env: map[string]string{bound: fuzzVariable}}
		got, err := fromEnv.ReadSecret(ctx, reference)
		if reference != bound {
			if err != ErrNoBinding || !got.IsZero() {
				t.Fatalf("%q: got %v, want ErrNoBinding", reference, err)
			}
			return
		}
		direct, directErr := ReadEnv(fuzzVariable)
		if !bytes.Equal(got.Reveal(), direct.Reveal()) || (err == nil) != (directErr == nil) ||
			err != nil && err.Error() != directErr.Error() {
			t.Fatalf("%q: through the binding %q (%v), directly %q (%v)", raw, got.Reveal(), err, direct.Reveal(), directErr)
		}
		if !bytes.Equal(got.Reveal(), secret.Reveal()) || (err == nil) != (errAgain == nil) || err != nil && !errors.Is(err, errAgain) {
			t.Fatalf("%q: ReadEnv %q (%v), admitValue %q (%v)", raw, got.Reveal(), err, secret.Reveal(), errAgain)
		}
	})
}
