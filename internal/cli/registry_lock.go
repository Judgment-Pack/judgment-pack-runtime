package cli

import (
	"errors"
	"strconv"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/federation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/spf13/cobra"
)

const lockLong = " Registry format 1.1.0 is proposed, not released; the federation package says so. Nothing is fetched, and no emitter or host is run."

func (a *App) registryLockCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "lock",
		Short: "Write, check and compute coverage of a registry lock (registry 1.1.0, proposed)",
		Long:  "A lock (registry format 1.1.0 §7, proposed) names a registry by digest, its kinds, each environment's resolved registry, and, for an assembly, the index and each source's statement and signature, with the emitters' generated files. The lock signature, the serial record and verification (§§10–11) are not built yet." + lockLong,
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	command.AddCommand(a.registryLockWriteCommand(), a.registryLockCheckCommand(), a.registryLockCoverageCommand())
	return command
}

// lockExpect is the expected inputs of §9: none has a default, and one not
// given is the operation's LOCK-EXPECTED, in its report.
type lockExpect struct {
	registryID, requireIndex, environment string
}

func (e *lockExpect) register(command *cobra.Command, coverage bool) {
	command.Flags().StringVar(&e.registryID, "registry-id", "", "The expected registry id, an absolute URI; not taken from the lock")
	command.Flags().StringVar(&e.requireIndex, "require-index", "", "Whether an index is required: true or false")
	if coverage {
		command.Flags().StringVar(&e.environment, "environment", "", "The environment the consumer loads")
	}
}

func (a *App) lockExpectation(name string, e *lockExpect) (federation.Expect, error) {
	var out federation.Expect
	if e.registryID != "" {
		out.RegistryID = &e.registryID
	}
	switch e.requireIndex {
	case "":
	case "true", "false":
		require := e.requireIndex == "true"
		out.RequireIndex = &require
	default:
		return out, a.operational(name, "json", result.ExitInvocation, "JPS-INVOCATION-INPUT", "Give --require-index as true or false.")
	}
	if e.environment != "" {
		out.Environment = &e.environment
	}
	return out, nil
}

// lockEmitters are the emitters given to a writer or a checker: each a
// checkout holding the manifest at its path and the files its owns matches,
// and for a writer the exit code of the host's check of it.
type lockEmitters struct {
	single, sourced, checks []string
}

func (e *lockEmitters) register(command *cobra.Command, write bool) {
	command.Flags().StringArrayVar(&e.single, "emitter", nil, "A single registry's emitter as PATH=DIR: the manifest's path in the repository, and the local checkout holding it and the files its owns matches; repeat for each")
	command.Flags().StringArrayVar(&e.sourced, "source-emitter", nil, "An assembly's emitter as SOURCE=PATH=DIR: the source whose repository holds it, the manifest's path there, and the local checkout; repeat for each")
	if write {
		command.Flags().StringArrayVar(&e.checks, "emitter-check", nil, "The exit code of the host's check of an emitter against this registry, as PATH=EXIT or SOURCE=PATH=EXIT, once for each emitter")
	}
}

