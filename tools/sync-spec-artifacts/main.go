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

// writeFile is a seam. A failure partway through materialization is the case
// this tool's staging exists for, and it cannot be provoked deterministically
// from outside, so the test injects one here rather than asserting the
// behaviour is probably right.
var writeFile = os.WriteFile

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
	// Stage is set only by a staged import (--staged): the bundle comes from a
	// specification commit's staged directories, which no release carries.
	Stage string `json:"stage,omitempty"`
}

// stagedStage is the one value a staged import writes into lock.source.stage.
const stagedStage = "staged-unreleased"

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
	staged := flag.String("staged", "", "import the staged draft selector named here from the checkout's staged directories (requires --source-ref as a full commit)")
	replace := flag.Bool("replace", false, "with --staged only: replace an existing staged bundle of the same selector")
	flag.Parse()

	if err := dispatch(*source, *destination, *sourceRef, *staged, *allowDirty, *replace); err != nil {
		fatal(err)
	}
}

// dispatch holds the flag combinations to the two import modes: a staged
// import takes an exact clean commit only, and --replace exists only for it.
func dispatch(source, destination, sourceRef, staged string, allowDirty, replace bool) error {
	if source == "" || destination == "" {
		return errors.New("both --source and --destination are required")
	}
	if staged != "" {
		if allowDirty {
			return errors.New("--staged imports only an exact clean commit; --allow-dirty is refused")
		}
		return runStaged(source, destination, sourceRef, staged, replace)
	}
	if replace {
		return errors.New("--replace applies only to a --staged import")
	}
	return run(source, destination, sourceRef, allowDirty)
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

	provenance, err := sourceState(sourceRoot, sourceRef, allowDirty)
	if err != nil {
		return err
	}
	lock, err := materialize(files, destinationRoot, suite.SpecVersion, provenance, false)
	if err != nil {
		return err
	}
	fmt.Printf("Imported JPS %s (%d files, %s)\n", suite.SpecVersion, len(files), lock.BundleDigest.Value)
	return nil
}

