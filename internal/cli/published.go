package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/published"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

const checkPublishedCommandName = "experimental check-published"

// checkPublishedCommand checks, over a git repository's own tags, that no
// pack or dialect document a tag published changed its bytes under the same
// identity (ADR-0090). It is internal/published's Check and nothing else; it
// reads the repository through git, writes nothing into it, evaluates
// nothing, and opens no network connection.
func (a *App) checkPublishedCommand() *cobra.Command {
	format := "human"
	repository := "."
	var tagPatterns, includePatterns, excludePatterns []string
	manifestPath, writePath := "", ""
	strict := false
	command := &cobra.Command{
		Use:   "check-published",
		Short: "Check, over a git repository's tags, that no published pack or dialect document changed its bytes under the same id and version (EXPERIMENTAL)",
		Long: "Read every release tag of a local git repository, and HEAD, and report each pack or named dialect document " +
			"whose bytes under one identity -- its kind, and the id and version the document itself declares -- are not the " +
			"bytes the first tag holding that identity published. Identity is read from the document, never from its path: " +
			"a file moved with the same bytes is no finding, and a changed file under a new version is no finding. Bytes are " +
			"compared exactly, by SHA-256, so a whitespace-only change is a finding. A document is a pack when it is a JSON " +
			"object with a specVersion member, and a named dialect document when it has a string formatVersion and a case " +
			"member; any other JSON file is counted and not read further. Each finding names the identity, both digests, both " +
			"refs (a tag or HEAD) and both paths; two documents under one identity in the tag that first published it are a " +
			"finding too. A selected file that cannot be read as a document at some tag -- not JSON this runtime accepts, " +
			"larger than the read limit every pack read applies, a symbolic link, or a document whose id or version is not a " +
			"non-empty string -- is reported as unreadable at that tag, never skipped, and hides no finding; so is a submodule " +
			"the selection reaches, which is not entered. Tree entries are read as git reads them, by the file type their mode " +
			"names; a mode spelled otherwise than git writes it is reported besides. Tags are ordered " +
			"by the commit graph, an ancestor before its descendants, never by name. --tags <glob> selects tags by name " +
			"(repeatable; default every tag; * matches within one slash-separated segment and ** across segments); " +
			"--include and --exclude <glob> select paths (default **/*.json). --manifest <file> checks against a manifest " +
			"pinning each identity by digest and first tag: other bytes under a pinned identity are a finding, as is a pinned " +
			"identity no selected tag holds; an identity the manifest does not pin is listed as new, and is a finding only " +
			"with --strict. --write-manifest <file> writes the manifest of this run, the given manifest's entries as they " +
			"are and every new identity at its first tag's bytes, as deterministic bytes, replacing only a file that holds a " +
			"manifest; a new identity with a changed or duplicate finding is withheld, pinned by no written manifest, since " +
			"which of its bytes came first can turn on commit dates. A " +
			"shallow clone is said to be one: without --manifest it is refused, since tags and history it lacks cannot be " +
			"read, and no manifest is written from it; a partial clone is refused. git must be on PATH; it is run with hooks, " +
			"pagers, fsmonitor, replace refs, credential helpers and every transport refused, against an explicit git " +
			"directory, and no tag name or path is ever one of its arguments. HEAD is the commit HEAD names; uncommitted " +
			"changes are not read. --format human writes a short text in which every name taken from the repository is " +
			"written as a JSON string in printable ASCII (a name that is not UTF-8, as Go quotes it); --format json writes the standard envelope with a publishedCheckVersion " +
			"member. It exits 0 when every identity keeps its bytes and everything selected was read, 1 when there is a " +
			"finding (status invalid), or something selected could not be read, or no tag was read and no manifest pin was " +
			"given, so nothing published was checked (status incomplete), 3 for a usage error, and " +
			"4 when the repository could not be read, or its trees name more than one run reads (1,000,000 directories, " +
			"distinct by path and tree, or 1 GiB of tree objects), with nothing reported. The check, its output and this command are " +
			"EXPERIMENTAL: each may change or be removed without a compatibility promise.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateFormat(format); err != nil {
				return a.operational(checkPublishedCommandName, format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			options := published.Options{Repository: repository, Strict: strict, Writing: writePath != ""}
			var err error
			if options.Tags, err = compileGlobs("--tags", tagPatterns); err == nil {
				if len(includePatterns) == 0 {
					includePatterns = []string{published.DefaultInclude}
				}
				if options.Selection.Include, err = compileGlobs("--include", includePatterns); err == nil {
					options.Selection.Exclude, err = compileGlobs("--exclude", excludePatterns)
				}
			}
			if err != nil {
				return a.operational(checkPublishedCommandName, format, result.ExitInvocation, "JPS-INVOCATION-PATTERN", err.Error())
			}
			if strict && manifestPath == "" {
				return a.operational(checkPublishedCommandName, format, result.ExitInvocation, "JPS-INVOCATION-STRICT", "--strict makes an identity the manifest does not pin a finding, so it needs --manifest.")
			}
			if repository == "" || repository == "-" || strings.Contains(repository, "://") || fssecure.IsRemotePath(repository) {
				return a.operational(checkPublishedCommandName, format, result.ExitInvocation, "JPS-INVOCATION-INPUT", "--repo names a local directory in a git repository; URLs, remote filesystems and standard input are not read.")
			}
			if manifestPath != "" {
				manifest, failure := a.readPublishedManifest(format, manifestPath)
				if failure != nil {
					return failure
				}
				options.Manifest = manifest
			}
			var target *manifestTarget
			if writePath != "" {
				var refusal error
				if target, refusal = a.openManifestTarget(format, writePath); refusal != nil {
					return refusal
				}
				defer target.dir.Close()
			}
			report, failure := published.Check(options)
			if failure != nil {
				return a.operational(checkPublishedCommandName, format, result.ExitIO, failure.Code, failure.Message)
			}
			if target != nil {
				if refusal := a.writePublishedManifest(format, target, report.Manifest); refusal != nil {
					return refusal
				}
			}
			if err := a.renderPublishedCheck(format, report, options, manifestPath, target); err != nil {
				return &handledExit{code: result.ExitIO}
			}
			if report.Status != published.StatusValid {
				return &handledExit{code: result.ExitInvalid}
			}
			return nil
		},
	}
	command.Flags().StringVar(&format, "format", format, "output format: human or json")
	command.Flags().StringVar(&repository, "repo", repository, "a directory in the local git repository to read")
	command.Flags().StringArrayVar(&tagPatterns, "tags", nil, "read only tags whose name, without refs/tags/, matches this glob (repeatable; default every tag)")
	command.Flags().StringArrayVar(&includePatterns, "include", nil, "read only paths that match this glob (repeatable; default "+published.DefaultInclude+")")
	command.Flags().StringArrayVar(&excludePatterns, "exclude", nil, "do not read paths that match this glob (repeatable)")
	command.Flags().StringVar(&manifestPath, "manifest", manifestPath, "check against this manifest of published identities, a file path or -")
	command.Flags().StringVar(&writePath, "write-manifest", writePath, "write this run's manifest to this file, replacing only a manifest")
	command.Flags().BoolVar(&strict, "strict", strict, "with --manifest, an identity a tag published that the manifest does not pin is a finding")
	return command
}

