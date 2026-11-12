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
	"slices"
	"sort"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// DraftVersion is the specification version every surface selects when a caller
// names none. EvaluatorDraftVersion is the newer bundled draft, whose Core adds
// the evaluator conformance class (§3.4) and publishes the evaluation corpus
// (§3.4.1); bundling it changes no default. It is also the exact version the claim
// in CONFORMANCE.md is made against. CoreVersion is separately selected; bundling
// it does not inherit an evaluation or whole-surface conformance claim.
//
// StagedDraftVersion is EXPERIMENTAL: a working selector the specification
// stages for RFC 0031 (annotations and value sets) and no release carries. Its
// bundle is imported from an exact specification commit and its lock says
// "staged-unreleased". It is Core 0.3.0 plus those two features, selected only
// by a pack that declares it, and the specification may rename or change it.
// No conformance claim is made for it.
const (
	DraftVersion          = "0.1.0-draft"
	EvaluatorDraftVersion = "0.2.0-draft"
	CoreVersion           = "0.3.0"
	StagedDraftVersion    = result.StagedDraftSpecVersion
)

// StagedStage is the lock.source.stage value of a staged bundle.
const StagedStage = "staged-unreleased"

// HasCore030Rules reports whether a specification version carries Core
// 0.3.0's document and evaluator rules: profiles, routed handoffs, and the
// retained-report budgets. Core 0.3.0 does, and so does the staged draft
// selector, which is 0.3.0 plus annotations and value sets. The predicate is
// result.HasCore030Rules, stated once; this is its name for bundle readers.
func HasCore030Rules(version string) bool { return result.HasCore030Rules(version) }

// IsStaged reports whether a version is a staged draft selector.
func IsStaged(version string) bool { return result.IsStagedDraft(version) }

// evaluationManifestPath is the bundled evaluation corpus's index, relative to
// one artifact set's root.
const evaluationManifestPath = "evaluation/manifest.json"

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
	// Stage is "staged-unreleased" for a staged draft selector's bundle and
	// absent for every released one.
	Stage string `json:"stage,omitempty"`
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
	return []string{DraftVersion, EvaluatorDraftVersion, CoreVersion, StagedDraftVersion}
}

func Load(version string) (*Set, error) {
	if !slices.Contains(SupportedVersions(), version) {
		return nil, &UnsupportedVersionError{Version: version}
	}
	root := path.Join("jps", version)
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
	if err := checkStage(version, lock.Source); err != nil {
		return nil, err
	}
	set := &Set{root: root, lock: lock, files: map[string]FileLock{}}
	if err := set.verify(); err != nil {
		return nil, err
	}
	return set, nil
}

// checkStage holds a lock's stage to its version. A staged bundle must say so,
// and only a staged selector's may: a staged source can never present as a
// released one, nor a released one as staged. A staged source is one exact,
// clean commit, named by its full object id.
func checkStage(version string, source LockSource) error {
	if IsStaged(version) != (source.Stage == StagedStage) || (source.Stage != "" && source.Stage != StagedStage) {
		return errors.New("embedded artifact lock does not state the stage its version requires")
	}
	if IsStaged(version) && (source.Kind != "immutable-git-ref" || source.Ref != source.BaseCommit || !fullObjectID(source.Ref) || source.WorktreeDirty) {
		return errors.New("a staged artifact lock must record one exact clean commit")
	}
	return nil
}

