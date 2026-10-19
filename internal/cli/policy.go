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
		Use: "policy", Short: "EXPERIMENTAL: offline Policy prototype and official draft tools",
		Long: "Offline tools for the nonnormative RFC 0023 Policy prototype 0.0.2 and independently released Policy draft 1.0.0. Discover formats explicitly with contract --all-formats; no-argument contract retains the prototype contract. Validate checks document forms and local references. Check also checks the exact supplied retained-material closure and specific Core references; it does not perform full Core semantic validation or evaluate anything. No approval, governing-adoption authority, publication, billing or source access is established. This experimental surface may change without compatibility promise.",
		Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	for _, operation := range []string{"contract", "init", "schema", "canonicalize", "digest", "validate", "check"} {
		group.AddCommand(a.policyOperation(operation))
	}
	return group
}

func (a *App) policyOperation(operation string) *cobra.Command {
	format, kind, profileVersion, id, version, materialsPath := "human", "", "", "", "", ""
	allFormats := false
	use, args := operation, cobra.NoArgs
	if operation == "canonicalize" || operation == "digest" || operation == "validate" || operation == "check" {
		use += " <document-or->"
		args = cobra.ExactArgs(1)
	}
	descriptions := map[string]string{
		"contract":     "Describe capabilities and pinned provenance; select the official draft explicitly",
		"init":         "Print a starter template; missing retained materials remain unchecked",
		"schema":       "Print the schema for an explicitly selected Policy kind and format",
		"canonicalize": "Print canonical document bytes; encoding success is not validation",
		"digest":       "Print the canonical document digest; raw-byte identity is separate",
		"validate":     "Check selected Policy document forms and local references offline",
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
		if operation == "contract" && cmd.Flags().Changed("all-formats") && cmd.Flags().Changed("format-version") {
			return a.operational(command, format, result.ExitInvocation, "JPS-POLICY-SELECTOR", "Use either --all-formats or --format-version.")
		}
		if cmd.Flags().Changed("format-version") && profileVersion == "" && operation != "init" && operation != "schema" {
			return a.operational(command, format, result.ExitInvocation, "JPS-POLICY-SELECTOR", "Provide a nonempty exact format version.")
		}
		var report policy.Report
		var registry *policy.Registry
		code := result.ExitSuccess
		switch operation {
		case "contract":
			if allFormats {
				r := policy.AllFormats(command)
				registry = &r
			} else if cmd.Flags().Changed("format-version") {
				report, code = policy.ContractFormat(command, profileVersion)
			} else {
				report = policy.Contract(command)
			}
		case "init":
			report, code = policy.Init(command, kind, profileVersion, id, version)
		case "schema":
			report, code = policy.Schema(command, kind, profileVersion)
		default:
			data, err := a.readInput(operands[0], policy.MaxDocumentBytes, "Policy document")
			if errors.Is(err, fssecure.ErrTooLarge) {
				data = make([]byte, policy.MaxDocumentBytes+1)
			} else if err != nil {
				return a.operational(command, format, result.ExitIO, "JPS-POLICY-READ", "The document could not be read as a bounded local regular file or standard input.")
			}
			switch operation {
			case "canonicalize":
				report, code = policy.CanonicalizeFormat(command, data, profileVersion)
			case "digest":
				report, code = policy.DigestFormat(command, data, profileVersion)
			case "validate":
				report, code = policy.ValidateFormat(command, data, profileVersion)
			case "check":
				materials, err := a.readInput(materialsPath, policy.MaxMaterialsWireBytes, "retained materials")
				if errors.Is(err, fssecure.ErrTooLarge) {
					materials = make([]byte, policy.MaxMaterialsWireBytes+1)
				} else if err != nil {
					return a.operational(command, format, result.ExitIO, "JPS-POLICY-MATERIALS-READ", "The retained-material input could not be read as a bounded local regular file or standard input.")
				}
				report, code = policy.CheckFormat(command, data, materials, a.engine, profileVersion)
			}
		}
		if format == "json" {
			var payload any = report
			if registry != nil {
				payload = *registry
			}
			if err := a.writeJSON(payload); err != nil {
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
				if registry != nil {
					_, err = fmt.Fprintf(a.out, "Policy format registry 1 · EXPERIMENTAL\n0.0.2 · nonnormative prototype · %s\n1.0.0 · official Policy draft · %s\nSelect a format explicitly for init/schema. No authority or conformance is established.\n", policy.SpecURL, policy.DraftURL)
				} else if report.FormatVersion == policy.CandidateVersion {
					_, err = fmt.Fprintf(a.out, "Policy draft %s · EXPERIMENTAL · draft maturity\nSpecification: %s\nKinds: policy, governing-adoption\nNo approval, current authority or standard conformance is established.\n", report.FormatVersion, report.Specification)
				} else {
					_, err = fmt.Fprintf(a.out, "Policy tools · EXPERIMENTAL · nonnormative prototype %s\nSpecification: %s\nKinds: policy, governing-adoption\nOperations: contract, init, schema, canonicalize, digest, validate, check\nValidate checks document forms and local references. Check checks exact supplied retained materials and specific Core references, without full Core semantic validation or evaluation.\nNo approval, governing-adoption authority or standard conformance is established.\n", policy.PrototypeVersion, policy.SpecURL)
				}
			case "schema":
				_, err = fmt.Fprint(a.out, report.Resource.Text)
			case "digest":
				_, err = fmt.Fprintln(a.out, report.CanonicalDigest)
			case "validate":
				if report.FormatVersion == policy.CandidateVersion {
					_, err = fmt.Fprintln(a.out, "Policy draft document and local-reference checks passed. Retained materials were not checked; no authority is established.")
				} else {
					_, err = fmt.Fprintln(a.out, "Prototype document and local-reference checks passed. Retained materials were not checked; no authority is established.")
				}
			case "check":
				if report.FormatVersion == policy.CandidateVersion {
					_, err = fmt.Fprintln(a.out, "Policy draft retained-material and reference checks passed. Full Core semantics and evaluation were not checked; no authority is established.")
				} else {
					_, err = fmt.Fprintln(a.out, "Prototype retained-material and reference checks passed. Full Core semantics and evaluation were not checked; no authority is established.")
				}
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
	}
	c.Flags().StringVar(&profileVersion, "format-version", "", "exact format: prototype 0.0.2 or official draft 1.0.0; required for init/schema, optional explicit dispatch otherwise")
	if operation == "contract" {
		c.Flags().BoolVar(&allFormats, "all-formats", false, "explicitly discover all formats in registry version 1")
	}
	if operation == "init" {
		c.Flags().StringVar(&id, "id", "", "required absolute artifact URI identifier")
		c.Flags().StringVar(&version, "version", "", "required exact three-component artifact version; separate from Policy format")
	}
	if operation == "check" {
		c.Flags().StringVar(&materialsPath, "materials", "", "required retained-material JSON file or -; private Runtime byte transport, not a release envelope")
	}
	return c
}
