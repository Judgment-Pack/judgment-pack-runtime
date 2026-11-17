package conformance

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/gitindex"
)

// linkRefusal returns check 6's rule for the symbolic links and junctions of
// the repository that lead out of it (ADR-0099's amendment), given by slash
// path in walk order: the run is refused at the first that it may not leave
// out of the copy, as every link that led out was refused before, and when
// there is none, every one is left out and reported (EMIT-LINK-OMITTED). A
// link is left out only when all of these hold:
//   - git does not track it: neither its path nor a path below it is in the
//     repository's index (trackedLinks), so it is not the repository's own
//     content but something made in its working tree, a virtual
//     environment for one;
//   - no owns glob could name it: none may match the link's path or a path
//     below it, with names read loosely (emit.Manifest.MayOwnAtOrBelow), so
//     no response path could be written at it or through it, and the host
//     would not refuse the repository for it (EMIT-LINK);
//   - the vectors path is clear of it: the vectors directory is not reached
//     through it, and does not lie at, above or below it (vectorPaths), since
//     the harness reads the vectors, and --update writes them, through the
//     directory as given.
//
// Each errs toward refusing. An index that cannot be read, or is not of a
// form gitindex reads, or a repository root with no .git directory, refuses
// at the first link; and a vectors directory that cannot be followed names
// every link.
func linkRefusal(m *emit.Manifest, root, vectorsDir string) func(outward []string) (string, bool) {
	return func(outward []string) (string, bool) {
		if len(outward) == 0 {
			return "", false
		}
		tracked, err := trackedLinks(root, outward)
		through, at := vectorPaths(root, vectorsDir)
		if at != "" {
			through = append(through, at)
		}
		for i, name := range outward {
			if err != nil || tracked[i] || m.MayOwnAtOrBelow(name) || slices.ContainsFunc(through, func(given string) bool { return mayMeet(given, name) }) {
				return name, true
			}
		}
		return "", false
	}
}

// trackedLinks says, for each link of links, by slash path, whether the
// index of the repository whose working tree is root tracks it: whether the
// link's path, or a path below it, is in the index. It reads the index
// itself (gitindex), never running git, and fails when it cannot read it:
// when root/.git is not a directory (none, or a linked worktree's or a
// submodule's .git file), or the index is missing, unreadable, past a
// bound, or not of a form gitindex reads.
//
// A path of the index and a link's path are compared byte for byte, and
// loosely too, since the file system the repository is on may read two
// names as one (looseKey). Every link below a submodule is tracked: its
// files are in the submodule's own index. So is every link below the
// directory that holds a segment of an index path that may be a Windows
// short name, holding "~" and a digit, which may be any name there. Each
// errs toward tracked.
func trackedLinks(root string, links []string) ([]bool, error) {
	byKey := make(map[string][]int, len(links))
	for i, link := range links {
		key := looseKey(link)
		byKey[key] = append(byKey[key], i)
	}
	tracked := make([]bool, len(links))
	// anyBelow holds the keys of the directories every link below which is
	// tracked: a submodule's, and one that holds a segment of an index path
	// that may be a short name.
	anyBelow := map[string]bool{}
	err := gitindex.ReadWorkTree(root, func(e gitindex.Entry) {
		key := looseKey(e.Path)
		// The path itself and each directory above it.
		for end := len(key); end > 0; end = strings.LastIndexByte(key[:end], '/') {
			for _, i := range byKey[key[:end]] {
				tracked[i] = true
			}
		}
		if e.IsGitlink() {
			anyBelow[key] = true
		}
		if !strings.Contains(key, "~") {
			return
		}
		segments := strings.Split(key, "/")
		for i, segment := range segments {
			if mayBeShortName(segment) {
				anyBelow[strings.Join(segments[:i], "/")] = true
				break
			}
		}
	})
	if err != nil {
		return nil, err
	}
	for i, link := range links {
		if tracked[i] || len(anyBelow) == 0 {
			continue
		}
		// Each directory above the link, the root ("") included.
		key := looseKey(link)
		tracked[i] = anyBelow[""]
		for end := strings.LastIndexByte(key, '/'); end > 0 && !tracked[i]; end = strings.LastIndexByte(key[:end], '/') {
			tracked[i] = anyBelow[key[:end]]
		}
	}
	return tracked, nil
}

