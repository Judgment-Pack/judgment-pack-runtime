package federation

import (
	"strconv"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/regsig"
)

// VerifyInput is what a lock signature verifier is given (§11, Verifying):
// the lock's bytes and its signature documents in the order given, the lock
// signature first; for an assembled lock its index and the source signature
// documents; the expected signer set and the expected inputs of §9; its
// serial record store and a floor. With Registry, the rest of §9 is checked
// with Kinds and Emitters; with Resolved or File, coverage (§9.1). None has a
// default, and none is taken from a lock or a document. A signature document
// is given as a reader, called only when the verification reads it: one
// beyond the count, or a lock signature after the first, is never read.
type VerifyInput struct {
	Lock             registry.Document
	LockSignatures   []GivenDocument
	SourceSignatures []GivenDocument
	Index            *registry.Document
	Signers          *Signers
	Expect           Expect
	// Floor is the lowest serial the verifier may accept, from 1 to
	// 2^53-1; nil gives none.
	Floor    *int64
	Records  *RecordStore
	Registry *registry.Document
	Kinds    []registry.Document
	Emitters []Emitter
	Resolved *registry.Document
	File     *CoveredFile
}

// GivenDocument reads a document given to a verifier. The verification calls
// it once, when it reaches the document, and never for one it does not read;
// an error it returns ends the verification and is Verify's error.
type GivenDocument func() (registry.Document, error)

// Held gives a document already read.
func Held(document registry.Document) GivenDocument {
	return func() (registry.Document, error) { return document, nil }
}

// Verification is a verification's result: the report, and when it holds
// LOCK-SERIAL-STATE, why the serial record could not be read, written or
// read back, naming the file, which the report does not carry.
type Verification struct {
	Report        Report
	RecordProblem string
}

// Verify runs §11's verification, and records the lock's serial and digest
// for the expected registry id when every step accepts it. A refused lock
// changes no record. The error is an *InputError for inputs that give no
// verification (no record store, a signature document with no reader, or two
// emitters at one path), the error a signature document's reader returned, or
// reports a report that could not be written in its canonical form.
func Verify(in VerifyInput) (Verification, error) {
	if in.Records == nil {
		return Verification{}, &InputError{Reason: "verification is given no serial record store"}
	}
	for _, given := range append(append([]GivenDocument(nil), in.LockSignatures...), in.SourceSignatures...) {
		if given == nil {
			return Verification{}, &InputError{Reason: "a signature document is given no reader"}
		}
	}
	keys, files, err := givenEmitters(in.Emitters)
	if err != nil {
		return Verification{}, err
	}
	v := &verification{in: in, collected: &collector{}}
	if err := v.run(keys, files); err != nil {
		return Verification{}, err
	}
	return Verification{Report: v.collected.report(v.registryDigest), RecordProblem: v.recordProblem}, nil
}

type verification struct {
	in             VerifyInput
	collected      *collector
	registryDigest string
	recordProblem  string
}

// state is LOCK-SERIAL-STATE, at the lock's root, for a record problem.
func (v *verification) state(problem *RecordError) {
	v.collected.add("LOCK-SERIAL-STATE", Location{Document: DocumentLock})
	v.recordProblem = problem.Error()
}

// signature records a signature document's failures, each with the
// document's key identifier when it has one.
func (v *verification) signature(document *signatureDocument, problems ...signatureProblem) {
	keyID := ""
	if document != nil {
		keyID = document.keyID
	}
	for _, p := range problems {
		v.collected.add(p.code, Location{Document: DocumentSignature, KeyID: keyID, Pointer: p.pointer})
	}
}

// unread is LOCK-SIG-EXTRA for a document refused before it is read, which
// names no key.
func (v *verification) unread() {
	v.collected.add("LOCK-SIG-EXTRA", Location{Document: DocumentSignature})
}

