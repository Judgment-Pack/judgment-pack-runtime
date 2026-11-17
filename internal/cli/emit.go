package cli

import (
	"context"
	"errors"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/spf13/cobra"
)

// emitCommand is the registry emitter host of spec RFC 0036 §1–§4
// (ADR-0098): it runs an emitter on a request formed from a registry, and
// writes the files it answers with into the paths its manifest owns, or
// compares them with the files on disk.
func (a *App) emitCommand() *cobra.Command {
	var manifestPath, registryPath, optionsPath, root string
	var otherManifests, kindPaths, contractPaths []string
	check, passStderr := false, false
	timeout := emit.DefaultTimeout
	command := &cobra.Command{
		Use:   "emit --manifest MANIFEST (--registry REGISTRY --kinds PATH... | --facts-contract FILE...) [--options FILE] [--check]",
		Short: "Run a registry emitter and write or check the files it owns (spec RFC 0036)",
		Long: "Run the emitter an emitter manifest names on a request formed from a registry, its kinds and an options document, and write the files it answers with into the paths the manifest owns. " +
			"The request holds only the kinds the manifest reads, each pinned version checked against its range, and options checked against the manifest's schema; it is RFC 8785 canonical JSON on the emitter's standard input. " +
			"The emitter runs from the repository root (--root, default the current directory) with an environment of only PATH (its absolute directories), HOME (a fresh empty directory), LANG=C.UTF-8 and the variables the manifest's env names, under a wall-clock limit (--timeout, default 120s), in a process group that is killed at the limit and once the emitter exits; a process it starts in a session of its own (setsid) escapes the group. " +
			"Its standard error is counted and discarded unless --emitter-stderr passes the first 64 KiB through. " +
			"Its response is validated whole before anything changes: every path relative, safe and owned, unique once case is folded, at most 10000 files of at most 16 MiB, 64 MiB in all. " +
			"Each file is written through a temporary file and a rename; an owned file the response does not hold is deleted; a symbolic link at or above an owned path refuses the run. " +
			"--check compares instead, byte for byte and mode for mode, and writes nothing. " +
			"--other-manifest names another emitter manifest of the repository, whose owns globs must not overlap this one's. " +
			"The report is JSON on standard output (emitReportVersion 1.0.0): paths, codes, digests and counts, never a file's contents or an env value. " +
			"Exit 0 when the files were written or are identical, 1 when --check found a difference, 2 when nothing was written or checked (an emitter failure, a refused manifest, request or response, or an unreadable path), and 3 for an unusable command line. " +
			"The emitter is code the repository chose to run, with the user's rights: it is bounded by its environment, its time limit and its output, and is not sandboxed beyond that. It follows the accepted RFC; the registry companion's normative text and vectors are not released yet.",
		Example: "jpack emit --manifest tools/emit-db-bootstrap/emitter.json --registry registry.json --kinds kinds/\n" +
			"jpack emit --manifest tools/emit-db-bootstrap/emitter.json --registry registry.json --kinds kinds/ --check",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			const name = "emit"
			invocation := func(code, message string) error {
				return a.operational(name, "json", result.ExitInvocation, code, message)
			}
			all := append(append(append([]string{manifestPath, registryPath, optionsPath}, otherManifests...), kindPaths...), contractPaths...)
			stdin := 0
			for _, path := range all {
				if strings.Contains(path, "://") || fssecure.IsRemotePath(path) {
					return invocation("JPS-INVOCATION-INPUT", "Use local files or standard input.")
				}
				if path == "-" {
					stdin++
				}
			}
			switch {
			case manifestPath == "":
				return invocation("JPS-INVOCATION-INPUT", "Supply --manifest, the emitter manifest.")
			case stdin > 1:
				return invocation("JPS-INVOCATION-INPUT", "Only one input can read standard input.")
			case timeout <= 0:
				return invocation("JPS-INVOCATION-ARGUMENTS", "--timeout must be a positive duration, such as 120s.")
			case registryPath == "" && len(kindPaths) > 0, registryPath != "" && len(kindPaths) == 0:
				return invocation("JPS-INVOCATION-INPUT", "Supply --registry and --kinds together.")
			}
			realRoot, err := emit.RealRoot(root)
			if err != nil {
				return invocation("JPS-INVOCATION-INPUT", "--root must name a directory.")
			}
			report := emit.NewReport(check)
			finish := func(report *emit.Report) error {
				if err := a.writeJSON(report); err != nil {
					return &handledExit{code: result.ExitIO}
				}
				if code := report.Exit(); code != 0 {
					return &handledExit{code: code}
				}
				return nil
			}
			unreadable := func(document, path string) error {
				return finish(report.Refuse(emitDiagnostic("EMIT-PATH-UNREADABLE", emit.Location{Document: document, Path: path})))
			}

			manifest, failure := a.readManifest(manifestPath, 0, report)
			if failure != nil {
				return finish(failure)
			}
			var others []*emit.Manifest
			for i, path := range otherManifests {
				other, failure := a.readManifest(path, i+1, report)
				if failure != nil {
					return finish(failure)
				}
				others = append(others, other)
			}
			switch {
			case manifest.Input == emit.InputResolved && registryPath == "":
				return invocation("JPS-INVOCATION-INPUT", "The manifest reads resolved registries: supply --registry and --kinds.")
			case manifest.Input == emit.InputResolved && len(contractPaths) > 0:
				return invocation("JPS-INVOCATION-INPUT", "--facts-contract is for a manifest whose input is factsContracts.")
			case manifest.Input == emit.InputFactsContracts && registryPath == "" && len(contractPaths) == 0:
				return invocation("JPS-INVOCATION-INPUT", "The manifest reads facts contracts: supply --registry and --kinds, --facts-contract, or both.")
			}

			var inputs emit.Inputs
			if registryPath != "" {
				files, err := expandKinds(kindPaths)
				switch {
				case errors.Is(err, errKindsTooMany):
					return invocation("JPS-INVOCATION-INPUT", "--kinds names more than 1024 kind declarations.")
				case err != nil:
					return unreadable(emit.DocumentKind, "")
				}
				registryDocument, err := a.boundedDocument(registryPath, registry.MaxRegistryBytes, "registry")
				if err != nil {
					return unreadable(emit.DocumentRegistry, "")
				}
				kinds := make([]registry.Document, 0, len(files))
				for _, file := range files {
					document, err := a.boundedDocument(file, registry.MaxKindBytes, "kind declaration")
					if err != nil {
						return unreadable(emit.DocumentKind, "")
					}
					kinds = append(kinds, document)
				}
				inputs.Registry = registry.ValidateDocuments(registryDocument, kinds)
			}
			for _, path := range contractPaths {
				document, err := a.boundedDocument(path, registry.MaxContractBytes, "facts contract")
				if err != nil {
					return unreadable(emit.DocumentContract, "")
				}
				contract, _ := registry.ReadFactsContract(document)
				if contract == nil {
					return finish(report.Refuse(emitDiagnostic("EMIT-REQUEST", emit.Location{Document: emit.DocumentContract})))
				}
				inputs.Contracts = append(inputs.Contracts, contract)
			}
			if optionsPath != "" {
				data, err := a.readInput(optionsPath, emit.MaxOptionsBytes, "options document")
				switch {
				case errors.Is(err, fssecure.ErrTooLarge):
					return finish(report.Refuse(emitDiagnostic("EMIT-OPTIONS", emit.Location{Document: emit.DocumentOptions})))
				case err != nil:
					return unreadable(emit.DocumentOptions, "")
				}
				value, code, pointer := registry.AdmitValue(data, emit.MaxOptionsBytes)
				if code != "" {
					return finish(report.Refuse(emitDiagnostic("EMIT-OPTIONS", emit.Location{Document: emit.DocumentOptions, Pointer: pointer})))
				}
				inputs.Options = value
			}
			options := emit.Options{Root: realRoot, Check: check, Timeout: timeout}
			if passStderr {
				options.Stderr = a.errOut
			}
			return finish(emit.Emit(context.Background(), manifest, others, inputs, options))
		},
	}
	command.Flags().StringVar(&manifestPath, "manifest", "", "The emitter manifest JSON file")
	command.Flags().StringArrayVar(&otherManifests, "other-manifest", nil, "Another emitter manifest of the repository, whose owns globs must not overlap; repeat as needed")
	command.Flags().StringVar(&registryPath, "registry", "", "The registry document (or - for standard input)")
	command.Flags().StringArrayVar(&kindPaths, "kinds", nil, "A kind declaration file, or a directory whose *.json files are kind declarations; repeat as needed")
	command.Flags().StringVar(&optionsPath, "options", "", "The options document JSON file; omitted, the options are {}")
	command.Flags().StringArrayVar(&contractPaths, "facts-contract", nil, "An authored facts contract, for a manifest whose input is factsContracts; repeat as needed")
	command.Flags().BoolVar(&check, "check", false, "Compare the response with the files on disk and write nothing")
	command.Flags().DurationVar(&timeout, "timeout", emit.DefaultTimeout, "The wall-clock limit on each emitter run")
	command.Flags().BoolVar(&passStderr, "emitter-stderr", false, "Pass the first 64 KiB of the emitter's standard error through to standard error")
	command.Flags().StringVar(&root, "root", ".", "The repository root: the emitter's working directory, and what owns globs are relative to")
	command.AddCommand(a.emitConformanceCommand())
	return command
}

