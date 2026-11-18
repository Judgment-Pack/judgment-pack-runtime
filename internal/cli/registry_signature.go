package cli

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/federation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/jcs"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/regsig"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/spf13/cobra"
)

// The signing commands of registry format 1.1.0 (§11, Signing): a seed is
// read from a file alone, held to the rules a trail's signing key is held to
// (internal/audit), and refused before anything is signed when its public
// key is not the one named. Neither the seed nor a key is ever written to
// standard output or standard error: a command names the key by its
// identifier alone.

const registryKeyRules = " The seed (64 hexadecimal characters, as jpack audit key generate writes one) is read from the file --key names, and from nothing else: no flag or environment variable carries a seed. It is held to the rules a trail's signing key is held to: named by its real path, with no symbolic link anywhere in it; one regular file with one name; on unix owned by the user the runtime runs as and readable by nobody else, in directories owned by root or by that user that nobody else can write unless their sticky bit is set. On Windows a key's privacy cannot be checked, so no key signs there. --public-key names a file holding the public key the seed is expected to have; a seed whose public key is another is refused before anything is signed. A signature document is never written over anything. Output names the key by its identifier alone, never the key or the seed."

// registrySigner reads the expected public key and the seed, and refuses a
// seed held to the key rules that fails one, or whose public key is not the
// one named.
func (a *App) registrySigner(name, keyPath, publicKeyPath string) (*audit.Signer, error) {
	usage := func(message string) (*audit.Signer, error) {
		return nil, a.operational(name, "json", result.ExitInvocation, "JPS-INVOCATION-INPUT", message)
	}
	if keyPath == "" || publicKeyPath == "" {
		return usage("Supply --key, the seed file to sign with, and --public-key, the file holding its public key.")
	}
	if keyPath == "-" || publicKeyPath == "-" || !localInput(keyPath) || !localInput(publicKeyPath) {
		return usage("Read --key and --public-key from local files.")
	}
	data, err := a.readInput(publicKeyPath, 4096, "public key")
	if err != nil {
		return nil, a.operational(name, "json", result.ExitIO, "JPS-INPUT-READ", "The public key "+display.Sanitize(publicKeyPath)+" could not be read as one bounded regular file.")
	}
	public, err := audit.ParsePublicKey(data)
	if err != nil {
		return usage("The public key " + display.Sanitize(publicKeyPath) + " is not one: " + err.Error() + ".")
	}
	expected := hex.EncodeToString(public)
	absoluteKey, err := filepath.Abs(keyPath)
	if err != nil {
		return usage("The signing key's path could not be made absolute.")
	}
	signer, err := audit.LoadSigner(absoluteKey, nil, &expected)
	if err != nil {
		return nil, a.operational(name, "json", result.ExitInvalid, "JPS-AUDIT-KEY-REFUSED", fmt.Sprintf("The signing key at %s is refused, so nothing was signed: %s.", display.Sanitize(absoluteKey), audit.KeyRefusal(err)))
	}
	return signer, nil
}

// writeSignature writes a new signature document and the command's output:
// the key's identifier, the digest signed under its member's name, and the
// document's path, as RFC 8785 canonical JSON and one newline.
func (a *App) writeSignature(name, target string, document []byte, keyID, member, digest string) error {
	if err := writeNewFile(target, document); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return a.operational(name, "json", result.ExitIO, "JPS-OUTPUT-WRITE", "Something is already at "+display.Sanitize(target)+", and a signature is never written over anything; remove it first to sign again.")
		}
		return a.operational(name, "json", result.ExitIO, "JPS-OUTPUT-WRITE", "The signature could not be written to "+display.Sanitize(target)+".")
	}
	output, err := jcs.EncodeDocument(map[string]any{"keyId": keyID, member: digest, "signature": display.Sanitize(target)})
	if err != nil {
		return &handledExit{code: result.ExitIO}
	}
	if _, err := a.out.Write(append(output, '\n')); err != nil {
		return &handledExit{code: result.ExitIO}
	}
	return nil
}

