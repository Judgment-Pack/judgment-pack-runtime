// Package lock implements the reviewed-set lock (ADR-0019): a generated,
// deterministic sibling of a project's jpack.json that pins the exact bytes of
// the configuration and of every document it declares.
//
// What it is for. A pack is law a project reviewed; a jpack.json is the index
// that says which law counts. Both are files in the same tree the agent asking
// for a decision can write to, so nothing inside that tree can stop an editor
// from amending the law and then asking again — the agent and the law share one
// write domain, and an in-tree rule is a rule the addressee can rewrite. What a
// runtime can do instead, without knowing anything about the client, is make
// every amendment an explicit act with a name and a diff, and make every
// decision record say which law it was judged under. That is this file's whole
// job: `packs lock` is the amendment, `packs verify` is the audit, and the
// deciding surfaces refuse to judge under law that does not match what was last
// declared reviewed.
//
// What it is NOT. It is not a wall. Anything that can edit a pack can run
// `packs lock` again, and this runtime cannot tell that apart from an author
// amending policy on purpose — which is the same act. The lock turns a silent
// edit into a recorded one; keeping the deciding party out of the law's write
// domain is a property of where the decision runs, not of what it checks, and
// ADR-0019 names that as the product-side work it does not build.
//
// Presence is the opt-in. A project with no lock file reaches nothing here: no
// verification, no refusal, no member in an audit record. That is the same shape
// the audit trail takes (ADR-0018), and for the same reason — a convention this
// runtime invented must cost nothing to a project that never adopted it.
//
// The document is generated and must be byte-deterministic: it is reviewed like
// any other artifact, so two runs over one tree that produce different bytes
// would put noise in a diff someone is supposed to read. Members are sorted (by
// the encoder, for maps), indented two spaces, and end with one newline.
package lock

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/graph"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

const (
	// Version is the lock document's shape, a single integer as a string on the
	// outputVersion, configVersion, and recordVersion precedent: a reader either
	// knows this shape or does not.
	Version = "1"
	// FailureCode and FailureMessage lead every refusal a deciding surface gives
	// for law that does not match the lock. The code is provisional, like every
	// JPS-* code, and the message steers rather than only refusing: a reader who
	// is told only "no" edits until the "no" stops.
	FailureCode = "JPS-LOCK-VERIFY"
	// The named per-entry diagnostics. Each names one thing that can differ, so
	// a report says which, rather than reporting one undifferentiated mismatch.
	CheckConfigDrift        = "config-drift"
	CheckDocumentDrift      = "document-drift"
	CheckDocumentMissing    = "document-missing"
	CheckLockEntryMissing   = "lock-entry-missing"
	CheckUndeclaredInConfig = "locked-but-undeclared"
)

// Digest is one document's exact bytes in the algorithm-prefixed form this
// runtime writes a digest in. It is audit's, reused rather than reimplemented:
// one spelling of "these bytes" across the trail and the lock is what lets a
// record and a lock entry be compared by eye.
func Digest(document []byte) string { return audit.Digest(document) }

// Document is one jpack.lock.json.
//
// Entries are keyed by the project's own decision or graph id, which is what a
// configuration is keyed by, so a reader comparing the two files reads down the
// same column. Each carries the declared path beside the digest: the path is not
// what is verified — the digest is — but a lock a human has to read is worth the
// one member that says which file each line is about.
type Document struct {
	LockVersion string           `json:"lockVersion"`
	Config      Config           `json:"config"`
	Packs       map[string]Entry `json:"packs,omitempty"`
	Graphs      map[string]Entry `json:"graphs,omitempty"`
}

// Config pins the index itself. Without it a lock would pin every pack a
// configuration names while leaving free the file that says which packs it
// names — and adding a pack is an amendment exactly as editing one is.
type Config struct {
	Digest string `json:"digest"`
}

// Entry is one declared document's reviewed bytes.
type Entry struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
}

// Failure reports why a lock operation could not run, on the shape every other
// project failure takes.
type Failure struct {
	Code     string
	Message  string
	ExitCode int
}

