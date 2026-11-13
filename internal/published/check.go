// Package published checks, over a git repository's own tags, that a pack or
// dialect document published under an identity (kind, id, version) never
// changes its bytes (ADR-0090). It reads the repository through git's
// read-only plumbing, never writes to it, starts no network connection, and
// evaluates nothing.
package published

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/lock"
)

// CheckVersion is the version of the report this package produces, in its
// JSON and its text. It moves when a member is renamed or removed or changes
// meaning, as outputVersion does; an added member does not move it.
const CheckVersion = "1"

// The two kinds of document a run reads.
const (
	KindPack    = "pack"
	KindDialect = "dialect"
)

// Finding codes.
const (
	// CodeChanged: a later tag, or HEAD, holds other bytes under an identity
	// an earlier tag published.
	CodeChanged = "changed"
	// CodeDuplicate: the tag that first published an identity holds two
	// documents with other bytes under it.
	CodeDuplicate = "duplicate"
	// CodeDiffersFromManifest: a tag or HEAD holds bytes under an identity
	// the manifest pins to another digest.
	CodeDiffersFromManifest = "differs-from-manifest"
	// CodeVanished: an identity the manifest pins is held by no tag the run
	// reads (HEAD alone does not answer a pin), in a repository that is not
	// shallow.
	CodeVanished = "vanished"
	// CodeNotInManifest: under --strict, an identity a tag published that
	// the manifest does not pin.
	CodeNotInManifest = "not-in-manifest"
)

// Report statuses.
const (
	StatusValid      = "valid"
	StatusInvalid    = "invalid"
	StatusIncomplete = "incomplete"
)

// Location sources.
const (
	SourceTag      = "tag"
	SourceHead     = "head"
	SourceManifest = "manifest"
)

// HeadRef is the ref a location at HEAD names.
const HeadRef = "HEAD"

// MaxObjectBytes bounds every object read: a document, a tree, a commit. It
// is the hard limit a pack document is read under everywhere else
// (project.ReadPack, packs diff), so a document this run does not read is one
// no pack reader of this runtime would read either.
const MaxObjectBytes = carrier.HardMaxBytes

// maxPathBytes bounds one path in a tree.
const maxPathBytes = 4096

// MaxListed bounds each list a report carries; the counts are exact.
const MaxListed = 1000

// Identity is what a document says it is.
type Identity struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Version string `json:"version"`
}

// Location is one place an identity's bytes were found or pinned.
type Location struct {
	Source string `json:"source"`
	Ref    string `json:"ref"`
	Path   string `json:"path,omitempty"`
	Digest string `json:"digest"`
}

// Finding is one identity whose bytes are not the bytes it was published
// with, or a manifest entry the repository does not answer.
type Finding struct {
	Code string `json:"code"`
	Identity
	Published Location  `json:"published"`
	Found     *Location `json:"found,omitempty"`
}

// Unreadable is one selected file that could not be read as a document.
type Unreadable struct {
	Ref    string `json:"ref"`
	Path   string `json:"path"`
	Digest string `json:"digest,omitempty"`
	Reason string `json:"reason"`
}

// UnreadTag is one matching tag whose tree could not be read.
type UnreadTag struct {
	Ref    string `json:"ref"`
	Reason string `json:"reason"`
}

// Report is one run's result.
type Report struct {
	Status    string
	Head      string
	Shallow   bool
	TagsTotal int
	TagsRead  int
	// UnreadTags are the matching tags that name no readable commit;
	// UnreadTagCount is exact when the list is capped.
	UnreadTags     []UnreadTag
	UnreadTagCount int
	// Identities is the number of identities a tag published.
	Identities int
	// Documents is the number of distinct document blobs read; OtherFiles
	// the number of distinct selected blobs that are JSON but neither a pack
	// nor a named dialect document.
	Documents       int
	OtherFiles      int
	Findings        []Finding
	FindingCount    int
	Unreadable      []Unreadable
	UnreadableCount int
	// With a manifest: the identities a tag published that it does not pin,
	// and, in a shallow repository, those it pins that no tag held answers;
	// the counts are exact when the lists are capped.
	New          []ManifestEntry
	NewCount     int
	NotHeld      []ManifestEntry
	NotHeldCount int
	// Manifest is what --write-manifest writes: the given manifest's entries
	// and every identity a tag published that it does not pin.
	Manifest Manifest
}

