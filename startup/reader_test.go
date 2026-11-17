package startup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The newline rule both readers share: exactly one trailing "\n" or "\r\n"
// goes, and every other byte stays.
var newlineCases = []struct {
	name, raw, want string
}{
	{"no newline", "abc", "abc"},
	{"one line feed", "abc\n", "abc"},
	{"one carriage return and line feed", "abc\r\n", "abc"},
	{"two line feeds keep one", "abc\n\n", "abc\n"},
	{"two CRLFs keep one", "abc\r\n\r\n", "abc\r\n"},
	{"line feed then CRLF keeps the line feed", "abc\n\r\n", "abc\n"},
	{"CRLF then line feed keeps the CRLF", "abc\r\n\n", "abc\r\n"},
	{"a lone carriage return stays", "abc\r", "abc\r"},
	{"a carriage return before two line feeds stays", "abc\r\n\n\n", "abc\r\n\n"},
	{"inner newlines stay", "a\nb\r\nc\n", "a\nb\r\nc"},
	{"spaces and tabs stay", " \tabc \t\n", " \tabc \t"},
	{"a leading newline stays", "\nabc", "\nabc"},
	{"a value that is only newlines keeps all but one", "\n\n", "\n"},
}

const testVariable = "JPACK_STARTUP_TEST_SECRET"

