package cli

import (
	"errors"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/spf13/cobra"
)

// registryImportCommand converts one YAML file in RFC 0034 §7's authoring
// subset to the canonical JSON of the registry or kind declaration it holds
// (ADR-0094). Every other registry command reads only JSON.
func (a *App) registryImportCommand() *cobra.Command {
	output := ""
	command := &cobra.Command{
		Use:   "import FILE [--output OUT]",
		Short: "Convert a YAML registry or kind declaration to its canonical JSON",
		Long: "Read one YAML file in RFC 0034 §7's authoring subset and write the registry or kind declaration it holds, which one being said by its format selector member, as RFC 8785 canonical JSON followed by one newline; a registry's kinds and entries are written in canonical order. " +
			"The subset is one YAML 1.2 document with no directive, tag, anchor, alias, merge key or complex key, and no key that spans lines or reads as anything but a string. A plain scalar is true, false, null, an integer or a decimal in JSON's spelling, or a string; one that YAML 1.1 or the YAML 1.2 core schema would read as another value (yes, on, ~, 0x1F, 1e3, .inf, 007, a date) is refused, and quoting it makes it a string. An empty node is refused: write null or \"\". " +
			"A file outside the subset, or whose document fails the admission JSON gets (size bound, depth, duplicate keys, the number rules, the exact selector), is refused: the report lists every refusal it found by line and column, never a value, nothing else is written, and the exit code is 1. " +
			"--output writes the document to a file, replaced only when the import succeeds; without it the document goes to standard output. Import checks nothing past admission: run validate on what it writes. It follows the accepted RFC and the registry companion released as registry-v1.0.0.",
		Example: "jpack registry import registry.yaml --output registry.json\njpack registry import kinds/db-role.yaml > kinds/db-role.json",
		Args:    cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			const name = "registry import"
			for _, path := range []string{args[0], output} {
				if strings.Contains(path, "://") || path != "" && path != "-" && fssecure.IsRemotePath(path) {
					return a.operational(name, "json", result.ExitInvocation, "JPS-INVOCATION-INPUT", "Use local files or standard input and output.")
				}
			}
			source := registry.Document{}
			data, err := a.readInput(args[0], registry.MaxImportBytes, "YAML file")
			switch {
			case errors.Is(err, fssecure.ErrTooLarge):
				source.Oversize = true
			case err != nil:
				return a.operational(name, "json", result.ExitIO, "JPS-INPUT-READ", "The YAML file could not be read as one bounded regular file or standard input stream.")
			default:
				source.Data = data
			}
			canonical, report := registry.Import(source)
			if !report.Valid {
				if err := a.writeJSON(report); err != nil {
					return &handledExit{code: result.ExitIO}
				}
				return &handledExit{code: result.ExitInvalid}
			}
			document := append(canonical, '\n')
			if output == "" || output == "-" {
				if _, err := a.out.Write(document); err != nil {
					return &handledExit{code: result.ExitIO}
				}
				return nil
			}
			if err := replaceFile(output, document); err != nil {
				return a.operational(name, "json", result.ExitIO, "JPS-REGISTRY-IMPORT-WRITE", "The document could not be written to --output, which is left as it was; a symbolic link is not written through.")
			}
			return nil
		},
	}
	command.Flags().StringVar(&output, "output", "", "write the document to this file, replaced only when the import succeeds (default: standard output)")
	return command
}