// verifyMaterialized re-reads every file the lock names and holds it to the
// recorded size and digest. The lock is the artifact consumers trust, so it may
// only describe bytes that are actually on disk.
func verifyMaterialized(root string, lock lockFile) error {
	for _, entry := range lock.Files {
		target := filepath.Join(root, filepath.FromSlash(entry.Path))
		data, err := readRegular(target)
		if err != nil {
			return fmt.Errorf("verify %s: %w", entry.Path, err)
		}
		if len(data) != entry.Bytes {
			return fmt.Errorf("verify %s: materialized %d bytes, lock records %d",
				entry.Path, len(data), entry.Bytes)
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != entry.SHA256 {
			return fmt.Errorf("verify %s: materialized digest %s, lock records %s",
				entry.Path, got, entry.SHA256)
		}
	}
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
	if len(parts) < 2 || !map[string]bool{"advisory": true, "carrier": true, "structural": true, "semantic": true, "valid": true}[parts[0]] {
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

// materialize reads every file not yet read, locks the set, writes it to a
// staging directory beside the destination, verifies it against the lock, and
// publishes it with one rename. With replace, an existing destination is moved
// aside first and removed only after the new bundle is in place.
func materialize(files []importFile, destinationRoot, specVersion string, provenance lockSource, replace bool) (lockFile, error) {
	var err error
	for index := range files {
		if files[index].data == nil {
			files[index].data, err = readRegular(files[index].sourcePath)
			if err != nil {
				return lockFile{}, err
			}
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].targetPath < files[j].targetPath })

	lock := lockFile{
		FormatVersion: 1,
		SpecVersion:   specVersion,
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

	encoded, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return lockFile{}, err
	}
	encoded = append(encoded, '\n')

	// Materialize into a staging directory this invocation owns, then publish
	// the destination with one rename. Writing straight into the destination
	// meant a late failure left a partial bundle behind -- and because the next
	// invocation correctly refuses an existing destination, that partial
	// directory blocked the retry that would have fixed it. Staging sits beside
	// the destination so the rename stays on one filesystem.
	if err := os.MkdirAll(filepath.Dir(destinationRoot), 0o755); err != nil {
		return lockFile{}, err
	}
	staging, err := os.MkdirTemp(filepath.Dir(destinationRoot), ".sync-spec-artifacts-")
	if err != nil {
		return lockFile{}, err
	}
	published := false
	defer func() {
		if !published {
			// Only ever the directory this invocation created. A failure never
			// removes the destination, whoever owns it.
			os.RemoveAll(staging)
		}
	}()

	for _, item := range files {
		target := filepath.Join(staging, filepath.FromSlash(item.targetPath))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return lockFile{}, err
		}
		if err := writeFile(target, item.data, 0o644); err != nil {
			return lockFile{}, err
		}
	}
	// The lock is the commit artifact and is written last, so a staging tree
	// without it is unfinished by construction.
	if err := writeFile(filepath.Join(staging, "lock.json"), encoded, 0o644); err != nil {
		return lockFile{}, err
	}
	// Read the materialized bytes back and hold them to the lock that is about
	// to describe them, rather than trusting the writes that just happened.
	if err := verifyMaterialized(staging, lock); err != nil {
		return lockFile{}, err
	}

	if replace {
		if err := publishReplacing(staging, destinationRoot, specVersion); err != nil {
			return lockFile{}, err
		}
		published = true
		return lock, nil
	}
	// Re-checked immediately before publishing: the destination is refused if
	// it appeared while this invocation was working. The rename itself refuses
	// a non-empty directory, so the remaining window is an empty directory
	// created in between -- Go exposes no portable exclusive directory rename.
	if _, err := os.Stat(destinationRoot); err == nil {
		return lockFile{}, fmt.Errorf("destination already exists: %s", destinationRoot)
	} else if !errors.Is(err, os.ErrNotExist) {
		return lockFile{}, err
	}
	if err := os.Rename(staging, destinationRoot); err != nil {
		return lockFile{}, fmt.Errorf("publish %s: %w", destinationRoot, err)
	}
	published = true
	return lock, nil
}

// --- staged draft selectors ---------------------------------------------------

// stagedSelectorPattern is the shape of a staged draft selector: a
// specification version that no release carries, so it always ends in -draft.
var stagedSelectorPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-draft$`)

var stagedExamplePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*\.json$`)

// stagedManifest is the staged document-case index: the released case shape
// under an envelope that names its status and draft selector, with no
// suiteVersion because it is in no corpus.
type stagedManifest struct {
	Status      string `json:"status"`
	SpecVersion string `json:"specVersion"`
	CaseSchema  string `json:"caseSchema"`
	Cases       []struct {
		Path string `json:"path"`
	} `json:"cases"`
}

// stagedEvaluationRows is the staged evaluation-row index. Held rows name a
// pack too, and that pack is imported with the others.
type stagedEvaluationRows struct {
	Status      string `json:"status"`
	SpecVersion string `json:"specVersion"`
	CaseSchema  string `json:"caseSchema"`
	Cases       []struct {
		Pack string `json:"pack"`
	} `json:"cases"`
	HeldCases []struct {
		Case struct {
			Pack string `json:"pack"`
		} `json:"case"`
	} `json:"heldCases"`
}