// Options selects what one run reads.
type Options struct {
	Repository string
	// Tags are the tag-name patterns; none means every tag.
	Tags      []Glob
	Selection Selection
	Manifest  *Manifest
	Strict    bool
	// Writing says the caller will write the run's manifest. A shallow
	// repository is then refused before anything is read: it cannot name the
	// first tag, or the first bytes, of an identity whose history it lacks,
	// so a manifest written from it could pin changed bytes.
	Writing bool
}

// Failure is a run that could not be completed: nothing it found is reported.
type Failure struct {
	Code    string
	Message string
}

func (f *Failure) Error() string { return f.Message }

func fail(code, format string, a ...any) *Failure {
	return &Failure{Code: code, Message: fmt.Sprintf(format, a...)}
}

// Failure codes.
const (
	FailureGit         = "JPS-PUBLISHED-GIT"
	FailureRepository  = "JPS-PUBLISHED-REPOSITORY"
	FailureShallow     = "JPS-PUBLISHED-SHALLOW"
	FailurePartial     = "JPS-PUBLISHED-PARTIAL-CLONE"
	FailureHead        = "JPS-PUBLISHED-HEAD"
	FailureReadFailure = "JPS-PUBLISHED-READ"
)

type snapshot struct {
	ref    string
	commit string
	tree   string
	tag    bool
}

func (s snapshot) source() string {
	if s.tag {
		return SourceTag
	}
	return SourceHead
}

// Check runs one check.
func Check(options Options) (*Report, *Failure) {
	g, err := openGit(options.Repository)
	if errors.Is(err, ErrNoGit) {
		return nil, fail(FailureGit, "git was not found on PATH; this command reads the repository through git and nothing was read.")
	}
	if err != nil {
		return nil, fail(FailureRepository, "%s", err.Error())
	}
	shallow, failure := g.shallow()
	if failure != nil {
		return nil, failure
	}
	if shallow && options.Writing {
		return nil, fail(FailureShallow, "The repository is a shallow clone: it cannot say which tag first published an identity, or with which bytes, so no manifest is written from it. Write the manifest from a clone with the full history and tags; nothing was read or written.")
	}
	if shallow && options.Manifest == nil {
		return nil, fail(FailureShallow, "The repository is a shallow clone: tags and history it does not hold cannot be read, so no result could be backed. Fetch the full history and tags, or check against a manifest with --manifest; nothing was reported.")
	}
	if failure := g.refusePromisor(); failure != nil {
		return nil, failure
	}
	objects, err := g.openObjects()
	if err != nil {
		return nil, fail(FailureRepository, "%s", err.Error())
	}
	defer objects.close()

	report := &Report{Shallow: shallow}
	head, headTree, problem, err := resolveCommit(objects, "HEAD^{commit}", 0)
	if err != nil {
		return nil, fail(FailureReadFailure, "%s", err.Error())
	}
	if problem != "" {
		return nil, fail(FailureHead, "HEAD does not name a readable commit (%s); nothing was read.", problem)
	}
	report.Head = head
	hashBytes := len(head) / 2

	tags, failure := g.tags(hashBytes * 2)
	if failure != nil {
		return nil, failure
	}
	report.TagsTotal = len(tags)
	snapshots := []snapshot{}
	for _, tag := range tags {
		if !matchesTag(options.Tags, strings.TrimPrefix(tag.ref, "refs/tags/")) {
			continue
		}
		commit, tree, problem, err := resolveCommit(objects, tag.id+"^{}", hashBytes)
		if err != nil {
			return nil, fail(FailureReadFailure, "%s", err.Error())
		}
		if problem != "" {
			report.UnreadTags = append(report.UnreadTags, UnreadTag{Ref: tag.ref, Reason: problem})
			continue
		}
		snapshots = append(snapshots, snapshot{ref: tag.ref, commit: commit, tree: tree, tag: true})
	}
	report.TagsRead = len(snapshots)
	report.UnreadTagCount = len(report.UnreadTags)
	report.UnreadTags = capped(report.UnreadTags)
	if failure := g.order(snapshots); failure != nil {
		return nil, failure
	}
	snapshots = append(snapshots, snapshot{ref: HeadRef, commit: head, tree: headTree})

	w := &walker{
		objects:    objects,
		hashBytes:  hashBytes,
		selection:  options.Selection,
		snapshots:  snapshots,
		visited:    map[string]struct{}{},
		blobs:      map[string]*blobInfo{},
		identities: map[Identity]*identityRecord{},
	}
	for index := range snapshots {
		if err := w.walk(index); err != nil {
			return nil, fail(FailureReadFailure, "%s", err.Error())
		}
	}
	report.Documents, report.OtherFiles = w.documents, w.other
	report.UnreadableCount = len(w.unreadable)
	report.Unreadable = capped(w.unreadable)
	w.findings(report, options)
	switch {
	case report.FindingCount > 0:
		report.Status = StatusInvalid
	case report.UnreadableCount > 0 || report.UnreadTagCount > 0:
		report.Status = StatusIncomplete
	default:
		report.Status = StatusValid
	}
	return report, nil
}

