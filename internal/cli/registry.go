package cli

import (
	"errors"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/spf13/cobra"
)

// registryCommand is the registry companion proposed by RFCs 0034 and 0035
// and released as registry-v1.0.0 (ADR-0093): validation with a value-free report, and resolution to one
// document per environment; and import from YAML (ADR-0094). It fetches
// nothing.
func (a *App) registryCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "registry",
		Short: "Import, validate and resolve a registry of adopter-declared kinds (spec RFCs 0034, 0035)",
		Long:  "Registry operations over local files: a registry document and the kind declarations it pins, in JSON, or in the YAML subset import converts to JSON (ADR-0094). Nothing is fetched: a secret reference is checked for form, version pin and environment scope and left as written. Reports never carry a value from the documents. It follows the accepted RFCs and the registry companion released as registry-v1.0.0.",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	command.AddCommand(a.registryImportCommand(), a.registryValidateCommand(), a.registryResolveCommand(), a.registryFactsCommand(), a.registryCheckPackCommand(), a.registryAssembleCommand(), a.registryCheckIndexCommand())
	return command
}

func (a *App) registryValidateCommand() *cobra.Command {
	var kinds []string
	command := &cobra.Command{
		Use:     "validate REGISTRY --kind KIND...",
		Short:   "Validate a registry with its kinds and write the report",
		Long:    "Validate a registry document with the kind declarations it pins, in RFC 0034's fixed order, and write the report as JSON. Exit 0 when the registry is valid (warnings allowed), 1 when it is not.",
		Example: "jpack registry validate registry.json --kind kinds/database.json --kind kinds/db-role.json",
		Args:    cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			const name = "registry validate"
			validated, failure := a.validateRegistry(name, args[0], kinds)
			if failure != nil {
				return failure
			}
			return a.registryReport(validated)
		},
	}
	command.Flags().StringArrayVar(&kinds, "kind", nil, "A kind declaration JSON file; repeat for each kind (or - for one)")
	return command
}

func (a *App) registryResolveCommand() *cobra.Command {
	var kinds []string
	environment := ""
	command := &cobra.Command{
		Use:     "resolve REGISTRY --kind KIND... --environment ENV",
		Short:   "Resolve a valid registry for one environment",
		Long:    "Validate the registry, then write its resolved document for one environment: RFC 8785 canonical JSON followed by one newline. An invalid registry writes the validation report instead and exits 1.",
		Example: "jpack registry resolve registry.json --kind kinds/database.json --environment prod",
		Args:    cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			const name = "registry resolve"
			if environment == "" {
				return a.operational(name, "json", result.ExitInvocation, "JPS-INVOCATION-ENVIRONMENT", "Supply --environment, one of the registry's environments.")
			}
			validated, failure := a.validateRegistry(name, args[0], kinds)
			if failure != nil {
				return failure
			}
			if !validated.Report.Valid {
				return a.registryReport(validated)
			}
			resolved, err := validated.Resolve(environment)
			if err != nil {
				return a.operational(name, "json", result.ExitInvocation, "JPS-INVOCATION-ENVIRONMENT", "The environment is not one of the registry's environments.")
			}
			if _, err := a.out.Write(append(resolved, '\n')); err != nil {
				return &handledExit{code: result.ExitIO}
			}
			return nil
		},
	}
	command.Flags().StringArrayVar(&kinds, "kind", nil, "A kind declaration JSON file; repeat for each kind (or - for one)")
	command.Flags().StringVar(&environment, "environment", "", "The environment to resolve")
	return command
}

// validateRegistry reads the inputs and validates them. A document over its
// size bound is a validation finding, not an operational error.
func (a *App) validateRegistry(name, registryPath string, kindPaths []string) (*registry.Result, error) {
	stdin := 0
	for _, path := range append([]string{registryPath}, kindPaths...) {
		if strings.Contains(path, "://") || fssecure.IsRemotePath(path) {
			return nil, a.operational(name, "json", result.ExitInvocation, "JPS-INVOCATION-INPUT", "Use local files or standard input.")
		}
		if path == "-" {
			stdin++
		}
	}
	if stdin > 1 {
		return nil, a.operational(name, "json", result.ExitInvocation, "JPS-INVOCATION-INPUT", "Only one input can read standard input.")
	}
	registryDocument := registry.Document{}
	data, err := a.readInput(registryPath, registry.MaxRegistryBytes, "registry")
	switch {
	case errors.Is(err, fssecure.ErrTooLarge):
		registryDocument.Oversize = true
	case err != nil:
		return nil, a.operational(name, "json", result.ExitIO, "JPS-INPUT-READ", "The registry could not be read as one bounded regular file or standard input stream.")
	default:
		registryDocument.Data = data
	}
	documents := make([]registry.Document, 0, len(kindPaths))
	for _, path := range kindPaths {
		data, err := a.readInput(path, registry.MaxKindBytes, "kind declaration")
		switch {
		case errors.Is(err, fssecure.ErrTooLarge):
			documents = append(documents, registry.Document{Oversize: true})
		case err != nil:
			return nil, a.operational(name, "json", result.ExitIO, "JPS-INPUT-READ", "A kind declaration could not be read as one bounded regular file or standard input stream.")
		default:
			documents = append(documents, registry.Document{Data: data})
		}
	}
	return registry.ValidateDocuments(registryDocument, documents), nil
}

func (a *App) registryReport(validated *registry.Result) error {
	if err := a.writeJSON(validated.Report); err != nil {
		return &handledExit{code: result.ExitIO}
	}
	if !validated.Report.Valid {
		return &handledExit{code: result.ExitInvalid}
	}
	return nil
}
