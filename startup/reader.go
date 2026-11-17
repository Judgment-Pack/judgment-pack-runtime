package startup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
)

// MaxSecretBytes bounds a secret's value, after its trailing line feed is
// trimmed. It is the largest payload the first store RFC 0035 names accepts.
const MaxSecretBytes = 64 * 1024

// Errors a reader returns, each wrapped with the variable or file it concerns.
// No error carries a secret's value or a secret reference's text.
var (
	// ErrNoBinding: the reference is bound to no variable and no file.
	ErrNoBinding = errors.New("startup: the secret reference has no binding")
	// ErrTwoBindings: the reference is bound to a variable and to a file.
	ErrTwoBindings = errors.New("startup: the secret reference is bound to a variable and to a file")
	// ErrNotSet: the environment variable is not set.
	ErrNotSet = errors.New("startup: the environment variable is not set")
	// ErrEmpty: the value is empty once its trailing line feed is trimmed.
	ErrEmpty = errors.New("startup: the secret value is empty")
	// ErrLimit: a value or a document is over its bound.
	ErrLimit = errors.New("startup: over the bound")
	// ErrNotRegular: the path does not lead to a regular file.
	ErrNotRegular = errors.New("startup: the secret file is not a regular file")
	// ErrWorldReadable: users other than the file's owner and group may read
	// the file.
	ErrWorldReadable = errors.New("startup: the secret file is world-readable")
	// ErrPermissionsUnknown: on this operating system the permission bits Go
	// reports do not say who may read a file, so no file is read.
	ErrPermissionsUnknown = errors.New("startup: this operating system's file permissions cannot be checked, so secret files are refused")
)

// A Reader reads the value of one secret reference
// (secret:<store>:<locator>, RFC 0035) from wherever it is delivered.
//
// A reader of this module never contacts a secret store: it reads what the
// platform delivered to the process. A later reader in its own package may
// read a store; it implements this same interface.
type Reader interface {
	ReadSecret(ctx context.Context, reference string) (Secret, error)
}

// Bindings is a [Reader] that maps each secret reference, exactly as the
// registry writes it, to where the platform delivered that secret version:
//   - Env maps a reference to the name of the environment variable that holds
//     its value, read by [ReadEnv];
//   - Files maps a reference to the path of the mounted file that holds its
//     value, read by [ReadFileWith] under FileRules.
//
// A reference in neither map is refused with [ErrNoBinding], and one in both
// with [ErrTwoBindings]. The reference is a key, never parsed or resolved.
type Bindings struct {
	Env   map[string]string
	Files map[string]string
	// FileRules applies to every file in Files. Its zero value is the
	// strict default of [ReadFile].
	FileRules FileRules
}

// FileRules relaxes what [ReadFileWith] accepts. The zero value is the strict
// default, which [ReadFile] applies.
type FileRules struct {
	// AllowOtherRead accepts a file that users other than its owner and
	// group may read. Platforms mount secret files that way unless told
	// otherwise: Cloud Run's secret volumes default to mode 0444, and
	// Kubernetes' to 0644. Prefer mounting with mode 0440 or 0400 and
	// leaving this false. Set it only where the platform cannot be told, and
	// where no other user shares the process's file system, such as a
	// container that runs one process.
	AllowOtherRead bool
}

// ReadSecret implements [Reader].
func (b Bindings) ReadSecret(ctx context.Context, reference string) (Secret, error) {
	if err := ctx.Err(); err != nil {
		return Secret{}, err
	}
	name, inEnv := b.Env[reference]
	path, inFiles := b.Files[reference]
	switch {
	case inEnv && inFiles:
		return Secret{}, ErrTwoBindings
	case inEnv:
		return ReadEnv(name)
	case inFiles:
		return ReadFileWith(path, b.FileRules)
	}
	return Secret{}, ErrNoBinding
}