// localInput is a path that names no URL and no remote share.
func localInput(path string) bool {
	return !strings.Contains(path, "://") && !fssecure.IsRemotePath(path)
}

func localOutput(path string) bool {
	return path != "-" && localInput(path)
}

func (a *App) registryLockSignCommand() *cobra.Command {
	expect := &lockExpect{}
	emitters := &lockEmitters{}
	var lockPath, registryPath, index, keyPath, publicKeyPath, output string
	var kinds []string
	command := &cobra.Command{
		Use:     "sign --lock FILE --registry FILE --kind FILE... [--index FILE] [--emitter PATH=DIR... | --source-emitter SOURCE=PATH=DIR...] --registry-id URI --require-index true|false --key SEED-FILE --public-key FILE [--output FILE]",
		Short:   "Check a lock and sign it with the standard owner's key (registry 1.1.0)",
		Long:    "Sign a lock by registry format 1.1.0 §§10–11: the lock is first checked as jpack registry lock check checks it, with the same inputs, and a lock that does not check, or is not canonical, is not signed: the report is written and nothing else. A lock that checks is signed over §10.3's message for its digest, and the lock signature document is written to <lock>.sig.json, or to --output. The output is {\"keyId\",\"lockDigest\",\"signature\"}, the key's identifier, the digest signed and the path the document is written to, as RFC 8785 canonical JSON and one newline. A signature says the key's holder signed this lock's digest; it does not say the registry is right, reviewed or current, or when it was signed." + registryKeyRules + lockLong,
		Example: "jpack registry lock sign --lock registry.lock.json --registry registry.json --kind kinds/database.json --registry-id https://example.test/registries/platform --require-index false --key /secure/standard.seed --public-key standard.pub",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			const name = "registry lock sign"
			usage := func(message string) error {
				return a.operational(name, "json", result.ExitInvocation, "JPS-INVOCATION-INPUT", message)
			}
			if lockPath == "" || registryPath == "" {
				return usage("Supply --lock and --registry.")
			}
			target := output
			if target == "" {
				if lockPath == "-" {
					return usage("Give --output for a lock read from standard input.")
				}
				target = lockPath + ".sig.json"
			}
			if !localOutput(target) {
				return usage("Write the signature to a local file.")
			}
			if failure := a.localInputs(name, append([]string{lockPath, registryPath, index}, kinds...)); failure != nil {
				return failure
			}
			expected, failure := a.lockExpectation(name, expect)
			if failure != nil {
				return failure
			}
			signer, failure := a.registrySigner(name, keyPath, publicKeyPath)
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
			if !report.Valid {
				return a.federationReport(report)
			}
			digest := federation.LockDigest(in.Lock.Data)
			document, err := signer.SignRegistryLock(digest)
			if err != nil {
				return a.operational(name, "json", result.ExitIO, "JPS-OUTPUT-WRITE", "The lock could not be signed.")
			}
			return a.writeSignature(name, target, document, signer.KeyID(), regsig.Lock.DigestMember, digest)
		},
	}
	expect.register(command, false)
	emitters.register(command, false)
	command.Flags().StringVar(&lockPath, "lock", "", "The lock to check and sign (or - for standard input, with --output)")
	command.Flags().StringVar(&registryPath, "registry", "", "The registry the lock names: the assembled one for an assembled lock")
	command.Flags().StringArrayVar(&kinds, "kind", nil, "A kind declaration the registry pins; repeat for each")
	command.Flags().StringVar(&index, "index", "", "The assembled index, for a lock that names one")
	command.Flags().StringVar(&keyPath, "key", "", "The file holding the standard owner's seed")
	command.Flags().StringVar(&publicKeyPath, "public-key", "", "The file holding the public key the seed is expected to have")
	command.Flags().StringVar(&output, "output", "", "Write the lock signature here instead of <lock>.sig.json")
	return command
}

func (a *App) registrySourceCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "source",
		Short: "Sign a repository's source statement (registry 1.1.0)",
		Long:  "A source statement (registry format 1.1.0 §10.2) is a repository's documents, by path and digest, at one commit; its repository's key signs it, and a lock writer and a verifier check it against the index." + lockLong,
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	command.AddCommand(a.registrySourceSignCommand())
	return command
}