// lockEmitterInputs reads each emitter: its manifest, read inside its
// checkout, and every regular file there its owns matches, scanned as check
// mode scans them and read inside the checkout, each with its mode and
// digest. A link, a path that is not a regular file, or an absent manifest
// is a read error.
func (a *App) lockEmitterInputs(name string, flags *lockEmitters, write bool) ([]federation.Emitter, error) {
	usage := func(message string) ([]federation.Emitter, error) {
		return nil, a.operational(name, "json", result.ExitInvocation, "JPS-INVOCATION-INPUT", message)
	}
	readFailure := func(message string) ([]federation.Emitter, error) {
		return nil, a.operational(name, "json", result.ExitIO, "JPS-INPUT-READ", message)
	}
	type given struct{ source, path, dir string }
	var emitters []given
	for _, flag := range flags.single {
		parts := strings.SplitN(flag, "=", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return usage("Give each --emitter as PATH=DIR.")
		}
		emitters = append(emitters, given{path: parts[0], dir: parts[1]})
	}
	for _, flag := range flags.sourced {
		parts := strings.SplitN(flag, "=", 3)
		if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
			return usage("Give each --source-emitter as SOURCE=PATH=DIR.")
		}
		emitters = append(emitters, given{source: parts[0], path: parts[1], dir: parts[2]})
	}
	if len(emitters) == 0 {
		if len(flags.checks) > 0 {
			return usage("Give --emitter-check only for an emitter given.")
		}
		return nil, nil
	}
	if !emit.ModesRecorded() {
		return usage("This file system records no executable bit, so an emitter's files cannot be read with their modes.")
	}
	key := func(e given) string {
		if e.source == "" {
			return e.path
		}
		return e.source + "=" + e.path
	}
	checks := map[string]int{}
	for _, flag := range flags.checks {
		at := strings.LastIndex(flag, "=")
		if at <= 0 {
			return usage("Give each --emitter-check as PATH=EXIT or SOURCE=PATH=EXIT.")
		}
		exit, err := strconv.Atoi(flag[at+1:])
		if err != nil {
			return usage("Give each --emitter-check's exit code as an integer.")
		}
		if _, repeated := checks[flag[:at]]; repeated {
			return usage("Give each emitter's check once.")
		}
		checks[flag[:at]] = exit
	}
	var out []federation.Emitter
	for _, e := range emitters {
		if strings.Contains(e.dir, "://") || fssecure.IsRemotePath(e.dir) {
			return usage("Read each emitter from a local directory.")
		}
		emitter := federation.Emitter{Source: e.source, Path: e.path, Files: []federation.EmitterFile{}}
		if write {
			exit, given := checks[key(e)]
			if !given {
				return usage("Give --emitter-check for each emitter: the host's check of it against this registry.")
			}
			delete(checks, key(e))
			emitter.Check = &exit
		}
		root, err := fssecure.OpenRoot(e.dir)
		if err != nil {
			return readFailure("An emitter's directory could not be opened.")
		}
		manifest, present, err := readContained(root, federation.Path{Path: e.path, MaxBytes: emit.MaxManifestBytes})
		if err != nil || !present {
			root.Close()
			return readFailure("An emitter manifest could not be read as one bounded regular file inside its directory.")
		}
		emitter.Manifest = manifest
		if parsed, _ := emit.ReadManifest(manifest.Data); !manifest.Oversize && parsed != nil {
			owned, problems := emit.ScanOwned(e.dir, parsed.Owns)
			if len(problems) > 0 {
				root.Close()
				return readFailure("An emitter's owned files could not be read: a link, or a path that is not a regular file or cannot be listed.")
			}
			for _, f := range owned {
				file, present, err := readContained(root, federation.Path{Path: f.Path, MaxBytes: emit.MaxFileBytes})
				if err != nil || !present || file.Oversize {
					root.Close()
					return readFailure("An emitter's owned file could not be read as one bounded regular file inside its directory.")
				}
				emitter.Files = append(emitter.Files, federation.EmitterFile{Path: f.Path, Mode: f.Mode, Digest: federation.FileDigest(file.Data)})
			}
		}
		root.Close()
		out = append(out, emitter)
	}
	if len(checks) > 0 {
		return usage("An --emitter-check names no emitter given.")
	}
	return out, nil
}

// lockInputFailure refuses a lock operation the library would not run: an
// input error names its reason and exits 3.
func (a *App) lockInputFailure(name string, err error) error {
	var input *federation.InputError
	if errors.As(err, &input) {
		return a.operational(name, "json", result.ExitInvocation, "JPS-INVOCATION-INPUT", "The inputs give no lock: "+input.Reason+".")
	}
	return nil
}

// localInputs refuses a remote input, and more than one input read from
// standard input.
func (a *App) localInputs(name string, paths []string) error {
	stdin := 0
	for _, path := range paths {
		if strings.Contains(path, "://") || fssecure.IsRemotePath(path) {
			return a.operational(name, "json", result.ExitInvocation, "JPS-INVOCATION-INPUT", "Use local files or standard input.")
		}
		if path == "-" {
			stdin++
		}
	}
	if stdin > 1 {
		return a.operational(name, "json", result.ExitInvocation, "JPS-INVOCATION-INPUT", "Only one input can read standard input.")
	}
	return nil
}

