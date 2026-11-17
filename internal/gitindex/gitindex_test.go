package gitindex

import (
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/gitindex/gitindextest"
)

type (
	builder = gitindextest.Builder
	entry   = gitindextest.Entry
)

// paths parses data and returns the paths of the entries it visits, its
// version and error.
func paths(data []byte) ([]string, uint32, error) {
	var got []string
	version, err := Parse(data, func(e Entry) { got = append(got, e.Path) })
	return got, version, err
}

// seal is an index file of body: body and its checksum.
func seal(body []byte) []byte {
	sum := sha1.Sum(body)
	return append(slices.Clone(body), sum[:]...)
}

// bodyOf is an index file without its checksum.
func bodyOf(data []byte) []byte { return slices.Clone(data[:len(data)-sha1.Size]) }

// resum gives data, an index with its body changed, a matching checksum.
func resum(data []byte) []byte { return seal(data[:len(data)-sha1.Size]) }

// The entries of each version, each with its mode and stage: a path in
// conflict has an entry at each stage; a path of 0xfff bytes or more has
// 0xfff in its flags; version 4 builds each path from the one before.
func TestParseReadsEachVersion(t *testing.T) {
	long := strings.Repeat("l", 0x1100)
	entries := []entry{
		{Path: "a.txt"},
		{Path: "b/c", Stage: 1}, {Path: "b/c", Stage: 2}, {Path: "b/c", Stage: 3},
		{Path: "b/" + long},
		{Path: "docs/guide.md"},
		{Path: "docs/guides/x.md"},
		{Path: "sub", Mode: 0o160000},
		{Path: "venv/bin/python", Mode: 0o120000},
		{Path: "z", Mode: 0o100755},
	}
	want := []Entry{{"a.txt", 0o100644, 0}, {"b/c", 0o100644, 1}, {"b/c", 0o100644, 2}, {"b/c", 0o100644, 3}, {"b/" + long, 0o100644, 0},
		{"docs/guide.md", 0o100644, 0}, {"docs/guides/x.md", 0o100644, 0}, {"sub", ModeGitlink, 0}, {"venv/bin/python", ModeLink, 0}, {"z", 0o100755, 0}}
	for _, version := range []uint32{2, 3, 4} {
		withFlags := slices.Clone(entries)
		if version > 2 {
			withFlags[5].Extended = 0x4000 // skip-worktree
			withFlags[6].Extended = 0x2000 // intent-to-add
		}
		var got []Entry
		gotVersion, err := Parse(builder{Version: version, Entries: withFlags}.Bytes(), func(e Entry) { got = append(got, e) })
		if err != nil || gotVersion != version || !slices.Equal(got, want) {
			t.Errorf("version %d: %d %v, %d entries", version, gotVersion, err, len(got))
		}
	}
	if got, _, err := paths(builder{Version: 2}.Bytes()); err != nil || len(got) != 0 {
		t.Errorf("no entries: %q %v", got, err)
	}
}