// runStaged imports the artifacts a specification commit stages for one draft
// selector: the staged schema, the staged document cases with the released
// case schema they are written against, the staged evaluation rows with the
// released row schema and their packs, and the staged examples declaring the
// selector. The source must be a clean checkout at an exact full commit, which
// the lock records with stage "staged-unreleased": the commit is immutable, and
// what it stages is in no release.
//
// With replace, an existing bundle of the same staged selector is replaced in
// one step; anything else at the destination is refused.
func runStaged(source, destination, sourceRef, selector string, replace bool) error {
	if !stagedSelectorPattern.MatchString(selector) {
		return fmt.Errorf("--staged must name a draft selector such as 0.4.0-draft, not %q", selector)
	}
	if !fullCommitPattern.MatchString(sourceRef) {
		return errors.New("--staged requires --source-ref to be the full commit digest the staged artifacts are taken from")
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
		if !replace {
			return fmt.Errorf("destination already exists: %s (use --replace to re-import a staged bundle)", destinationRoot)
		}
		if err := replaceableStagedBundle(destinationRoot, selector); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	} else {
		replace = false
	}

	stagedRoot := filepath.Join(sourceRoot, "conformance", "staged")
	manifestBytes, err := readRegular(filepath.Join(stagedRoot, "manifest.json"))
	if err != nil {
		return err
	}
	var suite stagedManifest
	if err := json.Unmarshal(manifestBytes, &suite); err != nil {
		return fmt.Errorf("decode staged manifest: %w", err)
	}
	if suite.Status != "staged" || suite.SpecVersion != selector || len(suite.Cases) == 0 {
		return fmt.Errorf("conformance/staged/manifest.json does not stage a non-empty suite for %s", selector)
	}
	if suite.CaseSchema != "../manifest.schema.json#/$defs/case" {
		return fmt.Errorf("staged manifest names case schema %q, not the released one", suite.CaseSchema)
	}
	files := []importFile{
		{sourcePath: filepath.Join(sourceRoot, "schema", "staged", "judgment-pack-core-"+selector+".schema.json"), targetPath: "schema.json"},
		{sourcePath: filepath.Join(stagedRoot, "manifest.json"), targetPath: "manifest.json", data: manifestBytes},
		{sourcePath: filepath.Join(sourceRoot, "conformance", "manifest.schema.json"), targetPath: "manifest.schema.json"},
	}
	seen := map[string]bool{}
	for _, item := range suite.Cases {
		if err := validateCasePath(item.Path); err != nil {
			return err
		}
		if seen[item.Path] {
			return fmt.Errorf("duplicate fixture path in staged manifest: %s", item.Path)
		}
		seen[item.Path] = true
		files = append(files, importFile{
			sourcePath: filepath.Join(stagedRoot, filepath.FromSlash(item.Path)),
			targetPath: path.Join("cases", item.Path),
		})
	}

	rows, err := stagedEvaluationFiles(sourceRoot, selector)
	if err != nil {
		return err
	}
	files = append(files, rows...)
	examples, err := stagedExampleFiles(sourceRoot, selector)
	if err != nil {
		return err
	}
	files = append(files, examples...)

	provenance, err := sourceState(sourceRoot, sourceRef, false)
	if err != nil {
		return err
	}
	provenance.Stage = stagedStage
	lock, err := materialize(files, destinationRoot, selector, provenance, replace)
	if err != nil {
		return err
	}
	fmt.Printf("Imported staged JPS %s from %s (%d files, %s)\n", selector, provenance.BaseCommit, len(files), lock.BundleDigest.Value)
	return nil
}

