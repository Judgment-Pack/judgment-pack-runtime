package federation

import (
	"strconv"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// CheckInput is what a lock check is given (§9): the lock, the registry it
// names and that registry's kind declarations, the index when the lock names
// one, the emitter manifests and their files when it names emitters, and the
// expected inputs. Index nil is no index given; Emitters empty is none.
type CheckInput struct {
	Lock     registry.Document
	Registry registry.Document
	Kinds    []registry.Document
	Index    *registry.Document
	Emitters []Emitter
	Expect   Expect
}

// CheckLock runs §9's lock check and returns its report. A failure in step 1
// or 2 ends the check; steps 3 to 8 report every diagnostic of their own. It
// does not assemble again (the index check does, §5.1), reads the serial for
// its form only, and verifies no signature (§11). The error is an
// *InputError for an emitter or a file given twice, with no report.
func CheckLock(in CheckInput) (Report, error) {
	keys, files, err := givenEmitters(in.Emitters)
	if err != nil {
		return Report{}, err
	}
	collected := &collector{}
	if in.Expect.missing(false) {
		collected.add("LOCK-EXPECTED", Location{Document: DocumentLock})
		return collected.report(""), nil
	}
	lock := readLock(in.Lock, in.Expect, collected)
	if lock == nil {
		return collected.report(""), nil
	}
	// Step 2: the registry, validated by registry §7 with the declarations
	// given.
	result := registry.ValidateDocuments(in.Registry, in.Kinds)
	for _, d := range result.Report.Diagnostics {
		collected.insert(fromRegistry(d, ""))
	}
	digest := ""
	if result.Report.Registry != nil {
		digest = result.Report.Registry.Digest
	}
	if !result.Report.Valid {
		collected.add("LOCK-REGISTRY-INVALID", Location{Document: registry.DocumentRegistry})
		return collected.report(digest), nil
	}
	root, _ := registry.AdmitDocument(in.Registry.Data, MaxRegistryBytes, "registryFormatVersion", registry.RegistryFormatVersion, registry.Location{Document: registry.DocumentRegistry})
	checkRegistry(lock, root, digest, collected)
	checkKinds(lock, root, collected)
	if err := checkResolved(lock, result, collected); err != nil {
		return Report{}, err
	}
	checkIndex(lock, in.Index, collected)
	checkEmitters(lock, in.Emitters, keys, files, collected)
	return collected.report(digest), nil
}

// checkRegistry is step 3: the lock's id, version and digest are the
// registry's.
func checkRegistry(lock, root map[string]any, digest string, collected *collector) {
	named := lock["registry"].(map[string]any)
	if root == nil || named["id"] != root["id"] || named["version"] != root["version"] || named["digest"] != digest {
		collected.add("LOCK-REGISTRY-MISMATCH", Location{Document: DocumentLock, Pointer: "/registry"})
	}
}

// checkKinds is step 4: the lock's kinds are the registry's pins, item for
// item, at the first item that differs or at /kinds when the lengths do.
func checkKinds(lock, root map[string]any, collected *collector) {
	locked := lock["kinds"].([]any)
	pins, _ := root["kinds"].([]any)
	if len(locked) != len(pins) {
		collected.add("LOCK-KINDS-MISMATCH", Location{Document: DocumentLock, Pointer: "/kinds"})
		return
	}
	for i := range locked {
		if !registry.JSONEqual(locked[i], pins[i]) {
			collected.add("LOCK-KINDS-MISMATCH", Location{Document: DocumentLock, Pointer: "/kinds/" + strconv.Itoa(i)})
			return
		}
	}
}

// checkResolved is step 5: the lock's environments are the registry's, in
// order, and each digest is that of the registry resolved there.
func checkResolved(lock map[string]any, result *registry.Result, collected *collector) error {
	locked := lock["resolved"].([]any)
	environments := result.Environments()
	for i := 0; i < max(len(locked), len(environments)); i++ {
		if i >= len(locked) {
			collected.add("LOCK-RESOLVED-MISMATCH", Location{Document: DocumentLock, Pointer: "/resolved"})
			continue
		}
		at := Location{Document: DocumentLock, Pointer: "/resolved/" + strconv.Itoa(i)}
		item := locked[i].(map[string]any)
		if i >= len(environments) || item["environment"] != environments[i] {
			collected.add("LOCK-RESOLVED-MISMATCH", at)
			continue
		}
		resolved, err := result.Resolve(environments[i])
		if err != nil {
			return err
		}
		if item["digest"] != FileDigest(resolved) {
			collected.add("LOCK-RESOLVED-MISMATCH", at)
		}
	}
	return nil
}

// checkIndex is steps 6 and 7: the index a lock names is given, admitted, is
// the one it names, names its registry and has exactly one standard item;
// and the lock's sources are the index's, each statement digest that of the
// statement rebuilt from the index's item. An index given for a lock that
// names none is refused, so that no caller believes provenance was checked.
func checkIndex(lock map[string]any, index *registry.Document, collected *collector) {
	named, hasIndex := lock["index"].(map[string]any)
	switch {
	case !hasIndex && index != nil:
		collected.add("LOCK-INDEX-UNLOCKED", Location{Document: DocumentIndex})
		return
	case !hasIndex:
		return
	case index == nil:
		collected.add("LOCK-INDEX-MISSING", Location{Document: DocumentLock, Pointer: "/index"})
		return
	case index.Oversize:
		collected.addRegistry("REG-DOC-SIZE", Location{Document: DocumentIndex})
		return
	}
	given, refusals := registry.AdmitDocument(index.Data, MaxIndexBytes, "indexFormatVersion", IndexFormatVersion, registry.Location{Document: DocumentIndex})
	for _, d := range refusals {
		collected.insert(fromRegistry(d, ""))
	}
	if given == nil {
		if len(refusals) == 0 {
			collected.failed()
		}
		return
	}
	mismatch := func(document, pointer string) {
		collected.add("LOCK-INDEX-MISMATCH", Location{Document: document, Pointer: pointer})
	}
	if digest, err := registry.Digest(given); err != nil || digest != named["digest"] {
		mismatch(DocumentLock, "/index")
		return
	}
	if lookupValue(given, "registry", "digest") != lookupValue(lock, "registry", "digest") {
		mismatch(DocumentIndex, "/registry/digest")
		return
	}
	items, _ := given["sources"].([]any)
	standards := 0
	for _, item := range items {
		if object, isObject := item.(map[string]any); isObject && object["role"] == RoleStandard {
			standards++
		}
	}
	if standards != 1 {
		mismatch(DocumentIndex, "/sources")
		return
	}
	locked := lock["sources"].([]any)
	for i := 0; i < max(len(locked), len(items)); i++ {
		if i >= len(locked) {
			collected.add("LOCK-SOURCES-MISMATCH", Location{Document: DocumentLock, Pointer: "/sources"})
			break
		}
		mine := locked[i].(map[string]any)
		var item map[string]any
		if i < len(items) {
			item, _ = items[i].(map[string]any)
		}
		if item == nil || mine["name"] != item["name"] || mine["role"] != item["role"] || mine["repository"] != item["repository"] ||
			mine["statement"] != statementDigest(item) {
			collected.add("LOCK-SOURCES-MISMATCH", Location{Document: DocumentLock, Pointer: "/sources/" + strconv.Itoa(i)})
		}
	}
}

// checkEmitters is step 8: a lock that names emitters is checked with them;
// each manifest and file it lists is given and equal to its item, and no
// file a manifest's owns matches is absent from its item.
func checkEmitters(lock map[string]any, emitters []Emitter, keys map[emitterKey]int, files []map[string]EmitterFile, collected *collector) {
	locked, named := lock["emitters"].([]any)
	if !named {
		return
	}
	if len(emitters) == 0 {
		collected.add("LOCK-EMITTER-MISSING", Location{Document: DocumentLock, Pointer: "/emitters"})
		return
	}
	for i, raw := range locked {
		item := raw.(map[string]any)
		at := "/emitters/" + strconv.Itoa(i)
		source, _ := item["source"].(string)
		position, given := keys[emitterKey{source, item["path"].(string)}]
		if !given {
			collected.add("LOCK-EMITTER-MISSING", Location{Document: DocumentLock, Pointer: at})
			continue
		}
		e := emitters[position]
		digest, owns, readable := emitterManifest(e, collected)
		if !readable || digest != item["digest"] {
			collected.add("LOCK-EMITTER-MISMATCH", Location{Document: DocumentLock, Pointer: at})
		}
		listed := map[string]bool{}
		for k, rawFile := range item["files"].([]any) {
			f := rawFile.(map[string]any)
			path := f["path"].(string)
			listed[path] = true
			file, given := files[position][path]
			switch {
			case !given:
				collected.add("LOCK-EMITTER-MISSING", Location{Document: DocumentLock, Pointer: at + "/files/" + strconv.Itoa(k)})
			case file.Mode != f["mode"] || file.Digest != f["digest"]:
				collected.add("LOCK-EMITTER-MISMATCH", Location{Document: DocumentLock, Pointer: at + "/files/" + strconv.Itoa(k)})
			}
		}
		for _, file := range e.Files {
			if !listed[file.Path] && ownedBy(owns, file.Path) {
				collected.add("LOCK-EMITTER-MISMATCH", Location{Document: DocumentLock, Pointer: at})
			}
		}
	}
}

// CoverageInput is what coverage is given (§9.1): the lock, the expected
// inputs with the environment, and a resolved registry, a generated file, or
// both. Each nil is not given.
type CoverageInput struct {
	Lock     registry.Document
	Expect   Expect
	Resolved *registry.Document
	File     *CoveredFile
}

// CoveredFile is a generated file a consumer asks the lock about: the source
// whose repository holds it in an assembled lock ("" otherwise), its path,
// its mode and its digest (FileDigest of its bytes).
type CoveredFile struct {
	Source string
	Path   string
	Mode   string
	Digest string
}

// Coverage runs §9.1: whether a lock, first held to §9's step 1, covers the
// resolved registry and the file given, for the registry id and environment
// expected. It establishes byte identity with what the lock names and
// re-derives nothing; a consumer verifies the lock (§11) first, and computes
// coverage over the bytes verification accepted. Its report names no
// registry. The error is an *InputError when neither a resolved registry nor
// a file is given.
func Coverage(in CoverageInput) (Report, error) {
	if in.Resolved == nil && in.File == nil {
		return Report{}, &InputError{Reason: "coverage is given neither a resolved registry nor a file"}
	}
	collected := &collector{}
	if in.Expect.missing(true) {
		collected.add("LOCK-EXPECTED", Location{Document: DocumentLock})
		return collected.report(""), nil
	}
	lock := readLock(in.Lock, in.Expect, collected)
	if lock == nil {
		return collected.report(""), nil
	}
	if in.Resolved != nil && !coversResolved(lock, *in.Expect.Environment, *in.Resolved) {
		at := "/resolved"
		for i, item := range lock["resolved"].([]any) {
			if item.(map[string]any)["environment"] == *in.Expect.Environment {
				at += "/" + strconv.Itoa(i)
				break
			}
		}
		collected.add("LOCK-NOT-COVERED", Location{Document: DocumentLock, Pointer: at})
	}
	if in.File != nil && !coversFile(lock, *in.File) {
		collected.add("LOCK-NOT-COVERED", Location{Document: DocumentLock, Pointer: "/emitters"})
	}
	return collected.report(""), nil
}

// coversResolved: a resolved registry is covered exactly when its
// environment is the expected one, its registry digest is the lock's, and
// its digest is the lock's resolved digest for that environment.
func coversResolved(lock map[string]any, environment string, resolved registry.Document) bool {
	if resolved.Oversize {
		return false
	}
	root, refusals := registry.AdmitDocument(resolved.Data, registry.MaxResolvedBytes, "resolvedFormatVersion", registry.ResolvedFormatVersion, registry.Location{Document: registry.DocumentRegistry})
	if root == nil || len(refusals) > 0 || root["environment"] != environment ||
		lookupValue(root, "registry", "digest") != lookupValue(lock, "registry", "digest") {
		return false
	}
	digest, err := registry.Digest(root)
	if err != nil {
		return false
	}
	for _, item := range lock["resolved"].([]any) {
		object := item.(map[string]any)
		if object["environment"] == environment {
			return object["digest"] == digest
		}
	}
	return false
}

// coversFile: a file is covered exactly when an emitters item, of its source
// in an assembled lock, lists its path with its mode and digest.
func coversFile(lock map[string]any, file CoveredFile) bool {
	_, assembled := lock["index"]
	items, _ := lock["emitters"].([]any)
	for _, raw := range items {
		item := raw.(map[string]any)
		if assembled && item["source"] != file.Source {
			continue
		}
		for _, f := range item["files"].([]any) {
			listed := f.(map[string]any)
			if listed["path"] == file.Path && listed["mode"] == file.Mode && listed["digest"] == file.Digest {
				return true
			}
		}
	}
	return false
}