func (v *verification) run(keys map[emitterKey]int, files []map[string]EmitterFile) error {
	in, collected := v.in, v.collected
	root := Location{Document: DocumentLock}

	// Step 1: the expected inputs, and the record, before the lock or any
	// document is read.
	for _, code := range verifierSignerProblems(in.Signers, in.Expect) {
		collected.add(code, root)
	}
	if in.Expect.missing(in.Resolved != nil || in.File != nil) {
		collected.add("LOCK-EXPECTED", root)
	}
	floor := in.Floor
	if floor != nil && (*floor < 1 || *floor > MaxSerial) {
		collected.add("LOCK-EXPECTED", root)
		floor = nil
	}
	var first *Record
	recordRead := true
	if in.Expect.RegistryID != nil && registry.IsAbsoluteURI(*in.Expect.RegistryID) {
		record, problem := in.Records.read(*in.Expect.RegistryID)
		if problem != nil {
			v.state(problem)
			recordRead = false
		}
		first = record
	}
	if recordRead && first == nil && in.Floor == nil {
		// First contact needs a floor: trust on first use is chosen by
		// giving a floor of 1, never by default.
		collected.add("LOCK-EXPECTED", root)
	}
	if collected.hasErrors() {
		return nil
	}

	// Step 2: the lock signature, before the lock's bytes are parsed. Only
	// the first document is read; each after it is LOCK-SIG-EXTRA, unread.
	if len(in.LockSignatures) == 0 {
		collected.add("LOCK-SIG-MISSING", root)
		return nil
	}
	for range in.LockSignatures[1:] {
		v.unread()
	}
	given, err := in.LockSignatures[0]()
	if err != nil {
		return err
	}
	lockSignature, problems := readSignature(given, regsig.Lock)
	v.signature(lockSignature, problems...)
	// A lock over its bound has no bytes held to check the signature
	// against: step 3 refuses it by its size.
	if len(problems) == 0 && !in.Lock.Oversize {
		if failure := lockSignature.check(in.Signers.Key, LockDigest(in.Lock.Data)); failure != nil {
			v.signature(lockSignature, *failure)
		}
	}
	if collected.hasErrors() {
		return nil
	}

	// Step 3: the lock, held to §9's step 1.
	lock := readLock(in.Lock, in.Expect, collected)
	if lock == nil {
		return nil
	}
	digest := LockDigest(in.Lock.Data)

	// Step 4: the serial, against the record or the floor.
	serial, _ := serialOf(lock["serial"])
	if code := serialRule(serial, digest, first, floor); code != "" {
		collected.add(code, Location{Document: DocumentLock, Pointer: "/serial"})
	}

	// Step 5: the index, held to §9's steps 6 and 7.
	checkIndex(lock, in.Index, collected)

	// Step 6: the source signatures.
	if err := v.sources(lock); err != nil {
		return err
	}

	// Step 7: the rest of §9 with the documents, and coverage.
	if in.Registry != nil {
		if err := v.rest(lock, keys, files); err != nil {
			return err
		}
	}
	if in.Resolved != nil || in.File != nil {
		checkCoverage(lock, in.Expect, in.Resolved, in.File, collected)
	}
	if collected.hasErrors() {
		return nil
	}

	// Recording: only a lock every step accepts, by compare and set against
	// the record step 1 read.
	if hook := currentRecordHooks().BeforeRecording; hook != nil {
		hook()
	}
	code, problem := in.Records.record(*in.Expect.RegistryID, first, serial, digest, floor)
	switch {
	case problem != nil:
		v.state(problem)
	case code != "":
		collected.add(code, Location{Document: DocumentLock, Pointer: "/serial"})
	}
	return nil
}

// sources is step 6. Counted in the order given, the lock signature first,
// the documents beyond the lock's sources count plus one are LOCK-SIG-EXTRA
// and never read. Each document read is matched to the item whose signature
// is its digest; an item no document matches is LOCK-SIG-MISSING, and a
// document that matches none, or a second for one item, LOCK-SIG-EXTRA. Each
// matched document is checked against the key the set names for its item
// and the digest of the statement rebuilt from the index's item of its name,
// never the lock's statement member. A document beyond the count is never
// read; the error is one a document's reader returned.
func (v *verification) sources(lock map[string]any) error {
	in, collected := v.in, v.collected
	items, _ := lock["sources"].([]any)
	room := max(0, len(items)+1-len(in.LockSignatures))
	given := in.SourceSignatures
	if len(given) > room {
		for range given[room:] {
			v.unread()
		}
		given = given[:room]
	}
	matched := map[int]*signatureDocument{}
	for _, reader := range given {
		document, err := reader()
		if err != nil {
			return err
		}
		read, problems := readSignature(document, regsig.Source)
		if len(problems) > 0 {
			v.signature(read, problems...)
			continue
		}
		target := -1
		for i, item := range items {
			if item.(map[string]any)["signature"] == read.identity {
				target = i
				break
			}
		}
		if _, taken := matched[target]; target < 0 || taken {
			v.signature(read, signatureProblem{"LOCK-SIG-EXTRA", ""})
			continue
		}
		matched[target] = read
	}
	repositories := map[string]string{}
	if in.Signers.Repositories != nil {
		repositories = in.Signers.Repositories
	}
	byName := indexItems(in.Index)
	for i, raw := range items {
		item := raw.(map[string]any)
		at := Location{Document: DocumentLock, Pointer: "/sources/" + strconv.Itoa(i)}
		repository, _ := item["repository"].(string)
		key, named := repositories[repository]
		if item["role"] == RoleStandard {
			// The standard source is signed by the standard owner's key
			// alone.
			if named {
				collected.add("LOCK-EXPECTED", at)
				continue
			}
			key = in.Signers.Key
		} else if !named {
			collected.add("LOCK-EXPECTED", at)
			continue
		}
		document, found := matched[i]
		if !found {
			collected.add("LOCK-SIG-MISSING", at)
			continue
		}
		name, _ := item["name"].(string)
		indexItem, indexed := byName[name]
		if !indexed {
			// The index is missing, not admitted, or names no such
			// source: step 5 has refused the lock.
			continue
		}
		if failure := document.check(key, statementDigest(indexItem)); failure != nil {
			v.signature(document, *failure)
		}
	}
	return nil
}

