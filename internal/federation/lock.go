package federation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"regexp"
	"strconv"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// MaxSerial is the highest serial a lock may hold (§7): 2^53 − 1. A writer
// whose previous lock holds it has no next serial (§8).
const MaxSerial = 1<<53 - 1

// The modes a locked file may have (§7), a response's (registry §11.3).
const (
	ModeRegular    = emit.ModeRegular
	ModeExecutable = emit.ModeExecutable
)

// The bounds of a lock's lists (§7, and the closed schema): a lock names
// each environment of its registry, which has at most MaxEnvironments, and
// each source of its index.
const (
	maxLockResolved = MaxEnvironments
	maxLockSources  = MaxSources
)

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// lockMembers are the members a lock may have (§7); lockRequired are those it
// must, beside its selector, which admission holds.
var (
	lockMembers = map[string]bool{
		"lockFormatVersion": true, "serial": true, "registry": true, "kinds": true, "resolved": true,
		"index": true, "sources": true, "emitters": true,
	}
	lockRequired = []string{"serial", "registry", "kinds", "resolved"}
)

// Expect is §9's expected inputs, given out of band every time: the registry
// id the caller expects, whether it requires an index and, for coverage
// (§9.1), the environment it serves. A nil member is not given. None has a
// default, and none is taken from a lock, a registry or a signature document.
type Expect struct {
	RegistryID   *string
	RequireIndex *bool
	Environment  *string
}

// missing reports whether an expected input is missing or not of its form:
// the registry id, which is an absolute URI, the index requirement, and for
// coverage the environment. Each is LOCK-EXPECTED, before anything is read.
func (e Expect) missing(coverage bool) bool {
	if e.RegistryID == nil || !registry.IsAbsoluteURI(*e.RegistryID) || e.RequireIndex == nil {
		return true
	}
	return coverage && (e.Environment == nil || !registry.ValidEnvironment(*e.Environment))
}

// Emitter is one emitter manifest (registry §11.1) given to a lock writer or
// a lock checker, with the files beside it.
type Emitter struct {
	// Source is the name of the source whose repository holds the manifest,
	// in an assembly; it is "" for a single registry.
	Source string
	// Path is the manifest's path in its repository (registry §11.3's form).
	Path string
	// Manifest is the manifest's bytes.
	Manifest registry.Document
	// Files are the files given beside it: for a writer, those on disk that
	// its owns matches; for a checker, the same, to be held to the lock.
	Files []EmitterFile
	// Check is, for a writer, the exit code of the host in check mode
	// (registry §11.4) against this registry. A writer requires it; a
	// checker reads no outcome.
	Check *int
}

// EmitterFile is one file given beside an emitter manifest: its path in the
// repository, its mode ("0644" or "0755") and its digest, FileDigest of its
// exact bytes.
type EmitterFile struct {
	Path   string
	Mode   string
	Digest string
}

// FileDigest is a file's digest in a lock (§7): "sha256:" and the SHA-256 of
// its exact bytes, in lowercase hexadecimal.
func FileDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// InputError refuses a lock operation whose inputs, as given, the text does
// not let it run on, and for which the text names no code: a first serial
// and a previous lock both given or neither (there is no default), an
// emitter given twice, or one with no host check outcome. It names the input
// by its kind and position, never by anything a document holds.
type InputError struct {
	Reason string
}

func (e *InputError) Error() string { return "lock: " + e.Reason }

// serialOf reads a serial (§7): a number with an integer value from 1 to
// MaxSerial.
func serialOf(value any) (int64, bool) {
	rat, ok := ratOf(value)
	if !ok || !rat.IsInt() || rat.Sign() <= 0 || rat.Cmp(new(big.Rat).SetInt64(MaxSerial)) > 0 {
		return 0, false
	}
	return rat.Num().Int64(), true
}

