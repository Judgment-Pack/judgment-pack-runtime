package fssecure

import (
	"errors"
	"path/filepath"
	"strings"
)

// ErrOutsideRoot reports that a relative path did not resolve inside the root it
// was resolved against. It is distinct from a read failure: the path may name a
// perfectly readable file, and the refusal is about where that file is.
var ErrOutsideRoot = errors.New("path resolves outside the root")

// Resolve joins one caller-declared relative path onto a root directory and
// refuses any result outside it.
//
// The check is lexical, so it can be applied before the named file exists — a
// project configuration is checked for escaping paths at validate time, not only
// when a read is attempted. ResolveWithin repeats the containment check against
// the canonicalized directory, which is where a symlinked intermediate component
// is caught; neither check substitutes for the other, and no caller that is about
// to read a file may stop at this one.
//
// An absolute path is refused rather than resolved: a root-relative convention
// that silently accepted "/etc/passwd" would have no root. A leading separator of
// either kind is refused on every platform, so a configuration written on Windows
// is refused identically on Unix and the reverse — a path is either inside the
// project or it is not, and that must not depend on who reads it.
func Resolve(root, relative string) (string, error) {
	if relative == "" || strings.ContainsRune(relative, 0) {
		return "", ErrOutsideRoot
	}
	if filepath.IsAbs(relative) || filepath.VolumeName(relative) != "" {
		return "", ErrOutsideRoot
	}
	if strings.HasPrefix(relative, "/") || strings.HasPrefix(relative, `\`) {
		return "", ErrOutsideRoot
	}
	if IsRemotePath(relative) {
		return "", ErrOutsideRoot
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target := filepath.Clean(filepath.Join(absoluteRoot, relative))
	if !within(absoluteRoot, target) {
		return "", ErrOutsideRoot
	}
	return target, nil
}

// ResolveWithin resolves one caller-declared relative path against root and
// refuses a path that leaves it by any route a check can see before the file is
// opened. It returns the canonicalized target, which is what a caller must open:
// resolving a path and then opening the *declared* one would put the two halves
// of the containment rule on two different files.
//
// Two of the three checks are here — Resolve refuses a lexically escaping path,
// and canonicalizing the containing directory refuses a path that reaches
// outside through a symlinked intermediate component. The third is the
// O_NOFOLLOW open, which refuses a final symlink and anything that is not a
// regular file; every caller that reads the returned path applies it, through
// ReadWithin or through ReadRegular directly. A caller that only cleaned the
// path would still follow a symlinked directory out of the root.
//
// A path whose containing directory does not exist is reported with the
// operating system's own error rather than as an escape: EvalSymlinks cannot
// canonicalize what is not there, and "the directory is missing" and "the path
// left the project" call for two different fixes.
func ResolveWithin(root, relative string) (string, error) {
	target, err := Resolve(root, relative)
	if err != nil {
		return "", err
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	canonicalRoot, err = filepath.Abs(canonicalRoot)
	if err != nil {
		return "", err
	}
	// The containing directory is canonicalized rather than the file: EvalSymlinks
	// fails on a path that does not exist, and the final component is handled by
	// the O_NOFOLLOW open the caller performs.
	canonicalDir, err := filepath.EvalSymlinks(filepath.Dir(target))
	if err != nil {
		return "", err
	}
	canonicalDir, err = filepath.Abs(canonicalDir)
	if err != nil {
		return "", err
	}
	canonicalTarget := filepath.Join(canonicalDir, filepath.Base(target))
	if !within(canonicalRoot, canonicalTarget) {
		return "", ErrOutsideRoot
	}
	return canonicalTarget, nil
}

// ReadWithin reads one bounded regular file named relative to root, refusing a
// path that leaves the root by any route.
//
// Three things are checked, and they catch different escapes: ResolveWithin
// refuses a lexically escaping path and one that reaches outside through a
// symlinked intermediate component, and ReadRegular's open refuses a final
// symlink and anything that is not a regular file. There is one implementation
// of the first two, so a caller that needs the path rather than the bytes gets
// the same containment this one does.
func ReadWithin(root, relative string, limit int64) ([]byte, error) {
	canonicalTarget, err := ResolveWithin(root, relative)
	if err != nil {
		return nil, err
	}
	return ReadRegular(canonicalTarget, limit)
}

// within reports whether target is root itself or lies beneath it. Comparing the
// cleaned strings with a trailing separator is what keeps a sibling directory
// whose name merely starts with the root's — "/project-backup" beside "/project"
// — from counting as inside it.
func within(root, target string) bool {
	if target == root {
		return true
	}
	return strings.HasPrefix(target, root+string(filepath.Separator))
}