// Generate builds the lock for one loaded project by reading every declared
// document through that project's own handle.
//
// A document that cannot be read is a refusal and not an entry: a lock is a
// statement that these exact bytes were reviewed, and a lock that quietly
// omitted the pack nobody could read would say the project's reviewed set is
// smaller than the project thinks it is.
func Generate(loaded *project.Project) (Document, *Failure) {
	document := Document{
		LockVersion: Version,
		Config:      Config{Digest: loaded.ConfigDigest},
	}
	if len(loaded.IDs) > 0 {
		document.Packs = make(map[string]Entry, len(loaded.IDs))
	}
	for _, id := range loaded.IDs {
		entry, _ := loaded.Entry(id)
		data, err := loaded.ReadPack(entry)
		if err != nil {
			return Document{}, &Failure{
				Code:     "JPS-LOCK-READ",
				Message:  fmt.Sprintf("The pack %q could not be read, so its reviewed bytes cannot be declared: %s", display.Sanitize(id), project.ReadFailureMessage(entry.Path, err)),
				ExitCode: result.ExitIO,
			}
		}
		document.Packs[id] = Entry{Path: entry.Path, Digest: Digest(data)}
	}
	if len(loaded.GraphIDs) > 0 {
		document.Graphs = make(map[string]Entry, len(loaded.GraphIDs))
	}
	for _, id := range loaded.GraphIDs {
		entry, _ := loaded.GraphEntry(id)
		data, err := loaded.ReadGraph(entry, graph.MaxGraphBytes)
		if err != nil {
			return Document{}, &Failure{
				Code:     "JPS-LOCK-READ",
				Message:  fmt.Sprintf("The graph %q could not be read, so its reviewed bytes cannot be declared: %s", display.Sanitize(id), project.ReadFailureMessage(entry.Path, err)),
				ExitCode: result.ExitIO,
			}
		}
		document.Graphs[id] = Entry{Path: entry.Path, Digest: Digest(data)}
	}
	return document, nil
}

// Encode renders one lock as the exact bytes written to disk: two-space indent,
// no HTML escaping, one trailing newline, and map members in sorted order
// because the encoder sorts them. Two runs over one tree produce identical
// bytes, which is what makes a lock reviewable in a diff.
func Encode(document Document) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(document); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// Load reads and decodes one project's lock through its own handle.
//
// The decode is the strict carrier decode every other document here gets — a
// duplicate member in a reviewed artifact is a defect, not a last-one-wins — and
// the version is read before the shape, so a lock from a later toolchain is told
// exactly that instead of being read as a broken one.
func Load(loaded *project.Project) (Document, *Failure) {
	name, _ := loaded.LockName()
	data, err := loaded.ReadLock()
	if err != nil {
		return Document{}, &Failure{
			Code:     "JPS-LOCK-READ",
			Message:  fmt.Sprintf("The reviewed-set lock %s could not be read: %s", display.Sanitize(loaded.LockPath()), project.ReadFailureMessage(name, err)),
			ExitCode: result.ExitIO,
		}
	}
	decoded, carrierFailure := carrier.Decode(data, carrier.DefaultLimits())
	if carrierFailure != nil {
		return Document{}, &Failure{
			Code:     "JPS-LOCK-JSON",
			Message:  fmt.Sprintf("The reviewed-set lock %s is not acceptable JSON: %s. It is generated: run jpack packs lock to write it again.", display.Sanitize(loaded.LockPath()), display.Sanitize(carrierFailure.Diagnostic.Message)),
			ExitCode: result.ExitInvalid,
		}
	}
	root, ok := decoded.(map[string]any)
	if !ok {
		return Document{}, &Failure{
			Code:     "JPS-LOCK-SHAPE",
			Message:  fmt.Sprintf("The reviewed-set lock %s must be a JSON object. It is generated: run jpack packs lock to write it again.", display.Sanitize(loaded.LockPath())),
			ExitCode: result.ExitInvalid,
		}
	}
	declared, _ := root["lockVersion"].(string)
	if declared != Version {
		return Document{}, &Failure{
			Code:     "JPS-LOCK-VERSION",
			Message:  fmt.Sprintf("The reviewed-set lock %s declares lockVersion %q, which this runtime does not read. It reads: %s.", display.Sanitize(loaded.LockPath()), display.Sanitize(declared), Version),
			ExitCode: result.ExitUnsupported,
		}
	}
	var document Document
	if err := json.Unmarshal(data, &document); err != nil {
		return Document{}, &Failure{
			Code:     "JPS-LOCK-SHAPE",
			Message:  fmt.Sprintf("The reviewed-set lock %s could not be decoded. It is generated: run jpack packs lock to write it again.", display.Sanitize(loaded.LockPath())),
			ExitCode: result.ExitInvalid,
		}
	}
	return document, nil
}

