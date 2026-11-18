package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/federation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/spf13/cobra"
)

// federationInputs are the flags of registry assemble and check-index: the
// manifest, each source's commit and checkout, and the known list of
// registry 1.1.0 §4, which has no default and is given as empty with
// --no-known.
type federationInputs struct {
	manifest    string
	sources     []string
	knownIndex  []string
	knownLocks  []string
	noKnown     bool
	description string
}

func (f *federationInputs) register(command *cobra.Command) {
	command.Flags().StringVar(&f.manifest, "federation", "", "The federation manifest (or - for standard input)")
	command.Flags().StringArrayVar(&f.sources, "source", nil, "A source as NAME=COMMIT=DIR: its name in the manifest, the full commit its bytes are read at, and the local directory holding them at that commit; repeat for each source")
	command.Flags().StringArrayVar(&f.knownIndex, "known-index", nil, "A known assembled index; repeat for each")
	command.Flags().StringArrayVar(&f.knownLocks, "known-lock", nil, "A known assembled registry's lock; repeat for each")
	command.Flags().BoolVar(&f.noKnown, "no-known", false, "State that the list of known assembled indexes and locks is empty")
}

const federationLong = " Each source's paths are read from its directory, as the manifest names them, and a path whose file is absent is not given (FED-SOURCE-MISSING); nothing is fetched. The known list has no default: give --known-index and --known-lock for each, or --no-known for none. The report is RFC 8785 canonical JSON followed by one newline. Registry format 1.1.0 is proposed, not released; the federation package says so."

func (a *App) registryAssembleCommand() *cobra.Command {
	inputs := &federationInputs{}
	registryOut, indexOut := "", ""
	command := &cobra.Command{
		Use:     "assemble --federation FILE --source NAME=COMMIT=DIR... (--known-index FILE... --known-lock FILE... | --no-known)",
		Short:   "Assemble one registry from a federation's sources and write the report (registry 1.1.0, proposed)",
		Long:    "Assemble the registry a federation manifest names from its sources, by registry format 1.1.0 §4 (proposed), and write the report. With no error, write the assembled registry and the index to --registry-out and --index-out when given; with an error, write neither. Exit 0 when the assembly has no error (warnings allowed), 1 when it has one." + federationLong,
		Example: "jpack registry assemble --federation federation.json --source billing=<commit>=checkouts/billing --source standard=<commit>=checkouts/platform --no-known --registry-out assembled.json --index-out index.json",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			const name = "registry assemble"
			for _, path := range []string{registryOut, indexOut} {
				if path != "" && (path == "-" || strings.Contains(path, "://") || fssecure.IsRemotePath(path)) {
					return a.operational(name, "json", result.ExitInvocation, "JPS-INVOCATION-INPUT", "Write --registry-out and --index-out to local files.")
				}
			}
			if registryOut != "" && registryOut == indexOut {
				return a.operational(name, "json", result.ExitInvocation, "JPS-INVOCATION-INPUT", "Write the assembled registry and the index to two files.")
			}
			in, failure := a.federationInput(name, inputs, "")
			if failure != nil {
				return failure
			}
			out, err := federation.Assemble(in)
			if err != nil {
				return a.federationFailure(name, inputs, err, "The assembly could not be written in its canonical form.")
			}
			if out.Report.Valid {
				for _, write := range []struct {
					path string
					data []byte
				}{{registryOut, out.Registry}, {indexOut, out.Index}} {
					if write.path != "" && replaceFile(write.path, write.data) != nil {
						return a.operational(name, "json", result.ExitIO, "JPS-OUTPUT-WRITE", "The assembled registry or the index could not be written.")
					}
				}
			}
			return a.federationReport(out.Report)
		},
	}
	inputs.register(command)
	command.Flags().StringVar(&registryOut, "registry-out", "", "Write the assembled registry here, for an assembly with no error")
	command.Flags().StringVar(&indexOut, "index-out", "", "Write the index here, for an assembly with no error")
	return command
}

