package conformance

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

const (
	MaxSuiteCases         = 10_000
	MaxSuiteBytes         = int64(100 * 1024 * 1024)
	MaxManifestBytes      = int64(10 * 1024 * 1024)
	MaxResultDiagnostics  = 1_000
	CorpusDigestAlgorithm = "sha256-length-prefixed-v1"
)

var casePathPattern = regexp.MustCompile(`^(?:carrier|structural|semantic|valid)/[a-z0-9][a-z0-9-]*\.json$`)

type Manifest struct {
	SuiteVersion string `json:"suiteVersion"`
	SpecVersion  string `json:"specVersion"`
	Cases        []struct {
		ID                  string   `json:"id"`
		Path                string   `json:"path"`
		Layer               string   `json:"layer"`
		ExpectedResult      string   `json:"expectedResult"`
		SupportedExtensions []string `json:"supportedExtensions"`
		ExpectedDiagnostic  *struct {
			Code string `json:"code"`
			Path string `json:"path"`
		} `json:"expectedDiagnostic"`
	} `json:"cases"`
}

type Runner struct {
	engine *validation.Engine
}

type suiteSource struct {
	manifestBytes []byte
	schemaBytes   []byte
	loadCase      func(string) ([]byte, error)
	corpusDigest  string
	provenance    string
}

func NewRunner(engine *validation.Engine) *Runner {
	return &Runner{engine: engine}
}

func (r *Runner) Run(suiteArgument, requestedVersion string) (result.Suite, *validation.OperationalFailure) {
	var source suiteSource
	var failure *validation.OperationalFailure
	if suiteArgument == "" {
		if requestedVersion == "" {
			requestedVersion = artifacts.DraftVersion
		}
		source, failure = bundledSource(requestedVersion)
	} else {
		source, failure = localSource(suiteArgument, requestedVersion)
	}
	if failure != nil {
		return result.Suite{}, failure
	}
	manifest, failure := validateManifest(source.manifestBytes, source.schemaBytes)
	if failure != nil {
		return result.Suite{}, failure
	}
	if requestedVersion != "" && manifest.SpecVersion != requestedVersion {
		return result.Suite{}, invocationFailure("JPS-SUITE-SPEC-VERSION", "Suite specVersion does not match --spec-version.")
	}

	output := result.Suite{
		OutputVersion:         result.OutputVersion,
		Tool:                  result.CurrentTool(),
		Command:               "spec test-conformance",
		Status:                "valid",
		SpecVersion:           manifest.SpecVersion,
		SuiteVersion:          manifest.SuiteVersion,
		CorpusDigest:          source.corpusDigest,
		CorpusDigestAlgorithm: CorpusDigestAlgorithm,
		Provenance:            source.provenance,
		Cases:                 []result.Case{},
		Diagnostics:           []result.Diagnostic{},
	}
	remainingDiagnostics := MaxResultDiagnostics
	for _, manifestCase := range manifest.Cases {
		fixture, err := source.loadCase(manifestCase.Path)
		if err != nil {
			return result.Suite{}, &validation.OperationalFailure{
				Code: "JPS-SUITE-FIXTURE-READ", Message: "A conformance fixture could not be read safely.", ExitCode: result.ExitIO,
			}
		}
		actual, operational := r.engine.Validate(fixture, validation.Options{
			Through:             manifestCase.Layer,
			ExpectedSpecVersion: manifest.SpecVersion,
			SupportedExtensions: manifestCase.SupportedExtensions,
			Limits:              carrier.DefaultLimits(),
		})
		if operational != nil {
			return result.Suite{}, operational
		}
		caseResult := result.Case{
			ID:                manifestCase.ID,
			ExpectedStatus:    manifestCase.ExpectedResult,
			ActualStatus:      actual.Status,
			Status:            "passed",
			ActualDiagnostics: actual.Diagnostics,
		}
		if manifestCase.ExpectedDiagnostic != nil {
			caseResult.ExpectedDiagnostic = &result.ExpectedDiagnostic{
				Code: manifestCase.ExpectedDiagnostic.Code,
				Path: manifestCase.ExpectedDiagnostic.Path,
			}
		}
		matches := actual.Status == manifestCase.ExpectedResult && expectedDiagnosticPresent(caseResult.ExpectedDiagnostic, actual.Diagnostics)
		if !matches {
			caseResult.Status = "mismatch"
			output.Status = "mismatch"
			output.Summary.Mismatched++
		} else {
			output.Summary.Passed++
		}
		if len(caseResult.ActualDiagnostics) > remainingDiagnostics {
			caseResult.ActualDiagnostics = append([]result.Diagnostic(nil), caseResult.ActualDiagnostics[:remainingDiagnostics]...)
			caseResult.DiagnosticsTruncated = true
			output.DiagnosticsTruncated = true
			remainingDiagnostics = 0
		} else {
			remainingDiagnostics -= len(caseResult.ActualDiagnostics)
		}
		if actual.DiagnosticsTruncated {
			caseResult.DiagnosticsTruncated = true
			output.DiagnosticsTruncated = true
		}
		if caseResult.ActualDiagnostics == nil {
			caseResult.ActualDiagnostics = []result.Diagnostic{}
		}
		output.Summary.Total++
		output.Cases = append(output.Cases, caseResult)
	}
	return output, nil
}

