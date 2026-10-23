package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/packadvice"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/spf13/cobra"
)

func (a *App) analyzePackCommand() *cobra.Command {
	options := packadvice.DefaultOptions()
	domainPath, format := "", "json"
	command := &cobra.Command{Use: "analyze-pack PACK", Short: "Report bounded experimental pack-design advice over an explicit finite domain", Long: "Rehearse exact Core 0.3.0 base pack bytes over caller-declared finite cases using Draft RFC 0027. Reads local inputs and writes only the report to stdout; no audit record or publication gate. Output is compact JSON (including with --pretty) to preserve the report byte ceiling. Complete reports and incomplete reports exit successfully; an unavailable analysis exits 1. This runtime's conformance claim and its version scope are stated, in full and only, in CONFORMANCE.md.", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		const name = "experimental analyze-pack"
		if format != "json" {
			return a.operational(name, "json", result.ExitInvocation, "JPS-INVOCATION-FORMAT", "Pack design analysis supports --format json only.")
		}
		if domainPath == "" || args[0] == "-" && domainPath == "-" {
			return a.operational(name, format, result.ExitInvocation, "JPS-INVOCATION-DOMAIN", "Supply --domain as one JSON document; pack and domain cannot both read standard input.")
		}
		for _, path := range []string{args[0], domainPath} {
			if strings.Contains(path, "://") || fssecure.IsRemotePath(path) {
				return a.operational(name, format, result.ExitInvocation, "JPS-INVOCATION-INPUT", "Use local files or standard input.")
			}
		}
		output := func(report any, refusal *packadvice.Refusal) error {
			if refusal != nil {
				report = refusal
			}
			if err := writeJSON(a.out, report, false); err != nil {
				return &handledExit{result.ExitIO}
			}
			if refusal != nil {
				return &handledExit{result.ExitInvalid}
			}
			return nil
		}
		pack, err := a.readInput(args[0], carrier.HardMaxBytes, "pack")
		if errors.Is(err, fssecure.ErrTooLarge) {
			return output(nil, packadvice.Refuse("pack-unsupported"))
		}
		if err != nil {
			return a.operational(name, format, result.ExitIO, "JPS-INPUT-READ", "The pack could not be read as one bounded regular file or standard input stream.")
		}
		domain, err := a.readInput(domainPath, packadvice.MaxBytes, "domain")
		if errors.Is(err, fssecure.ErrTooLarge) {
			return output(nil, packadvice.Refuse("domain-limit"))
		}
		if err != nil {
			return a.operational(name, format, result.ExitIO, "JPS-INPUT-READ", "The domain could not be read as one bounded regular file or standard input stream.")
		}
		report, refusal := packadvice.Analyze(evaluation.NewEngine(a.engine), pack, domain, options)
		return output(report, refusal)
	}}
	command.Flags().StringVar(&domainPath, "domain", "", "Exact finite-cases domain JSON file (or -)")
	command.Flags().StringVar(&format, "format", "json", "Output format: json")
	command.Flags().BoolVar(&options.IntentionalFallback, "intentional-fallback", false, "Declare fallback intentional for this finite domain only")
	command.Flags().IntVar(&options.MaxWork, "max-work", options.MaxWork, "Analyzer evaluator/predicate units (1..100000; separate from evaluator work)")
	command.Flags().IntVar(&options.MaxFindings, "max-findings", options.MaxFindings, "Finding slots, reserving one incomplete marker (1..256)")
	command.Example = fmt.Sprintf("jpack experimental analyze-pack pack.json --domain domain.json --max-work %d", options.MaxWork)
	return command
}