func (a *App) registryCheckIndexCommand() *cobra.Command {
	inputs := &federationInputs{}
	index := ""
	command := &cobra.Command{
		Use:     "check-index --index FILE --federation FILE --source NAME=COMMIT=DIR... (--known-index FILE... --known-lock FILE... | --no-known)",
		Short:   "Check an assembled index against a second assembly and write the report (registry 1.1.0, proposed)",
		Long:    "Assemble again from the sources at the commits the index names, by registry format 1.1.0 §5.1 (proposed), and hold the index that assembly would write to the given one, JCS for JCS. Exit 0 when they are equal and the assembly has no error, 1 otherwise. A verifier that cannot read the repositories holds only the index's word for where the bytes came from." + federationLong,
		Example: "jpack registry check-index --index index.json --federation federation.json --source billing=<commit>=checkouts/billing --source standard=<commit>=checkouts/platform --no-known",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			const name = "registry check-index"
			if index == "" {
				return a.operational(name, "json", result.ExitInvocation, "JPS-INVOCATION-INPUT", "Supply --index, the assembled index to check.")
			}
			in, failure := a.federationInput(name, inputs, index)
			if failure != nil {
				return failure
			}
			document, failure := a.federationDocument(name, index, federation.MaxIndexBytes, "index")
			if failure != nil {
				return failure
			}
			report, err := federation.CheckIndex(in, document)
			if err != nil {
				return a.federationFailure(name, inputs, err, "The check could not be written in its canonical form.")
			}
			return a.federationReport(report)
		},
	}
	inputs.register(command)
	command.Flags().StringVar(&index, "index", "", "The assembled index to check (or - for standard input)")
	return command
}

// federationFailure refuses an assembly that returned an error: a known
// index or lock that is not admitted is an input error naming the flag and
// the file it gave, and anything else is an output that could not be
// written.
func (a *App) federationFailure(name string, inputs *federationInputs, err error, unwritten string) error {
	var known *federation.KnownError
	if !errors.As(err, &known) {
		return a.operational(name, "json", result.ExitIO, "JPS-OUTPUT-WRITE", unwritten)
	}
	flag, paths := "--known-index", inputs.knownIndex
	if known.Lock {
		flag, paths = "--known-lock", inputs.knownLocks
	}
	given := ""
	if known.Position >= 0 && known.Position < len(paths) {
		given = " " + strconv.Quote(paths[known.Position])
	}
	return a.operational(name, "json", result.ExitIO, "JPS-INPUT-READ", fmt.Sprintf("The %s%s (number %d) could not be admitted (%s): a known index or lock that cannot be read cannot refuse the source it names.", flag, given, known.Position+1, known.Code))
}

// federationDocument reads one bounded input; one over its bound is given
// as such, and refused by the operation as REG-DOC-SIZE.
func (a *App) federationDocument(name, path string, limit int, what string) (registry.Document, error) {
	data, err := a.readInput(path, int64(limit), what)
	switch {
	case errors.Is(err, fssecure.ErrTooLarge):
		return registry.Document{Oversize: true}, nil
	case err != nil:
		return registry.Document{}, a.operational(name, "json", result.ExitIO, "JPS-INPUT-READ", "The "+what+" could not be read as one bounded regular file or standard input stream.")
	}
	return registry.Document{Data: data}, nil
}