func bundledSource(version string) (suiteSource, *validation.OperationalFailure) {
	set, err := artifacts.Load(version)
	if err != nil {
		var unsupported *artifacts.UnsupportedVersionError
		if errors.As(err, &unsupported) {
			return suiteSource{}, &validation.OperationalFailure{
				Code: "JPS-CAPABILITY-SPEC-VERSION", Message: "The exact JPS specification version is not bundled with this CLI.", ExitCode: result.ExitUnsupported,
			}
		}
		return suiteSource{}, internalFailure("JPS-ARTIFACT-INTEGRITY", "Bundled artifacts failed their integrity check.")
	}
	manifestBytes, err := set.Manifest()
	if err != nil {
		return suiteSource{}, internalFailure("JPS-ARTIFACT-MANIFEST", "Bundled conformance metadata is unavailable.")
	}
	schemaBytes, err := set.ManifestSchema()
	if err != nil {
		return suiteSource{}, internalFailure("JPS-ARTIFACT-MANIFEST-SCHEMA", "Bundled conformance schema is unavailable.")
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return suiteSource{}, internalFailure("JPS-ARTIFACT-MANIFEST", "Bundled conformance metadata is invalid.")
	}
	cases := make(map[string][]byte, len(manifest.Cases))
	for _, manifestCase := range manifest.Cases {
		data, err := set.Case(manifestCase.Path)
		if err != nil {
			return suiteSource{}, internalFailure("JPS-ARTIFACT-INTEGRITY", "Bundled conformance fixtures are unavailable.")
		}
		cases[manifestCase.Path] = data
	}
	lock := set.Lock()
	return suiteSource{
		manifestBytes: manifestBytes,
		schemaBytes:   schemaBytes,
		loadCase:      set.Case,
		corpusDigest:  suiteDigest(manifestBytes, schemaBytes, cases),
		provenance:    lock.Source.Kind,
	}, nil
}