func compileGlobs(flag string, patterns []string) ([]published.Glob, error) {
	globs := make([]published.Glob, 0, len(patterns))
	for _, pattern := range patterns {
		glob, err := published.CompileGlob(pattern)
		if err != nil {
			return nil, fmt.Errorf("%s %s: %v", flag, strconv.QuoteToASCII(pattern), err)
		}
		globs = append(globs, glob)
	}
	return globs, nil
}

// readPublishedManifest reads the manifest --manifest names, within the
// bound every manifest is read and written under.
func (a *App) readPublishedManifest(format, manifestPath string) (*published.Manifest, error) {
	data, err := a.readInput(manifestPath, published.MaxManifestBytes, "the --manifest file")
	if errors.Is(err, fssecure.ErrTooLarge) {
		return nil, a.operational(checkPublishedCommandName, format, result.ExitIO, "JPS-RESOURCE-INPUT-BYTE-LIMIT",
			fmt.Sprintf("The manifest %s exceeds the %d-byte limit every manifest is read under; nothing was checked.", display.Sanitize(manifestPath), published.MaxManifestBytes))
	}
	if err != nil {
		return nil, a.operational(checkPublishedCommandName, format, result.ExitIO, "JPS-INPUT-READ",
			fmt.Sprintf("The manifest %s could not be read as one bounded regular file; nothing was checked.", display.Sanitize(manifestPath)))
	}
	manifest, err := published.ReadManifest(data)
	if errors.Is(err, published.ErrManifestVersion) {
		return nil, a.operational(checkPublishedCommandName, format, result.ExitUnsupported, "JPS-PUBLISHED-MANIFEST-VERSION",
			fmt.Sprintf("The manifest %s was not read: %s; nothing was checked.", display.Sanitize(manifestPath), err.Error()))
	}
	if err != nil {
		return nil, a.operational(checkPublishedCommandName, format, result.ExitInvalid, "JPS-PUBLISHED-MANIFEST",
			fmt.Sprintf("The manifest %s was refused: %s; nothing was checked.", display.Sanitize(manifestPath), display.Sanitize(err.Error())))
	}
	return manifest, nil
}