func (a *App) lockKinds(name string, paths []string) ([]registry.Document, error) {
	var out []registry.Document
	for _, path := range paths {
		document, failure := a.federationDocument(name, path, registry.MaxKindBytes, "kind declaration")
		if failure != nil {
			return nil, failure
		}
		out = append(out, document)
	}
	return out, nil
}

func (a *App) registryLockWriteCommand() *cobra.Command {
	inputs := &federationInputs{}
	emitters := &lockEmitters{}
	var registryPath, previous, firstSerial, key, out string
	var kinds, signatures, repositoryKeys []string
	command := &cobra.Command{
		Use:     "write (--registry FILE --kind FILE... | --federation FILE --source NAME=COMMIT=DIR... --source-signature NAME=FILE... --key HEX --repository-key URI=HEX... (--known-index FILE... --known-lock FILE... | --no-known)) (--previous FILE | --first-serial N) [--out FILE]",
		Short:   "Write the lock of a registry or an assembly (registry 1.1.0, proposed)",
		Long:    "Write a lock by registry format 1.1.0 §8 (proposed): the registry's digest, its kinds and each environment's resolved digest; for an assembly, the index's digest and each source's statement and accepted source signature, each verified by the key the signer set names (--key, the standard owner's, and --repository-key for each repository); and the emitters' files. The serial is one above the --previous lock's, or the --first-serial given out of band: there is no default. Each emitter needs --emitter-check, the exit code of the host's check of it against this registry; the lock lists emitters only when every check exited 0. The lock is written to --out, by default <name>.lock.json beside a <name>.json registry or manifest, as its RFC 8785 bytes. Exit 0 when written; 1 when refused, writing {\"refused\": [codes]} and no lock." + lockLong,
		Example: "jpack registry lock write --registry registry.json --kind kinds/database.json --kind kinds/db-role.json --previous registry.lock.json",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			const name = "registry lock write"
			usage := func(message string) error {
				return a.operational(name, "json", result.ExitInvocation, "JPS-INVOCATION-INPUT", message)
			}
			if (previous == "") == (firstSerial == "") {
				return usage("Give --previous, the registry id's previous lock, or --first-serial, the first serial given out of band, and not both: there is no default.")
			}
			single := registryPath != ""
			if single == (inputs.manifest != "") {
				return usage("Give --registry with its --kind declarations, or --federation with its sources, and not both.")
			}
			if single && (len(inputs.sources)+len(inputs.knownIndex)+len(inputs.knownLocks) > 0 || inputs.noKnown) {
				return usage("A single registry is given no sources and no known list.")
			}
			if !single && len(kinds) > 0 {
				return usage("An assembly's kinds are its standard source's.")
			}
			if out == "" {
				input := registryPath
				if !single {
					input = inputs.manifest
				}
				if input == "-" || !strings.HasSuffix(input, ".json") {
					return usage("Give --out: an input not named <name>.json has no default lock name.")
				}
				out = strings.TrimSuffix(input, ".json") + ".lock.json"
			}
			if out == "-" || strings.Contains(out, "://") || fssecure.IsRemotePath(out) {
				return usage("Write --out to a local file.")
			}
			files := append([]string{registryPath, previous, inputs.manifest}, kinds...)
			files = append(files, append(inputs.knownIndex, inputs.knownLocks...)...)
			for _, flag := range signatures {
				parts := strings.SplitN(flag, "=", 2)
				if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
					return usage("Give each --source-signature as NAME=FILE.")
				}
				files = append(files, parts[1])
			}
			if failure := a.localInputs(name, files); failure != nil {
				return failure
			}
			in := federation.WriteInput{}
			if firstSerial != "" {
				serial, err := strconv.ParseInt(firstSerial, 10, 64)
				if err != nil {
					return usage("Give --first-serial as an integer.")
				}
				in.FirstSerial = &serial
			}
			if single {
				if len(signatures)+len(repositoryKeys) > 0 || key != "" {
					return usage("A single registry's lock names no source signature and no signer set.")
				}
				document, failure := a.federationDocument(name, registryPath, registry.MaxRegistryBytes, "registry")
				if failure != nil {
					return failure
				}
				in.Registry = &document
				if in.Kinds, failure = a.lockKinds(name, kinds); failure != nil {
					return failure
				}
			} else {
				assembly, failure := a.federationInput(name, inputs, previous)
				if failure != nil {
					return failure
				}
				in.Assembly = &assembly
				if len(signatures) > 0 {
					in.SourceSignatures = map[string]registry.Document{}
				}
				for _, flag := range signatures {
					parts := strings.SplitN(flag, "=", 2)
					if _, repeated := in.SourceSignatures[parts[0]]; repeated {
						return usage("Give each source's signature once.")
					}
					document, failure := a.federationDocument(name, parts[1], federation.MaxSignatureBytes, "source signature")
					if failure != nil {
						return failure
					}
					in.SourceSignatures[parts[0]] = document
				}
				in.Signers = &federation.Signers{Key: key}
				if len(repositoryKeys) > 0 {
					in.Signers.Repositories = map[string]string{}
				}
				for _, flag := range repositoryKeys {
					at := strings.LastIndex(flag, "=")
					if at <= 0 {
						return usage("Give each --repository-key as URI=HEX.")
					}
					if _, repeated := in.Signers.Repositories[flag[:at]]; repeated {
						return usage("Give each repository's key once.")
					}
					in.Signers.Repositories[flag[:at]] = flag[at+1:]
				}
			}
			if previous != "" {
				document, failure := a.federationDocument(name, previous, federation.MaxLockBytes, "previous lock")
				if failure != nil {
					return failure
				}
				in.Previous = &document
			}
			var failure error
			if in.Emitters, failure = a.lockEmitterInputs(name, emitters, true); failure != nil {
				return failure
			}
			lock, err := federation.WriteLock(in)
			var refusal *federation.Refusal
			switch {
			case errors.As(err, &refusal):
				return a.lockRefusal(refusal)
			case err != nil:
				if failure := a.lockInputFailure(name, err); failure != nil {
					return failure
				}
				return a.federationFailure(name, inputs, err, "The lock could not be written in its canonical form.")
			}
			if replaceFile(out, lock) != nil {
				return a.operational(name, "json", result.ExitIO, "JPS-OUTPUT-WRITE", "The lock could not be written.")
			}
			return nil
		},
	}
	inputs.register(command)
	emitters.register(command, true)
	command.Flags().StringVar(&registryPath, "registry", "", "The registry to lock (or - for standard input)")
	command.Flags().StringArrayVar(&kinds, "kind", nil, "A kind declaration the registry pins; repeat for each")
	command.Flags().StringArrayVar(&signatures, "source-signature", nil, "An assembly's source signature as NAME=FILE: the source's name in the manifest and its signature document; repeat for each source")
	command.Flags().StringVar(&key, "key", "", "The standard owner's public key, 64 lowercase hexadecimal characters, obtained out of band: it signs the standard source's statement")
	command.Flags().StringArrayVar(&repositoryKeys, "repository-key", nil, "A repository's public key as URI=HEX: the repository identity and the key that signs its statements; repeat for each")
	command.Flags().StringVar(&previous, "previous", "", "The registry id's previous lock: the serial is one above its")
	command.Flags().StringVar(&firstSerial, "first-serial", "", "The first serial, from 1 to 9007199254740991, given out of band when there is no previous lock")
	command.Flags().StringVar(&out, "out", "", "Write the lock here (default <name>.lock.json beside a <name>.json input)")
	return command
}