func capped[T any](list []T) []T {
	if len(list) > MaxListed {
		return list[:MaxListed]
	}
	if list == nil {
		return []T{}
	}
	return list
}

func matchesTag(patterns []Glob, name string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, pattern := range patterns {
		if pattern.Match(name) {
			return true
		}
	}
	return false
}

// shallow asks git whether the repository is shallow. An answer other than
// true or false (a git too old to know the option echoes it) is a refusal.
func (g *git) shallow() (bool, *Failure) {
	output, err := g.run([]string{"rev-parse", "--is-shallow-repository"}, nil)
	if err != nil {
		return false, fail(FailureRepository, "%s", err.Error())
	}
	switch strings.TrimSpace(string(output)) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fail(FailureRepository, "This git cannot say whether the repository is shallow (rev-parse --is-shallow-repository); a git of version 2.15 or later is required, and nothing was read.")
}

// refusePromisor refuses a partial clone: a repository with a promisor
// remote may lack blobs, and reading one would make git fetch it. The fetch
// is refused twice over anyway (protocol.allow=never, GIT_NO_LAZY_FETCH), so
// such a repository would report blobs as missing; it is refused here instead,
// with the reason, rather than reported as unreadable document by document.
func (g *git) refusePromisor() *Failure {
	command := g.command([]string{"config", "--get-regexp", `^(extensions\.partialclone|remote\..+\.promisor)$`})
	var stdout bytes.Buffer
	var stderr boundedBuffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 && stderr.Len() == 0 {
			return nil
		}
		return fail(FailureRepository, "%s", newGitError("config", &stderr, err).Error())
	}
	for _, line := range strings.Split(stdout.String(), "\n") {
		key, value, _ := strings.Cut(line, " ")
		if key == "" {
			continue
		}
		if key == "extensions.partialclone" || !isFalse(value) {
			return fail(FailurePartial, "The repository is a partial clone (%s): blobs it does not hold would have to be fetched, and this command fetches nothing. Check a full clone; nothing was read.", key)
		}
	}
	return nil
}

func isFalse(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "false", "no", "off", "0":
		return true
	}
	return false
}

type tagRef struct {
	ref string
	id  string
}

// tags lists every tag, annotated and lightweight, by its full ref name and
// the object id it names, in ref-name order.
func (g *git) tags(hexLength int) ([]tagRef, *Failure) {
	tags := []tagRef{}
	err := g.stream([]string{"for-each-ref", "--format=%(objectname)%00%(refname)", "refs/tags/"}, nil, func(line []byte) error {
		id, ref, found := bytes.Cut(line, []byte{0})
		if !found || !isObjectID(string(id), hexLength) || !bytes.HasPrefix(ref, []byte("refs/tags/")) {
			return fmt.Errorf("git for-each-ref wrote a line this command cannot read")
		}
		tags = append(tags, tagRef{ref: string(ref), id: string(id)})
		return nil
	})
	if err != nil {
		return nil, fail(FailureRepository, "%s", err.Error())
	}
	sort.Slice(tags, func(i, j int) bool { return tags[i].ref < tags[j].ref })
	return tags, nil
}