func TestReadEnvTrimsExactlyOneTrailingNewline(t *testing.T) {
	for _, c := range newlineCases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(testVariable, c.raw)
			secret, err := ReadEnv(testVariable)
			if err != nil {
				t.Fatal(err)
			}
			if got := string(secret.Reveal()); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestReadEnvRefusals(t *testing.T) {
	if _, err := ReadEnv("JPACK_STARTUP_TEST_NEVER_SET"); !errors.Is(err, ErrNotSet) {
		t.Errorf("unset: got %v, want ErrNotSet", err)
	}
	for _, raw := range []string{"", "\n", "\r\n"} {
		t.Setenv(testVariable, raw)
		if _, err := ReadEnv(testVariable); !errors.Is(err, ErrEmpty) {
			t.Errorf("%q: got %v, want ErrEmpty", raw, err)
		}
	}
	t.Setenv(testVariable, "")
	_, err := ReadEnv(testVariable)
	if err == nil || !strings.Contains(err.Error(), testVariable) {
		t.Errorf("the refusal does not name the variable: %v", err)
	}
	if !onUnix {
		// Windows bounds a variable at 32,767 characters, below MaxSecretBytes;
		// TestAdmitValueHoldsTheBound covers the bound there.
		return
	}
	t.Setenv(testVariable, strings.Repeat("v", MaxSecretBytes)+"\r\n")
	if secret, err := ReadEnv(testVariable); err != nil || len(secret.Reveal()) != MaxSecretBytes {
		t.Errorf("at the bound: got %v", err)
	}
	t.Setenv(testVariable, strings.Repeat("v", MaxSecretBytes+1))
	if _, err := ReadEnv(testVariable); !errors.Is(err, ErrLimit) {
		t.Errorf("over the bound: got %v, want ErrLimit", err)
	}
}

func TestAdmitValueHoldsTheBound(t *testing.T) {
	for raw, want := range map[string]error{
		strings.Repeat("v", MaxSecretBytes):            nil,
		strings.Repeat("v", MaxSecretBytes) + "\n":     nil,
		strings.Repeat("v", MaxSecretBytes) + "\r\n":   nil,
		strings.Repeat("v", MaxSecretBytes+1):          ErrLimit,
		strings.Repeat("v", MaxSecretBytes) + "\n\n":   ErrLimit,
		strings.Repeat("v", MaxSecretBytes) + "\r\r\n": ErrLimit,
	} {
		if _, err := admitValue([]byte(raw)); !errors.Is(err, want) {
			t.Errorf("%d bytes: got %v, want %v", len(raw), err, want)
		}
	}
}

func TestReadEnvErrorsCarryNoValue(t *testing.T) {
	if !onUnix {
		t.Skip("Windows bounds a variable below MaxSecretBytes")
	}
	t.Setenv(testVariable, syntheticValue+strings.Repeat("v", MaxSecretBytes))
	_, err := ReadEnv(testVariable)
	if !errors.Is(err, ErrLimit) {
		t.Fatalf("got %v, want ErrLimit", err)
	}
	assertNoValue(t, "over the bound", err.Error())
}

func TestBindingsReadEachReferenceFromItsBinding(t *testing.T) {
	const fromEnv = "secret:gcp-sm:projects/example-dev/secrets/env-bound/versions/1"
	const fromFile = "secret:gcp-sm:projects/example-dev/secrets/file-bound/versions/2"
	const both = "secret:gcp-sm:projects/example-dev/secrets/both/versions/3"
	t.Setenv(testVariable, "from-the-environment\n")
	path := filepath.Join(t.TempDir(), "mounted")
	writeFile(t, path, "from-a-file\n", 0o600)
	bindings := Bindings{
		Env:   map[string]string{fromEnv: testVariable, both: testVariable},
		Files: map[string]string{fromFile: path, both: path},
	}
	var reader Reader = bindings
	ctx := context.Background()
	if secret, err := reader.ReadSecret(ctx, fromEnv); err != nil || string(secret.Reveal()) != "from-the-environment" {
		t.Errorf("env: got %v", err)
	}
	if onUnix {
		if secret, err := reader.ReadSecret(ctx, fromFile); err != nil || string(secret.Reveal()) != "from-a-file" {
			t.Errorf("file: got %v", err)
		}
	} else if _, err := reader.ReadSecret(ctx, fromFile); !errors.Is(err, ErrPermissionsUnknown) {
		t.Errorf("file: got %v, want ErrPermissionsUnknown", err)
	}
	if _, err := reader.ReadSecret(ctx, both); !errors.Is(err, ErrTwoBindings) {
		t.Errorf("both: got %v, want ErrTwoBindings", err)
	}
	// A reference is a key: one written differently is not the same one.
	for _, reference := range []string{"secret:gcp-sm:projects/example-dev/secrets/unbound/versions/1", fromEnv + " ", strings.ToUpper(fromEnv), ""} {
		_, err := reader.ReadSecret(ctx, reference)
		if !errors.Is(err, ErrNoBinding) {
			t.Errorf("%q: got %v, want ErrNoBinding", reference, err)
		}
		if err != nil && reference != "" && strings.Contains(err.Error(), "unbound") {
			t.Errorf("the refusal carries the reference: %v", err)
		}
	}
	if _, err := (Bindings{}).ReadSecret(ctx, fromEnv); !errors.Is(err, ErrNoBinding) {
		t.Errorf("empty bindings: got %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := reader.ReadSecret(canceled, fromEnv); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled: got %v", err)
	}
}

func TestBindingsErrorsCarryNoReference(t *testing.T) {
	const reference = "secret:gcp-sm:projects/example-prod/secrets/never-printed/versions/9"
	ctx := context.Background()
	for _, bindings := range []Bindings{
		{},
		{Env: map[string]string{reference: "JPACK_STARTUP_TEST_NEVER_SET"}},
		{Env: map[string]string{reference: "X"}, Files: map[string]string{reference: "x"}},
		{Files: map[string]string{reference: filepath.Join(t.TempDir(), "absent")}},
	} {
		_, err := bindings.ReadSecret(ctx, reference)
		if err == nil {
			t.Fatal("an unreadable binding was read")
		}
		for _, part := range []string{"never-printed", "example-prod", "versions/9"} {
			if strings.Contains(err.Error(), part) {
				t.Errorf("the refusal %q carries %q", err, part)
			}
		}
	}
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	// Chmod, unlike the create, is not narrowed by the umask.
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}