// manifestTarget is the file --write-manifest names, held before the run.
type manifestTarget struct {
	dir  *fssecure.Root
	name string
	path string
	// existing is the manifest there now, which the write replaces, or nil.
	existing os.FileInfo
}

// openManifestTarget opens the directory the file --write-manifest names is
// in and holds what is there to what a run may replace: nothing, or a
// regular file, not a symbolic link, that holds a manifest this runtime
// reads -- the one --manifest read included, since a run that checks a
// manifest and writes it back advances it. Anything else is left as it is.
// It is done before the repository is read, so a target that would be
// refused costs no run.
func (a *App) openManifestTarget(format, target string) (*manifestTarget, error) {
	refuse := func(code string, exit int, why string) error {
		return a.operational(checkPublishedCommandName, format, exit, code,
			fmt.Sprintf("--write-manifest %s %s; nothing was read or written.", display.Sanitize(target), why))
	}
	if target == "-" || strings.Contains(target, "://") || fssecure.IsRemotePath(target) {
		return nil, refuse("JPS-INVOCATION-OUTPUT", result.ExitInvocation, "is not a local file path; the manifest is written to a file, never to standard output or a URL")
	}
	dir, name := filepath.Split(target)
	if name == "" || name == "." || name == ".." {
		return nil, refuse("JPS-INVOCATION-OUTPUT", result.ExitInvocation, "names no file")
	}
	if dir == "" {
		dir = "."
	}
	opened, err := fssecure.OpenRoot(dir)
	if err != nil {
		return nil, refuse("JPS-PUBLISHED-WRITE", result.ExitIO, "is in a directory that could not be opened")
	}
	held := &manifestTarget{dir: opened, name: name, path: target}
	info, err := opened.Lstat(name)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return held, nil
	case err != nil:
		opened.Close()
		return nil, refuse("JPS-PUBLISHED-WRITE", result.ExitIO, "could not be examined")
	case info.Mode()&os.ModeSymlink != 0:
		opened.Close()
		return nil, refuse("JPS-INVOCATION-OUTPUT", result.ExitInvocation, "is a symbolic link; a manifest is written to a file named by its own path")
	case !info.Mode().IsRegular():
		opened.Close()
		return nil, refuse("JPS-INVOCATION-OUTPUT", result.ExitInvocation, "is not a regular file")
	}
	data, err := opened.Read(name, published.MaxManifestBytes)
	if err == nil {
		_, err = published.ReadManifest(data)
	}
	if err != nil {
		opened.Close()
		return nil, refuse("JPS-INVOCATION-OUTPUT", result.ExitInvocation, "holds something other than a manifest this runtime reads, and is left as it is; name a new file, or a manifest")
	}
	held.existing = info
	return held, nil
}

