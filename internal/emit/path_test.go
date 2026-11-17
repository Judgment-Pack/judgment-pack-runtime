package emit

import (
	"slices"
	"testing"
)

// The names git itself refuses as ".git" are those of git's
// t/t1014-read-tree-confusing.sh, with core.protectNTFS and core.protectHFS
// set: each is git's directory on NTFS or on HFS+.
func TestIsGitNameMakesGitsOwnChecks(t *testing.T) {
	for _, name := range []string{
		// t1014-read-tree-confusing.sh
		".git", ".GIT", "\u200c.Git", ".gI\u200cT", ".GiT\u200c", "git~1", ".git. ", ".git...:alternate-stream",
		".git::$INDEX_ALLOCATION", `.git\foobar`,
		// The rest of each check: the short name in another case, dots and
		// spaces, every code point next_hfs_char skips, and a name that ends
		// in malformed UTF-8.
		"GIT~1", "Git~1. .", ".git ", ".git.", "\u200d.git", ".\u200eg\u200fit", ".g\u202ai\u202bt\u202c", ".git\u202d\u202e",
		"\u206a.\u206bg\u206ci\u206dt\u206e\u206f", "\ufeff.git", ".git\xff", ".git/x",
	} {
		if !isGitName(name) {
			t.Errorf("%q is not read as .git", name)
		}
	}
	for _, name := range []string{
		"", ".", "git", ".gi", "git~", "git~2", "git~10", "git~1x", ".git-x", ".gitignore", ".github", ".gitmodules",
		"x.git", ".git\u00e9", ".g\u00eft", "\u00a0.git", ".git\u00a0", "gen",
	} {
		if isGitName(name) {
			t.Errorf("%q is read as .git", name)
		}
	}
}

// A path or an owns glob with a segment that NTFS or HFS+ reads as ".git"
// is refused, and the walk of the owned paths does not enter one.
func TestPathRulesRefuseEveryNameOfGitsDirectory(t *testing.T) {
	m := mustManifest(t, manifestDoc([]string{"exit", "0"}, nil))
	for _, path := range []string{"gen/vendor/GIT~1/hooks/pre-commit", "gen/vendor/git~1", "gen/.g\u200cit/config", "gen/\ufeff.GIT/HEAD"} {
		_, findings := ParseResponse([]byte(responseJSON(t, map[string]any{"path": path, "text": "x\n"})), "1.0.0", nil, m)
		if got := codes(findings); !slices.Equal(got, []string{"EMIT-RESPONSE@/files/0/path"}) {
			t.Errorf("%q: %v", path, got)
		}
	}
	for _, glob := range []string{"gen/GIT~1/**", "gen/*/.gi\u200dt/*"} {
		if _, err := parseGlob(glob); err == nil {
			t.Errorf("%q parsed as a glob", glob)
		}
	}
}
