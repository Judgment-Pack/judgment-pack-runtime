// Command sync-spec-artifacts imports an exact local JPS artifact snapshot.
// It is a maintainer tool and is never used by runtime validation.
package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const sourceRepository = "https://github.com/Judgment-Pack/judgment-pack-spec"

var (
	fullCommitPattern     = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
	evaluationPackPattern = regexp.MustCompile(`^packs/[a-z0-9][a-z0-9-]*\.json$`)
)

type manifest struct {
	SpecVersion string `json:"specVersion"`
	Cases       []struct {
		Path string `json:"path"`
	} `json:"cases"`
}

// evaluationManifest is the evaluation corpus index. It appears only in
// specification versions that define the evaluator conformance class, so its
// absence is not an error; each case names one pack fixture under packs/.
type evaluationManifest struct {
	SuiteVersion string `json:"suiteVersion"`
	SpecVersion  string `json:"specVersion"`
	Cases        []struct {
		Pack string `json:"pack"`
	} `json:"cases"`
}

type lockFile struct {
	FormatVersion int        `json:"formatVersion"`
	SpecVersion   string     `json:"specVersion"`
	Source        lockSource `json:"source"`
	BundleDigest  digest     `json:"bundleDigest"`
	Files         []fileLock `json:"files"`
}

type lockSource struct {
	Repository    string `json:"repository"`
	Kind          string `json:"kind"`
	BaseCommit    string `json:"baseCommit"`
	Ref           string `json:"ref,omitempty"`
	WorktreeDirty bool   `json:"worktreeDirty"`
}

type digest struct {
	Algorithm string `json:"algorithm"`
	Value     string `json:"value"`
}