// writePublishedManifest puts the run's manifest at the target, whole or not
// at all, by the rename fssecure's ReplaceByRename makes.
func (a *App) writePublishedManifest(format string, target *manifestTarget, manifest published.Manifest) error {
	data := published.EncodeManifest(manifest)
	if int64(len(data)) > published.MaxManifestBytes {
		return a.operational(checkPublishedCommandName, format, result.ExitIO, "JPS-PUBLISHED-WRITE",
			fmt.Sprintf("The manifest would be %d bytes, over the %d-byte limit every manifest is read under, so no run could read it back; nothing was written.", len(data), published.MaxManifestBytes))
	}
	if err := target.dir.ReplaceByRename(target.name, data, target.existing); err != nil {
		why := "could not be written"
		if errors.Is(err, fssecure.ErrReplacedChanged) {
			why = "changed while the repository was read, and is left as it is"
		}
		return a.operational(checkPublishedCommandName, format, result.ExitIO, "JPS-PUBLISHED-WRITE",
			fmt.Sprintf("--write-manifest %s %s; nothing was written.", display.Sanitize(target.path), why))
	}
	return nil
}

// publishedCheckOutput is one check-published report in JSON.
type publishedCheckOutput struct {
	OutputVersion         string                 `json:"outputVersion"`
	Tool                  result.Tool            `json:"tool"`
	Command               string                 `json:"command"`
	Status                string                 `json:"status"`
	PublishedCheckVersion string                 `json:"publishedCheckVersion"`
	Repository            publishedRepository    `json:"repository"`
	Tags                  publishedTags          `json:"tags"`
	Identities            int                    `json:"identities"`
	Documents             int                    `json:"documents"`
	OtherFiles            int                    `json:"otherFiles"`
	FindingCount          int                    `json:"findingCount"`
	Findings              []published.Finding    `json:"findings"`
	UnreadableCount       int                    `json:"unreadableCount"`
	Unreadable            []published.Unreadable `json:"unreadable"`
	Manifest              *publishedManifest     `json:"manifest,omitempty"`
	ListLimit             int                    `json:"listLimit"`
}

type publishedRepository struct {
	Head    string `json:"head"`
	Shallow bool   `json:"shallow"`
}

type publishedTags struct {
	Total       int                   `json:"total"`
	Read        int                   `json:"read"`
	UnreadCount int                   `json:"unreadCount"`
	Unread      []published.UnreadTag `json:"unread"`
}

type publishedManifest struct {
	Checked       string                    `json:"checked,omitempty"`
	Strict        bool                      `json:"strict"`
	NewCount      int                       `json:"newCount"`
	New           []published.ManifestEntry `json:"new"`
	NotHeldCount  int                       `json:"notHeldCount"`
	NotHeld       []published.ManifestEntry `json:"notHeld"`
	WithheldCount int                       `json:"withheldCount"`
	Withheld      []published.Identity      `json:"withheld"`
	Entries       int                       `json:"entries"`
	WrittenTo     string                    `json:"writtenTo,omitempty"`
}

func nonNil[T any](list []T) []T {
	if list == nil {
		return []T{}
	}
	return list
}