func (a *App) registrySourceSignCommand() *cobra.Command {
	inputs := &federationInputs{}
	var keyPath, publicKeyPath, output string
	command := &cobra.Command{
		Use:     "sign --federation FILE --source NAME=COMMIT=DIR --key SEED-FILE --public-key FILE --output FILE",
		Short:   "Sign one source's statement at its commit with its repository's key (registry 1.1.0)",
		Long:    "Sign a source statement by registry format 1.1.0 §§10–11: the statement of the one --source is built from its own documents at its commit, read from its directory as the manifest names them, each admitted as the assembler admits it (§4, steps 1 and 2 for that source alone). A source whose documents are refused is not signed: the report is written and nothing else. No other source is read, so nothing here checks the assembly. The source signature document is written to --output. The output is {\"keyId\",\"signature\",\"sourceDigest\"}, the key's identifier, the path the document is written to and the statement's digest, as RFC 8785 canonical JSON and one newline. A signature says the key's holder vouched for these documents at this commit, and for nothing else." + registryKeyRules + lockLong,
		Example: "jpack registry source sign --federation federation.json --source billing=<commit>=. --key /secure/billing.seed --public-key billing.pub --output billing.sig.json",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			const name = "registry source sign"
			usage := func(message string) error {
				return a.operational(name, "json", result.ExitInvocation, "JPS-INVOCATION-INPUT", message)
			}
			if len(inputs.sources) != 1 {
				return usage("Give exactly one --source, the source to sign, as NAME=COMMIT=DIR.")
			}
			if output == "" || !localOutput(output) {
				return usage("Give --output, the local file the source signature is written to.")
			}
			signer, failure := a.registrySigner(name, keyPath, publicKeyPath)
			if failure != nil {
				return failure
			}
			inputs.noKnown = true
			in, failure := a.federationInput(name, inputs, "")
			if failure != nil {
				return failure
			}
			source := strings.SplitN(inputs.sources[0], "=", 2)[0]
			statement, report, err := federation.StatementOf(in, source)
			var input *federation.InputError
			switch {
			case errors.As(err, &input):
				return usage("The inputs give no statement: " + input.Reason + ".")
			case err != nil:
				return a.operational(name, "json", result.ExitIO, "JPS-OUTPUT-WRITE", "The statement could not be written in its canonical form.")
			case statement == nil:
				return a.federationReport(report)
			}
			document, err := signer.SignRegistrySource(statement.Digest)
			if err != nil {
				return a.operational(name, "json", result.ExitIO, "JPS-OUTPUT-WRITE", "The statement could not be signed.")
			}
			return a.writeSignature(name, output, document, signer.KeyID(), regsig.Source.DigestMember, statement.Digest)
		},
	}
	command.Flags().StringVar(&inputs.manifest, "federation", "", "The federation manifest (or - for standard input)")
	command.Flags().StringArrayVar(&inputs.sources, "source", nil, "The source to sign, as NAME=COMMIT=DIR: its name in the manifest, the full commit, and the local directory holding its files at that commit")
	command.Flags().StringVar(&keyPath, "key", "", "The file holding the repository's seed")
	command.Flags().StringVar(&publicKeyPath, "public-key", "", "The file holding the public key the seed is expected to have")
	command.Flags().StringVar(&output, "output", "", "Write the source signature here")
	return command
}