// lockForm is §7's form, which the closed schema states: the pointers at
// which a lock is not of it. It does not hold the lock to its canonical
// bytes, its registry or its index; the check does.
func lockForm(v map[string]any) []string {
	var out []string
	for _, name := range sortedKeys(v) {
		if !lockMembers[name] {
			out = append(out, carrier.Pointer([]string{name}))
		}
	}
	for _, name := range lockRequired {
		if _, ok := v[name]; !ok {
			out = append(out, "/"+name)
		}
	}
	if value, ok := v["serial"]; ok {
		if _, ok := serialOf(value); !ok {
			out = append(out, "/serial")
		}
	}
	if value, ok := v["registry"]; ok && !registryIdentityForm(value) {
		out = append(out, "/registry")
	}
	if value, ok := v["kinds"]; ok {
		list, isList := value.([]any)
		if !isList {
			out = append(out, "/kinds")
		}
		for i, pin := range list {
			if !pinForm(pin) {
				out = append(out, "/kinds/"+strconv.Itoa(i))
			}
		}
	}
	if value, ok := v["resolved"]; ok {
		list, isList := value.([]any)
		if !isList || len(list) < 1 || len(list) > maxLockResolved {
			out = append(out, "/resolved")
		}
		for i, item := range list {
			object, isObject := item.(map[string]any)
			environment, _ := object["environment"].(string)
			if !isObject || !exactly(object, "environment", "digest") || !registry.ValidEnvironment(environment) || !isDigest(object["digest"]) {
				out = append(out, "/resolved/"+strconv.Itoa(i))
			}
		}
	}
	_, hasIndex := v["index"]
	_, hasSources := v["sources"]
	if hasIndex {
		object, isObject := v["index"].(map[string]any)
		if !isObject || !exactly(object, "digest") || !isDigest(object["digest"]) {
			out = append(out, "/index")
		}
	}
	switch {
	case hasIndex && !hasSources:
		out = append(out, "/sources")
	case hasSources && !hasIndex:
		out = append(out, "/index")
	}
	if hasSources {
		list, isList := v["sources"].([]any)
		if !isList || len(list) < 1 || len(list) > maxLockSources {
			out = append(out, "/sources")
		}
		for i, item := range list {
			object, isObject := item.(map[string]any)
			name, _ := object["name"].(string)
			role, _ := object["role"].(string)
			if !isObject || !exactly(object, "name", "role", "repository", "statement", "signature") ||
				!registry.ValidKindName(name) || (role != RoleStandard && role != RoleRepository) ||
				!validRepository(object["repository"]) || !isDigest(object["statement"]) || !isDigest(object["signature"]) {
				out = append(out, "/sources/"+strconv.Itoa(i))
			}
		}
	}
	if value, ok := v["emitters"]; ok {
		out = append(out, emittersForm(value, hasIndex)...)
	}
	return out
}

// emittersForm is the form of a lock's emitters (§7): each item names its
// manifest's path and digest and the files its owns matches, sorted by path,
// and its source exactly in an assembled lock; the items are sorted by
// source and then path, and no two share both.
func emittersForm(value any, assembled bool) []string {
	list, isList := value.([]any)
	if !isList {
		return []string{"/emitters"}
	}
	var out []string
	var keys [][2]string
	for i, item := range list {
		at := "/emitters/" + strconv.Itoa(i)
		object, isObject := item.(map[string]any)
		path, _ := object["path"].(string)
		files, filesList := object["files"].([]any)
		members := []string{"path", "digest", "files"}
		source := ""
		if assembled {
			members = append(members, "source")
			source, _ = object["source"].(string)
		}
		if !isObject || !exactly(object, members...) || !validPath(path) || !isDigest(object["digest"]) || !filesList ||
			(assembled && !registry.ValidKindName(source)) {
			out = append(out, at)
			continue
		}
		keys = append(keys, [2]string{source, path})
		var paths []string
		for k, file := range files {
			f, isObject := file.(map[string]any)
			filePath, _ := f["path"].(string)
			mode, _ := f["mode"].(string)
			if !isObject || !exactly(f, "path", "mode", "digest") || !validPath(filePath) ||
				(mode != ModeRegular && mode != ModeExecutable) || !isDigest(f["digest"]) {
				out = append(out, at+"/files/"+strconv.Itoa(k))
				continue
			}
			paths = append(paths, filePath)
		}
		for k := 1; k < len(paths); k++ {
			if paths[k-1] >= paths[k] {
				out = append(out, at+"/files")
				break
			}
		}
	}
	for k := 1; k < len(keys); k++ {
		if keys[k-1][0] > keys[k][0] || keys[k-1][0] == keys[k][0] && keys[k-1][1] >= keys[k][1] {
			out = append(out, "/emitters")
			break
		}
	}
	return out
}