type fileLock struct {
	Path   string `json:"path"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type importFile struct {
	sourcePath string
	targetPath string
	data       []byte
}

func main() {
	source := flag.String("source", "", "path to the judgment-pack-spec checkout")
	destination := flag.String("destination", "", "new artifact-set directory")
	sourceRef := flag.String("source-ref", "", "exact clean Git commit or tag for immutable provenance")
	allowDirty := flag.Bool("allow-dirty", false, "explicitly import an unreleased local snapshot")
	flag.Parse()

	if *source == "" || *destination == "" {
		fatal(errors.New("both --source and --destination are required"))
	}
	if err := run(*source, *destination, *sourceRef, *allowDirty); err != nil {
		fatal(err)
	}
}

func run(source, destination, sourceRef string, allowDirty bool) error {
	if (sourceRef == "" && !allowDirty) || (sourceRef != "" && allowDirty) {
		return errors.New("choose exactly one provenance mode: --source-ref or --allow-dirty")
	}
	sourceRoot, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	destinationRoot, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	if sourceRoot == destinationRoot || strings.HasPrefix(destinationRoot, sourceRoot+string(os.PathSeparator)) {
		return errors.New("destination must not be inside the specification checkout")
	}
	if _, err := os.Stat(destinationRoot); err == nil {
		return fmt.Errorf("destination already exists: %s", destinationRoot)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	manifestBytes, err := readRegular(filepath.Join(sourceRoot, "conformance", "manifest.json"))
	if err != nil {
		return err
	}
	var suite manifest
	if err := json.Unmarshal(manifestBytes, &suite); err != nil {
		return fmt.Errorf("decode conformance manifest: %w", err)
	}
	if suite.SpecVersion == "" || len(suite.Cases) == 0 {
		return errors.New("manifest does not identify a non-empty suite")
	}

	files := []importFile{
		{sourcePath: filepath.Join(sourceRoot, "schema", "judgment-pack-core.schema.json"), targetPath: "schema.json"},
		{sourcePath: filepath.Join(sourceRoot, "conformance", "manifest.json"), targetPath: "manifest.json", data: manifestBytes},
		{sourcePath: filepath.Join(sourceRoot, "conformance", "manifest.schema.json"), targetPath: "manifest.schema.json"},
	}
	seen := map[string]bool{}
	for _, item := range suite.Cases {
		if err := validateCasePath(item.Path); err != nil {
			return err
		}
		if seen[item.Path] {
			return fmt.Errorf("duplicate fixture path in manifest: %s", item.Path)
		}
		seen[item.Path] = true
		files = append(files, importFile{
			sourcePath: filepath.Join(sourceRoot, "conformance", filepath.FromSlash(item.Path)),
			targetPath: path.Join("cases", item.Path),
		})
	}

	evaluationFiles, err := evaluationCorpusFiles(sourceRoot, suite.SpecVersion)
	if err != nil {
		return err
	}
	files = append(files, evaluationFiles...)

	for index := range files {
		if files[index].data == nil {
			files[index].data, err = readRegular(files[index].sourcePath)
			if err != nil {
				return err
			}
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].targetPath < files[j].targetPath })

	provenance, err := sourceState(sourceRoot, sourceRef, allowDirty)
	if err != nil {
		return err
	}
	lock := lockFile{
		FormatVersion: 1,
		SpecVersion:   suite.SpecVersion,
		Source:        provenance,
		BundleDigest:  digest{Algorithm: "sha256-length-prefixed-v1"},
	}
	bundle := sha256.New()
	for _, item := range files {
		sum := sha256.Sum256(item.data)
		lock.Files = append(lock.Files, fileLock{
			Path:   item.targetPath,
			Bytes:  len(item.data),
			SHA256: hex.EncodeToString(sum[:]),
		})
		writeDigestField(bundle, []byte(item.targetPath))
		writeDigestField(bundle, item.data)
	}
	lock.BundleDigest.Value = hex.EncodeToString(bundle.Sum(nil))

	if err := os.MkdirAll(destinationRoot, 0o755); err != nil {
		return err
	}
	for _, item := range files {
		target := filepath.Join(destinationRoot, filepath.FromSlash(item.targetPath))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, item.data, 0o644); err != nil {
			return err
		}
	}
	encoded, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(filepath.Join(destinationRoot, "lock.json"), encoded, 0o644); err != nil {
		return err
	}
	fmt.Printf("Imported JPS %s (%d files, %s)\n", suite.SpecVersion, len(files), lock.BundleDigest.Value)
	return nil
}

// evaluationCorpusFiles lists the evaluation-corpus artifacts to import: the
// manifest, its schema, and every pack fixture a case names. A specification
// version that publishes no evaluation corpus contributes nothing, which is how
// an older version whose Core defines no evaluator class still imports.
func evaluationCorpusFiles(sourceRoot, specVersion string) ([]importFile, error) {
	root := filepath.Join(sourceRoot, "conformance", "evaluation")
	manifestPath := filepath.Join(root, "manifest.json")
	if _, err := os.Lstat(manifestPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	manifestBytes, err := readRegular(manifestPath)
	if err != nil {
		return nil, err
	}
	var corpus evaluationManifest
	if err := json.Unmarshal(manifestBytes, &corpus); err != nil {
		return nil, fmt.Errorf("decode evaluation manifest: %w", err)
	}
	if corpus.SuiteVersion == "" || len(corpus.Cases) == 0 {
		return nil, errors.New("evaluation manifest does not identify a non-empty corpus")
	}
	if corpus.SpecVersion != specVersion {
		return nil, fmt.Errorf("evaluation corpus targets JPS %s, not %s", corpus.SpecVersion, specVersion)
	}
	files := []importFile{
		{sourcePath: manifestPath, targetPath: path.Join("evaluation", "manifest.json"), data: manifestBytes},
		{sourcePath: filepath.Join(root, "manifest.schema.json"), targetPath: path.Join("evaluation", "manifest.schema.json")},
	}
	seen := map[string]bool{}
	for _, item := range corpus.Cases {
		if err := validateEvaluationPackPath(item.Pack); err != nil {
			return nil, err
		}
		if seen[item.Pack] {
			continue
		}
		seen[item.Pack] = true
		files = append(files, importFile{
			sourcePath: filepath.Join(root, filepath.FromSlash(item.Pack)),
			targetPath: path.Join("evaluation", item.Pack),
		})
	}
	return files, nil
}

func validateEvaluationPackPath(value string) error {
	if !evaluationPackPattern.MatchString(value) || strings.ContainsAny(value, "\\\x00") || path.IsAbs(value) || path.Clean(value) != value {
		return fmt.Errorf("unsafe pack path in evaluation manifest: %q", value)
	}
	return nil
}

func validateCasePath(value string) error {
	if value == "" || strings.ContainsAny(value, "\\\x00") || path.IsAbs(value) || path.Clean(value) != value {
		return fmt.Errorf("unsafe fixture path in manifest: %q", value)
	}
	parts := strings.Split(value, "/")
	if len(parts) < 2 || !map[string]bool{"carrier": true, "structural": true, "semantic": true, "valid": true}[parts[0]] {
		return fmt.Errorf("unsupported fixture path in manifest: %q", value)
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("unsafe fixture path in manifest: %q", value)
		}
	}
	return nil
}

func readRegular(filePath string) ([]byte, error) {
	info, err := os.Lstat(filePath)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("artifact is not a regular file: %s", filePath)
	}
	return os.ReadFile(filePath)
}

func sourceState(root, sourceRef string, allowDirty bool) (lockSource, error) {
	remoteCommand := exec.Command("git", "-C", root, "remote", "get-url", "origin")
	remoteOutput, err := remoteCommand.Output()
	if err != nil {
		return lockSource{}, fmt.Errorf("resolve source origin: %w", err)
	}
	if normalizeRepositoryURL(strings.TrimSpace(string(remoteOutput))) != normalizeRepositoryURL(sourceRepository) {
		return lockSource{}, errors.New("source origin is not the official judgment-pack-spec repository")
	}
	commitCommand := exec.Command("git", "-C", root, "rev-parse", "HEAD")
	commitOutput, err := commitCommand.Output()
	if err != nil {
		return lockSource{}, fmt.Errorf("resolve source commit: %w", err)
	}
	baseCommit := strings.TrimSpace(string(commitOutput))
	statusCommand := exec.Command("git", "-C", root, "status", "--porcelain")
	statusOutput, err := statusCommand.Output()
	if err != nil {
		return lockSource{}, fmt.Errorf("inspect source worktree: %w", err)
	}
	dirty := len(statusOutput) > 0
	if sourceRef != "" {
		if dirty {
			return lockSource{}, errors.New("immutable imports require a clean source worktree")
		}
		revision, err := immutableRevisionSpec(sourceRef)
		if err != nil {
			return lockSource{}, err
		}
		refCommand := exec.Command("git", "-C", root, "rev-parse", "--verify", revision)
		refOutput, err := refCommand.Output()
		if err != nil {
			return lockSource{}, fmt.Errorf("resolve source ref: %w", err)
		}
		if strings.TrimSpace(string(refOutput)) != baseCommit {
			return lockSource{}, errors.New("source ref does not resolve to the checked-out commit")
		}
		return lockSource{
			Repository: sourceRepository,
			Kind:       "immutable-git-ref",
			BaseCommit: baseCommit,
			Ref:        sourceRef,
		}, nil
	}
	if !allowDirty {
		return lockSource{}, errors.New("unreleased snapshots require --allow-dirty")
	}
	return lockSource{
		Repository:    sourceRepository,
		Kind:          "unreleased-local-snapshot",
		BaseCommit:    baseCommit,
		WorktreeDirty: dirty,
	}, nil
}

func immutableRevisionSpec(sourceRef string) (string, error) {
	if fullCommitPattern.MatchString(sourceRef) {
		return sourceRef + "^{commit}", nil
	}
	tag := strings.TrimPrefix(sourceRef, "refs/tags/")
	if tag == "" || strings.HasPrefix(tag, "refs/") || strings.ContainsAny(tag, "\x00\r\n") {
		return "", errors.New("source ref must be a full commit digest or an exact tag")
	}
	return "refs/tags/" + tag + "^{commit}", nil
}

func normalizeRepositoryURL(value string) string {
	value = strings.TrimSpace(strings.TrimSuffix(value, "/"))
	value = strings.TrimSuffix(value, ".git")
	value = strings.TrimPrefix(value, "git@github.com:")
	value = strings.TrimPrefix(value, "ssh://git@github.com/")
	value = strings.TrimPrefix(value, "https://github.com/")
	return strings.ToLower(value)
}

func writeDigestField(writer io.Writer, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = writer.Write(length[:])
	_, _ = writer.Write(value)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "sync-spec-artifacts:", err)
	os.Exit(1)
}
