package cli

import (
	"fmt"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifactversion"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/spf13/cobra"
)

func (a *App) artifactCommand() *cobra.Command {
	group := &cobra.Command{Use: "artifact", Short: "Offline artifact authoring utilities", Args: cobra.NoArgs, RunE: func(c *cobra.Command, _ []string) error { return c.Help() }}
	var base, candidate string
	format := "human"
	c := &cobra.Command{Use: "version --base <x.y.z> [--candidate <x.y.z>]", Short: "Calculate patch, minor and major version options without saving", Long: "Calculate exact three-component version options and optionally compare a candidate numerically. Does not inspect content, assess compatibility, reserve an identity, change a format version, or authorize a save. No project or file access.", Args: cobra.NoArgs, RunE: func(c *cobra.Command, _ []string) error {
		command := "artifact version"
		if err := validateFormat(format); err != nil {
			return a.operational(command, format, result.ExitInvocation, "JPS-INVOCATION-FORMAT", err.Error())
		}
		var proposed *string
		if c.Flags().Changed("candidate") {
			proposed = &candidate
		}
		report, code := artifactversion.Options(command, base, proposed)
		var err error
		if format == "json" {
			err = a.writeJSON(report)
		} else if code == 0 {
			_, err = fmt.Fprintf(a.out, "Base: %s\nPatch: %s\nMinor: %s\nMajor: %s\n", report.Base, report.Choices.Patch, report.Choices.Minor, report.Choices.Major)
			if err == nil && proposed != nil {
				_, err = fmt.Fprintf(a.out, "Candidate: %s (%s)\n", report.Candidate, report.Comparison)
			}
			if err == nil {
				_, err = fmt.Fprintln(a.out, "Choose the bump after reviewing compatibility. Nothing has been saved.")
			}
		} else {
			for _, d := range report.Diagnostics {
				if _, err = fmt.Fprintln(a.errOut, d.Code+": "+d.Message); err != nil {
					break
				}
			}
		}
		if err != nil {
			return &handledExit{code: result.ExitIO}
		}
		if code != 0 {
			return &handledExit{code: code}
		}
		return nil
	}}
	c.Flags().StringVar(&base, "base", "", "Saved artifact version (required)")
	c.Flags().StringVar(&candidate, "candidate", "", "Optional candidate version to compare")
	c.Flags().StringVar(&format, "format", "human", "Output format: human or json")
	group.AddCommand(c)
	return group
}