// stagedEvaluationFiles finds the one staged evaluation-row directory whose
// cases.json declares the selector, and lists it with the released row schema
// and every pack its rows and held rows name.
func stagedEvaluationFiles(sourceRoot, selector string) ([]importFile, error) {
	root := filepath.Join(sourceRoot, "conformance", "evaluation")
	entries, err := os.ReadDir(filepath.Join(root, "staged"))
	if err != nil {
		return nil, err
	}
	var found []importFile
	directories := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		directory := filepath.Join(root, "staged", entry.Name())
		casesPath := filepath.Join(directory, "cases.json")
		if _, err := os.Lstat(casesPath); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		data, err := readRegular(casesPath)
		if err != nil {
			return nil, err
		}
		var rows stagedEvaluationRows
		if err := json.Unmarshal(data, &rows); err != nil {
			return nil, fmt.Errorf("decode %s: %w", casesPath, err)
		}
		if rows.SpecVersion != selector {
			continue
		}
		directories++
		if rows.Status != "staged" || len(rows.Cases) == 0 {
			return nil, fmt.Errorf("%s does not stage a non-empty row set", casesPath)
		}
		if rows.CaseSchema != "../../manifest.schema.json#/$defs/case" {
			return nil, fmt.Errorf("%s names row schema %q, not the released one", casesPath, rows.CaseSchema)
		}
		found = append(found,
			importFile{sourcePath: casesPath, targetPath: path.Join("evaluation", "cases.json"), data: data},
			importFile{sourcePath: filepath.Join(root, "manifest.schema.json"), targetPath: path.Join("evaluation", "manifest.schema.json")},
		)
		packs := []string{}
		for _, item := range rows.Cases {
			packs = append(packs, item.Pack)
		}
		for _, held := range rows.HeldCases {
			packs = append(packs, held.Case.Pack)
		}
		seen := map[string]bool{}
		for _, pack := range packs {
			if err := validateEvaluationPackPath(pack); err != nil {
				return nil, err
			}
			if seen[pack] {
				continue
			}
			seen[pack] = true
			found = append(found, importFile{
				sourcePath: filepath.Join(directory, filepath.FromSlash(pack)),
				targetPath: path.Join("evaluation", pack),
			})
		}
	}
	if directories != 1 {
		return nil, fmt.Errorf("expected exactly one staged evaluation-row set for %s, found %d", selector, directories)
	}
	return found, nil
}

// stagedExampleFiles lists the staged example packs that declare the selector.
func stagedExampleFiles(sourceRoot, selector string) ([]importFile, error) {
	directory := filepath.Join(sourceRoot, "examples", "staged")
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	var found []importFile
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if !stagedExamplePattern.MatchString(entry.Name()) {
			return nil, fmt.Errorf("unsupported staged example name: %q", entry.Name())
		}
		full := filepath.Join(directory, entry.Name())
		data, err := readRegular(full)
		if err != nil {
			return nil, err
		}
		var declared struct {
			SpecVersion string `json:"specVersion"`
		}
		if json.Unmarshal(data, &declared) != nil || declared.SpecVersion != selector {
			continue
		}
		found = append(found, importFile{sourcePath: full, targetPath: path.Join("example", entry.Name()), data: data})
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("no staged example declares %s", selector)
	}
	return found, nil
}

// replaceableStagedBundle admits a destination for --replace only when it is a
// staged bundle of the same selector: a released bundle, or anything that is
// not a bundle, is never replaced.
func replaceableStagedBundle(destinationRoot, selector string) error {
	raw, err := readRegular(filepath.Join(destinationRoot, "lock.json"))
	if err != nil {
		return fmt.Errorf("--replace: the destination is not an artifact bundle: %w", err)
	}
	var existing lockFile
	if err := json.Unmarshal(raw, &existing); err != nil {
		return fmt.Errorf("--replace: the destination lock is unreadable: %w", err)
	}
	if existing.SpecVersion != selector || existing.Source.Stage != stagedStage {
		return fmt.Errorf("--replace: the destination is not a staged %s bundle", selector)
	}
	return nil
}

// publishReplacing moves the existing staged bundle aside, renames the staging
// directory into place, and only then removes the old bundle. If the second
// rename fails, the old bundle is moved back.
func publishReplacing(staging, destinationRoot, selector string) error {
	if err := replaceableStagedBundle(destinationRoot, selector); err != nil {
		return err
	}
	aside, err := os.MkdirTemp(filepath.Dir(destinationRoot), ".sync-spec-artifacts-replaced-")
	if err != nil {
		return err
	}
	// MkdirTemp reserved a unique name; the rename needs it absent.
	if err := os.Remove(aside); err != nil {
		return err
	}
	if err := os.Rename(destinationRoot, aside); err != nil {
		return fmt.Errorf("move the existing bundle aside: %w", err)
	}
	if err := os.Rename(staging, destinationRoot); err != nil {
		if restore := os.Rename(aside, destinationRoot); restore != nil {
			return fmt.Errorf("publish %s: %v; the previous bundle is at %s", destinationRoot, err, aside)
		}
		return fmt.Errorf("publish %s: %w", destinationRoot, err)
	}
	return os.RemoveAll(aside)
}