func registryIdentityForm(value any) bool {
	object, isObject := value.(map[string]any)
	id, _ := object["id"].(string)
	version, _ := object["version"].(string)
	return isObject && exactly(object, "id", "version", "digest") && registry.IsAbsoluteURI(id) &&
		registry.ValidVersion(version) && isDigest(object["digest"])
}

// pinForm is a pin of registry §4: kind, id, version and digest.
func pinForm(value any) bool {
	object, isObject := value.(map[string]any)
	kind, _ := object["kind"].(string)
	id, _ := object["id"].(string)
	version, _ := object["version"].(string)
	return isObject && exactly(object, "kind", "id", "version", "digest") && registry.ValidKindName(kind) &&
		registry.IsAbsoluteURI(id) && registry.ValidVersion(version) && isDigest(object["digest"])
}

// exactly reports whether object has these members and no other.
func exactly(object map[string]any, members ...string) bool {
	if len(object) != len(members) {
		return false
	}
	for _, name := range members {
		if _, ok := object[name]; !ok {
			return false
		}
	}
	return true
}

func isDigest(value any) bool {
	text, isString := value.(string)
	return isString && digestPattern.MatchString(text)
}

// readLock runs §9's step 1 on a lock, given the expected inputs: its
// admission, its form (LOCK-FORM), its canonical bytes (LOCK-NOT-CANONICAL),
// the expected registry id (LOCK-UNEXPECTED-REGISTRY) and the index
// requirement (LOCK-INDEX-REQUIRED). It returns the lock, or nil when the
// step fails; a failure ends the check.
func readLock(lock registry.Document, expect Expect, collected *collector) map[string]any {
	failed := false
	at := func(code, pointer string) {
		failed = true
		collected.add(code, Location{Document: DocumentLock, Pointer: pointer})
	}
	if lock.Oversize {
		collected.addRegistry("REG-DOC-SIZE", Location{Document: DocumentLock})
		return nil
	}
	root, refusals := registry.AdmitDocument(lock.Data, MaxLockBytes, "lockFormatVersion", LockFormatVersion, registry.Location{Document: DocumentLock})
	for _, d := range refusals {
		collected.insert(fromRegistry(d, ""))
	}
	if root == nil {
		if len(refusals) == 0 {
			collected.failed()
		}
		return nil
	}
	if bad := lockForm(root); len(bad) > 0 {
		for _, pointer := range bad {
			at("LOCK-FORM", pointer)
		}
		return nil
	}
	if !canonicalLock(lock.Data, root) {
		at("LOCK-NOT-CANONICAL", "")
	}
	if id, _ := lookupValue(root, "registry", "id").(string); id != *expect.RegistryID {
		at("LOCK-UNEXPECTED-REGISTRY", "/registry/id")
	}
	if _, named := root["index"]; *expect.RequireIndex && !named {
		at("LOCK-INDEX-REQUIRED", "/index")
	}
	if failed {
		return nil
	}
	return root
}

// canonicalLock reports whether a lock's bytes, without at most one final
// line feed, are its JCS bytes (§7).
func canonicalLock(data []byte, root map[string]any) bool {
	canonical, err := jcs.EncodeDocument(root)
	return err == nil && bytes.Equal(canonical, bytes.TrimSuffix(data, []byte("\n")))
}

