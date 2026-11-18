package federation

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// WriteInput is what a lock writer is given (§8): a registry and its kind
// declarations, or an assembly (§4) with one source signature document per
// source, by source name, and the expected signer set; the previous lock of
// the registry id, or the first serial, given out of band (there is no
// default); and the emitter manifests, each with the outcome of the host in
// check mode and the files its owns matches.
type WriteInput struct {
	Registry *registry.Document
	Kinds    []registry.Document

	Assembly         *Input
	SourceSignatures map[string]registry.Document
	Signers          *Signers

	Previous    *registry.Document
	FirstSerial *int64

	Emitters []Emitter
}

// Signers is §11's expected signer set, each key 64 lowercase hexadecimal
// characters: Key, the standard owner's, which signs the lock and the
// standard source's statement, and Repositories, the repository key map from
// repository identity to the key that signs that repository's statements.
// Repositories nil is no map given.
type Signers struct {
	Key          string
	Repositories map[string]string
}

// Refusal is a lock writer's refusal: no lock is written. Codes are the
// codes of §12.2 that refuse it, sorted and distinct; the text gives writing
// a lock no report (§2.1), so a refusal is its codes alone.
type Refusal struct {
	Codes []string
}

func (r *Refusal) Error() string {
	return "lock: refused (" + strings.Join(r.Codes, ", ") + ")"
}

// lockWriter gathers a writer's refusals.
type lockWriter struct {
	refused map[string]bool
}

func (w *lockWriter) refuse(code string) {
	if _, listed := Codes[code]; !listed {
		panic("federation: unlisted diagnostic code " + code)
	}
	w.refused[code] = true
}

// WriteLock runs §8 and returns the lock's JCS bytes, with no final line
// feed. It refuses (a *Refusal) a registry validation finds an error in, or
// an assembly with an error (LOCK-REGISTRY-INVALID); a previous lock of
// another registry id (LOCK-UNEXPECTED-REGISTRY); a previous lock at
// MaxSerial, or a lock not of its form or over its bound (LOCK-FORM); a
// source signature that does not verify, with its code; and an emitter whose
// host check did not exit 0 (LOCK-EMITTER-MISMATCH). Every refusal found is
// reported. The error is an *InputError for inputs the text does not let it
// run on, and a *KnownError for an assembly's known index or lock that is not
// admitted.
func WriteLock(in WriteInput) ([]byte, error) {
	if err := writeInputs(in); err != nil {
		return nil, err
	}
	w := &lockWriter{refused: map[string]bool{}}
	var root map[string]any
	var result *registry.Result
	digest := ""
	var a *assembly
	if in.Registry != nil {
		result = registry.ValidateDocuments(*in.Registry, in.Kinds)
		if result.Report.Valid && result.Report.Registry != nil {
			root, _ = registry.AdmitDocument(in.Registry.Data, MaxRegistryBytes, "registryFormatVersion", registry.RegistryFormatVersion, registry.Location{Document: registry.DocumentRegistry})
			digest = result.Report.Registry.Digest
		}
	} else {
		a = &assembly{collected: &collector{}}
		if err := a.run(*in.Assembly); err != nil {
			return nil, err
		}
		if !a.collected.hasErrors() && a.assembled != nil && a.index != nil {
			root, result, digest = a.assembled.root, a.assembled.result, a.digest()
		}
	}
	if root == nil || digest == "" {
		w.refuse("LOCK-REGISTRY-INVALID")
	}
	id, _ := root["id"].(string)
	serial := w.serial(in, id, root != nil)
	var sources []any
	if a != nil {
		if err := emitterSources(in.Emitters, a); err != nil {
			return nil, err
		}
		if a.index != nil {
			sources = w.sources(a.index, in.SourceSignatures, in.Signers)
		}
	}
	emitters := w.emitters(in.Emitters)
	if len(w.refused) > 0 {
		return nil, w.refusal()
	}
	resolved := []any{}
	for _, environment := range result.Environments() {
		document, err := result.Resolve(environment)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, map[string]any{"environment": environment, "digest": FileDigest(document)})
	}
	lock := map[string]any{
		"lockFormatVersion": LockFormatVersion,
		"serial":            json.Number(strconv.FormatInt(serial, 10)),
		"registry":          map[string]any{"id": root["id"], "version": root["version"], "digest": digest},
		"kinds":             root["kinds"],
		"resolved":          resolved,
	}
	if a != nil {
		indexDigest, err := registry.Digest(a.index)
		if err != nil {
			return nil, err
		}
		lock["index"] = map[string]any{"digest": indexDigest}
		lock["sources"] = sources
	}
	if len(in.Emitters) > 0 {
		lock["emitters"] = emitters
	}
	data, err := jcs.EncodeDocument(lock)
	if err != nil {
		return nil, err
	}
	// The lock is read back as a checker reads it: a lock over its bound, or
	// one not of its form (an emitter's path or a file's mode, say), is
	// never written.
	written, refusals := registry.AdmitDocument(data, MaxLockBytes, "lockFormatVersion", LockFormatVersion, registry.Location{Document: DocumentLock})
	if written == nil || len(refusals) > 0 || len(lockForm(written)) > 0 {
		w.refuse("LOCK-FORM")
		return nil, w.refusal()
	}
	return data, nil
}