func localSource(argument, requestedVersion string) (suiteSource, *validation.OperationalFailure) {
	if fssecure.IsRemotePath(argument) {
		return suiteSource{}, &validation.OperationalFailure{Code: "JPS-SUITE-PATH", Message: "Remote filesystem suite paths are not supported.", ExitCode: result.ExitIO}
	}
	root, manifestPath, err := resolveSuiteRoot(argument)
	if err != nil {
		return suiteSource{}, &validation.OperationalFailure{Code: "JPS-SUITE-PATH", Message: "Suite must be a safe directory or manifest.json path.", ExitCode: result.ExitIO}
	}
	manifestBytes, err := readRegularNoSymlink(manifestPath, MaxManifestBytes)
	if err != nil {
		return suiteSource{}, &validation.OperationalFailure{Code: "JPS-SUITE-MANIFEST-READ", Message: "Suite manifest could not be read safely.", ExitCode: result.ExitIO}
	}
	schemaPath := filepath.Join(root, "manifest.schema.json")
	schemaBytes, err := readRegularNoSymlink(schemaPath, MaxManifestBytes)
	if err != nil {
		return suiteSource{}, &validation.OperationalFailure{Code: "JPS-SUITE-MANIFEST-SCHEMA-READ", Message: "Suite manifest schema could not be read safely.", ExitCode: result.ExitIO}
	}

	version := requestedVersion
	if version == "" {
		value, failure := carrier.Decode(manifestBytes, carrier.DefaultLimits())
		if failure != nil {
			if failure.Resource {
				return suiteSource{}, &validation.OperationalFailure{Code: failure.Diagnostic.Code, Message: failure.Diagnostic.Message, ExitCode: result.ExitIO}
			}
			return suiteSource{}, invocationFailure("JPS-SUITE-MANIFEST", "Suite manifest is not valid strict JSON.")
		}
		object, ok := value.(map[string]any)
		if !ok {
			return suiteSource{}, invocationFailure("JPS-SUITE-MANIFEST", "Suite manifest root must be an object.")
		}
		rawVersion, exists := object["specVersion"]
		if !exists {
			return suiteSource{}, invocationFailure("JPS-SUITE-MANIFEST", "Suite manifest requires specVersion.")
		}
		versionText, versionOK := rawVersion.(string)
		if !versionOK || versionText == "" {
			return suiteSource{}, invocationFailure("JPS-SUITE-MANIFEST", "Suite manifest specVersion must be a non-empty string.")
		}
		version = versionText
	}
	set, err := artifacts.Load(version)
	if err != nil {
		return suiteSource{}, &validation.OperationalFailure{Code: "JPS-CAPABILITY-SPEC-VERSION", Message: "Suite targets a JPS version not bundled with this CLI.", ExitCode: result.ExitUnsupported}
	}
	bundledSchema, err := set.ManifestSchema()
	if err != nil || sha256.Sum256(schemaBytes) != sha256.Sum256(bundledSchema) {
		return suiteSource{}, invocationFailure("JPS-SUITE-MANIFEST-SCHEMA", "Local suite manifest schema must match the bundled schema for its exact JPS version.")
	}

	manifest, failure := validateManifest(manifestBytes, schemaBytes)
	if failure != nil {
		return suiteSource{}, failure
	}
	if len(manifest.Cases) > MaxSuiteCases {
		return suiteSource{}, &validation.OperationalFailure{Code: "JPS-RESOURCE-SUITE-CASE-LIMIT", Message: "Suite exceeds the configured case limit.", ExitCode: result.ExitIO}
	}
	totalBytes := int64(len(manifestBytes) + len(schemaBytes))
	caseBytes := map[string][]byte{}
	for _, manifestCase := range manifest.Cases {
		if err := validateCasePath(manifestCase.Path); err != nil {
			return suiteSource{}, invocationFailure("JPS-SUITE-FIXTURE-PATH", "Suite contains an unsafe fixture path.")
		}
		fixturePath, err := resolveFixture(root, manifestCase.Path)
		if err != nil {
			return suiteSource{}, &validation.OperationalFailure{Code: "JPS-SUITE-FIXTURE-PATH", Message: "Suite fixture path could not be resolved safely.", ExitCode: result.ExitIO}
		}
		data, err := readRegularNoSymlink(fixturePath, carrier.HardMaxBytes)
		if err != nil {
			return suiteSource{}, &validation.OperationalFailure{Code: "JPS-SUITE-FIXTURE-READ", Message: "Suite fixture could not be read safely.", ExitCode: result.ExitIO}
		}
		totalBytes += int64(len(data))
		if totalBytes > MaxSuiteBytes {
			return suiteSource{}, &validation.OperationalFailure{Code: "JPS-RESOURCE-SUITE-BYTE-LIMIT", Message: "Suite exceeds the configured aggregate byte limit.", ExitCode: result.ExitIO}
		}
		caseBytes[manifestCase.Path] = data
	}
	digestValue := suiteDigest(manifestBytes, schemaBytes, caseBytes)
	return suiteSource{
		manifestBytes: manifestBytes,
		schemaBytes:   schemaBytes,
		loadCase: func(casePath string) ([]byte, error) {
			data, ok := caseBytes[casePath]
			if !ok {
				return nil, os.ErrNotExist
			}
			return data, nil
		},
		corpusDigest: digestValue,
		provenance:   "local-suite",
	}, nil
}

func validateManifest(manifestBytes, schemaBytes []byte) (Manifest, *validation.OperationalFailure) {
	value, failure := carrier.Decode(manifestBytes, carrier.DefaultLimits())
	if failure != nil {
		if failure.Resource {
			return Manifest{}, &validation.OperationalFailure{Code: failure.Diagnostic.Code, Message: failure.Diagnostic.Message, ExitCode: result.ExitIO}
		}
		return Manifest{}, invocationFailure("JPS-SUITE-MANIFEST", "Suite manifest is not valid strict JSON.")
	}
	compiled, err := validation.CompileSchema(schemaBytes, "urn:judgmentpack:jps:conformance-manifest")
	if err != nil {
		return Manifest{}, internalFailure("JPS-SUITE-MANIFEST-SCHEMA", "Suite manifest schema could not be compiled.")
	}
	if err := compiled.Validate(value); err != nil {
		return Manifest{}, invocationFailure("JPS-SUITE-MANIFEST", "Suite manifest does not satisfy its schema.")
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return Manifest{}, internalFailure("JPS-SUITE-MANIFEST", "Suite manifest could not be normalized internally.")
	}
	var manifest Manifest
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		return Manifest{}, internalFailure("JPS-SUITE-MANIFEST", "Suite manifest could not be decoded internally.")
	}
	if len(manifest.Cases) == 0 || len(manifest.Cases) > MaxSuiteCases {
		return Manifest{}, invocationFailure("JPS-SUITE-MANIFEST", "Suite case count is outside supported limits.")
	}
	seenIDs := map[string]bool{}
	seenPaths := map[string]bool{}
	for _, manifestCase := range manifest.Cases {
		if seenIDs[manifestCase.ID] || seenPaths[manifestCase.Path] {
			return Manifest{}, invocationFailure("JPS-SUITE-MANIFEST", "Suite case IDs and paths must be unique.")
		}
		seenIDs[manifestCase.ID] = true
		seenPaths[manifestCase.Path] = true
		if err := validateCasePath(manifestCase.Path); err != nil {
			return Manifest{}, invocationFailure("JPS-SUITE-FIXTURE-PATH", "Suite contains an unsafe fixture path.")
		}
		category := strings.SplitN(manifestCase.Path, "/", 2)[0]
		if category == "valid" {
			if manifestCase.Layer != "semantic" || manifestCase.ExpectedResult != "valid" || manifestCase.ExpectedDiagnostic != nil {
				return Manifest{}, invocationFailure("JPS-SUITE-MANIFEST", "Valid-category cases must be semantic valid cases without an expected diagnostic.")
			}
		} else if manifestCase.Layer != category || manifestCase.ExpectedResult == "valid" || manifestCase.ExpectedDiagnostic == nil {
			return Manifest{}, invocationFailure("JPS-SUITE-MANIFEST", "Negative case category, layer, result, and diagnostic must agree.")
		}
	}
	return manifest, nil
}