// verifyKeys reads the expected signer set from a file: one JSON object of
// key, the expected key (for an assembly, the standard owner's), and
// repositories, the repository key map, each key 64 lowercase hexadecimal
// characters; a member left out is not given. Whether each key is of its
// form and trusted is the verification's to say, in its report. Nothing read
// from the file is written in a message.
func (a *App) verifyKeys(name, path string) (*federation.Signers, error) {
	refused := func() (*federation.Signers, error) {
		return nil, a.operational(name, "json", result.ExitInvocation, "JPS-INVOCATION-INPUT", "The --keys file is not one JSON object of key, a string, and repositories, an object of strings, each member once.")
	}
	data, err := a.readInput(path, 1<<20, "expected keys")
	if err != nil {
		return nil, a.operational(name, "json", result.ExitIO, "JPS-INPUT-READ", "The --keys file could not be read as one bounded regular file.")
	}
	value, failure := carrier.Decode(data, carrier.Limits{MaxDepth: 3, MaxNodes: 2*federation.MaxSources + 8, MaxStringBytes: 64 * 1024})
	root, isObject := value.(map[string]any)
	if failure != nil || !isObject {
		return refused()
	}
	signers := &federation.Signers{}
	for member, raw := range root {
		switch member {
		case "key":
			key, isString := raw.(string)
			if !isString {
				return refused()
			}
			signers.Key = key
		case "repositories":
			object, isObject := raw.(map[string]any)
			if !isObject {
				return refused()
			}
			signers.Repositories = map[string]string{}
			for identity, rawKey := range object {
				key, isString := rawKey.(string)
				if !isString {
					return refused()
				}
				signers.Repositories[identity] = key
			}
		default:
			return refused()
		}
	}
	return signers, nil
}

// givenDocument is a bounded input read only when a verification reaches it,
// with federationDocument's refusal when it cannot be read.
func (a *App) givenDocument(name, path string, limit int, what string) federation.GivenDocument {
	return func() (registry.Document, error) {
		return a.federationDocument(name, path, limit, what)
	}
}

