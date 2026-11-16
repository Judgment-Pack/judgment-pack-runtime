package cli

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/spf13/cobra"
)

// maxKindFiles bounds the kind declarations one --kinds directory may supply.
const maxKindFiles = 1024

// factsExitNotJudged is the exit code of a registry facts run that wrote no
// batch because its inputs could not be judged: the outcome gate's "could not
// judge" (ADR-0092), which RFC 0037 §3 gives an invalid registry.
const factsExitNotJudged = 2

// registryFactsCommand writes a kind's entries as a facts batch for the
// outcome gate, and optionally the kind's facts contract (RFC 0037 §2–§4,
// ADR-0095).
func (a *App) registryFactsCommand() *cobra.Command {
	var registryPath, kindName, environment, contractPath string
	var kindPaths []string
	allEnvironments := false
	command := &cobra.Command{
		Use:   "facts --registry REGISTRY --kinds PATH... --kind KIND (--environment ENV | --all-environments)",
		Short: "Write a kind's entries as a facts batch for evaluate's outcome gate (spec RFC 0037)",
		Long: "Validate the registry, then write one batch line per entry of the kind that applies in the environment, in canonical entry order: " +
			`{"facts": <facts document>, "id": "<kind>/<entry-id>"}, RFC 8785 canonical, one per line. ` +
			"In a facts document numbers are decimal strings in one normal form, a secret reference is only its store and scope, and each entry reference carries its target's kind, id and value, one level deep. " +
			"--all-environments writes every environment's lines in the registry's order, with ids <environment>:<kind>/<entry-id>. --contract also writes the kind's generated facts contract. " +
			"--kinds names a kind declaration file or a directory whose *.json files are read; repeat it as needed. " +
			"Nothing is written unless every line can be: an invalid registry, or an entry whose facts document exceeds the facts-document limit, writes the report to standard error and exits 2. " +
			"Exit 0 when the batch was written (an empty one included), 3 for an unusable command line, a kind the registry does not pin or an environment it does not declare, and 4 when an input could not be read or an output written.",
		Example: "jpack registry facts --registry registry.json --kinds kinds/ --kind db-role --environment prod > prod-roles.jsonl\n" +
			"jpack experimental evaluate --pack-id db-role-review --batch prod-roles.jsonl --format json --require-outcome approved",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			const name = "registry facts"
			invocation := func(code, message string) error {
				return a.operational(name, "text", result.ExitInvocation, code, message)
			}
			switch {
			case registryPath == "":
				return invocation("JPS-INVOCATION-INPUT", "Supply --registry, the registry document.")
			case len(kindPaths) == 0:
				return invocation("JPS-INVOCATION-INPUT", "Supply --kinds, a kind declaration file or a directory of them.")
			case kindName == "":
				return invocation("JPS-INVOCATION-KIND", "Supply --kind, the name of a kind the registry pins.")
			case (environment == "") == !allEnvironments:
				return invocation("JPS-INVOCATION-ENVIRONMENT", "Supply exactly one of --environment and --all-environments.")
			case contractPath == "-" || contractPath != "" && (strings.Contains(contractPath, "://") || fssecure.IsRemotePath(contractPath)):
				return invocation("JPS-INVOCATION-OUTPUT", "--contract names a local file; standard output holds the batch.")
			}
			files, failure := a.kindFiles(name, kindPaths)
			if failure != nil {
				return failure
			}
			validated, failure := a.readRegistryForFacts(name, registryPath, files)
			if failure != nil {
				return failure
			}
			if !validated.Report.Valid {
				return a.factsNotJudged(validated.Report)
			}
			environments := []string{environment}
			if allEnvironments {
				environments = validated.Environments()
			}
			var contract []byte
			if contractPath != "" {
				generated, diagnostics, err := validated.FactsContract(kindName)
				switch {
				case errors.Is(err, registry.ErrNoKind):
					return invocation("JPS-INVOCATION-KIND", "The registry pins no kind of that name with a usable declaration.")
				case err != nil:
					return a.operational(name, "text", result.ExitInternal, "JPS-INTERNAL", "The facts contract could not be generated.")
				case len(diagnostics) > 0:
					return a.factsNotJudged(registry.Report{ReportFormatVersion: validated.Report.ReportFormatVersion, Registry: validated.Report.Registry, Diagnostics: diagnostics})
				}
				contract = append(generated, '\n')
			}
			writeContract := func() error {
				if contract == nil {
					return nil
				}
				if err := replaceFile(contractPath, contract); err != nil {
					return errContractWrite
				}
				return nil
			}
			diagnostics, err := validated.WriteFacts(kindName, environments, allEnvironments, a.out, writeContract)
			switch {
			case errors.Is(err, registry.ErrNoKind):
				return invocation("JPS-INVOCATION-KIND", "The registry pins no kind of that name with a usable declaration.")
			case errors.Is(err, registry.ErrNoEnvironment):
				return invocation("JPS-INVOCATION-ENVIRONMENT", "The environment is not one of the registry's environments.")
			case errors.Is(err, errContractWrite):
				return a.operational(name, "text", result.ExitIO, "JPS-OUTPUT-WRITE", "The facts contract could not be written; no batch line was written.")
			case errors.Is(err, registry.ErrWrite):
				return &handledExit{code: result.ExitIO}
			case err != nil:
				return a.operational(name, "text", result.ExitInternal, "JPS-INTERNAL", "A facts document could not be built.")
			case len(diagnostics) > 0:
				return a.factsNotJudged(registry.Report{ReportFormatVersion: validated.Report.ReportFormatVersion, Registry: validated.Report.Registry, Diagnostics: diagnostics})
			}
			return nil
		},
	}
	command.Flags().StringVar(&registryPath, "registry", "", "The registry document (or - for standard input)")
	command.Flags().StringArrayVar(&kindPaths, "kinds", nil, "A kind declaration file, or a directory whose *.json files are kind declarations; repeat as needed")
	command.Flags().StringVar(&kindName, "kind", "", "The kind whose entries are written")
	command.Flags().StringVar(&environment, "environment", "", "The environment whose resolved entries are written")
	command.Flags().BoolVar(&allEnvironments, "all-environments", false, "Write every environment's lines, ids prefixed with <environment>:")
	command.Flags().StringVar(&contractPath, "contract", "", "Also write the kind's generated facts contract to this file")
	return command
}

