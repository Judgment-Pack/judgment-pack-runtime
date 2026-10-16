package cli

import (
	"errors"
	"fmt"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/dialect"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/spf13/cobra"
	"io"
)

func (a *App) dialectCommand() *cobra.Command {
	group := &cobra.Command{Use: "dialect", Short: "Offline dialect document tools", Long: "Offline authoring tools for a pinned draft dialect contract. Encoding checks JSON, the canonical number domain, declared version and version-3 identity spelling; it does not validate the full dialect schema, prepare inputs, check source capabilities, or authorize execution. Validate checks version-2 and version-3 document forms and internal references for operation, selected-file and research sources. Version-3 unbound operations are checked as declarations only. Other version-3 source kinds, bindings, profiles and preparations are not checked. Use contract to inspect the implemented operations.", Args: cobra.NoArgs, RunE: func(c *cobra.Command, _ []string) error { return c.Help() }}
	for _, operation := range []string{"contract", "init", "canonicalize", "digest", "validate", "schema", "reference", "check"} {
		group.AddCommand(a.dialectOperation(operation))
	}
	return group
}
func (a *App) dialectOperation(operation string) *cobra.Command {
	format := "human"
	versionText := "2"
	formatVersion, artifactID, artifactVersion := "", "", ""
	packPath := ""
	use := operation
	args := cobra.NoArgs
	if operation == "canonicalize" || operation == "digest" || operation == "validate" || operation == "check" {
		use += " <dialect-or->"
		args = cobra.ExactArgs(1)
	}
	descriptions := map[string]string{"schema": "Print the Runtime-derived version-2 or version-3 authoring schema", "reference": "Print the exact pinned dialect reference offline", "check": "Check declared dialect targets against a validated pack inventory", "validate": "Validate supported version-2 and version-3 dialect document forms and internal references", "contract": "Describe the pinned contract and implemented capabilities", "init": "Print a generic starter dialect without saving it", "canonicalize": "Print canonical dialect bytes without full document validation", "digest": "Print the digest of canonical dialect bytes without full document validation"}
	c := &cobra.Command{Use: use, Short: descriptions[operation], Args: args, RunE: func(cmd *cobra.Command, args []string) error {
		command := "dialect " + operation
		if err := validateFormat(format); err != nil {
			return a.operational(command, format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
		}
		if operation == "check" && (packPath == "" || (packPath == "-" && args[0] == "-")) {
			return a.operational(command, format, result.ExitInvocation, "JPS-DIALECT-PACK-ARGUMENT", "Check requires --pack; only one operand can read standard input.")
		}
		if cmd.Flags().Changed("format-version") && formatVersion == "" {
			return a.operational(command, format, result.ExitInvocation, "JPS-DIALECT-FORMAT", "A named format selector cannot be empty.")
		}
		if formatVersion != "" && cmd.Flags().Changed("dialect-version") {
			return a.operational(command, format, result.ExitInvocation, "JPS-DIALECT-FORMAT", "Choose a legacy version or a named format, not both.")
		}
		if formatVersion == "" && (cmd.Flags().Changed("id") || cmd.Flags().Changed("artifact-version")) {
			return a.operational(command, format, result.ExitInvocation, "JPS-DIALECT-FORMAT", "Artifact identity requires --format-version.")
		}
		version := 2
		if operation == "init" || operation == "schema" {
			if versionText != "2" && versionText != "3" {
				return a.operational(command, format, result.ExitUnsupported, "JPS-DIALECT-VERSION", "Dialect version must be exactly 2 or 3.")
			}
			if versionText == "3" {
				version = 3
			}
		}
		var report dialect.Report
		code := result.ExitSuccess
		switch operation {
		case "contract":
			report = dialect.Contract(command)
		case "init":
			if formatVersion != "" {
				report, code = dialect.InitArtifact(command, formatVersion, artifactID, artifactVersion)
			} else {
				report, code = dialect.Init(command, version)
			}
		case "schema":
			if formatVersion != "" {
				report, code = dialect.ArtifactSchema(command, formatVersion)
			} else {
				report, code = dialect.Schema(command, version)
			}
		case "reference":
			report = dialect.Reference(command)
		default:
			data, err := a.readInput(args[0], dialect.MaxDocumentBytes, "dialect")
			if errors.Is(err, fssecure.ErrTooLarge) {
				data = make([]byte, dialect.MaxDocumentBytes+1)
			} else if err != nil {
				return a.operational(command, format, result.ExitIO, "JPS-DIALECT-READ", "The dialect input could not be read as a bounded local regular file or standard input.")
			}
			if operation == "check" {
				pack, err := a.readInput(packPath, dialect.MaxPackBytes, "pack")
				if errors.Is(err, fssecure.ErrTooLarge) {
					pack = make([]byte, dialect.MaxPackBytes+1)
				} else if err != nil {
					return a.operational(command, format, result.ExitIO, "JPS-DIALECT-PACK-READ", "The pack input could not be read as a bounded local regular file or standard input.")
				}
				report, code = dialect.Check(command, data, pack, a.engine)
			} else if operation == "validate" {
				report, code = dialect.Validate(command, data)
			} else {
				report, code = dialect.Encode(command, data)
			}
		}
		if format == "json" {
			if err := a.writeJSON(report); err != nil {
				return &handledExit{code: result.ExitIO}
			}
		} else if report.Compatibility != nil {
			if err := renderDialectCompatibility(a.out, report.Compatibility); err != nil {
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
				_, err = fmt.Fprintf(a.out, "Dialect document tools · draft\nSpecification: %s\nOperations: contract, init, canonicalize, digest, validate, schema, reference, check\nEncoding versions: 2, 3\nNamed formats: 2.0.0, 3.0.0\nDocument validation versions: 2, 3\nValidation source kinds: operation, selected-file, research\nUnbound operation declarations: supported; binding verification: not supported\nPack checks cover declared inventory only. Value types, profile compatibility and preparation verification are not checked.\n", dialect.SpecURL)
			case "schema", "reference":
				_, err = fmt.Fprint(a.out, report.Resource.Text)
			case "validate":
				_, err = fmt.Fprintf(a.out, "Version-%d document checks passed. Profiles, preparations and pack compatibility were not checked.\n", report.DialectVersion)
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
	if operation == "init" || operation == "schema" {
		c.Flags().StringVar(&versionText, "dialect-version", versionText, "legacy dialect version: exactly 2 or 3")
		c.Flags().StringVar(&formatVersion, "format-version", "", "named format: exactly 2.0.0 or 3.0.0")
		if operation == "init" {
			c.Flags().StringVar(&artifactID, "id", "", "stable artifact identifier")
			c.Flags().StringVar(&artifactVersion, "artifact-version", "", "three-component artifact version")
		}
	}
	if operation == "check" {
		c.Flags().StringVar(&packPath, "pack", "", "pack file or - for standard input; required")
	}
	return c
}

func renderDialectCompatibility(out io.Writer, c *dialect.Compatibility) error {
	if _, err := fmt.Fprintf(out, "Declared mapping coverage: %s\nPack: %s · %s\n", c.Status, display.Sanitize(c.Pack.ID), display.Sanitize(c.Pack.Version)); err != nil {
		return err
	}
	for _, part := range []struct {
		name    string
		entries []dialect.CoverageEntry
	}{{"Fact", c.Facts}, {"Evidence", c.Evidence}} {
		for _, entry := range part.entries {
			if _, err := fmt.Fprintf(out, "%s %q: %s\n", part.name, display.Sanitize(entry.Target), entry.Status); err != nil {
				return err
			}
		}
	}
	for _, note := range c.Notes {
		if _, err := fmt.Fprintf(out, "%s · %s · %s %q\n", note.Severity, note.Code, note.Kind, display.Sanitize(note.Target)); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(out, "The inventory over-approximates reads. Types, actual values, profiles, preparation and execution were not checked.")
	return err
}