func (a *App) renderPublishedCheck(format string, report *published.Report, options published.Options, manifestPath string, target *manifestTarget) error {
	var manifest *publishedManifest
	if options.Manifest != nil || target != nil {
		manifest = &publishedManifest{
			Checked: manifestPath, Strict: options.Strict,
			NewCount: report.NewCount, New: nonNil(report.New),
			NotHeldCount: report.NotHeldCount, NotHeld: nonNil(report.NotHeld),
			WithheldCount: report.WithheldCount, Withheld: nonNil(report.Withheld),
			Entries: len(report.Manifest.Documents),
		}
		if target != nil {
			manifest.WrittenTo = target.path
		}
	}
	if format == "json" {
		return a.writeJSON(publishedCheckOutput{
			OutputVersion: result.OutputVersion, Tool: result.CurrentTool(), Command: checkPublishedCommandName,
			Status: report.Status, PublishedCheckVersion: published.CheckVersion,
			Repository: publishedRepository{Head: report.Head, Shallow: report.Shallow},
			Tags:       publishedTags{Total: report.TagsTotal, Read: report.TagsRead, UnreadCount: report.UnreadTagCount, Unread: nonNil(report.UnreadTags)},
			Identities: report.Identities, Documents: report.Documents, OtherFiles: report.OtherFiles,
			FindingCount: report.FindingCount, Findings: nonNil(report.Findings),
			UnreadableCount: report.UnreadableCount, Unreadable: nonNil(report.Unreadable),
			Manifest: manifest, ListLimit: published.MaxListed,
		})
	}
	q := asciiName
	var out strings.Builder
	switch report.Status {
	case published.StatusValid:
		if report.TagsRead == 0 {
			fmt.Fprintf(&out, "valid: no tag was read, of %d in the repository, and HEAD keeps the bytes the manifest pins\n", report.TagsTotal)
			break
		}
		fmt.Fprintf(&out, "valid: %d published identities keep the bytes their first tag published, at every later tag read and at HEAD\n", report.Identities)
	case published.StatusInvalid:
		fmt.Fprintf(&out, "invalid: %d finding(s)\n", report.FindingCount)
	default:
		if report.NothingPublished {
			fmt.Fprintf(&out, "incomplete: no tag was read, of %d in the repository, and no manifest pin was given, so nothing published was checked\n", report.TagsTotal)
			break
		}
		fmt.Fprintf(&out, "incomplete: no finding, and %d selected document(s) and %d tag(s) could not be read\n", report.UnreadableCount, report.UnreadTagCount)
	}
	history := "full history"
	if report.Shallow {
		history = "a shallow clone: only the tags and history it holds were read"
	}
	fmt.Fprintf(&out, "publishedCheckVersion %s · HEAD %s · %s\n", published.CheckVersion, report.Head, history)
	fmt.Fprintf(&out, "tags: %d in the repository, %d selected and read, %d selected and not readable\n", report.TagsTotal, report.TagsRead, report.UnreadTagCount)
	fmt.Fprintf(&out, "documents: %d distinct read, %d other JSON file(s)\n", report.Documents, report.OtherFiles)
	for _, finding := range report.Findings {
		fmt.Fprintf(&out, "- %s: %s %s version %s\n", finding.Code, finding.Kind, q(finding.ID), q(finding.Version))
		fmt.Fprintf(&out, "  published: %s\n", describeLocation(finding.Published))
		if finding.Found != nil {
			fmt.Fprintf(&out, "  found:     %s\n", describeLocation(*finding.Found))
		}
	}
	more(&out, report.FindingCount, len(report.Findings), "finding(s)")
	for _, unreadable := range report.Unreadable {
		ref := "at HEAD"
		if unreadable.Ref != published.HeadRef {
			ref = "at tag " + q(unreadable.Ref)
		}
		fmt.Fprintf(&out, "- unreadable %s, %s: %s\n", ref, q(unreadable.Path), display.Sanitize(unreadable.Reason))
	}
	more(&out, report.UnreadableCount, len(report.Unreadable), "unreadable file(s)")
	for _, tag := range report.UnreadTags {
		fmt.Fprintf(&out, "- tag %s not read: %s\n", q(tag.Ref), display.Sanitize(tag.Reason))
	}
	more(&out, report.UnreadTagCount, len(report.UnreadTags), "unread tag(s)")
	if manifest != nil {
		if manifest.Checked != "" {
			fmt.Fprintf(&out, "manifest %s: %d new identit(ies), %d pinned identit(ies) no tag held\n", q(manifest.Checked), report.NewCount, report.NotHeldCount)
			for _, entry := range report.New {
				fmt.Fprintf(&out, "- new: %s %s version %s, first tag %s, %s\n", entry.Kind, q(entry.ID), q(entry.Version), q(entry.FirstTag), entry.Digest)
			}
			more(&out, report.NewCount, len(report.New), "new identit(ies)")
			for _, entry := range report.NotHeld {
				fmt.Fprintf(&out, "- not held by this shallow clone: %s %s version %s, first tag %s, %s\n", entry.Kind, q(entry.ID), q(entry.Version), q(entry.FirstTag), entry.Digest)
			}
			more(&out, report.NotHeldCount, len(report.NotHeld), "pinned identit(ies) not held")
		}
		if manifest.WrittenTo != "" {
			fmt.Fprintf(&out, "manifest written: %s, %d entries\n", q(manifest.WrittenTo), manifest.Entries)
		}
		if report.WithheldCount > 0 {
			fmt.Fprintf(&out, "withheld from the manifest: %d identit(ies) no given entry pins, with a finding of their bytes; which bytes came first can turn on commit dates, so no pin is written for them\n", report.WithheldCount)
			for _, identity := range report.Withheld {
				fmt.Fprintf(&out, "- withheld: %s %s version %s\n", identity.Kind, q(identity.ID), q(identity.Version))
			}
			more(&out, report.WithheldCount, len(report.Withheld), "withheld identit(ies)")
		}
	}
	_, err := a.out.Write([]byte(out.String()))
	return err
}

