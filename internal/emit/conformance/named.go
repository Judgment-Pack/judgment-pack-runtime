package conformance

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit"
)

// linkNamer returns check 6's test for a symbolic link or junction of the
// repository that leads out of it, by its slash path: whether a path the run
// writes or reads could name it. Such a link refuses the run, as every link
// that led out did before; any other is left out of the copy and reported
// (EMIT-LINK-OMITTED, ADR-0099's amendment). A path names the link when:
//   - an owns glob may match the link's path or a path below it, with names
//     read loosely (emit.Manifest.MayOwnAtOrBelow): a response path could be
//     written at it or through it, and the host refuses such a link
//     (EMIT-LINK);
//   - the vectors directory is reached through it, or lies at, above or
//     below it (vectorPaths): the harness reads the vectors, and --update
//     writes them, through the directory as given.
//
// Both are decided before the copy is made, from the manifest and the
// directory as given, and each errs toward naming: a vectors directory that
// cannot be followed names every link.
func linkNamer(m *emit.Manifest, root, vectorsDir string) func(string) bool {
	through, at := vectorPaths(root, vectorsDir)
	if at != "" {
		through = append(through, at)
	}
	return func(name string) bool {
		if m.MayOwnAtOrBelow(name) {
			return true
		}
		for _, given := range through {
			if mayMeet(given, name) {
				return true
			}
		}
		return false
	}
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