func expectedDiagnosticPresent(expected *result.ExpectedDiagnostic, actual []result.Diagnostic) bool {
	if expected == nil {
		return len(actual) == 0
	}
	for _, diagnostic := range actual {
		if diagnostic.Code == expected.Code && diagnostic.InstancePath == expected.Path {
			return true
		}
	}
	return false
}

func resolveSuiteRoot(argument string) (string, string, error) {
	info, err := os.Lstat(argument)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return "", "", errors.New("unsafe suite path")
	}
	if info.IsDir() {
		absoluteRoot, err := filepath.Abs(argument)
		if err != nil {
			return "", "", err
		}
		root, err := filepath.EvalSymlinks(absoluteRoot)
		if err != nil {
			return "", "", errors.New("suite root cannot be canonicalized")
		}
		return root, filepath.Join(root, "manifest.json"), nil
	}
	if !info.Mode().IsRegular() || filepath.Base(argument) != "manifest.json" {
		return "", "", errors.New("suite file must be manifest.json")
	}
	absoluteManifest, err := filepath.Abs(argument)
	if err != nil {
		return "", "", err
	}
	root, err := filepath.EvalSymlinks(filepath.Dir(absoluteManifest))
	if err != nil {
		return "", "", errors.New("suite root cannot be canonicalized")
	}
	manifestPath := filepath.Join(root, "manifest.json")
	resolvedManifest, err := filepath.EvalSymlinks(absoluteManifest)
	if err != nil || filepath.Clean(resolvedManifest) != filepath.Clean(manifestPath) {
		return "", "", errors.New("suite manifest cannot be canonicalized safely")
	}
	return root, manifestPath, nil
}

func validateCasePath(value string) error {
	if !casePathPattern.MatchString(value) || strings.ContainsAny(value, "\\\x00") || path.IsAbs(value) || path.Clean(value) != value {
		return errors.New("unsafe case path")
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return errors.New("unsafe case path")
		}
	}
	return nil
}

func resolveFixture(root, logicalPath string) (string, error) {
	current := root
	for _, part := range strings.Split(logicalPath, "/") {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("fixture path contains a symlink or missing component")
		}
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	fixtureAbs, err := filepath.Abs(current)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(rootAbs, fixtureAbs)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", errors.New("fixture escapes suite root")
	}
	return fixtureAbs, nil
}

func readRegularNoSymlink(filePath string, limit int64) ([]byte, error) {
	return fssecure.ReadRegular(filePath, limit)
}

func suiteDigest(manifestBytes, schemaBytes []byte, cases map[string][]byte) string {
	files := map[string][]byte{"manifest.json": manifestBytes, "manifest.schema.json": schemaBytes}
	for casePath, data := range cases {
		files[path.Join("cases", casePath)] = data
	}
	paths := make([]string, 0, len(files))
	for filePath := range files {
		paths = append(paths, filePath)
	}
	sort.Strings(paths)
	digest := sha256.New()
	for _, filePath := range paths {
		writeDigestField(digest, []byte(filePath))
		writeDigestField(digest, files[filePath])
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func writeDigestField(writer io.Writer, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = writer.Write(length[:])
	_, _ = writer.Write(value)
}

func invocationFailure(code, message string) *validation.OperationalFailure {
	return &validation.OperationalFailure{Code: code, Message: message, ExitCode: result.ExitInvocation}
}

func internalFailure(code, message string) *validation.OperationalFailure {
	return &validation.OperationalFailure{Code: code, Message: message, ExitCode: result.ExitInternal}
}