func describeLocation(location published.Location) string {
	q := asciiName
	switch location.Source {
	case published.SourceManifest:
		return fmt.Sprintf("the manifest, first tag %s, %s", q(strings.TrimPrefix(location.Ref, "refs/tags/")), location.Digest)
	case published.SourceHead:
		return fmt.Sprintf("HEAD, %s, %s", q(location.Path), location.Digest)
	}
	return fmt.Sprintf("tag %s, %s, %s", q(location.Ref), q(location.Path), location.Digest)
}

// asciiName writes a name taken from the repository -- a ref, a path, an id
// or version a document declares -- as a JSON string in printable ASCII, as
// packs diff writes a pack's strings, so no character of it can act on a
// terminal or break the line. A name that is not UTF-8, which git admits in a
// path, is written as Go quotes it, each such byte as \x and two hexadecimal
// digits.
func asciiName(name string) string {
	if !utf8.ValidString(name) {
		return strconv.QuoteToASCII(name)
	}
	var builder strings.Builder
	builder.WriteByte('"')
	for _, character := range name {
		switch {
		case character == '"' || character == '\\':
			builder.WriteByte('\\')
			builder.WriteRune(character)
		case strings.ContainsRune("\b\f\n\r\t", character):
			builder.WriteString(map[rune]string{'\b': `\b`, '\f': `\f`, '\n': `\n`, '\r': `\r`, '\t': `\t`}[character])
		case character >= 0x20 && character <= 0x7e:
			builder.WriteRune(character)
		case character > 0xffff:
			high, low := utf16.EncodeRune(character)
			fmt.Fprintf(&builder, `\u%04x\u%04x`, high, low)
		default:
			fmt.Fprintf(&builder, `\u%04x`, character)
		}
	}
	builder.WriteByte('"')
	return builder.String()
}

func more(out *strings.Builder, total, listed int, what string) {
	if total > listed {
		fmt.Fprintf(out, "- and %d more %s, not listed\n", total-listed, what)
	}
}