func fullObjectID(ref string) bool {
	if len(ref) != 40 && len(ref) != 64 {
		return false
	}
	for _, r := range ref {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
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

// HasEvaluationCorpus reports whether this set bundles the evaluation corpus of
// JPS §3.4.1. A specification version whose Core defines no evaluator class
// publishes none.
func (s *Set) HasEvaluationCorpus() bool {
	_, recorded := s.files[evaluationManifestPath]
	return recorded
}

// EvaluationManifest returns the bundled evaluation-corpus manifest bytes.
func (s *Set) EvaluationManifest() ([]byte, error) {
	return s.Read(evaluationManifestPath)
}

// EvaluationManifestSchema returns the schema the evaluation-corpus manifest is
// carried by.
func (s *Set) EvaluationManifestSchema() ([]byte, error) {
	return s.Read(path.Join("evaluation", "manifest.schema.json"))
}

// EvaluationPack returns one evaluation-corpus pack fixture by its manifest
// path, which is always a single file directly under packs/. Anything else is
// refused before a path is built, so a manifest cannot reach an artifact outside
// the corpus even though every read is also checked against the lock.
func (s *Set) EvaluationPack(relative string) ([]byte, error) {
	name, ok := strings.CutPrefix(relative, "packs/")
	if !ok || name == "" || strings.ContainsRune(name, '/') || !strings.HasSuffix(name, ".json") {
		return nil, fmt.Errorf("not an evaluation-corpus pack path: %s", relative)
	}
	return s.Read(path.Join("evaluation", "packs", name))
}

// Staged reports whether this set is a staged draft selector's bundle.
func (s *Set) Staged() bool {
	return s.lock.Source.Stage == StagedStage
}

// stagedRowsPath is the staged evaluation-row index of a staged bundle.
const stagedRowsPath = "evaluation/cases.json"

// HasStagedEvaluationRows reports whether this set bundles staged evaluation
// rows, which are in no corpus.
func (s *Set) HasStagedEvaluationRows() bool {
	_, recorded := s.files[stagedRowsPath]
	return recorded
}

// StagedEvaluationRows returns the staged evaluation-row index bytes.
func (s *Set) StagedEvaluationRows() ([]byte, error) {
	return s.Read(stagedRowsPath)
}

// StagedExamples lists the staged example packs a staged bundle carries, by
// file name, sorted.
func (s *Set) StagedExamples() []string {
	names := []string{}
	for relative := range s.files {
		if name, ok := strings.CutPrefix(relative, "example/"); ok && !strings.ContainsRune(name, '/') {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// StagedExample returns one staged example pack by file name.
func (s *Set) StagedExample(name string) ([]byte, error) {
	if name == "" || strings.ContainsRune(name, '/') {
		return nil, fmt.Errorf("not a staged example name: %s", name)
	}
	return s.Read(path.Join("example", name))
}

func (s *Set) File(relative string) (FileLock, bool) {
	item, ok := s.files[relative]
	return item, ok
}

// ExampleInfo describes one bundled valid example: its stable name (the case
// file stem), the one-line focus from the manifest, and the specification
// section it exercises. It is metadata only; Example returns the bytes.
type ExampleInfo struct {
	Name        string
	Focus       string
	SpecSection string
}

// UnknownExampleError reports a request for an example name that is not one of
// the bundled valid examples.
type UnknownExampleError struct {
	Name string
}

func (e *UnknownExampleError) Error() string {
	return "unknown example: " + e.Name
}

// Examples enumerates the bundled valid conformance cases as examples, sorted
// by name. These are the digest-locked fixtures the set already embeds, offered
// read-only; they are version-pinned corpus fixtures, not authored templates.
func (s *Set) Examples() ([]ExampleInfo, error) {
	manifestBytes, err := s.Manifest()
	if err != nil {
		return nil, err
	}
	var manifest struct {
		Cases []struct {
			Path        string `json:"path"`
			Focus       string `json:"focus"`
			SpecSection string `json:"specSection"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, err
	}
	examples := make([]ExampleInfo, 0, len(manifest.Cases))
	for _, entry := range manifest.Cases {
		name, ok := exampleName(entry.Path)
		if !ok {
			continue
		}
		examples = append(examples, ExampleInfo{Name: name, Focus: entry.Focus, SpecSection: entry.SpecSection})
	}
	sort.Slice(examples, func(i, j int) bool { return examples[i].Name < examples[j].Name })
	return examples, nil
}

// Example returns one bundled valid example's document bytes and metadata by
// name. The name is resolved against the enumerated examples, never used to
// build a path from caller input, so it cannot escape the embedded valid-case
// set; an unknown name yields *UnknownExampleError.
func (s *Set) Example(name string) ([]byte, ExampleInfo, error) {
	examples, err := s.Examples()
	if err != nil {
		return nil, ExampleInfo{}, err
	}
	for _, example := range examples {
		if example.Name == name {
			data, err := s.Case("valid/" + name + ".json")
			if err != nil {
				return nil, ExampleInfo{}, err
			}
			return data, example, nil
		}
	}
	return nil, ExampleInfo{}, &UnknownExampleError{Name: name}
}

// exampleName maps a manifest case path to a stable example name and reports
// whether the path is a valid, single-segment case under valid/.
func exampleName(casePath string) (string, bool) {
	const prefix = "valid/"
	const suffix = ".json"
	if !strings.HasPrefix(casePath, prefix) || !strings.HasSuffix(casePath, suffix) {
		return "", false
	}
	name := casePath[len(prefix) : len(casePath)-len(suffix)]
	if name == "" || strings.ContainsRune(name, '/') {
		return "", false
	}
	return name, true
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