func isObjectID(text string, hexLength int) bool {
	if hexLength != 0 && len(text) != hexLength {
		return false
	}
	if len(text) != 40 && len(text) != 64 {
		return false
	}
	for index := 0; index < len(text); index++ {
		if c := text[index]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// resolveCommit peels name to a commit and reads the commit's tree. A name
// that does not reach a readable commit returns the reason as problem.
func resolveCommit(objects *objects, name string, hashBytes int) (commit, tree, problem string, err error) {
	found, content, err := objects.read(name, MaxObjectBytes)
	if errors.Is(err, errObjectTooLarge) {
		return "", "", "its commit object is larger than the read limit", nil
	}
	if err != nil {
		return "", "", "", err
	}
	if found.missing {
		return "", "", "the object it names is not in this repository", nil
	}
	if found.kind != "commit" {
		return "", "", "it names a " + found.kind + ", not a commit", nil
	}
	if !isObjectID(found.id, hashBytes*2) {
		return "", "", "", fmt.Errorf("git cat-file named an object this command cannot read")
	}
	header, _, _ := bytes.Cut(content, []byte("\n"))
	tree, ok := strings.CutPrefix(string(header), "tree ")
	if !ok || !isObjectID(tree, len(found.id)) {
		return "", "", "its commit object does not begin with a tree", nil
	}
	return found.id, tree, "", nil
}

// order sorts the tags by the commit graph: oldest first in git's
// topological order, so an ancestor always comes before its descendants,
// and tags of one commit by name. Commits no ancestry relates are ordered as
// that walk orders them, by commit date.
func (g *git) order(snapshots []snapshot) *Failure {
	if len(snapshots) == 0 {
		return nil
	}
	wanted := map[string]int{}
	var input strings.Builder
	for _, s := range snapshots {
		if _, seen := wanted[s.commit]; !seen {
			wanted[s.commit] = -1
			input.WriteString(s.commit + "\n")
		}
	}
	line := 0
	err := g.stream([]string{"rev-list", "--topo-order", "--stdin"}, strings.NewReader(input.String()), func(text []byte) error {
		if position, ok := wanted[string(text)]; ok && position < 0 {
			wanted[string(text)] = line
		}
		line++
		return nil
	})
	if err != nil {
		return fail(FailureRepository, "%s", err.Error())
	}
	for commit, position := range wanted {
		if position < 0 {
			return fail(FailureRepository, "git rev-list did not list the tagged commit %s.", commit)
		}
	}
	sort.SliceStable(snapshots, func(i, j int) bool {
		left, right := wanted[snapshots[i].commit], wanted[snapshots[j].commit]
		if left != right {
			return left > right
		}
		return snapshots[i].ref < snapshots[j].ref
	})
	return nil
}

type blobInfo struct {
	digest   string
	identity Identity
	other    bool
	problem  string
}

type occurrence struct {
	snapshot int
	path     string
	digest   string
}

// identityRecord keeps, for one identity, the first place each distinct
// digest of it was found, in the order the snapshots were walked.
type identityRecord struct {
	digests   []occurrence
	published bool
}

type walker struct {
	objects    *objects
	hashBytes  int
	selection  Selection
	snapshots  []snapshot
	visited    map[string]struct{}
	blobs      map[string]*blobInfo
	identities map[Identity]*identityRecord
	unreadable []Unreadable
	documents  int
	other      int
}

type directory struct {
	path string
	tree string
}

type treeEntry struct {
	mode string
	name string
	id   string
}

// walk reads one snapshot's tree. A directory already read at the same path
// with the same tree id, in this or an earlier snapshot, is not read again:
// every document under it was already found there, at an earlier or equal
// place in the order, so it adds nothing a finding needs.
func (w *walker) walk(index int) error {
	stack := []directory{{path: "", tree: w.snapshots[index].tree}}
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		key := current.path + "\x00" + current.tree
		if _, seen := w.visited[key]; seen {
			continue
		}
		w.visited[key] = struct{}{}
		entries, problem, err := w.readTree(current.tree)
		if err != nil {
			return err
		}
		if problem != "" {
			w.unread(index, displayDir(current.path), "", problem)
			continue
		}
		subdirectories := []directory{}
		for _, entry := range entries {
			full := entry.name
			if current.path != "" {
				full = current.path + "/" + entry.name
			}
			if entry.name == "" || entry.name == "." || entry.name == ".." || strings.Contains(entry.name, "/") {
				if w.selection.selects(full) || (entry.mode == "40000" && w.selection.descends(full)) {
					w.unread(index, full, "", "its tree entry has a name git does not admit; not read")
				}
				continue
			}
			if len(full) > maxPathBytes {
				if w.selection.selects(full) || (entry.mode == "40000" && w.selection.descends(full)) {
					w.unread(index, full[:maxPathBytes], "", fmt.Sprintf("its path is longer than %d bytes; not read", maxPathBytes))
				}
				continue
			}
			switch entry.mode {
			case "40000":
				if w.selection.descends(full) {
					subdirectories = append(subdirectories, directory{path: full, tree: entry.id})
				}
			case "160000":
				// A submodule is another repository's commit; it is not entered.
			case "100644", "100755", "100664":
				if w.selection.selects(full) {
					if err := w.document(index, full, entry.id); err != nil {
						return err
					}
				}
			case "120000":
				if w.selection.selects(full) {
					w.unread(index, full, "", "it is a symbolic link, not a regular file; not read")
				}
			default:
				if w.selection.selects(full) {
					w.unread(index, full, "", "its tree entry mode "+entry.mode+" is not a file mode; not read")
				}
			}
		}
		for position := len(subdirectories) - 1; position >= 0; position-- {
			stack = append(stack, subdirectories[position])
		}
	}
	return nil
}

func displayDir(path string) string {
	if path == "" {
		return "."
	}
	return path
}

func (w *walker) unread(index int, path, digest, reason string) {
	w.unreadable = append(w.unreadable, Unreadable{Ref: w.snapshots[index].ref, Path: path, Digest: digest, Reason: reason})
}

// readTree reads one tree object and parses its entries.
func (w *walker) readTree(id string) ([]treeEntry, string, error) {
	found, content, err := w.objects.read(id, MaxObjectBytes)
	if errors.Is(err, errObjectTooLarge) {
		return nil, fmt.Sprintf("its tree object is larger than %d bytes; not read", MaxObjectBytes), nil
	}
	if err != nil {
		return nil, "", err
	}
	if found.missing {
		return nil, "its tree object is not in this repository; not read", nil
	}
	if found.kind != "tree" {
		return nil, "its tree id names a " + found.kind + "; not read", nil
	}
	entries := []treeEntry{}
	for len(content) > 0 {
		space := bytes.IndexByte(content, ' ')
		if space <= 0 {
			return nil, "its tree object cannot be parsed; not read", nil
		}
		mode := string(content[:space])
		content = content[space+1:]
		end := bytes.IndexByte(content, 0)
		if end < 0 || len(content)-end-1 < w.hashBytes {
			return nil, "its tree object cannot be parsed; not read", nil
		}
		name := string(content[:end])
		content = content[end+1:]
		entries = append(entries, treeEntry{mode: mode, name: name, id: hex.EncodeToString(content[:w.hashBytes])})
		content = content[w.hashBytes:]
	}
	return entries, "", nil
}

// document records one selected file. Its blob is read and classified once,
// however many snapshots and paths hold it.
func (w *walker) document(index int, path, id string) error {
	info, known := w.blobs[id]
	if !known {
		classified, err := w.classify(id)
		if err != nil {
			return err
		}
		info = classified
		w.blobs[id] = info
	}
	if info.problem != "" {
		w.unread(index, path, info.digest, info.problem)
		return nil
	}
	if info.other {
		return nil
	}
	record := w.identities[info.identity]
	if record == nil {
		record = &identityRecord{}
		w.identities[info.identity] = record
	}
	if w.snapshots[index].tag {
		record.published = true
	}
	for _, seen := range record.digests {
		if seen.digest == info.digest {
			return nil
		}
	}
	record.digests = append(record.digests, occurrence{snapshot: index, path: path, digest: info.digest})
	return nil
}

// classify reads one blob, takes the SHA-256 of its exact bytes, and reads
// its identity. It decodes with the carrier decoder and limits every pack
// read applies; a document that does not decode is unreadable, never
// skipped. A JSON object with a specVersion member is a pack; one with a
// string formatVersion and a case member is a named dialect document; any
// other JSON value is neither and is counted, not read further.
func (w *walker) classify(id string) (*blobInfo, error) {
	found, content, err := w.objects.read(id, MaxObjectBytes)
	if errors.Is(err, errObjectTooLarge) {
		return &blobInfo{problem: fmt.Sprintf("it is larger than the %d-byte limit a pack document is read under; not read", MaxObjectBytes)}, nil
	}
	if err != nil {
		return nil, err
	}
	if found.missing {
		return &blobInfo{problem: "its blob is not in this repository; not read"}, nil
	}
	if found.kind != "blob" {
		return &blobInfo{problem: "its tree entry names a " + found.kind + "; not read"}, nil
	}
	info := &blobInfo{digest: lock.Digest(content)}
	decoded, decodeFailure := carrier.Decode(content, carrier.DefaultLimits())
	if decodeFailure != nil {
		info.problem = "it is not acceptable JSON: " + decodeFailure.Diagnostic.Message
		return info, nil
	}
	root, isObject := decoded.(map[string]any)
	kind := ""
	if isObject {
		if _, pack := root["specVersion"]; pack {
			kind = KindPack
		} else if _, named := root["formatVersion"].(string); named {
			if _, hasCase := root["case"]; hasCase {
				kind = KindDialect
			}
		}
	}
	if kind == "" {
		info.other = true
		w.other++
		return info, nil
	}
	documentID, idOK := root["id"].(string)
	version, versionOK := root["version"].(string)
	if !idOK || documentID == "" || !versionOK || version == "" {
		info.problem = "it is a " + kind + " document whose id or version is not a non-empty string, so its identity cannot be read"
		return info, nil
	}
	info.identity = Identity{Kind: kind, ID: documentID, Version: version}
	w.documents++
	return info, nil
}

func (w *walker) location(o occurrence) Location {
	s := w.snapshots[o.snapshot]
	return Location{Source: s.source(), Ref: s.ref, Path: o.path, Digest: o.digest}
}

func (w *walker) firstTag(o occurrence) string {
	return strings.TrimPrefix(w.snapshots[o.snapshot].ref, "refs/tags/")
}

// findings compares every identity with the bytes it was published with: the
// manifest's digest when the manifest pins it, otherwise the first tag that
// holds it. Each other digest found is one finding, at the first place it
// was found.
func (w *walker) findings(report *Report, options Options) {
	identities := make([]Identity, 0, len(w.identities))
	for identity := range w.identities {
		identities = append(identities, identity)
	}
	sortIdentities(identities)
	pinned := map[Identity]ManifestEntry{}
	if options.Manifest != nil {
		for _, entry := range options.Manifest.Documents {
			pinned[entry.identity()] = entry
		}
	}
	findings := []Finding{}
	written := Manifest{ManifestVersion: ManifestVersion, Documents: []ManifestEntry{}}
	if options.Manifest != nil {
		written.Documents = append(written.Documents, options.Manifest.Documents...)
	}
	for _, identity := range identities {
		record := w.identities[identity]
		if record.published {
			report.Identities++
		}
		entry, isPinned := pinned[identity]
		var reference Location
		switch {
		case isPinned:
			reference = Location{Source: SourceManifest, Ref: "refs/tags/" + entry.FirstTag, Digest: entry.Digest}
		case record.published:
			reference = w.location(record.digests[0])
		default:
			continue
		}
		for _, seen := range record.digests {
			if seen.digest == reference.Digest {
				continue
			}
			code := CodeChanged
			if isPinned {
				code = CodeDiffersFromManifest
			} else if seen.snapshot == record.digests[0].snapshot {
				code = CodeDuplicate
			}
			found := w.location(seen)
			findings = append(findings, Finding{Code: code, Identity: identity, Published: reference, Found: &found})
		}
		if isPinned {
			// Only a tag answers a pin: an identity HEAD alone still holds
			// has vanished from every tag the run reads.
			if record.published {
				delete(pinned, identity)
			}
			continue
		}
		if !record.published {
			continue
		}
		first := record.digests[0]
		newEntry := ManifestEntry{Kind: identity.Kind, ID: identity.ID, Version: identity.Version, Digest: first.digest, FirstTag: w.firstTag(first)}
		written.Documents = append(written.Documents, newEntry)
		if options.Manifest != nil {
			report.New = append(report.New, newEntry)
			if options.Strict {
				findings = append(findings, Finding{Code: CodeNotInManifest, Identity: identity, Published: w.location(first)})
			}
		}
	}
	if options.Manifest != nil {
		for _, entry := range options.Manifest.Documents {
			if _, unanswered := pinned[entry.identity()]; !unanswered {
				continue
			}
			if report.Shallow {
				report.NotHeld = append(report.NotHeld, entry)
				continue
			}
			findings = append(findings, Finding{Code: CodeVanished, Identity: entry.identity(),
				Published: Location{Source: SourceManifest, Ref: "refs/tags/" + entry.FirstTag, Digest: entry.Digest}})
		}
	}
	sortEntries(written.Documents)
	report.Manifest = written
	report.FindingCount = len(findings)
	report.Findings = capped(findings)
	report.NewCount, report.NotHeldCount = len(report.New), len(report.NotHeld)
	report.New = capped(report.New)
	report.NotHeld = capped(report.NotHeld)
}

func sortIdentities(identities []Identity) {
	sort.Slice(identities, func(i, j int) bool { return identities[i].less(identities[j]) })
}

func (i Identity) less(other Identity) bool {
	if i.Kind != other.Kind {
		return i.Kind < other.Kind
	}
	if i.ID != other.ID {
		return i.ID < other.ID
	}
	return i.Version < other.Version
}