// LockDigest is a lock's digest (§7): the SHA-256 of its bytes without one
// final line feed, which for a lock of its canonical bytes is its registry
// §2.3 identity.
func LockDigest(data []byte) string {
	return FileDigest(bytes.TrimSuffix(data, []byte("\n")))
}

// emitterManifest admits a given emitter manifest and reads it (registry
// §11.1). It returns the manifest's digest and its owns globs, and ok false
// when it is not admitted, its admission diagnostics collected (document
// emitter, with its source), or when it is admitted and is no emitter
// manifest: no host check of it exits 0, so no lock can name it.
func emitterManifest(e Emitter, collected *collector) (digest string, owns []emit.Glob, ok bool) {
	location := Location{Document: DocumentEmitter, Source: e.Source}
	if e.Manifest.Oversize {
		collected.addRegistry("REG-DOC-SIZE", location)
		return "", nil, false
	}
	root, refusals := registry.AdmitDocument(e.Manifest.Data, emit.MaxManifestBytes, "emitterManifestVersion", emit.ManifestVersion, registry.Location{Document: DocumentEmitter})
	for _, d := range refusals {
		collected.insert(fromRegistry(d, e.Source))
	}
	if root == nil {
		if len(refusals) == 0 {
			collected.failed()
		}
		return "", nil, false
	}
	digest, err := registry.Digest(root)
	if err != nil {
		return "", nil, false
	}
	manifest, _ := emit.ReadManifest(e.Manifest.Data)
	if manifest == nil {
		return digest, nil, false
	}
	return digest, manifest.Owns, true
}

// ownedBy reports whether an owns glob matches path.
func ownedBy(owns []emit.Glob, path string) bool {
	for _, glob := range owns {
		if glob.Match(path) {
			return true
		}
	}
	return false
}

// emitterKey is an emitter's place in a lock: its source and its path.
type emitterKey struct{ source, path string }

// givenEmitters indexes the emitters given by source and path, and each one's
// files by path. An emitter or a file given twice is an input error: the
// text compares one manifest, and one file, with each item.
func givenEmitters(emitters []Emitter) (map[emitterKey]int, []map[string]EmitterFile, error) {
	keys := map[emitterKey]int{}
	files := make([]map[string]EmitterFile, len(emitters))
	for i, e := range emitters {
		key := emitterKey{e.Source, e.Path}
		if _, repeated := keys[key]; repeated {
			return nil, nil, &InputError{Reason: fmt.Sprintf("emitter %d is given twice", i+1)}
		}
		keys[key] = i
		files[i] = map[string]EmitterFile{}
		for _, f := range e.Files {
			if _, repeated := files[i][f.Path]; repeated {
				return nil, nil, &InputError{Reason: fmt.Sprintf("emitter %d is given one file twice", i+1)}
			}
			files[i][f.Path] = f
		}
	}
	return keys, files, nil
}

// statement is §10.2's source statement of an index's source item: its
// repository, commit, and every document's path, role, content digest and
// digest, in the index's order. ok is false for an item that has not those
// members.
func statement(item any) (map[string]any, bool) {
	object, isObject := item.(map[string]any)
	repository, hasRepository := object["repository"]
	commit, hasCommit := object["commit"]
	documents, isList := object["documents"].([]any)
	if !isObject || !hasRepository || !hasCommit || !isList {
		return nil, false
	}
	out := make([]any, 0, len(documents))
	for _, document := range documents {
		d, isObject := document.(map[string]any)
		if !isObject {
			return nil, false
		}
		kept := map[string]any{}
		for _, name := range []string{"path", "role", "contentDigest", "digest"} {
			value, present := d[name]
			if !present {
				return nil, false
			}
			kept[name] = value
		}
		out = append(out, kept)
	}
	return map[string]any{"repository": repository, "commit": commit, "documents": out}, true
}

// statementDigest is the digest (registry §2.3) of an index item's
// statement, or "" for an item that has none.
func statementDigest(item any) string {
	s, ok := statement(item)
	if !ok {
		return ""
	}
	digest, err := registry.Digest(s)
	if err != nil {
		return ""
	}
	return digest
}