// readManifest reads and checks one emitter manifest; index 0 is the one run.
// It returns the report refused when the manifest is unreadable or invalid.
func (a *App) readManifest(path string, index int, report *emit.Report) (*emit.Manifest, *emit.Report) {
	data, err := a.readInput(path, emit.MaxManifestBytes, "emitter manifest")
	switch {
	case errors.Is(err, fssecure.ErrTooLarge):
		return nil, report.Refuse(emitDiagnostic("EMIT-MANIFEST", emit.Location{Document: emit.DocumentManifest, Manifest: index}))
	case err != nil:
		return nil, report.Refuse(emitDiagnostic("EMIT-PATH-UNREADABLE", emit.Location{Document: emit.DocumentManifest, Manifest: index}))
	}
	manifest, diagnostics := emit.ReadManifest(data)
	if manifest == nil {
		for i := range diagnostics {
			diagnostics[i].Location.Manifest = index
		}
		return nil, report.Refuse(diagnostics...)
	}
	return manifest, nil
}

// boundedDocument reads one registry input, marking a document over its
// bound as oversize, which validation reports.
func (a *App) boundedDocument(path string, limit int, what string) (registry.Document, error) {
	data, err := a.readInput(path, int64(limit), what)
	switch {
	case errors.Is(err, fssecure.ErrTooLarge):
		return registry.Document{Oversize: true}, nil
	case err != nil:
		return registry.Document{}, err
	}
	return registry.Document{Data: data}, nil
}

func emitDiagnostic(code string, location emit.Location) emit.Diagnostic {
	return emit.NewDiagnostic(code, location)
}
