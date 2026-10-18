package cli

import (
	"errors"
	"fmt"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/display"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/policy"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/spf13/cobra"
)

func (a *App) policyCommand() *cobra.Command {
	group := &cobra.Command{
		Use: "policy", Short: "EXPERIMENTAL: offline nonnormative Policy prototype tools",
		Long: "Offline tools for the nonnormative RFC 0023 Policy prototype, exact format 0.0.2. Validate checks document forms and local references. Check also checks the exact supplied retained-material closure and specific Core references; it does not perform full Core semantic validation or evaluate anything. No approval, governing-adoption authority, publication, billing or source access is established. This experimental surface may change without compatibility promise.",
		Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	for _, operation := range []string{"contract", "init", "schema", "canonicalize", "digest", "validate", "check"} {
		group.AddCommand(a.policyOperation(operation))
	}
	return group
}

func (a *App) policyOperation(operation string) *cobra.Command {
	format, kind, profileVersion, id, version, materialsPath := "human", "", "", "", "", ""
	use, args := operation, cobra.NoArgs
	if operation == "canonicalize" || operation == "digest" || operation == "validate" || operation == "check" {
		use += " <document-or->"
		args = cobra.ExactArgs(1)
	}
	descriptions := map[string]string{
		"contract":     "Describe exact prototype capabilities, limits and pinned provenance",
		"init":         "Print a starter template; missing retained materials remain unchecked",
		"schema":       "Print the schema for an explicitly selected prototype kind and format",
		"canonicalize": "Print canonical document bytes; encoding success is not validation",
		"digest":       "Print the canonical document digest; raw-byte identity is separate",
		"validate":     "Check prototype document forms and local references offline",
		"check":        "Check exact supplied retained materials and references offline",
	}
	c := &cobra.Command{Use: use, Short: descriptions[operation], Args: args, RunE: func(cmd *cobra.Command, operands []string) error {
		command := "experimental policy " + operation
		if err := validateFormat(format); err != nil {
			return a.operational(command, format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
		}
		if operation == "check" && (materialsPath == "" || materialsPath == "-" && operands[0] == "-") {
			return a.operational(command, format, result.ExitInvocation, "JPS-POLICY-MATERIALS-ARGUMENT", "Check requires --materials; only one operand can read standard input.")
		}
		if (operation == "init" || operation == "schema") && (!cmd.Flags().Changed("kind") || !cmd.Flags().Changed("format-version")) {
			return a.operational(command, format, result.ExitInvocation, "JPS-POLICY-SELECTOR", "Explicit --kind and --format-version are required.")
		}
		if operation == "init" && (!cmd.Flags().Changed("id") || !cmd.Flags().Changed("version")) {
			return a.operational(command, format, result.ExitInvocation, "JPS-POLICY-IDENTITY", "Init requires explicit --id and --version.")
		}
		var report policy.Report
		code := result.ExitSuccess
		switch operation {
		case "contract":
			report = policy.Contract(command)
		case "init":
			report, code = policy.Init(command, kind, profileVersion, id, version)
		case "schema":
			report, code = policy.Schema(command, kind, profileVersion)
		default:
			data, err := a.readInput(operands[0], policy.MaxDocumentBytes, "Policy prototype document")
			if errors.Is(err, fssecure.ErrTooLarge) {
				data = make([]byte, policy.MaxDocumentBytes+1)
			} else if err != nil {
				return a.operational(command, format, result.ExitIO, "JPS-POLICY-READ", "The document could not be read as a bounded local regular file or standard input.")
			}
			switch operation {
			case "canonicalize":
				report, code = policy.Canonicalize(command, data)
			case "digest":
				report, code = policy.Digest(command, data)
			case "validate":
				report, code = policy.Validate(command, data)
			case "check":
				materials, err := a.readInput(materialsPath, policy.MaxMaterialsWireBytes, "retained materials")
				if errors.Is(err, fssecure.ErrTooLarge) {
					materials = make([]byte, policy.MaxMaterialsWireBytes+1)
				} else if err != nil {
					return a.operational(command, format, result.ExitIO, "JPS-POLICY-MATERIALS-READ", "The retained-material input could not be read as a bounded local regular file or standard input.")
				}
				report, code = policy.Check(command, data, materials, a.engine)
			}
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
				_, err = fmt.Fprintf(a.out, "Policy tools · EXPERIMENTAL · nonnormative prototype %s\nSpecification: %s\nKinds: policy, governing-adoption\nOperations: contract, init, schema, canonicalize, digest, validate, check\nValidate checks document forms and local references. Check checks exact supplied retained materials and specific Core references, without full Core semantic validation or evaluation.\nNo approval, governing-adoption authority or standard conformance is established.\n", policy.PrototypeVersion, policy.SpecURL)
			case "schema":
				_, err = fmt.Fprint(a.out, report.Resource.Text)
			case "digest":
				_, err = fmt.Fprintln(a.out, report.CanonicalDigest)
			case "validate":
				_, err = fmt.Fprintln(a.out, "Prototype document and local-reference checks passed. Retained materials were not checked; no authority is established.")
			case "check":
				_, err = fmt.Fprintln(a.out, "Prototype retained-material and reference checks passed. Full Core semantics and evaluation were not checked; no authority is established.")
			case "init":
				_, err = fmt.Fprintln(a.errOut, "Starter template: retained materials are missing and unchecked; no publication or authority is established.")
				if err == nil {
					_, err = fmt.Fprint(a.out, report.Document)
				}
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
		c.Flags().StringVar(&kind, "kind", "", "required exact kind: policy or governing-adoption")
		c.Flags().StringVar(&profileVersion, "format-version", "", "required exact nonnormative prototype format: 0.0.2")
	}
	if operation == "init" {
		c.Flags().StringVar(&id, "id", "", "required absolute artifact URI identifier")
		c.Flags().StringVar(&version, "version", "", "required exact three-component artifact version; separate from prototype format")
	}
	if operation == "check" {
		c.Flags().StringVar(&materialsPath, "materials", "", "required retained-material JSON file or -; private Runtime byte transport, not a release envelope")
	}
	return c
}
