package artifacts

import (
	"crypto/sha256"
	"embed"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
)

const DraftVersion = "0.1.0-draft"

//go:embed jps
var embedded embed.FS

type Lock struct {
	FormatVersion int        `json:"formatVersion"`
	SpecVersion   string     `json:"specVersion"`
	Source        LockSource `json:"source"`
	BundleDigest  Digest     `json:"bundleDigest"`
	Files         []FileLock `json:"files"`
}

type LockSource struct {
	Repository    string `json:"repository"`
	Kind          string `json:"kind"`
	BaseCommit    string `json:"baseCommit"`
	Ref           string `json:"ref,omitempty"`
	WorktreeDirty bool   `json:"worktreeDirty"`
}

type Digest struct {
	Algorithm string `json:"algorithm"`
	Value     string `json:"value"`
}

type FileLock struct {
	Path   string `json:"path"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type Set struct {
	root  string
	lock  Lock
	files map[string]FileLock
}

type UnsupportedVersionError struct {
	Version string
}

func (e *UnsupportedVersionError) Error() string {
	return "unsupported JPS specification version"
}

func SupportedVersions() []string {
	return []string{DraftVersion}
}

func Load(version string) (*Set, error) {
	if version != DraftVersion {
		return nil, &UnsupportedVersionError{Version: version}
	}
	root := path.Join("jps", DraftVersion)
	lockBytes, err := embedded.ReadFile(path.Join(root, "lock.json"))
	if err != nil {
		return nil, fmt.Errorf("read embedded artifact lock: %w", err)
	}
	var lock Lock
	if err := json.Unmarshal(lockBytes, &lock); err != nil {
		return nil, fmt.Errorf("decode embedded artifact lock: %w", err)
	}
	if lock.FormatVersion != 1 || lock.SpecVersion != version {
		return nil, errors.New("embedded artifact lock is incompatible")
	}
	set := &Set{root: root, lock: lock, files: map[string]FileLock{}}
	if err := set.verify(); err != nil {
		return nil, err
	}
	return set, nil
}

func (s *Set) Lock() Lock {
	return s.lock
}

func (s *Set) Read(relative string) ([]byte, error) {
	if _, ok := s.files[relative]; !ok {
		return nil, fmt.Errorf("artifact is not recorded in lock: %s", relative)
	}
	return embedded.ReadFile(path.Join(s.root, relative))
}

func (s *Set) Schema() ([]byte, error) {
	return s.Read("schema.json")
}

func (s *Set) Manifest() ([]byte, error) {
	return s.Read("manifest.json")
}

func (s *Set) ManifestSchema() ([]byte, error) {
	return s.Read("manifest.schema.json")
}

func (s *Set) Case(relative string) ([]byte, error) {
	return s.Read(path.Join("cases", relative))
}

func (s *Set) File(relative string) (FileLock, bool) {
	item, ok := s.files[relative]
	return item, ok
}

func (s *Set) verify() error {
	if s.lock.BundleDigest.Algorithm != "sha256-length-prefixed-v1" {
		return errors.New("unknown embedded bundle digest algorithm")
	}
	items := append([]FileLock(nil), s.lock.Files...)
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	bundle := sha256.New()
	for _, item := range items {
		if item.Path == "" || path.IsAbs(item.Path) || path.Clean(item.Path) != item.Path {
			return fmt.Errorf("unsafe embedded artifact path: %q", item.Path)
		}
		if _, exists := s.files[item.Path]; exists {
			return fmt.Errorf("duplicate embedded artifact path: %s", item.Path)
		}
		data, err := embedded.ReadFile(path.Join(s.root, item.Path))
		if err != nil {
			return fmt.Errorf("read embedded artifact %s: %w", item.Path, err)
		}
		if len(data) != item.Bytes {
			return fmt.Errorf("embedded artifact size mismatch: %s", item.Path)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != item.SHA256 {
			return fmt.Errorf("embedded artifact digest mismatch: %s", item.Path)
		}
		s.files[item.Path] = item
		writeDigestField(bundle, []byte(item.Path))
		writeDigestField(bundle, data)
	}
	if hex.EncodeToString(bundle.Sum(nil)) != s.lock.BundleDigest.Value {
		return errors.New("embedded artifact bundle digest mismatch")
	}
	entries, err := fs.ReadDir(embedded, s.root)
	if err != nil || len(entries) == 0 {
		return errors.New("embedded artifact set is empty")
	}
	return nil
}

func writeDigestField(writer interface{ Write([]byte) (int, error) }, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = writer.Write(length[:])
	_, _ = writer.Write(value)
}