func (a *App) registryLockVerifyCommand() *cobra.Command {
	expect := &lockExpect{}
	emitters := &lockEmitters{}
	var lockPath, index, keysPath, floor, recordDir, registryPath, resolved, file, fileSource string
	var signatures, sourceSignatures, kinds []string
	command := &cobra.Command{
		Use:     "verify --lock FILE --signature FILE [--index FILE --source-signature FILE...] --keys FILE --registry-id URI --require-index true|false --record-dir DIR [--floor N] [--registry FILE --kind FILE... [emitters]] [--environment ENV (--resolved FILE | --file PATH=LOCAL)]",
		Short:   "Verify a lock's signatures and serial, record it, and write the report (registry 1.1.0)",
		Long:    "Verify a lock by registry format 1.1.0 §11, in its order: the expected inputs and the serial record; the lock signature, checked against the expected key before the lock is parsed; the lock, held to §9's step 1; its serial against the record or the floor; the index; each source's signature against the key the signer set names for it and the statement rebuilt from the index; then, when --registry is given, the rest of §9, and coverage when --resolved or --file is. Signature documents are counted in the order given, the --signature documents first: only the first lock signature is read, and every document beyond the lock's sources count plus one is LOCK-SIG-EXTRA and never opened, so a path that names nothing, a directory or a pipe there is not an input error. --keys names the expected signer set, {\"key\",\"repositories\"}, given every time and never taken from a document. The serial record is kept in --record-dir, one file per registry id, which has no default: with no record for the registry id, --floor must be given, and a floor of 1 accepts whatever lock comes first (trust on first use, chosen by giving it). Only a lock every step accepts is recorded, by compare and set under a lock file beside the record; a lock file another verifier left behind is never removed, and is LOCK-SERIAL-STATE, named on standard error, until it is. The report is RFC 8785 canonical JSON followed by one newline, and names keys by their identifiers. Exit 0 when the lock is accepted and recorded, 1 when it is not." + lockLong,
		Example: "jpack registry lock verify --lock registry.lock.json --signature registry.lock.json.sig.json --keys expected-keys.json --registry-id https://example.test/registries/platform --require-index false --record-dir /var/lib/registry-records --floor 1",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			const name = "registry lock verify"
			usage := func(message string) error {
				return a.operational(name, "json", result.ExitInvocation, "JPS-INVOCATION-INPUT", message)
			}
			if lockPath == "" {
				return usage("Supply --lock.")
			}
			if recordDir == "" || !localOutput(recordDir) {
				return usage("Supply --record-dir, the local directory that keeps the serial records; it has no default.")
			}
			if registryPath == "" && (len(kinds) > 0 || len(emitters.single)+len(emitters.sourced) > 0) {
				return usage("Give --kind and emitters only with --registry.")
			}
			if fileSource != "" && file == "" {
				return usage("Give --file-source only with --file.")
			}
			paths := append([]string{lockPath, index, keysPath, registryPath, resolved}, kinds...)
			paths = append(append(paths, signatures...), sourceSignatures...)
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
			in := federation.VerifyInput{Expect: expected, Records: federation.NewRecordStore(recordDir)}
			if floor != "" {
				value, err := strconv.ParseInt(floor, 10, 64)
				if err != nil {
					return usage("Give --floor as a whole number, the lowest serial to accept.")
				}
				in.Floor = &value
			}
			if keysPath != "" {
				if in.Signers, failure = a.verifyKeys(name, keysPath); failure != nil {
					return failure
				}
			}
			if in.Lock, failure = a.federationDocument(name, lockPath, federation.MaxLockBytes, "lock"); failure != nil {
				return failure
			}
			// The signature documents are read when the verification
			// reaches each: one it never reads is never opened. One read
			// that cannot be is an input error, as any other input is.
			for _, path := range signatures {
				in.LockSignatures = append(in.LockSignatures, a.givenDocument(name, path, regsig.MaxBytes, "lock signature"))
			}
			for _, path := range sourceSignatures {
				in.SourceSignatures = append(in.SourceSignatures, a.givenDocument(name, path, regsig.MaxBytes, "source signature"))
			}
			if index != "" {
				document, failure := a.federationDocument(name, index, federation.MaxIndexBytes, "index")
				if failure != nil {
					return failure
				}
				in.Index = &document
			}
			if registryPath != "" {
				document, failure := a.federationDocument(name, registryPath, registry.MaxRegistryBytes, "registry")
				if failure != nil {
					return failure
				}
				in.Registry = &document
				if in.Kinds, failure = a.lockKinds(name, kinds); failure != nil {
					return failure
				}
				if in.Emitters, failure = a.lockEmitterInputs(name, emitters, false); failure != nil {
					return failure
				}
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
			verified, err := federation.Verify(in)
			if err != nil {
				var handled *handledExit
				if errors.As(err, &handled) {
					// A signature document that could not be read: its
					// reader has written the refusal.
					return err
				}
				if failure := a.lockInputFailure(name, err); failure != nil {
					return failure
				}
				return a.operational(name, "json", result.ExitIO, "JPS-OUTPUT-WRITE", "The report could not be written in its canonical form.")
			}
			if verified.RecordProblem != "" {
				fmt.Fprintln(a.errOut, name+": "+display.Sanitize(verified.RecordProblem))
			}
			return a.federationReport(verified.Report)
		},
	}
	expect.register(command, true)
	emitters.register(command, false)
	command.Flags().StringVar(&lockPath, "lock", "", "The lock to verify (or - for standard input)")
	command.Flags().StringArrayVar(&signatures, "signature", nil, "The lock signature document; only the first given is read")
	command.Flags().StringArrayVar(&sourceSignatures, "source-signature", nil, "A source signature document, for an assembled lock; repeat for each source")
	command.Flags().StringVar(&index, "index", "", "The assembled index, for a lock that names one")
	command.Flags().StringVar(&keysPath, "keys", "", "The expected signer set: a JSON file of key and, for an assembly, repositories")
	command.Flags().StringVar(&recordDir, "record-dir", "", "The directory that keeps the serial records, one file per registry id")
	command.Flags().StringVar(&floor, "floor", "", "The lowest serial to accept; required with no record for the registry id")
	command.Flags().StringVar(&registryPath, "registry", "", "The registry the lock names, to check the rest of §9 with")
	command.Flags().StringArrayVar(&kinds, "kind", nil, "A kind declaration the registry pins, with --registry; repeat for each")
	command.Flags().StringVar(&resolved, "resolved", "", "A resolved registry the consumer holds, for coverage")
	command.Flags().StringVar(&file, "file", "", "A generated file the consumer holds, for coverage, as PATH=LOCAL")
	command.Flags().StringVar(&fileSource, "file-source", "", "The source whose repository holds the file, for an assembled lock")
	return command
}