// lockRefusal writes a writer's refusal, its codes as RFC 8785 JSON and one
// newline, and exits 1; no lock is written.
func (a *App) lockRefusal(refusal *federation.Refusal) error {
	codes := make([]any, 0, len(refusal.Codes))
	for _, code := range refusal.Codes {
		codes = append(codes, code)
	}
	canonical, err := jcs.EncodeDocument(map[string]any{"refused": codes})
	if err != nil {
		return &handledExit{code: result.ExitIO}
	}
	if _, err := a.out.Write(append(canonical, '\n')); err != nil {
		return &handledExit{code: result.ExitIO}
	}
	return &handledExit{code: result.ExitInvalid}
}

func (a *App) registryLockCheckCommand() *cobra.Command {
	expect := &lockExpect{}
	emitters := &lockEmitters{}
	var lockPath, registryPath, index string
	var kinds []string
	command := &cobra.Command{
		Use:     "check --lock FILE --registry FILE --kind FILE... [--index FILE] [--emitter PATH=DIR... | --source-emitter SOURCE=PATH=DIR...] --registry-id URI --require-index true|false",
		Short:   "Check a lock against its registry, index and emitters and write the report (registry 1.1.0, proposed)",
		Long:    "Check a lock by registry format 1.1.0 §9 (proposed): its form and canonical bytes, the expected registry id and index requirement, then the registry (validated), its kinds and each environment's resolved digest, the index and each source's statement when the lock names an index, and each emitter's manifest and owned files when it names emitters. An index given for a lock that names none, or one missing for a lock that names one, is refused, never skipped. A check establishes byte identity, not authorship: verification (§11) is not built yet. The report is RFC 8785 canonical JSON followed by one newline. Exit 0 when the lock checks, 1 when it does not." + lockLong,
		Example: "jpack registry lock check --lock registry.lock.json --registry registry.json --kind kinds/database.json --kind kinds/db-role.json --registry-id https://example.test/registries/platform --require-index false",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			const name = "registry lock check"
			if lockPath == "" || registryPath == "" {
				return a.operational(name, "json", result.ExitInvocation, "JPS-INVOCATION-INPUT", "Supply --lock and --registry.")
			}
			if failure := a.localInputs(name, append([]string{lockPath, registryPath, index}, kinds...)); failure != nil {
				return failure
			}
			expected, failure := a.lockExpectation(name, expect)
			if failure != nil {
				return failure
			}
			in := federation.CheckInput{Expect: expected}
			if in.Lock, failure = a.federationDocument(name, lockPath, federation.MaxLockBytes, "lock"); failure != nil {
				return failure
			}
			if in.Registry, failure = a.federationDocument(name, registryPath, registry.MaxRegistryBytes, "registry"); failure != nil {
				return failure
			}
			if in.Kinds, failure = a.lockKinds(name, kinds); failure != nil {
				return failure
			}
			if index != "" {
				document, failure := a.federationDocument(name, index, federation.MaxIndexBytes, "index")
				if failure != nil {
					return failure
				}
				in.Index = &document
			}
			if in.Emitters, failure = a.lockEmitterInputs(name, emitters, false); failure != nil {
				return failure
			}
			report, err := federation.CheckLock(in)
			if err != nil {
				if failure := a.lockInputFailure(name, err); failure != nil {
					return failure
				}
				return a.operational(name, "json", result.ExitIO, "JPS-OUTPUT-WRITE", "The check could not be written in its canonical form.")
			}
			return a.federationReport(report)
		},
	}
	expect.register(command, false)
	emitters.register(command, false)
	command.Flags().StringVar(&lockPath, "lock", "", "The lock to check (or - for standard input)")
	command.Flags().StringVar(&registryPath, "registry", "", "The registry the lock names: the assembled one for an assembled lock")
	command.Flags().StringArrayVar(&kinds, "kind", nil, "A kind declaration the registry pins; repeat for each")
	command.Flags().StringVar(&index, "index", "", "The assembled index, for a lock that names one")
	return command
}

