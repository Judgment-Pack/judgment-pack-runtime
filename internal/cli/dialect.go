package cli

import (
	"errors"
	"fmt"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/dialect"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/spf13/cobra"
)

func (a *App) dialectCommand() *cobra.Command {
	group := &cobra.Command{Use: "dialect", Short: "Offline dialect document tools", Long: "Offline authoring tools for a pinned draft dialect contract. Encoding checks JSON, the canonical number domain and the declared version; it does not validate the full dialect schema, prepare inputs, check source capabilities, or authorize execution. Use contract to inspect the implemented operations.", Args: cobra.NoArgs, RunE: func(c *cobra.Command, _ []string) error { return c.Help() }}
	for _, operation := range []string{"contract", "init", "canonicalize", "digest"} {
		group.AddCommand(a.dialectOperation(operation))
	}
	return group
}
func (a *App) dialectOperation(operation string) *cobra.Command {
	format := "human"
	version := 2
	use := operation
	args := cobra.NoArgs
	if operation == "canonicalize" || operation == "digest" {
		use += " <dialect-or->"
		args = cobra.ExactArgs(1)
	}
	descriptions := map[string]string{"contract": "Describe the pinned contract and implemented capabilities", "init": "Print a generic starter dialect without saving it", "canonicalize": "Print canonical dialect bytes without full document validation", "digest": "Print the digest of canonical dialect bytes without full document validation"}
	c := &cobra.Command{Use: use, Short: descriptions[operation], Args: args, RunE: func(_ *cobra.Command, args []string) error {
		command := "dialect " + operation
		if err := validateFormat(format); err != nil {
			return a.operational(command, format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
		}
		var report dialect.Report
		code := result.ExitSuccess
		switch operation {
		case "contract":
			report = dialect.Contract(command)
		case "init":
			report, code = dialect.Init(command, version)
		default:
			data, err := a.readInput(args[0], dialect.MaxDocumentBytes, "dialect")
			if errors.Is(err, fssecure.ErrTooLarge) {
				data = make([]byte, dialect.MaxDocumentBytes+1)
			} else if err != nil {
				return a.operational(command, format, result.ExitIO, "JPS-DIALECT-READ", "The dialect input could not be read as a bounded local regular file or standard input.")
			}
			report, code = dialect.Encode(command, data)
		}
		if format == "json" {
			if err := a.writeJSON(report); err != nil {
				return &handledExit{code: result.ExitIO}
			}
		} else if code != result.ExitSuccess {
			for _, diagnostic := range report.Diagnostics {
				if _, err := fmt.Fprintln(a.errOut, diagnostic.Code+": "+display.Sanitize(diagnostic.Message)); err != nil {
					return &handledExit{code: result.ExitIO}
				}
			}
		} else {
			var err error
			switch operation {
			case "contract":
				_, err = fmt.Fprintf(a.out, "Dialect document tools · draft\nSpecification: %s\nOperations: contract, init, canonicalize, digest\nEncoding versions: 2, 3\nFull document validation, pack compatibility and preparation verification are not implemented by these tools.\n", dialect.SpecURL)
			case "digest":
				_, err = fmt.Fprintln(a.out, report.Digest)
			default:
				_, err = fmt.Fprint(a.out, report.Document)
			}
			if err != nil {
				return &handledExit{code: result.ExitIO}
			}
		}
		if code != result.ExitSuccess {
			return &handledExit{code: code}
		}
		return nil
	}}
	c.Flags().StringVar(&format, "format", format, "output format: human or json; JSON includes scope and provenance")
	if operation == "init" {
		c.Flags().IntVar(&version, "dialect-version", version, "dialect encoding version: 2 or 3")
	}
	return c
}
