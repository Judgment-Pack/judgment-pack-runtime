package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/lock"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// configFlagUsage is one sentence, used identically wherever the configuration
// can be selected, so the resolution order is never described two ways.
const configFlagUsage = "path to the project's jpack.json; defaults to $JPACK_CONFIG, then ./jpack.json"

// packsCommand is the project-convention namespace (ADR-0012).
//
// It is deliberately a top-level namespace and not a member of spec: nothing
// under it is specified. spec commands answer questions the Judgment Pack
// Specification defines; these answer questions about how one project happens to
// keep its packs, which is this runtime's convention and no other
// implementation's obligation.
func (a *App) packsCommand() *cobra.Command {
	packs := &cobra.Command{
		Use:   "packs",
		Short: "Work with the packs a project declares in its jpack.json (a runtime convention)",
		Long: "Operate on the packs a project declares in its jpack.json (ADR-0012). This convention is " +
			"this runtime's and is NOT part of the Judgment Pack Specification: it names a project's packs and " +
			"where their files are, so one short decision id reaches the same pack from a shell, from a CI step, " +
			"and from an agent's tool call. It selects nothing at decision time -- choosing which decision to ask " +
			"is the application's, and the pack itself judges whether it applies -- it templates nothing, and it " +
			"declares no pack's version as a truth of its own. The pack document's own id and version members are " +
			"what a pack is; the configuration's expectedVersion pin, the optional filename convention, and the " +
			"packId and packVersion members of an evaluation payload are references checked against that document " +
			"rather than second places a version is stated. Every file is read through one rooted reader, and a " +
			"declared path that leaves the configuration's own directory is refused when the configuration is " +
			"validated and again when the file is read. The configuration is selected by --config, then " +
			"$JPACK_CONFIG, then ./jpack.json. Hints a configuration carries are non-normative guidance for an " +
			"agent gathering inputs: this runtime holds no credential, opens no network connection, and never " +
			"reads a source one names. One command here, packs test, runs the EXPERIMENTAL evaluator -- that " +
			"surface may change or be removed without compatibility promise, and the conformance claim for it is " +
			"stated, in full and only, in CONFORMANCE.md, which this text does not restate.",
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	// --config is registered on the six subcommands that read a configuration
	// rather than on this one as a persistent flag. packs schema prints an
	// embedded artifact and touches no configuration; a flag inherited into its
	// help would promise an effect it does not have.
	packs.AddCommand(
		a.packsListCommand(),
		a.packsValidateCommand(),
		a.packsTestCommand(),
		a.packsLockCommand(),
		a.packsVerifyCommand(),
		a.packsLintCommand(),
		a.packsSchemaCommand(),
	)
	return packs
}

// packsLintCommand holds every consulted pointer to a producer (ADR-0022) —
// the inverse of validate's hint-key check. validate holds the declared
// hints to the document; lint holds the document's consulted pointers to a
// producer declaration, because the defect it catches never errors on its
// own: a pointer no source feeds is merely unknowable, every rule touching
// it escalates, and the system looks conservative rather than broken.
func (a *App) packsLintCommand() *cobra.Command {
	format := "human"
	id := ""
	configPath := ""
	producersPath := ""
	command := &cobra.Command{
		Use:   "lint",
		Short: "Check that every pointer the packs consult has a producer",
		Long: "Check the packs a project declares against a producer declaration: every fact pointer a pack's " +
			"conditions consult must have a producer, and declared and supplied evidence must agree in both " +
			"directions. Without --producers, the configuration's own facts and evidence hints are the producer " +
			"declaration — a hint is the project saying where that answer lives. With --producers, an explicit " +
			"manifest ({\"producersVersion\":\"1\",\"facts\":[...pointers],\"evidence\":[...ids]}) is, for an " +
			"application whose producer set is wider than its hints. A consulted pointer no producer supplies " +
			"never errors at run time: the condition is unknowable, every rule touching it escalates, and the " +
			"system looks conservative rather than broken — this command is where that defect fails loudly " +
			"instead. A pack using draft-RFC collection quantifiers reports its fact half as skipped rather than " +
			"checked, because element-relative pointers cannot be held to a flat producer set (ADR-0020, " +
			"ADR-0022); a skipped check is reported, never silently passed. Exit 0 when a check passed and none " +
			"failed, 1 otherwise — a run in which nothing was checkable is skipped, not passed.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateFormat(format); err != nil {
				return a.operational("packs lint", format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			var manifest *project.Producers
			if producersPath != "" {
				if producersPath != "-" && fssecure.IsRemotePath(producersPath) {
					return a.operational("packs lint", format, result.ExitInvocation, "JPS-INVOCATION-INPUT", "Remote filesystem input paths are not supported.")
				}
				data, err := a.readPack(producersPath, project.MaxProducersBytes)
				if err != nil {
					return a.operational("packs lint", format, result.ExitIO, "JPS-LINT-PRODUCERS-READ", "The producer manifest could not be read: "+err.Error())
				}
				decoded, decodeErr := project.DecodeProducers(data)
				if decodeErr != nil {
					return a.operational("packs lint", format, result.ExitInvalid, "JPS-LINT-PRODUCERS", decodeErr.Error())
				}
				manifest = decoded
			}
			loaded, failure := a.loadProject(configPath, "packs lint", format)
			if failure != nil {
				return failure
			}
			defer loaded.Close()
			output, projectFailure := loaded.Lint(manifest, id, "packs lint")
			if projectFailure != nil {
				return a.projectFailure("packs lint", format, projectFailure)
			}
			if err := a.renderPackProducersLint(format, output); err != nil {
				return &handledExit{code: result.ExitIO}
			}
			code := result.ExitSuccess
			if output.Status != "passed" {
				code = result.ExitInvalid
			}
			return &handledExit{code: code}
		},
	}
	command.Flags().StringVar(&format, "format", format, "output format: human or json")
	command.Flags().StringVar(&id, "id", id, "lint one declared pack by its decision id instead of all of them")
	command.Flags().StringVar(&configPath, "config", configPath, configFlagUsage)
	command.Flags().StringVar(&producersPath, "producers", producersPath, "path to a producer manifest or -; omit to lint against the configuration's own hints")
	return command
}

func (a *App) packsLockCommand() *cobra.Command {
	format := "human"
	configPath := ""
	command := &cobra.Command{
		Use:   "lock",
		Short: "Declare the project's current documents as its reviewed set",
		Long: "Write the project's reviewed-set lock (ADR-0019): a generated sibling of jpack.json named " +
			"jpack.lock.json, pinning the exact bytes of the configuration and of every pack and graph it " +
			"declares. Running this command IS the amendment -- it is how a project says, in a file a reviewer " +
			"can diff, that the law changed on purpose. It reviews nothing and approves nothing: your pull " +
			"request is still the approval, exactly as it is for the documents themselves. The file is " +
			"deterministic, so re-running it over an unchanged tree rewrites identical bytes and leaves no diff " +
			"to read. Its presence is what turns verification on: with no lock file, nothing anywhere in this " +
			"runtime behaves differently, and with one, the deciding surfaces refuse to evaluate law that differs " +
			"from it. A configuration that does not load is refused before anything is written -- a lock over a " +
			"configuration this runtime cannot read would declare a set nobody can check.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			const commandName = "packs lock"
			if err := validateFormat(format); err != nil {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			loaded, failure := a.loadProject(configPath, commandName, format)
			if failure != nil {
				return failure
			}
			defer loaded.Close()
			lockName, named := loaded.LockName()
			if !named {
				return a.projectFailure(commandName, format, loaded.LockNameFailure())
			}
			// The generated file never lands on a document the configuration
			// declares. Writing there would destroy declared law with a
			// generated artifact and then record the digest of what it
			// destroyed — the one write in this runtime that could take
			// something away.
			if owner, declared := loaded.DeclaresPath(lockName); declared {
				return a.operational(commandName, format, result.ExitInvalid, "JPS-LOCK-PATH",
					fmt.Sprintf("The reviewed-set lock would be written at %s, which this configuration declares as %s. Rename that document, or name the configuration something whose lock lands elsewhere; nothing was written.",
						display.Sanitize(loaded.LockPath()), display.Sanitize(owner)))
			}
			// The audit directory is the other thing a generated file must not
			// land on or above: a file there leaves the directory uncreatable,
			// and every later evaluation then refuses after evaluating because
			// its record cannot be written.
			if loaded.ContainsAuditDir(lockName) {
				return a.operational(commandName, format, result.ExitInvalid, "JPS-LOCK-PATH",
					fmt.Sprintf("The reviewed-set lock would be written at %s, which is the audit directory this configuration declares, or a component above it. Move one of the two; nothing was written.",
						display.Sanitize(loaded.LockPath())))
			}
			// The arithmetic that says the lock cannot fit is available from the
			// configuration alone, so it is done before a single document is
			// read: a generator must not spend an unbounded read hashing
			// thousands of documents only to find its own output unreadable.
			if least, tooLarge := lock.TooLargeToWrite(loaded, project.MaxLockBytes); tooLarge {
				return a.operational(commandName, format, result.ExitIO, "JPS-RESOURCE-LOCK-BYTE-LIMIT",
					fmt.Sprintf("The reviewed set of %d declared document(s) cannot encode within the %d-byte limit every reader of this file applies — %d bytes at the least; nothing was written or read.",
						len(loaded.IDs)+len(loaded.GraphIDs), project.MaxLockBytes, least))
			}
			document, lockFailure := lock.Generate(loaded)
			if lockFailure != nil {
				return a.lockFailure(commandName, format, lockFailure)
			}
			contents, err := lock.Encode(document)
			if err != nil {
				return a.operational(commandName, format, result.ExitInternal, "JPS-LOCK-ENCODE", "The reviewed-set lock could not be encoded.")
			}
			// A generator must not emit a document its own reader refuses. Past
			// the limit the lock would be unreadable by packs verify and by
			// every deciding surface, and the refusal's steer — run packs lock
			// — would regenerate the same unreadable file.
			if int64(len(contents)) > project.MaxLockBytes {
				return a.operational(commandName, format, result.ExitIO, "JPS-RESOURCE-LOCK-BYTE-LIMIT",
					fmt.Sprintf("The reviewed set of %d declared document(s) encodes to %d bytes, past the %d-byte limit every reader of this file applies; nothing was written.",
						len(loaded.IDs)+len(loaded.GraphIDs), len(contents), project.MaxLockBytes))
			}
			if err := loaded.WriteLock(contents); err != nil {
				return a.operational(commandName, format, result.ExitIO, "JPS-LOCK-WRITE", "The reviewed-set lock could not be written into the configuration's own directory.")
			}
			output := lockReport(loaded, document, commandName)
			output.WrittenTo = loaded.LockPath()
			if err := a.renderPackLock(format, output); err != nil {
				return &handledExit{code: result.ExitIO}
			}
			return nil
		},
	}
	command.Flags().StringVar(&format, "format", format, "output format: human or json")
	command.Flags().StringVar(&configPath, "config", configPath, configFlagUsage)
	return command
}

func (a *App) packsVerifyCommand() *cobra.Command {
	format := "human"
	configPath := ""
	command := &cobra.Command{
		Use:   "verify",
		Short: "Check the project's documents against its reviewed-set lock",
		Long: "Compare a project's current documents against the reviewed set its jpack.lock.json declares " +
			"(ADR-0019), and report every difference by name: config-drift when the configuration's own bytes " +
			"changed, document-drift when a declared pack or graph changed, document-missing when one cannot be " +
			"read, lock-entry-missing when the configuration declares a document the reviewed set does not name, " +
			"locked-but-undeclared when the reviewed set names one the configuration dropped, and " +
			"path-mismatch when an entry records a path the configuration does not declare. Every " +
			"difference is reported, not the first. It says what changed and never whether the change was " +
			"right: an amendment and a tampering look identical to a runtime, and only the people reviewing the " +
			"diff can tell them apart. Exit 0 when the project matches its reviewed set, 1 when anything " +
			"differs. A project with no lock has nothing to verify against, which is reported as an operational " +
			"refusal naming the command that would create one rather than as a failed verification.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			const commandName = "packs verify"
			if err := validateFormat(format); err != nil {
				return a.operational(commandName, format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			loaded, failure := a.loadProject(configPath, commandName, format)
			if failure != nil {
				return failure
			}
			defer loaded.Close()
			if _, named := loaded.LockName(); !named {
				return a.projectFailure(commandName, format, loaded.LockNameFailure())
			}
			if !loaded.HasLock() {
				return a.lockFailure(commandName, format, lock.NoLockFailure(loaded))
			}
			set, lockFailure := lock.Open(loaded)
			if lockFailure != nil {
				return a.lockFailure(commandName, format, lockFailure)
			}
			output := verificationReport(loaded, set.Document, lock.Verify(loaded, set.Document), commandName)
			if err := a.renderPackLockVerification(format, output); err != nil {
				return &handledExit{code: result.ExitIO}
			}
			code := result.ExitSuccess
			if output.Status != "valid" {
				code = result.ExitInvalid
			}
			return &handledExit{code: code}
		},
	}
	command.Flags().StringVar(&format, "format", format, "output format: human or json")
	command.Flags().StringVar(&configPath, "config", configPath, configFlagUsage)
	return command
}

// lockReport composes the payload packs lock writes: the reviewed set as it was
// just declared, in the sorted order every project payload uses.
func lockReport(loaded *project.Project, document lock.Document, command string) result.PackLock {
	output := result.PackLock{
		OutputVersion: result.OutputVersion,
		Tool:          result.CurrentTool(),
		Command:       command,
		Status:        "valid",
		Kind:          result.ProjectKind,
		ConfigPath:    loaded.ConfigPath,
		LockPath:      loaded.LockPath(),
		LockVersion:   document.LockVersion,
		ConfigDigest:  document.Config.Digest,
		Entries:       []result.LockedID{},
	}
	for _, id := range loaded.IDs {
		entry := document.Packs[id]
		output.Entries = append(output.Entries, result.LockedID{Kind: "pack", ID: id, Path: entry.Path, Digest: entry.Digest})
	}
	for _, id := range loaded.GraphIDs {
		entry := document.Graphs[id]
		output.Entries = append(output.Entries, result.LockedID{Kind: "graph", ID: id, Path: entry.Path, Digest: entry.Digest})
	}
	output.Summary.Total = len(output.Entries)
	output.Summary.Passed = len(output.Entries)
	return output
}

// verificationReport composes the payload packs verify writes. The summary
// counts declared documents rather than findings, and the configuration's own
// drift is a configuration-level check beside them: the configuration is not one
// of the documents the counts are about.
func verificationReport(loaded *project.Project, document lock.Document, checks []lock.Check, command string) result.PackLockVerification {
	output := result.PackLockVerification{
		OutputVersion: result.OutputVersion,
		Tool:          result.CurrentTool(),
		Command:       command,
		Status:        "valid",
		Kind:          result.ProjectKind,
		ConfigPath:    loaded.ConfigPath,
		LockPath:      loaded.LockPath(),
		LockVersion:   document.LockVersion,
		Findings:      []result.LockFinding{},
	}
	// The summary counts declared documents, which is what Total is. A finding
	// about the configuration is a configuration-level check, and one about an
	// entry the configuration no longer declares counts nowhere in a total of
	// documents the configuration declares — it gets its own number. Failures
	// are keyed by kind and id together, because a pack and a graph may share
	// an id and two drifted documents must not collapse into one.
	failed := map[string]bool{}
	for _, check := range checks {
		switch check.Name {
		case lock.CheckConfigDrift:
			output.Checks = append(output.Checks, result.PackCheck{
				Name:   check.Name,
				Status: result.PackCheckFailed,
				Detail: check.Detail,
			})
		case lock.CheckUndeclaredInConfig:
			output.StaleEntries++
		default:
			failed[check.Kind+"/"+check.ID] = true
		}
		output.Findings = append(output.Findings, result.LockFinding{
			Name:   check.Name,
			Kind:   check.Kind,
			ID:     check.ID,
			Path:   check.Path,
			Detail: check.Detail,
		})
		output.Status = "invalid"
	}
	output.Summary.Total = len(loaded.IDs) + len(loaded.GraphIDs)
	output.Summary.Failed = len(failed)
	output.Summary.Passed = output.Summary.Total - output.Summary.Failed
	if len(output.Checks) == 0 {
		output.Checks = append(output.Checks, result.PackCheck{Name: lock.CheckConfigDrift, Status: result.PackCheckPassed})
	}
	return output
}

// lockFailure reports one reviewed-set failure through the same operational
// path every other refusal takes. Like a project failure it carries no §8.4
// class: a lock that does not match is not an evaluation error, because no
// evaluation was attempted.
func (a *App) lockFailure(command, format string, failure *lock.Failure) error {
	return a.operational(command, format, failure.ExitCode, failure.Code, failure.Message)
}

func (a *App) packsListCommand() *cobra.Command {
	format := "human"
	configPath := ""
	command := &cobra.Command{
		Use:   "list",
		Short: "List the project's packs: decision id, the document's own id and version, path, description",
		Long: "List every pack the project's jpack.json declares. Each row carries the project's decision id " +
			"beside the pack document's own id and version, which are two different names and are reported as two " +
			"members for that reason, plus the path, the description, whether a matrix is declared, and whether an " +
			"expectedVersion pin agrees with the document. It reads each pack document to report its identity and " +
			"does not validate it: a project's inventory has to be readable while one pack is mid-edit, and a pack " +
			"that could not be read is listed with the reason instead of being dropped. Run packs validate for the " +
			"verdict on each document.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateFormat(format); err != nil {
				return a.operational("packs list", format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			loaded, failure := a.loadProject(configPath, "packs list", format)
			if failure != nil {
				return failure
			}
			defer loaded.Close()
			if err := a.renderPackInventory(format, loaded.Inventory("packs list")); err != nil {
				return &handledExit{code: result.ExitIO}
			}
			return nil
		},
	}
	command.Flags().StringVar(&format, "format", format, "output format: human or json")
	command.Flags().StringVar(&configPath, "config", configPath, configFlagUsage)
	return command
}

func (a *App) packsValidateCommand() *cobra.Command {
	format := "human"
	id := ""
	configPath := ""
	command := &cobra.Command{
		Use:   "validate",
		Short: "Check the project configuration and every pack it declares",
		Long: "Check one project's jpack.json and the files it points at. The configuration itself is held to " +
			"the embedded schema printed by packs schema and to a configVersion this runtime accepts; a " +
			"configuration that fails either is refused before a single pack is read. Each declared pack is then " +
			"checked in six named steps, and every step is reported with its status so a reader can tell a check " +
			"that passed from one that was never asked for: the declared path resolves inside the configuration's " +
			"own directory, lexically and when resolved against the handle held open on that directory; the pack document " +
			"validates through the semantic layer, exactly as spec validate reports it; an expectedVersion pin, " +
			"when the entry carries one, equals the version the pack document declares; the filename, when it " +
			"follows the optional <decision-id>-<semver>.pack.json convention, names the configuration's key and " +
			"the document's version -- a filename outside the convention is not a defect and is skipped, and one " +
			"inside it is held to it; every hint key names something the pack document has -- an evidence hint a " +
			"declared evidence-requirement id, a fact hint a pointer some condition reads -- because a hint this " +
			"runtime never resolves is instructions an agent follows; and a declared matrix loads as rows. It " +
			"evaluates nothing and runs no row: that is packs test. Exit 0 when every check passed, 1 when any " +
			"failed.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateFormat(format); err != nil {
				return a.operational("packs validate", format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			loaded, failure := a.loadProject(configPath, "packs validate", format)
			if failure != nil {
				return failure
			}
			defer loaded.Close()
			output, projectFailure := loaded.Validate(a.engine, id, "packs validate")
			if projectFailure != nil {
				return a.projectFailure("packs validate", format, projectFailure)
			}
			if err := a.renderPackValidation(format, output); err != nil {
				return &handledExit{code: result.ExitIO}
			}
			code := result.ExitSuccess
			if output.Status != "valid" {
				code = result.ExitInvalid
			}
			return &handledExit{code: code}
		},
	}
	command.Flags().StringVar(&format, "format", format, "output format: human or json")
	command.Flags().StringVar(&id, "id", id, "check one declared pack by its decision id instead of all of them")
	command.Flags().StringVar(&configPath, "config", configPath, configFlagUsage)
	return command
}

func (a *App) packsTestCommand() *cobra.Command {
	format := "human"
	id := ""
	configPath := ""
	command := &cobra.Command{
		Use:   "test",
		Short: "EXPERIMENTAL SURFACE: run each pack's instance matrix through the evaluator",
		Long: "Run every declared pack's instance matrix through this runtime's evaluator and report every row. " +
			"This command runs the EXPERIMENTAL evaluator (ADR-0007, ADR-0011): that surface may change or be " +
			"removed without compatibility promise, and the conformance claim for it is stated, in full and only, " +
			"in CONFORMANCE.md -- read that file for the claim, its exact version scope, its evidence, and " +
			"everything it does not assert. This text states no part of it, and neither does any payload this " +
			"command writes; every payload carries a conformanceClaimReference member pointing at that file. A row " +
			"is judged exactly as a row of the bundled evaluation corpus is, by the same code: the RFC 8785 " +
			"canonical §8.3 disposition compared byte for byte against the row's, or the §8.4 error class and " +
			"phase the row expects. Matrix rows use the same case-carrier shape that corpus uses -- id, facts, " +
			"evidenceAvailability, supportedExtensions, and exactly one of expectedDisposition and " +
			"expectedErrorClass. What this reports is what one project's own rows did: it is not the " +
			"specification's corpus, it is evidence about the pack a project wrote rather than about this " +
			"implementation, and no row is an authorization or a statement that acting on a disposition is correct " +
			"(§3.5). A pack that declares no matrix is reported as skipped and never as passed, and a run in which " +
			"no row ran at all is reported skipped rather than passed: a green gate over zero rows would say a " +
			"project was tested when nothing was. Exit 0 when every row matched its expectation, 1 when any did " +
			"not and 1 when no row ran.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateFormat(format); err != nil {
				return a.operational("packs test", format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			loaded, failure := a.loadProject(configPath, "packs test", format)
			if failure != nil {
				return failure
			}
			defer loaded.Close()
			output, projectFailure := loaded.Test(evaluation.NewEngine(a.engine), id, "packs test")
			if projectFailure != nil {
				return a.projectFailure("packs test", format, projectFailure)
			}
			if err := a.renderPackTest(format, output); err != nil {
				return &handledExit{code: result.ExitIO}
			}
			code := result.ExitSuccess
			if output.Status != "passed" {
				code = result.ExitInvalid
			}
			return &handledExit{code: code}
		},
	}
	command.Flags().StringVar(&format, "format", format, "output format: human or json")
	command.Flags().StringVar(&id, "id", id, "run one declared pack's matrix by its decision id instead of all of them")
	command.Flags().StringVar(&configPath, "config", configPath, configFlagUsage)
	return command
}

// packsSchemaCommand mirrors spec schema, including its two write guards. The
// schema it prints is this runtime's own and names no specification version:
// jpack.json is a runtime convention with a version of its own.
func (a *App) packsSchemaCommand() *cobra.Command {
	format := "human"
	writeTarget := ""
	command := &cobra.Command{
		Use:   "schema",
		Short: "Inspect or write the jpack.json configuration schema",
		Long: "Print the JSON Schema this runtime holds a jpack.json to, or write its exact bytes. The schema is " +
			"closed: every member it does not name is rejected, so a misspelled key in a configuration is an error " +
			"rather than a silently ignored intention. It describes a convention of this runtime and not any part " +
			"of the Judgment Pack Specification, which is why it names no specification version and carries a " +
			"configVersion of its own.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := validateFormat(format); err != nil {
				return a.operational("packs schema", format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
			}
			if writeTarget == "-" && format == "json" {
				return a.operational("packs schema", format, result.ExitInvocation, "JPS-INVOCATION-STDOUT", "--write - cannot be combined with --format json.")
			}
			if writeTarget != "" && writeTarget != "-" && fssecure.IsRemotePath(writeTarget) {
				return a.operational("packs schema", format, result.ExitInvocation, "JPS-INVOCATION-OUTPUT", "Remote filesystem output paths are not supported.")
			}
			schemaBytes := project.Schema()
			if writeTarget == "-" {
				if _, err := a.out.Write(schemaBytes); err != nil {
					return &handledExit{code: result.ExitIO}
				}
				return nil
			}
			output := project.SchemaDescription("packs schema")
			if writeTarget != "" {
				if err := writeNewFile(writeTarget, schemaBytes); err != nil {
					return a.operational("packs schema", format, result.ExitIO, "JPS-SCHEMA-WRITE", "Schema destination must be a new writable file.")
				}
				output.WrittenTo = writeTarget
			}
			if err := a.renderConfigSchema(format, output); err != nil {
				return &handledExit{code: result.ExitIO}
			}
			return nil
		},
	}
	command.Flags().StringVar(&format, "format", format, "output format: human or json")
	command.Flags().StringVar(&writeTarget, "write", writeTarget, "write the schema's exact bytes to a new file or -")
	return command
}

// loadProject resolves and loads the project configuration for one command,
// reporting a load failure in that command's own format. The returned error is
// already a handled exit; a caller returns it unchanged.
func (a *App) loadProject(configPath, command, format string) (*project.Project, error) {
	loaded, failure := project.Load(project.Locate(configPath))
	if failure != nil {
		return nil, a.projectFailure(command, format, failure)
	}
	return loaded, nil
}

// projectFailure reports one project-convention failure through the same
// operational path every other refusal takes. There is no §8.4 class here and
// there must not be: a configuration this runtime could not read is not an
// evaluation error, because no evaluation was ever attempted.
func (a *App) projectFailure(command, format string, failure *project.Failure) error {
	return a.operational(command, format, failure.ExitCode, failure.Code, failure.Message)
}