func (a *App) registryLockCoverageCommand() *cobra.Command {
	expect := &lockExpect{}
	var lockPath, resolved, file, fileSource string
	command := &cobra.Command{
		Use:     "coverage --lock FILE --registry-id URI --require-index true|false --environment ENV (--resolved FILE | --file PATH=LOCAL [--file-source NAME])",
		Short:   "Say whether a lock covers a resolved registry or a generated file and write the report (registry 1.1.0, proposed)",
		Long:    "Compute coverage by registry format 1.1.0 §9.1 (proposed): the lock is first held to §9's step 1, then a resolved registry is covered when it is of the expected environment and is the one the lock names for it, and a generated file when an emitters item lists its path, mode (read from disk) and digest, of its --file-source in an assembled lock. Coverage establishes byte identity with what the lock names; a consumer verifies the lock first (§11, not built yet). The report names no registry, and is RFC 8785 canonical JSON followed by one newline. Exit 0 when covered, 1 when not." + lockLong,
		Example: "jpack registry lock coverage --lock registry.lock.json --registry-id https://example.test/registries/platform --require-index false --environment prod --resolved resolved-prod.json",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			const name = "registry lock coverage"
			usage := func(message string) error {
				return a.operational(name, "json", result.ExitInvocation, "JPS-INVOCATION-INPUT", message)
			}
			if lockPath == "" {
				return usage("Supply --lock.")
			}
			if resolved == "" && file == "" {
				return usage("Supply --resolved, a resolved registry, or --file, a generated file, or both.")
			}
			if fileSource != "" && file == "" {
				return usage("Give --file-source only with --file.")
			}
			paths := []string{lockPath, resolved}
			parts := []string{"", ""}
			if file != "" {
				parts = strings.SplitN(file, "=", 2)
				if len(parts) != 2 || parts[0] == "" || parts[1] == "" || parts[1] == "-" {
					return usage("Give --file as PATH=LOCAL: the file's path in the repository, and the local file.")
				}
				paths = append(paths, parts[1])
				if !emit.ModesRecorded() {
					return usage("This file system records no executable bit, so a file's mode cannot be read.")
				}
			}
			if failure := a.localInputs(name, paths); failure != nil {
				return failure
			}
			expected, failure := a.lockExpectation(name, expect)
			if failure != nil {
				return failure
			}
			in := federation.CoverageInput{Expect: expected}
			if in.Lock, failure = a.federationDocument(name, lockPath, federation.MaxLockBytes, "lock"); failure != nil {
				return failure
			}
			if resolved != "" {
				document, failure := a.federationDocument(name, resolved, registry.MaxResolvedBytes, "resolved registry")
				if failure != nil {
					return failure
				}
				in.Resolved = &document
			}
			if file != "" {
				covered, failure := a.coveredFile(name, parts[1])
				if failure != nil {
					return failure
				}
				covered.Source, covered.Path = fileSource, parts[0]
				in.File = covered
			}
			report, err := federation.Coverage(in)
			if err != nil {
				if failure := a.lockInputFailure(name, err); failure != nil {
					return failure
				}
				return a.operational(name, "json", result.ExitIO, "JPS-OUTPUT-WRITE", "The report could not be written in its canonical form.")
			}
			return a.federationReport(report)
		},
	}
	expect.register(command, true)
	command.Flags().StringVar(&lockPath, "lock", "", "The lock (or - for standard input)")
	command.Flags().StringVar(&resolved, "resolved", "", "A resolved registry the consumer holds")
	command.Flags().StringVar(&file, "file", "", "A generated file the consumer holds, as PATH=LOCAL: its path in the repository and the local file, whose mode is read from disk")
	command.Flags().StringVar(&fileSource, "file-source", "", "The source whose repository holds the file, for an assembled lock")
	return command
}

// coveredFile reads a generated file: one bounded regular file, never
// through a link, with its mode as git records it and its digest.
func (a *App) coveredFile(name, path string) (*federation.CoveredFile, error) {
	file, err := a.openInput(path, "generated file")
	if err != nil {
		return nil, a.operational(name, "json", result.ExitIO, "JPS-INPUT-READ", "The generated file could not be read as one bounded regular file.")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, a.operational(name, "json", result.ExitIO, "JPS-INPUT-READ", "The generated file could not be read as one bounded regular file.")
	}
	data, err := readBounded(file, emit.MaxFileBytes)
	if err != nil {
		return nil, a.operational(name, "json", result.ExitIO, "JPS-INPUT-READ", "The generated file could not be read as one bounded regular file.")
	}
	mode := emit.ModeRegular
	if info.Mode().Perm()&0o100 != 0 {
		mode = emit.ModeExecutable
	}
	return &federation.CoveredFile{Mode: mode, Digest: federation.FileDigest(data)}, nil
}