// ReadEnv reads a secret from the environment variable name. It trims exactly
// one trailing "\n" or "\r\n" and keeps every other byte. It refuses a
// variable that is not set ([ErrNotSet]), a value that is then empty
// ([ErrEmpty]), and one over [MaxSecretBytes] ([ErrLimit]).
func ReadEnv(name string) (Secret, error) {
	raw, ok := os.LookupEnv(name)
	if !ok {
		return Secret{}, fmt.Errorf("%w: %s", ErrNotSet, name)
	}
	secret, err := admitValue([]byte(raw))
	if err != nil {
		return Secret{}, fmt.Errorf("%w: environment variable %s", err, name)
	}
	return secret, nil
}

// ReadFile reads a secret from the file at path, following symbolic links as
// a mounted secret volume uses them. It refuses a path that does not lead to a
// regular file ([ErrNotRegular]) and a file whose permission bits let others
// read it ([ErrWorldReadable]), and checks both again on the opened file, so
// the bytes read are those of the file checked. Where the operating system's
// permission bits do not describe who may read a file, it refuses every file
// ([ErrPermissionsUnknown]). Then it applies [ReadEnv]'s rules: exactly one
// trailing "\n" or "\r\n" is trimmed, and an empty value or one over
// [MaxSecretBytes] is refused.
func ReadFile(path string) (Secret, error) {
	return ReadFileWith(path, FileRules{})
}

// ReadFileWith is [ReadFile] under rules: with rules.AllowOtherRead, a file
// others may read is accepted rather than refused [ErrWorldReadable]. Every
// other check stands, and a file whose permission bits cannot be checked is
// still refused [ErrPermissionsUnknown].
func ReadFileWith(path string, rules FileRules) (Secret, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Secret{}, fmt.Errorf("startup: %w", err)
	}
	// Checked before the open as well, so that a named pipe at the path is
	// refused rather than waited on. One put in the file's place between this
	// check and the open is not waited on either: the open does not block
	// (openNoWait), and the check on the opened file refuses it.
	if err := checkFile(info, rules); err != nil {
		return Secret{}, fmt.Errorf("%w: %s", err, path)
	}
	file, err := openFile(path)
	if err != nil {
		return Secret{}, fmt.Errorf("startup: %w", err)
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return Secret{}, fmt.Errorf("startup: %w", err)
	}
	if err := checkFile(info, rules); err != nil {
		return Secret{}, fmt.Errorf("%w: %s", err, path)
	}
	// Two bytes more than the bound for a trimmed "\r\n", and one to tell a
	// value over the bound from one at it.
	raw, err := io.ReadAll(io.LimitReader(file, MaxSecretBytes+3))
	defer clear(raw)
	if err != nil {
		return Secret{}, fmt.Errorf("startup: %w", err)
	}
	secret, err := admitValue(raw)
	if err != nil {
		return Secret{}, fmt.Errorf("%w: %s", err, path)
	}
	return secret, nil
}

// openFile opens path for reading without waiting on a named pipe.
func openFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|openNoWait, 0)
}

// checkFile refuses what is not a regular file, then applies the operating
// system's permission check.
func checkFile(info fs.FileInfo, rules FileRules) error {
	if !info.Mode().IsRegular() {
		return ErrNotRegular
	}
	return checkPermissions(info.Mode().Perm(), rules)
}

// admitValue trims one trailing line feed and holds the rest to the value's
// rules.
func admitValue(raw []byte) (Secret, error) {
	value := trimNewline(raw)
	switch {
	case len(value) == 0:
		return Secret{}, ErrEmpty
	case len(value) > MaxSecretBytes:
		return Secret{}, ErrLimit
	}
	return NewSecret(value), nil
}

// trimNewline removes exactly one trailing "\r\n" or "\n", and nothing else.
func trimNewline(raw []byte) []byte {
	if value, found := bytes.CutSuffix(raw, []byte("\r\n")); found {
		return value
	}
	value, _ := bytes.CutSuffix(raw, []byte("\n"))
	return value
}