// Check is one named finding about one entry. Kind is "pack", "graph", or ""
// for the configuration itself, and it is part of the finding's identity: a
// pack and a graph may share an id, and a report that keyed on the id alone
// would collapse two documents into one.
type Check struct {
	Name   string
	Kind   string
	ID     string
	Path   string
	Detail string
}

// Verify reports every difference between one project's current documents and
// the lock: the configuration's own bytes, each declared pack and graph, and
// each locked entry the configuration no longer declares.
//
// Findings are returned in a deterministic order — the configuration, then packs
// by id, then graphs by id, then the locked-but-undeclared entries by id — so
// two runs over one tree report the same thing in the same order.
func Verify(loaded *project.Project, document Document) []Check {
	checks := []Check{}
	if document.Config.Digest != loaded.ConfigDigest {
		checks = append(checks, Check{
			Name:   CheckConfigDrift,
			Path:   loaded.ConfigPath,
			Detail: "The configuration's own bytes differ from the reviewed set. Adding, removing, or re-pointing a declared pack is an amendment.",
		})
	}
	checks = append(checks, verifySet(loaded, document.Packs, loaded.IDs, "pack", func(id string) (string, []byte, error) {
		entry, _ := loaded.Entry(id)
		data, err := loaded.ReadPack(entry)
		return entry.Path, data, err
	})...)
	checks = append(checks, verifySet(loaded, document.Graphs, loaded.GraphIDs, "graph", func(id string) (string, []byte, error) {
		entry, _ := loaded.GraphEntry(id)
		data, err := loaded.ReadGraph(entry, graph.MaxGraphBytes)
		return entry.Path, data, err
	})...)
	return checks
}

// verifySet compares one keyed half of the lock against one keyed half of the
// configuration. Both directions are reported: a declared document with no lock
// entry is law nobody declared reviewed, and a locked entry the configuration
// dropped is a reviewed set that no longer describes the project.
func verifySet(loaded *project.Project, locked map[string]Entry, declared []string, kind string, read func(string) (string, []byte, error)) []Check {
	checks := []Check{}
	for _, id := range declared {
		path, data, err := read(id)
		entry, inLock := locked[id]
		if !inLock {
			checks = append(checks, Check{
				Name:   CheckLockEntryMissing,
				Kind:   kind,
				ID:     id,
				Path:   path,
				Detail: fmt.Sprintf("The configuration declares this %s and the reviewed set does not name it.", kind),
			})
			continue
		}
		if err != nil {
			checks = append(checks, Check{
				Name:   CheckDocumentMissing,
				Kind:   kind,
				ID:     id,
				Path:   path,
				Detail: project.ReadFailureMessage(path, err),
			})
			continue
		}
		if got := Digest(data); got != entry.Digest {
			checks = append(checks, Check{
				Name:   CheckDocumentDrift,
				Kind:   kind,
				ID:     id,
				Path:   path,
				Detail: fmt.Sprintf("The %s document's bytes differ from the reviewed set.", kind),
			})
		}
	}
	unknown := []string{}
	for id := range locked {
		if _, found := findDeclared(loaded, kind, id); !found {
			unknown = append(unknown, id)
		}
	}
	sort.Strings(unknown)
	for _, id := range unknown {
		checks = append(checks, Check{
			Name:   CheckUndeclaredInConfig,
			Kind:   kind,
			ID:     id,
			Path:   locked[id].Path,
			Detail: fmt.Sprintf("The reviewed set names this %s and the configuration no longer declares it.", kind),
		})
	}
	return checks
}

// findDeclared answers whether the configuration still declares one id of one
// kind, without a second copy of the lookup rule in each caller.
func findDeclared(loaded *project.Project, kind, id string) (string, bool) {
	if kind == "graph" {
		entry, ok := loaded.GraphEntry(id)
		return entry.Path, ok
	}
	entry, ok := loaded.Entry(id)
	return entry.Path, ok
}

// Applied is one declared document an evaluation is about to apply, named by
// the digest of the exact bytes it will apply — not by a path the lock check
// could read a second time.
//
// That distinction is the whole of the deciding check's integrity. Reading the
// file again to digest it makes the verified bytes and the evaluated bytes two
// different reads of one pathname, and a writer between them yields a
// disposition from one document carrying a review established over another.
// internal/project's own reader refuses to hand back pathnames for exactly this
// reason; a lock check that re-opened a path would reintroduce the gap the
// handle exists to close.
type Applied struct {
	Kind   string
	ID     string
	Digest string
}