var errContractWrite = errors.New("the facts contract could not be written")

// factsNotJudged writes a report to standard error, keeping standard output
// for batch lines, and exits 2: nothing was written.
func (a *App) factsNotJudged(report registry.Report) error {
	report.Valid = false
	if err := writeJSON(a.errOut, report, a.pretty); err != nil {
		return &handledExit{code: result.ExitIO}
	}
	return &handledExit{code: factsExitNotJudged}
}

// readRegistryForFacts reads and validates the inputs as validateRegistry
// does, and reports a failure to read on standard error, which keeps standard
// output for batch lines alone.
func (a *App) readRegistryForFacts(name, registryPath string, kindPaths []string) (*registry.Result, error) {
	stdin := 0
	for _, path := range append([]string{registryPath}, kindPaths...) {
		if strings.Contains(path, "://") || fssecure.IsRemotePath(path) {
			return nil, a.operational(name, "text", result.ExitInvocation, "JPS-INVOCATION-INPUT", "Use local files or standard input.")
		}
		if path == "-" {
			stdin++
		}
	}
	if stdin > 1 {
		return nil, a.operational(name, "text", result.ExitInvocation, "JPS-INVOCATION-INPUT", "Only one input can read standard input.")
	}
	read := func(path string, limit int, what string) (registry.Document, error) {
		data, err := a.readInput(path, int64(limit), what)
		switch {
		case errors.Is(err, fssecure.ErrTooLarge):
			return registry.Document{Oversize: true}, nil
		case err != nil:
			return registry.Document{}, a.operational(name, "text", result.ExitIO, "JPS-INPUT-READ", "The "+what+" could not be read as one bounded regular file or standard input stream.")
		}
		return registry.Document{Data: data}, nil
	}
	registryDocument, err := read(registryPath, registry.MaxRegistryBytes, "registry")
	if err != nil {
		return nil, err
	}
	documents := make([]registry.Document, 0, len(kindPaths))
	for _, path := range kindPaths {
		document, err := read(path, registry.MaxKindBytes, "kind declaration")
		if err != nil {
			return nil, err
		}
		documents = append(documents, document)
	}
	return registry.ValidateDocuments(registryDocument, documents), nil
}

// kindFiles expands --kinds: a file is itself, and a directory is its *.json
// entries in name order, not recursively. A directory given through a
// symbolic link is refused, as each file is when it is opened.
func (a *App) kindFiles(name string, paths []string) ([]string, error) {
	var files []string
	for _, path := range paths {
		if path == "-" {
			files = append(files, path)
			continue
		}
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() {
			files = append(files, path)
			continue
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, a.operational(name, "text", result.ExitIO, "JPS-INPUT-READ", "A --kinds directory could not be read.")
		}
		var names []string
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
				names = append(names, entry.Name())
			}
		}
		sort.Strings(names)
		for _, file := range names {
			files = append(files, filepath.Join(path, file))
		}
		if len(files) > maxKindFiles {
			break
		}
	}
	if len(files) > maxKindFiles {
		return nil, a.operational(name, "text", result.ExitInvocation, "JPS-INVOCATION-INPUT", fmt.Sprintf("--kinds names more than %d kind declarations.", maxKindFiles))
	}
	return files, nil
}