// looseKey is the key under which a file system that reads names loosely may
// read two slash paths as one: emit.NameKey (case, Unicode normalization and
// the code points HFS+ ignores), with a backslash read as a separator and
// each segment's trailing dots and spaces dropped, as Windows reads them.
// Two paths no file system reads as one may share a key too.
func looseKey(path string) string {
	segments := strings.Split(strings.ReplaceAll(emit.NameKey(path), `\`, "/"), "/")
	for i, segment := range segments {
		segments[i] = strings.TrimRight(segment, ". ")
	}
	return strings.Join(segments, "/")
}

// mayBeShortName says whether a segment may be a Windows short name, which
// the file system generates as a name's first characters, "~" and a number.
func mayBeShortName(segment string) bool {
	for i := 0; i+1 < len(segment); i++ {
		if segment[i] == '~' && segment[i+1] >= '0' && segment[i+1] <= '9' {
			return true
		}
	}
	return false
}

// mayMeet says whether the slash path name, as found on disk, may be the
// slash path given, or a path above or below it, on a file system that reads
// names loosely: each segment the two share in order has one NameKey, or
// given's holds "~", which Windows can read as the short name of any name.
// The path "." is the repository root, above every path.
func mayMeet(given, name string) bool {
	if given == "." || name == "." {
		return true
	}
	want, have := strings.Split(given, "/"), strings.Split(name, "/")
	for i := range min(len(want), len(have)) {
		if !strings.Contains(want[i], "~") && emit.NameKey(want[i]) != emit.NameKey(have[i]) {
			return false
		}
	}
	return true
}

// vectorPaths follows the vectors directory dir, made absolute and cleaned as
// filepath.Abs makes it and the harness opens it, from its volume's root,
// part by part and link by link, as the system does: a link's target is
// taken in place of the link, and each ".." of a target from where the parts
// before it led. Whenever it stands on the repository root (os.SameFile), it
// counts the parts below it. It returns the repository paths of the links it
// followed while in the repository (through), and the repository path where
// dir ends (at): "" when that is outside the repository, and "." when it is
// the root or above it, so that every path of the repository lies below it.
// What it cannot follow (a part it cannot read, a link it cannot read or
// with a target rooted but not absolute, more than maxLinkHops links) is
// "." too: every link is then named.
func vectorPaths(root, dir string) (through []string, at string) {
	rootInfo, err := os.Stat(root)
	if err != nil {
		return nil, "."
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return nil, "."
	}
	isRoot := func(full string) bool {
		info, err := os.Stat(full)
		return err == nil && os.SameFile(info, rootInfo)
	}
	volume := filepath.VolumeName(absolute)
	current := volume + string(filepath.Separator)
	rest := splitParts(absolute[len(volume):])
	var below []string
	inRoot := isRoot(current)
	for hops := 0; len(rest) > 0; {
		part := rest[0]
		rest = rest[1:]
		switch part {
		case ".":
			continue
		case "..":
			if inRoot && len(below) > 0 {
				below = below[:len(below)-1]
			} else {
				inRoot = false
			}
			current = filepath.Dir(current)
		default:
			next := filepath.Join(current, part)
			info, err := os.Lstat(next)
			if err != nil {
				return nil, "."
			}
			if info.Mode()&(fs.ModeSymlink|fs.ModeIrregular) == 0 {
				current = next
				if inRoot {
					below = append(below, part)
				}
				break
			}
			// A symbolic link, or a junction, which os.Readlink reads too.
			if hops++; hops > maxLinkHops {
				return nil, "."
			}
			target, err := os.Readlink(next)
			if err != nil {
				return nil, "."
			}
			if inRoot {
				through = append(through, path.Join(append(below, part)...))
			}
			switch targetVolume := filepath.VolumeName(target); {
			case filepath.IsAbs(target):
				current, inRoot, below = targetVolume+string(filepath.Separator), false, nil
				target = target[len(targetVolume):]
			case targetVolume != "" || target != "" && os.IsPathSeparator(target[0]):
				return nil, "."
			}
			rest = append(splitParts(target), rest...)
		}
		if !inRoot && isRoot(current) {
			inRoot, below = true, nil
		}
	}
	if inRoot {
		if len(below) == 0 {
			return through, "."
		}
		return through, path.Join(below...)
	}
	// The repository at or below where dir ends lies below it too.
	if ends, err := os.Stat(current); err == nil {
		for above := root; ; above = filepath.Dir(above) {
			if info, err := os.Stat(above); err == nil && os.SameFile(info, ends) {
				return through, "."
			}
			if filepath.Dir(above) == above {
				break
			}
		}
	}
	return through, ""
}

// splitParts splits a path into its parts at each separator.
func splitParts(name string) []string {
	return strings.FieldsFunc(name, func(r rune) bool { return r < 0x80 && os.IsPathSeparator(uint8(r)) })
}