// Two index files git wrote: version 3, an intent-to-add entry giving it
// extended flags; and version 4, with its compressed paths and a TREE
// extension.
const (
	gitVersion3 = "4449524300000003000000066afc212609308ce56afc212609308ce50000083000204c99000081a4000003e8000003e8" +
		"0000000278981922613b2afb6025042ff6bd878ac1994e850005612e74787400000000006afc212609308ce56afc2126" +
		"09308ce50000083000204ca6000081a4000003e8000003e80000000201058d844a98d293a3b03a8615a34700e4ed2be3" +
		"000d646f63732f67756964652e6d6400000000006afc212609308ce56afc212609308ce50000083000204ca8000081a4" +
		"000003e8000003e800000002587be6b4c3f93f93c489c0111bba5596147a26cb0010646f63732f6775696465732f782e" +
		"6d640000000000000000000000000000000000000000000000000000000081a4000000000000000000000000e69de29b" +
		"b2d1d6434b8b29ae775ad8c2e48c5391400920006c617465722e747874000000000000006afc212609308ce56afc2126" +
		"09308ce50000083000204cad000081a4000003e8000003e8000000021a9cc2b7fbfa834924f4c03780d767ccbecf0c9c" +
		"000c76656e762f62696e2f7069700000000000006afc212609308ce56afc212609308ce50000083000204cab0000a000" +
		"000003e8000003e800000014b41f4ee7719b0c0ac780e7ab62fe919d8b7ed0c4000f76656e762f62696e2f707974686f" +
		"6e000000856982d88c3860b834f8847cecfd479852b2f914"
	gitVersion4 = "4449524300000004000000056afc21260aad145a6afc21260aad145a0000083000205d1c000081a4000003e8000003e8" +
		"0000000278981922613b2afb6025042ff6bd878ac1994e85000500612e747874006afc21260aad145a6afc21260aad14" +
		"5a0000083000205d3a000081a4000003e8000003e80000000201058d844a98d293a3b03a8615a34700e4ed2be3000d05" +
		"646f63732f67756964652e6d64006afc21260aad145a6afc21260aad145a0000083000205d51000081a4000003e80000" +
		"03e800000002587be6b4c3f93f93c489c0111bba5596147a26cb001003732f782e6d64006afc21260adbcbe26afc2126" +
		"0adbcbe20000083000205d6c000081a4000003e8000003e8000000021a9cc2b7fbfa834924f4c03780d767ccbecf0c9c" +
		"000c1076656e762f62696e2f706970006afc21260aad145a6afc21260aad145a0000083000205d6a0000a000000003e8" +
		"000003e800000014b41f4ee7719b0c0ac780e7ab62fe919d8b7ed0c4000f027974686f6e00545245450000008e003520" +
		"320aaa81d12f603bf888c08e55a57dd4d11d836c41a9646f6373003220310ad5a77bd9bba7b0d005e82b153df75b9e51" +
		"c270ef677569646573003120300a5e966922f6944392754b5f6a8105103b9edb101e76656e76003220310af7deca1f1b" +
		"e7a8a34114030e20cc7ff8c677d73c62696e003220300a84627af827b7414942f8e16c01ae6f0da5612ef537bdc2eeb0" +
		"989b52051433a3930c334030df3498"
)

func TestParseReadsGitsOwnIndexFiles(t *testing.T) {
	cases := []struct {
		hex     string
		version uint32
		want    []string
	}{
		{gitVersion3, 3, []string{"a.txt", "docs/guide.md", "docs/guides/x.md", "later.txt", "venv/bin/pip", "venv/bin/python"}},
		{gitVersion4, 4, []string{"a.txt", "docs/guide.md", "docs/guides/x.md", "venv/bin/pip", "venv/bin/python"}},
	}
	for _, c := range cases {
		data, err := hex.DecodeString(c.hex)
		if err != nil {
			t.Fatal(err)
		}
		got, version, err := paths(data)
		if err != nil || version != c.version || !slices.Equal(got, c.want) {
			t.Errorf("version %d: %d %v %q", c.version, version, err, got)
		}
	}
}