// federationInput reads the inputs of an assembly. Each source's paths are
// read under its directory, contained in it: a path that leaves it, or a
// symbolic link, is a read error, and a path that is absent is not given.
func (a *App) federationInput(name string, inputs *federationInputs, index string) (federation.Input, error) {
	usage := func(message string) (federation.Input, error) {
		return federation.Input{}, a.operational(name, "json", result.ExitInvocation, "JPS-INVOCATION-INPUT", message)
	}
	if inputs.manifest == "" {
		return usage("Supply --federation, the federation manifest.")
	}
	if inputs.noKnown == (len(inputs.knownIndex)+len(inputs.knownLocks) > 0) {
		return usage("Give the known list: --known-index and --known-lock for each known assembled index and lock, or --no-known for none.")
	}
	files := append([]string{inputs.manifest, index}, append(inputs.knownIndex, inputs.knownLocks...)...)
	stdin := 0
	for _, path := range files {
		if strings.Contains(path, "://") || fssecure.IsRemotePath(path) {
			return usage("Use local files or standard input.")
		}
		if path == "-" {
			stdin++
		}
	}
	if stdin > 1 {
		return usage("Only one input can read standard input.")
	}
	type given struct{ commit, dir string }
	sources := map[string]given{}
	for _, flag := range inputs.sources {
		parts := strings.SplitN(flag, "=", 3)
		if len(parts) != 3 || parts[0] == "" || parts[2] == "" {
			return usage("Give each --source as NAME=COMMIT=DIR.")
		}
		if _, repeated := sources[parts[0]]; repeated {
			return usage("Give each source once.")
		}
		if strings.Contains(parts[2], "://") || fssecure.IsRemotePath(parts[2]) {
			return usage("Read each source from a local directory.")
		}
		sources[parts[0]] = given{commit: parts[1], dir: parts[2]}
	}
	if len(sources) > federation.MaxSources {
		return usage("A manifest names at most 256 sources.")
	}
	manifest, failure := a.federationDocument(name, inputs.manifest, federation.MaxManifestBytes, "federation manifest")
	if failure != nil {
		return federation.Input{}, failure
	}
	in := federation.Input{Manifest: manifest, Sources: map[string]federation.SourceInput{}}
	if len(inputs.knownIndex) > federation.MaxKnown || len(inputs.knownLocks) > federation.MaxKnown {
		// FED-KNOWN-LIMIT refuses the assembly before any source, or any
		// document of the list, is read (§4).
		in.Known = federation.Known{Indexes: make([]registry.Document, len(inputs.knownIndex)), Locks: make([]registry.Document, len(inputs.knownLocks))}
		return in, nil
	}
	known := func(paths []string, limit int, what string) ([]registry.Document, error) {
		out := []registry.Document{}
		for _, path := range paths {
			document, failure := a.federationDocument(name, path, limit, what)
			if failure != nil {
				return nil, failure
			}
			out = append(out, document)
		}
		return out, nil
	}
	var err error
	if in.Known.Indexes, err = known(inputs.knownIndex, federation.MaxIndexBytes, "known index"); err != nil {
		return federation.Input{}, err
	}
	if in.Known.Locks, err = known(inputs.knownLocks, federation.MaxLockBytes, "known lock"); err != nil {
		return federation.Input{}, err
	}
	paths := map[string][]federation.Path{}
	if !manifest.Oversize {
		paths = federation.SourcePaths(manifest.Data)
	}
	for sourceName, s := range sources {
		files := map[string]registry.Document{}
		if wanted := paths[sourceName]; len(wanted) > 0 {
			root, err := fssecure.OpenRoot(s.dir)
			if err != nil {
				return federation.Input{}, a.operational(name, "json", result.ExitIO, "JPS-INPUT-READ", "A source's directory could not be opened.")
			}
			for _, p := range wanted {
				document, present, err := readContained(root, p)
				if err != nil {
					root.Close()
					return federation.Input{}, a.operational(name, "json", result.ExitIO, "JPS-INPUT-READ", "A source's path could not be read as one bounded regular file inside its directory.")
				}
				if present {
					files[p.Path] = document
				}
			}
			root.Close()
		}
		in.Sources[sourceName] = federation.SourceInput{Commit: s.commit, Files: files}
	}
	return in, nil
}

// readContained reads one path of a source under its directory. An absent
// file is not given.
func readContained(root *fssecure.Root, p federation.Path) (registry.Document, bool, error) {
	file, err := root.Open(p.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return registry.Document{}, false, nil
	}
	if err != nil {
		return registry.Document{}, false, err
	}
	defer file.Close()
	data, err := readBounded(file, int64(p.MaxBytes))
	switch {
	case errors.Is(err, fssecure.ErrTooLarge):
		return registry.Document{Oversize: true}, true, nil
	case err != nil:
		return registry.Document{}, false, err
	}
	return registry.Document{Data: data}, true, nil
}

func (a *App) federationReport(report federation.Report) error {
	canonical, err := report.Canonical()
	if err != nil {
		return &handledExit{code: result.ExitIO}
	}
	if _, err := a.out.Write(append(canonical, '\n')); err != nil {
		return &handledExit{code: result.ExitIO}
	}
	if !report.Valid {
		return &handledExit{code: result.ExitInvalid}
	}
	return nil
}