// AppliedPack names one declared pack by the bytes about to be evaluated.
func AppliedPack(id string, document []byte) Applied {
	return Applied{Kind: "pack", ID: id, Digest: Digest(document)}
}

// AppliedGraph names one declared graph by the bytes it was decoded from. The
// graph document carries that digest already, taken where the bytes and the
// decoded shape are known to be one thing.
func AppliedGraph(id, digest string) Applied {
	return Applied{Kind: "graph", ID: id, Digest: digest}
}

// VerifyDeciding is the check a deciding surface makes before it evaluates: the
// configuration's own bytes, plus the bytes of the declared documents this one
// evaluation is about to apply.
//
// It is narrower than Verify on purpose, in two ways. It asks about the
// documents this decision reaches rather than the whole project, because
// refusing one decision for an unrelated pack's drift would make an unrelated
// edit stop an unrelated decision. And it verifies the bytes it was handed
// rather than re-reading the file, because a decision's claim must be about the
// bytes that produced it. `packs verify` keeps the re-reading form, which is
// exactly the question that command asks.
//
// The configuration is checked from the digest taken when it was decoded, so it
// is bound the same way: it is what says which pack a decision id names, and a
// drifted configuration means the id may not name what it named when the set was
// reviewed.
//
// A document named by path — or passed inline over the wire — is a draft: it
// appears in no Applied, is never refused for being unlocked, and is what
// reviewed:false in an audit record means.
func VerifyDeciding(loaded *project.Project, document Document, applied []Applied) []Check {
	checks := []Check{}
	if document.Config.Digest != loaded.ConfigDigest {
		checks = append(checks, Check{
			Name:   CheckConfigDrift,
			Path:   loaded.ConfigPath,
			Detail: "The configuration's own bytes differ from the reviewed set, so which pack a decision id names may have changed since it was reviewed.",
		})
	}
	for _, one := range sortedApplied(applied) {
		checks = append(checks, verifyApplied(loaded, document, one)...)
	}
	return checks
}

// verifyApplied compares one document about to be applied against its entry in
// the reviewed set.
func verifyApplied(loaded *project.Project, document Document, applied Applied) []Check {
	locked := document.Packs
	path, declared := "", false
	if entry, ok := loaded.Entry(applied.ID); ok && applied.Kind == "pack" {
		path, declared = entry.Path, true
	}
	if applied.Kind == "graph" {
		locked = document.Graphs
		if entry, ok := loaded.GraphEntry(applied.ID); ok {
			path, declared = entry.Path, true
		}
	}
	// An id the configuration does not declare is not the lock's finding. The
	// surface that resolved it says so in its own words — the graph validator
	// names the id and lists the configured ones — and a lock refusal here would
	// state the opposite of what is wrong and steer at a command that cannot fix
	// it.
	if !declared {
		return nil
	}
	entry, inLock := locked[applied.ID]
	if !inLock {
		return []Check{{
			Name:   CheckLockEntryMissing,
			Kind:   applied.Kind,
			ID:     applied.ID,
			Path:   path,
			Detail: fmt.Sprintf("The configuration declares this %s and the reviewed set does not name it.", applied.Kind),
		}}
	}
	if applied.Digest != entry.Digest {
		return []Check{{
			Name:   CheckDocumentDrift,
			Kind:   applied.Kind,
			ID:     applied.ID,
			Path:   path,
			Detail: fmt.Sprintf("The %s document's bytes differ from the reviewed set.", applied.Kind),
		}}
	}
	return nil
}

// sortedApplied orders the documents one evaluation applies, so a refusal names
// them in the same order on every run.
func sortedApplied(applied []Applied) []Applied {
	ordered := append([]Applied{}, applied...)
	sort.Slice(ordered, func(left, right int) bool {
		if ordered[left].Kind != ordered[right].Kind {
			return ordered[left].Kind < ordered[right].Kind
		}
		return ordered[left].ID < ordered[right].ID
	})
	return ordered
}