// Version 4's compression: the count of bytes to take off the previous path
// is an offset varint, one byte below 128 and more above; the first entry
// takes off nothing, and none takes off more than the previous path holds.
func TestParseVersion4Compression(t *testing.T) {
	for v, want := range map[uint64][]byte{0: {0}, 127: {0x7f}, 128: {0x80, 0x00}, 300: {0x81, 0x2c}, 16511: {0xff, 0x7f}, 16512: {0x80, 0x80, 0x00}} {
		if got := gitindextest.Varint(v); !slices.Equal(got, want) {
			t.Errorf("varint %d: %x, want %x", v, got, want)
		}
		// The data is long enough for any path the count could take off.
		r := &reader{data: append(slices.Clone(want), make([]byte, 1<<16)...)}
		if got, ok := r.varint(); !ok || got != v || r.at != len(want) {
			t.Errorf("read varint %x: %d %v at %d", want, got, ok, r.at)
		}
	}
	long := strings.Repeat("p", 300)
	entries := []entry{{Path: long + "/a"}, {Path: long + "/b"}, {Path: "q"}, {Path: "q/r/s"}, {Path: "qq"}}
	data := builder{Version: 4, Entries: entries}.Bytes()
	got, _, err := paths(data)
	if want := []string{long + "/a", long + "/b", "q", "q/r/s", "qq"}; err != nil || !slices.Equal(got, want) {
		t.Fatalf("paths: %v %q", err, got)
	}
	// The third entry takes off all 302 bytes of the one before: two bytes.
	third := strings.Index(string(data), "\x81\x2eq\x00")
	if third < 0 {
		t.Fatalf("no two-byte count in %x", data)
	}
	for name, change := range map[string]func([]byte){
		"one more than the previous path": func(d []byte) { d[third+1]++ },
		"a first entry that takes off":    func(d []byte) { d[12+62] = 1 },
		"a count longer than written":     func(d []byte) { d[third+1] |= 0x80 },
	} {
		changed := slices.Clone(data)
		change(changed)
		if _, _, err := paths(resum(changed)); !errors.Is(err, ErrFormat) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// One fewer: the path read is another, and its length is not the
	// flags'.
	fewer := slices.Clone(data)
	fewer[third+1]--
	if got, _, err := paths(resum(fewer)); !errors.Is(err, ErrFormat) {
		t.Errorf("one fewer: %q %v", got, err)
	}
}

// Optional extensions, signature 'A' to 'Z', are passed over, whatever they
// hold.
func TestParsePassesOverOptionalExtensions(t *testing.T) {
	var extensions [][]byte
	for _, signature := range []string{"TREE", "REUC", "UNTR", "FSMN", "EOIE", "IEOT", "ZZZZ"} {
		extensions = append(extensions, gitindextest.Extension(signature, []byte("\x00anything\xff")))
	}
	extensions = append(extensions, gitindextest.Extension("TREE", nil))
	for _, version := range []uint32{2, 3, 4} {
		data := builder{Version: version, Entries: []entry{{Path: "a"}, {Path: "b/c"}}, Extensions: extensions}.Bytes()
		if got, _, err := paths(data); err != nil || !slices.Equal(got, []string{"a", "b/c"}) {
			t.Errorf("version %d: %q %v", version, got, err)
		}
	}
}

// Every index this reader does not read is refused, never read in part:
// ErrFormat, or ErrBound past a bound.
func TestParseRefusals(t *testing.T) {
	good := builder{Version: 3, Entries: []entry{{Path: "a"}, {Path: "b/c"}}}
	data := good.Bytes()
	with := func(change func(b *builder)) []byte {
		b := good
		b.Entries = slices.Clone(good.Entries)
		change(&b)
		return b.Bytes()
	}
	changed := func(change func(d []byte) []byte) []byte {
		return change(slices.Clone(data))
	}
	// One entry, "abc", cut after its path, before the NUL; and two, the
	// second cut inside its fixed part.
	abc := bodyOf(builder{Version: 2, Entries: []entry{{Path: "abc"}}}.Bytes())
	two := bodyOf(builder{Version: 2, Entries: []entry{{Path: "abc"}, {Path: "abd"}}}.Bytes())
	cases := map[string]struct {
		data []byte
		want error
	}{
		"a checksum that does not match": {changed(func(d []byte) []byte { d[len(d)-1] ^= 1; return d }), ErrFormat},
		"a byte of the body changed":     {changed(func(d []byte) []byte { d[12+62] = 'z'; return d }), ErrFormat},
		"truncated":                      {data[:len(data)-1], ErrFormat},
		"truncated, checksum made again": {seal(data[:12+64]), ErrFormat},
		"cut inside an entry":            {seal(two[:12+72+60]), ErrFormat},
		"shorter than a header":          {data[:20], ErrFormat},
		"empty":                          {nil, ErrFormat},
		"another signature":              {resum(changed(func(d []byte) []byte { d[0] = 'X'; return d })), ErrFormat},
		"version 1":                      {builder{Version: 1}.Bytes(), ErrFormat},
		"version 5":                      {builder{Version: 5}.Bytes(), ErrFormat},
		"SHA-256":                        {builder{Version: 2, Entries: good.Entries, SHA256: true}.Bytes(), ErrFormat},
		"a split index (link)":           {with(func(b *builder) { b.Extensions = [][]byte{gitindextest.Extension("link", make([]byte, 20))} }), ErrFormat},
		"a sparse index (sdir)":          {with(func(b *builder) { b.Extensions = [][]byte{gitindextest.Extension("sdir", nil)} }), ErrFormat},
		"another required extension":     {with(func(b *builder) { b.Extensions = [][]byte{gitindextest.Extension("abcd", nil)} }), ErrFormat},
		"an extension past the end":      {seal(append(bodyOf(data), "TREE\x00\x00\x00\x09\x01"...)), ErrFormat},
		"bytes after the extensions":     {with(func(b *builder) { b.Extensions = [][]byte{gitindextest.Extension("TREE", nil), {1, 2, 3}} }), ErrFormat},
		"more entries than the bytes":    {resum(changed(func(d []byte) []byte { d[11] = 200; return d })), ErrFormat},
		"an extended flag in version 2":  {with(func(b *builder) { b.Version = 2; b.Entries[0].Extended = 0x4000 }), ErrFormat},
		"a reserved extended flag":       {with(func(b *builder) { b.Entries[0].Extended = 0x8000 }), ErrFormat},
		"an unused extended flag":        {with(func(b *builder) { b.Entries[0].Extended = 0x0001 }), ErrFormat},
		"entries out of order":           {with(func(b *builder) { b.Entries[0].Path = "c" }), ErrFormat},
		"a path twice at stage 0":        {with(func(b *builder) { b.Entries[1].Path = "a" }), ErrFormat},
		"stages out of order":            {with(func(b *builder) { b.Entries = []entry{{Path: "a", Stage: 2}, {Path: "a", Stage: 1}} }), ErrFormat},
		"an empty path":                  {with(func(b *builder) { b.Entries[0].Path = "" }), ErrFormat},
		"a .. segment":                   {with(func(b *builder) { b.Entries[0].Path = "../a" }), ErrFormat},
		"a . segment":                    {with(func(b *builder) { b.Entries[0].Path = "a/./b" }), ErrFormat},
		"an empty segment":               {with(func(b *builder) { b.Entries[0].Path = "a//b" }), ErrFormat},
		"a leading slash":                {with(func(b *builder) { b.Entries[0].Path = "/a" }), ErrFormat},
		"a sparse directory entry":       {with(func(b *builder) { b.Entries[0].Path = "a/"; b.Entries[0].Mode = 0o40000 }), ErrFormat},
		"a directory's mode":             {with(func(b *builder) { b.Entries[0].Mode = 0o40000 }), ErrFormat},
		"another mode type":              {with(func(b *builder) { b.Entries[0].Mode = 0o60644 }), ErrFormat},
		"a length the flags do not give": {resum(changed(func(d []byte) []byte { d[12+61]++; return d })), ErrFormat},
		"padding that is not NUL":        {resum(changed(func(d []byte) []byte { d[12+64+70] = 'x'; return d })), ErrFormat},
		"a path with no NUL":             {seal(abc[:12+65]), ErrFormat},
	}
	for name, c := range cases {
		if version, err := Parse(c.data, func(Entry) {}); !errors.Is(err, c.want) || version != 0 {
			t.Errorf("%s: version %d, %v; want %v", name, version, err, c.want)
		}
	}
	// The control: the index the cases change is read.
	if got, _, err := paths(data); err != nil || !slices.Equal(got, []string{"a", "b/c"}) {
		t.Fatalf("control: %q %v", got, err)
	}
}

func TestParseBounds(t *testing.T) {
	data := builder{Version: 2, Entries: []entry{{Path: "a"}, {Path: "b"}, {Path: "c"}}}.Bytes()
	for name, set := range map[string]func(){
		"bytes":   func() { maxBytes = len(data) - 1 },
		"entries": func() { maxEntries = 2 },
	} {
		set()
		if _, _, err := paths(data); !errors.Is(err, ErrBound) {
			t.Errorf("%s: %v", name, err)
		}
		maxBytes, maxEntries = MaxBytes, MaxEntries
	}
	// At the bounds, the index is read.
	maxBytes, maxEntries = len(data), 3
	t.Cleanup(func() { maxBytes, maxEntries = MaxBytes, MaxEntries })
	if got, _, err := paths(data); err != nil || len(got) != 3 {
		t.Fatalf("at the bounds: %q %v", got, err)
	}
	// An entry count past the bound is refused before any entry is read.
	big := slices.Clone(data)
	binary.BigEndian.PutUint32(big[8:], 4)
	if _, _, err := paths(resum(big)); !errors.Is(err, ErrBound) {
		t.Fatalf("count: %v", err)
	}
}

// ReadWorkTree reads root/.git/index only when root/.git is a directory:
// with none, a file (a linked worktree or a submodule), or a link to one,
// it refuses; so does an index that is missing, not a file, or too large.
func TestReadWorkTree(t *testing.T) {
	root := t.TempDir()
	gitindextest.Write(t, root, "b/c", "a")
	var got []string
	if err := ReadWorkTree(root, func(e Entry) { got = append(got, e.Path) }); err != nil || !slices.Equal(got, []string{"a", "b/c"}) {
		t.Fatalf("read: %q %v", got, err)
	}
	read := func(root string) error { return ReadWorkTree(root, func(Entry) {}) }
	if err := read(t.TempDir()); !errors.Is(err, ErrNoRepository) {
		t.Errorf("no .git: %v", err)
	}
	worktree := t.TempDir()
	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: "+filepath.Join(root, ".git")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := read(worktree); !errors.Is(err, ErrNotDirectory) {
		t.Errorf(".git a file: %v", err)
	}
	noIndex := t.TempDir()
	if err := os.Mkdir(filepath.Join(noIndex, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := read(noIndex); !errors.Is(err, ErrUnreadable) {
		t.Errorf("no index: %v", err)
	}
	if err := os.Mkdir(filepath.Join(noIndex, ".git", "index"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := read(noIndex); !errors.Is(err, ErrUnreadable) {
		t.Errorf("an index that is a directory: %v", err)
	}
	corrupt := t.TempDir()
	gitindextest.Write(t, corrupt, "a")
	if err := os.WriteFile(filepath.Join(corrupt, ".git", "index"), []byte("DIRC\x00\x00\x00\x02"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := read(corrupt); !errors.Is(err, ErrFormat) {
		t.Errorf("a corrupt index: %v", err)
	}
	maxBytes = 10
	t.Cleanup(func() { maxBytes = MaxBytes })
	if err := read(root); !errors.Is(err, ErrBound) {
		t.Errorf("a large index: %v", err)
	}
	maxBytes = MaxBytes
	if runtime.GOOS == "windows" {
		return
	}
	linked := t.TempDir()
	if err := os.Symlink(filepath.Join(root, ".git"), filepath.Join(linked, ".git")); err != nil {
		t.Fatal(err)
	}
	if err := read(linked); !errors.Is(err, ErrNotDirectory) {
		t.Errorf(".git a link: %v", err)
	}
}