func (w *lockWriter) refusal() *Refusal {
	codes := make([]string, 0, len(w.refused))
	for code := range w.refused {
		codes = append(codes, code)
	}
	slices.Sort(codes)
	return &Refusal{Codes: codes}
}

// writeInputs refuses inputs the text does not let a writer run on: a
// registry and an assembly both given or neither; a single registry given
// source signatures or a signer set; a first serial and a previous lock both
// given or neither; and an emitter with no host check outcome, of the wrong
// kind of source, or given twice.
func writeInputs(in WriteInput) error {
	switch {
	case (in.Registry == nil) == (in.Assembly == nil):
		return &InputError{Reason: "give a registry or an assembly, and not both"}
	case in.Registry != nil && (in.SourceSignatures != nil || in.Signers != nil):
		return &InputError{Reason: "a single registry's lock names no source signature and no signer set"}
	case in.Previous != nil && in.FirstSerial != nil:
		return &InputError{Reason: "give the previous lock or the first serial, and not both"}
	case in.Previous == nil && in.FirstSerial == nil:
		return &InputError{Reason: "give the previous lock or the first serial: there is no default"}
	}
	for i, e := range in.Emitters {
		switch {
		case e.Check == nil:
			return &InputError{Reason: fmt.Sprintf("emitter %d is given without the outcome of the host's check", i+1)}
		case in.Registry != nil && e.Source != "":
			return &InputError{Reason: fmt.Sprintf("emitter %d names a source, and a single registry has none", i+1)}
		case in.Assembly != nil && e.Source == "":
			return &InputError{Reason: fmt.Sprintf("emitter %d names no source, and an assembly's emitters each name theirs", i+1)}
		}
	}
	_, _, err := givenEmitters(in.Emitters)
	return err
}

// emitterSources refuses an assembly's emitter whose source the manifest
// does not name.
func emitterSources(emitters []Emitter, a *assembly) error {
	if a.manifest == nil {
		return nil
	}
	for i, e := range emitters {
		if !slices.ContainsFunc(a.manifest.sources, func(s *source) bool { return s.name == e.Source }) {
			return &InputError{Reason: fmt.Sprintf("emitter %d names a source the manifest does not", i+1)}
		}
	}
	return nil
}

// serial is the lock's serial (§8): one above the previous lock's, which is
// of the lock's form and of the registry id; or the first serial given out
// of band. A previous lock at MaxSerial leaves no next serial, and a first
// serial out of range gives no lock of its form: each is LOCK-FORM. The
// previous lock's id is compared only with a registry that validates.
func (w *lockWriter) serial(in WriteInput, id string, known bool) int64 {
	if in.Previous == nil {
		first := *in.FirstSerial
		if first < 1 || first > MaxSerial {
			w.refuse("LOCK-FORM")
			return 0
		}
		return first
	}
	var previous map[string]any
	if !in.Previous.Oversize {
		previous, _ = registry.AdmitDocument(in.Previous.Data, MaxLockBytes, "lockFormatVersion", LockFormatVersion, registry.Location{Document: DocumentLock})
	}
	if previous == nil || len(lockForm(previous)) > 0 {
		w.refuse("LOCK-FORM")
		return 0
	}
	if known && lookupValue(previous, "registry", "id") != id {
		w.refuse("LOCK-UNEXPECTED-REGISTRY")
		return 0
	}
	last, _ := serialOf(previous["serial"])
	if last >= MaxSerial {
		w.refuse("LOCK-FORM")
		return 0
	}
	return last + 1
}

// emitters is the lock's emitters (§7): for each manifest whose host check
// exited 0, its path, digest and the files its owns matches, sorted by path,
// and its source in an assembly; the items sorted by source and path. A
// check that did not exit 0, or a manifest that is no emitter manifest, is
// LOCK-EMITTER-MISMATCH.
func (w *lockWriter) emitters(emitters []Emitter) []any {
	type keyed struct {
		key  emitterKey
		item map[string]any
	}
	var items []keyed
	for _, e := range emitters {
		digest, owns, readable := emitterManifest(e, &collector{})
		if *e.Check != 0 || !readable {
			w.refuse("LOCK-EMITTER-MISMATCH")
			continue
		}
		files := slices.Clone(e.Files)
		slices.SortFunc(files, func(a, b EmitterFile) int { return strings.Compare(a.Path, b.Path) })
		listed := []any{}
		for _, f := range files {
			if ownedBy(owns, f.Path) {
				listed = append(listed, map[string]any{"path": f.Path, "mode": f.Mode, "digest": f.Digest})
			}
		}
		item := map[string]any{"path": e.Path, "digest": digest, "files": listed}
		if e.Source != "" {
			item["source"] = e.Source
		}
		items = append(items, keyed{emitterKey{e.Source, e.Path}, item})
	}
	slices.SortFunc(items, func(a, b keyed) int {
		if c := strings.Compare(a.key.source, b.key.source); c != 0 {
			return c
		}
		return strings.Compare(a.key.path, b.key.path)
	})
	out := make([]any, 0, len(items))
	for _, k := range items {
		out = append(out, k.item)
	}
	return out
}