// DecidingFailure turns the findings of VerifyDeciding into the one refusal a
// deciding surface reports.
//
// It steers, and the steer is the point of the whole convention: an amendment is
// legitimate and this runtime cannot tell one from tampering, so the message
// names the two honest ways forward — declare the amendment, or restore the
// reviewed bytes — rather than leaving a reader to find the edit that makes the
// refusal go away. The first finding leads; the rest are counted, because a
// message that listed every drifted document would bury the one to act on.
func DecidingFailure(loaded *project.Project, checks []Check) *Failure {
	if len(checks) == 0 {
		return nil
	}
	first := checks[0]
	subject := display.Sanitize(first.Path)
	if first.ID != "" {
		subject = fmt.Sprintf("%q (%s)", display.Sanitize(first.ID), display.Sanitize(first.Path))
	}
	more := ""
	if len(checks) > 1 {
		more = fmt.Sprintf(" %d further difference(s) were found; run jpack packs verify for the complete report.", len(checks)-1)
	}
	return &Failure{
		Code: FailureCode,
		Message: fmt.Sprintf(
			"This evaluation was refused because the law it would apply is not the law %s declares reviewed: %s at %s. %s%s Run jpack packs lock to declare the amendment, or restore the reviewed bytes.",
			display.Sanitize(loaded.LockPath()), display.Sanitize(first.Name), subject, display.Sanitize(first.Detail), more),
		ExitCode: result.ExitInvalid,
	}
}

// Consult is the whole of what a deciding surface does about the lock: nothing
// at all when there is no project, no lock, or no declared document in play, a
// refusal when the law this evaluation applies differs from the reviewed set,
// and otherwise the bit an audit record carries.
//
// applied names the declared documents this one evaluation applies, each by the
// digest of the bytes it applies. draft says at least one document it applies
// was not declared — a pack named by path, a pack passed inline over the wire, a
// graph document that is not one the configuration declares. A draft is never
// refused for being unlocked: writing a pack and trying it is the author's loop,
// and a convention that made the loop illegal would be a convention authors
// route around.
//
// A run that applies no declared document reaches the lock not at all — not even
// to read it. That order is deliberate: an unreadable lock, or one from a newer
// toolchain, must not stop someone drafting, because there is no declared law in
// play for it to say anything about.
//
// The returned bit is nil when the project declares no lock, so a record from an
// unlocked project carries no member rather than a false one. It is true only
// when every document applied was declared and every one of them matched; a
// draft anywhere makes it false, because a record saying "reviewed" about a run
// that applied an undeclared document would be the strongest claim in the trail
// and the least true.
func Consult(loaded *project.Project, applied []Applied, draft bool) (*bool, *Failure) {
	if loaded == nil || !loaded.HasLock() {
		return nil, nil
	}
	if len(applied) == 0 {
		reviewed := !draft
		return &reviewed, nil
	}
	document, failure := Load(loaded)
	if failure != nil {
		return nil, failure
	}
	if checks := VerifyDeciding(loaded, document, applied); len(checks) > 0 {
		return nil, DecidingFailure(loaded, checks)
	}
	reviewed := !draft
	return &reviewed, nil
}

// LawCheck is the deciding check a surface hands to a caller that reads
// documents itself — the graph evaluator, which resolves each node's pack
// inside its own loop.
//
// It exists so the check happens where the bytes are, at the one point they are
// in hand and before the node they belong to evaluates, without the graph
// package learning what a lock is. The returned failure is the surface's own,
// already carrying the code and the steer.
type LawCheck func(decisionID string, document []byte) *Failure

// NodeCheck builds the LawCheck a graph run applies to each node's pack as it
// is read. It returns nil when there is nothing to check — no project, no lock —
// so a caller can hand the result over unconditionally.
//
// An id the configuration does not declare is passed over: the graph's own
// reference checks name it, and they name it correctly.
func NodeCheck(loaded *project.Project, document Document) LawCheck {
	if loaded == nil || !loaded.HasLock() {
		return nil
	}
	return func(decisionID string, applied []byte) *Failure {
		checks := VerifyDeciding(loaded, Document{
			// The configuration is checked once by the surface, before the run
			// starts; repeating it per node would report one drift as many.
			Config: Config{Digest: loaded.ConfigDigest},
			Packs:  document.Packs,
		}, []Applied{AppliedPack(decisionID, applied)})
		if len(checks) == 0 {
			return nil
		}
		return DecidingFailure(loaded, checks)
	}
}

// NoLockFailure is what packs verify reports for a project that has no lock:
// there is nothing to verify, and the way to have something is one command.
// It is an operational refusal rather than a verdict, because "no lock" is not
// a project failing verification — it is a project that never declared a
// reviewed set.
func NoLockFailure(loaded *project.Project) *Failure {
	return &Failure{
		Code:     "JPS-LOCK-ABSENT",
		Message:  fmt.Sprintf("There is no reviewed-set lock at %s, so there is nothing to verify against. Run jpack packs lock to declare the project's current documents as its reviewed set.", display.Sanitize(loaded.LockPath())),
		ExitCode: result.ExitIO,
	}
}
