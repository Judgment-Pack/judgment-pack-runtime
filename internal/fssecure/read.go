// Package fssecure reaches a project's files under a containment rule: a
// project's declared paths are relative to that project's own directory, and
// none of them may leave it.
//
// Nearly everything here reads. The exceptions are the two writes a project can
// ask this runtime to make, both into that project's own tree: Root.Append with
// the Root.MakeDir that prepares its directory, for the audit trail of ADR-0018,
// and Root.Replace, for the generated reviewed-set lock of ADR-0019. They are in
// this package rather than at the surfaces that call them precisely because the
// containment rule is not weaker for a write: the same handle, the same lexical
// refusal, the same final-component checks, and no pathname handed to anything.
//
// The rule is enforced by Root, which holds the directory open and resolves every
// access against that handle. Containment therefore holds through the open and not
// merely up to it — the distinction matters, because a resolver that returns a
// pathname for someone else to open leaves an interval in which the filesystem
// can be rearranged underneath the decision. Root has no such interval; see its
// documentation for what that does and does not cover.
//
// ReadRegular is the unrooted reader, for the one thing that has no root: a file
// the operator named directly on the command line, including the project
// configuration that *defines* a root before one exists. It applies the
// final-component and regular-file checks but makes no containment claim, and it
// must not be used for a path read out of a configuration.
package fssecure

import (
	"errors"
	"io"
	"os"
)

// ErrTooLarge reports that a read stopped because the input exceeded the
// caller's byte limit, rather than being missing or not a regular file. It lets
// a caller distinguish a size-limit failure from a generic read failure.
var ErrTooLarge = errors.New("input exceeds the byte limit")

// ReadRegular reads one bounded regular file named by pathname.
//
// It says nothing about where the file is. Every path that comes out of a project
// configuration goes through Root instead, which is the only thing in this
// package that enforces containment.
func ReadRegular(filePath string, limit int64) ([]byte, error) {
	file, err := openRegular(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readBounded(file, limit)
}

// readBounded reads a file already opened and checked by its caller, refusing one
// byte past the limit so "exactly at the limit" and "over it" stay distinguishable.
func readBounded(file *os.File, limit int64) ([]byte, error) {
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("path is not a regular file")
	}
	if info.Size() > limit {
		return nil, ErrTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, ErrTooLarge
	}
	return data, nil
}