// indexItems are an admitted index's source items by name.
func indexItems(index *registry.Document) map[string]any {
	out := map[string]any{}
	if index == nil || index.Oversize {
		return out
	}
	root, _ := registry.AdmitDocument(index.Data, MaxIndexBytes, "indexFormatVersion", IndexFormatVersion, registry.Location{Document: DocumentIndex})
	items, _ := root["sources"].([]any)
	for _, raw := range items {
		if item, isObject := raw.(map[string]any); isObject {
			if name, isString := item["name"].(string); isString {
				if _, seen := out[name]; !seen {
					out[name] = item
				}
			}
		}
	}
	return out
}

// rest is §9's steps 2 to 5 and 8 with the documents given: the registry,
// validated, its kinds, each environment's resolved digest, and the emitters.
func (v *verification) rest(lock map[string]any, keys map[emitterKey]int, files []map[string]EmitterFile) error {
	in, collected := v.in, v.collected
	result := registry.ValidateDocuments(*in.Registry, in.Kinds)
	for _, d := range result.Report.Diagnostics {
		collected.insert(fromRegistry(d, ""))
	}
	if result.Report.Registry != nil {
		v.registryDigest = result.Report.Registry.Digest
	}
	if !result.Report.Valid {
		collected.add("LOCK-REGISTRY-INVALID", Location{Document: registry.DocumentRegistry})
		return nil
	}
	root, _ := registry.AdmitDocument(in.Registry.Data, MaxRegistryBytes, "registryFormatVersion", registry.RegistryFormatVersion, registry.Location{Document: registry.DocumentRegistry})
	checkRegistry(lock, root, v.registryDigest, collected)
	checkKinds(lock, root, collected)
	if err := checkResolved(lock, result, collected); err != nil {
		return err
	}
	checkEmitters(lock, in.Emitters, keys, files, collected)
	return nil
}

// checkCoverage is §9.1 over a lock already read: a resolved registry, and a
// file, each covered or LOCK-NOT-COVERED.
func checkCoverage(lock map[string]any, expect Expect, resolved *registry.Document, file *CoveredFile, collected *collector) {
	if resolved != nil && !coversResolved(lock, *expect.Environment, *resolved) {
		at := "/resolved"
		for i, item := range lock["resolved"].([]any) {
			if item.(map[string]any)["environment"] == *expect.Environment {
				at += "/" + strconv.Itoa(i)
				break
			}
		}
		collected.add("LOCK-NOT-COVERED", Location{Document: DocumentLock, Pointer: at})
	}
	if file != nil && !coversFile(lock, *file) {
		collected.add("LOCK-NOT-COVERED", Location{Document: DocumentLock, Pointer: "/emitters"})
	}
}

// verifierSignerProblems is the signer set's part of step 1: an expected
// key missing or refused by the trust rule is LOCK-SIG-EXPECTED-KEY; a map
// given with no index required, or an index required with no map, a map that
// gives a repository the standard owner's key, or one key to two repository
// identities, keys compared as 32 bytes, is LOCK-EXPECTED.
func verifierSignerProblems(signers *Signers, expect Expect) []string {
	var out []string
	key := ""
	var repositories map[string]string
	if signers != nil {
		key, repositories = signers.Key, signers.Repositories
	}
	if !trustedKey(key) {
		out = append(out, "LOCK-SIG-EXPECTED-KEY")
	}
	for _, identity := range sortedKeys(repositories) {
		if !trustedKey(repositories[identity]) {
			out = append(out, "LOCK-SIG-EXPECTED-KEY")
		}
	}
	if expect.RequireIndex != nil && (repositories != nil) != *expect.RequireIndex {
		out = append(out, "LOCK-EXPECTED")
	}
	seen := map[string]bool{}
	for _, identity := range sortedKeys(repositories) {
		repositoryKey := repositories[identity]
		if !hex64.MatchString(repositoryKey) {
			continue
		}
		// Of their form, lowercase hexadecimal: equal text is equal bytes.
		if repositoryKey == key || seen[repositoryKey] {
			out = append(out, "LOCK-EXPECTED")
		}
		seen[repositoryKey] = true
	}
	return out
}