// registryCheckPackCommand checks a pack against a facts contract (RFC 0037
// §5, ADR-0095).
func (a *App) registryCheckPackCommand() *cobra.Command {
	var contractPath string
	command := &cobra.Command{
		Use:   "check-pack PACK --contract CONTRACT",
		Short: "Check the facts a pack reads against a facts contract, without evaluating (spec RFC 0037)",
		Long: "Read a facts contract and a pack, and report, for each fact condition of the pack's evaluation form, a pointer the contract does not list (FACTS-POINTER-UNKNOWN), an ordered comparison on a pointer the contract does not type decimal (FACTS-TYPE-ORDERED), " +
			"and an equals or not-equals operand, or an item of an in, that the pointer can never hold (FACTS-VALUE-UNREACHABLE). These are advisories: nothing is evaluated, and an evaluation is never changed by them. " +
			"The report is JSON on standard output (factsCheckFormatVersion 1.0.0) and names the contract and the pack by digest only. Exit 0 when there is no diagnostic, 1 when there is an advisory or the contract is refused, " +
			"the evaluator's exit code when the pack is refused, 3 for an unusable command line and 4 when an input could not be read.",
		Example: "jpack registry check-pack db-role-review.json --contract db-role.contract.json",
		Args:    cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			const name = "registry check-pack"
			if contractPath == "" {
				return a.operational(name, "json", result.ExitInvocation, "JPS-INVOCATION-INPUT", "Supply --contract, the facts contract.")
			}
			for _, path := range []string{args[0], contractPath} {
				if strings.Contains(path, "://") || fssecure.IsRemotePath(path) {
					return a.operational(name, "json", result.ExitInvocation, "JPS-INVOCATION-INPUT", "Use local files or standard input.")
				}
			}
			if args[0] == "-" && contractPath == "-" {
				return a.operational(name, "json", result.ExitInvocation, "JPS-INVOCATION-INPUT", "Only one input can read standard input.")
			}
			report := registry.FactsCheck{FactsCheckFormatVersion: registry.FactsCheckFormatVersion, Diagnostics: []registry.Diagnostic{}}
			finish := func() error {
				if err := a.writeJSON(report); err != nil {
					return &handledExit{code: result.ExitIO}
				}
				if len(report.Diagnostics) > 0 {
					return &handledExit{code: result.ExitInvalid}
				}
				return nil
			}
			document := registry.Document{}
			data, err := a.readInput(contractPath, registry.MaxContractBytes, "facts contract")
			switch {
			case errors.Is(err, fssecure.ErrTooLarge):
				document.Oversize = true
			case err != nil:
				return a.operational(name, "json", result.ExitIO, "JPS-INPUT-READ", "The facts contract could not be read as one bounded regular file or standard input stream.")
			default:
				document.Data = data
			}
			contract, diagnostics := registry.ReadFactsContract(document)
			if contract == nil {
				report.Diagnostics = diagnostics
				return finish()
			}
			report.Contract = &registry.Digested{Digest: contract.Digest}
			pack, err := a.readInput(args[0], carrier.HardMaxBytes, "pack")
			if errors.Is(err, fssecure.ErrTooLarge) {
				return a.operational(name, "json", result.ExitIO, "JPS-RESOURCE-INPUT-BYTE-LIMIT", fmt.Sprintf("The pack exceeds the hard %d-byte limit; nothing was checked.", carrier.HardMaxBytes))
			}
			if err != nil {
				return a.operational(name, "json", result.ExitIO, "JPS-INPUT-READ", "The pack could not be read as one bounded regular file or standard input stream.")
			}
			selected := result.LegacyEvaluatorContract()
			chosen, selectionFailure := evaluation.SelectContract(pack)
			if selectionFailure != nil && selectionFailure.Code != "JPS-EVALUATION-PACK-SPEC-VERSION" {
				return a.evaluationFailure(name, "json", selectionFailure)
			}
			if selectionFailure == nil {
				selected = chosen
			}
			admitted := evaluation.NewEngine(a.engine).AdmitPack(pack)
			form, refused := admitted.EvaluationForm(evaluation.Options{SpecVersion: selected.Version(), Command: name, EvidenceSupplied: true})
			if refused != nil {
				return a.evaluationFailure(name, "json", refused, selected)
			}
			digest := admitted.Digest()
			report.Pack = &registry.Digested{Digest: "sha256:" + hex.EncodeToString(digest[:])}
			report.Diagnostics = contract.CheckPack(form)
			return finish()
		},
	}
	command.Flags().StringVar(&contractPath, "contract", "", "The facts contract JSON file (or -)")
	return command
}
