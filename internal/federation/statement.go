package federation

import (
	"strconv"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// SourceStatement is a source statement (§10.2): its JCS bytes and its
// digest (registry §2.3), which a source signature signs.
type SourceStatement struct {
	Statement []byte
	Digest    string
}

// StatementOf is the statement of one source of a federation manifest, as
// the repository that holds it signs it (§11, Signing): its documents, at the
// commit given, read from its own bytes. It runs the assembly's step 1 on the
// manifest and, for this source alone, its checks of the commit, the
// repository and each path given, then step 2 (each path's content digest,
// its import and its admission), and builds the statement the index's item
// for the source would hold. A refusal is a report with an error, and no
// statement; a source the manifest does not name, or not given, is an
// *InputError. No other source is read, so nothing here checks the assembly.
func StatementOf(in Input, name string) (*SourceStatement, Report, error) {
	a := &assembly{collected: &collector{}}
	refused := func() (*SourceStatement, Report, error) { return nil, a.collected.report(""), nil }
	if in.Manifest.Oversize {
		a.collected.addRegistry("REG-DOC-SIZE", Location{Document: DocumentFederation})
		return refused()
	}
	m := readManifest(in.Manifest.Data, a.collected)
	if m == nil || a.collected.hasErrors() {
		return refused()
	}
	var s *source
	for _, candidate := range m.sources {
		if candidate.name == name {
			s = candidate
		}
	}
	given, ok := in.Sources[name]
	switch {
	case s == nil || name == "":
		return nil, Report{}, &InputError{Reason: "the manifest names no source " + strconv.Quote(name)}
	case !ok:
		return nil, Report{}, &InputError{Reason: "source " + strconv.Quote(name) + " is not given"}
	}
	s.commit, s.files = given.Commit, given.Files
	if !commitPattern.MatchString(s.commit) {
		a.collected.add("FED-COMMIT", Location{Document: DocumentFederation, Pointer: s.at()})
	}
	repository, isString := s.repository.(string)
	if !s.hasRepository || !isString || !validRepository(s.repository) {
		a.collected.add("FED-REPOSITORY", Location{Document: DocumentFederation, Pointer: s.at("repository")})
	}
	for _, p := range s.paths {
		if _, given := s.files[p.path]; !given {
			a.collected.add("FED-SOURCE-MISSING", Location{Document: DocumentFederation, Pointer: p.at})
		}
	}
	if a.collected.hasErrors() {
		return refused()
	}
	m.sources = []*source{s}
	a.manifest = m
	if !a.step2() {
		return refused()
	}
	documents := make([]any, 0, len(s.documents))
	for _, path := range sortedKeys(s.documents) {
		d := s.documents[path]
		documents = append(documents, map[string]any{"path": d.path, "role": d.role, "contentDigest": d.contentDigest, "digest": d.digest})
	}
	statement, _ := statement(map[string]any{"repository": repository, "commit": s.commit, "documents": documents})
	data, err := jcs.EncodeDocument(statement)
	if err != nil {
		return nil, Report{}, err
	}
	digest, err := registry.Digest(statement)
	if err != nil {
		return nil, Report{}, err
	}
	return &SourceStatement{Statement: data, Digest: digest}, a.collected.report(""), nil
}
