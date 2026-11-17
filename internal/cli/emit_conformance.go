package cli

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit/conformance"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
	"github.com/spf13/cobra"
)

// emitConformanceCommand is the emitter conformance harness of spec RFC 0036
// §5 (ADR-0099): it runs an emitter on its author's vectors and reports each
// check of the vectors' harness level, and the harness's hostile responses.
func (a *App) emitConformanceCommand() *cobra.Command {
	var manifestPath, vectorsPath, root string
	update := false
	timeout := emit.DefaultTimeout
	command := &cobra.Command{
		Use:   "conformance --manifest MANIFEST --vectors DIR [--update]",
		Short: "Check a registry emitter against its vectors (spec RFC 0036 §5)",
		Long: "Run the emitter an emitter manifest names on each vector of a vector directory, as `jpack emit` runs it, and apply the checks of the directory's harness level. " +
			"A vector is a directory holding request.json, or registry.json with kinds/ and an optional options.json from which the request is formed as `jpack emit` forms it; and expected/, exactly the files the response must contain (for none, the one empty file expected/~empty, which --update writes), or expected-failure.json, {\"code\": CODE}, the code the run must fail with. " +
			"DIR/harness.json, {\"harnessLevel\": \"1.0\"}, names the level; without it the level is 1.0, and a level this harness does not know refuses the run. " +
			"Level 1.0 is six checks: golden equality, byte for byte and mode for mode; determinism, two more runs with another HOME, TZ, LANG and SOURCE_DATE_EPOCH, the manifest's env held, answering the same bytes; path safety and bounds, with the harness's own hostile responses, each a row of its own; kind filtering, the request formed again with a synthetic undeclared kind and the registry digest restored, which must be the same bytes and get the same response; references, no run of 32 or more characters of a secret reference outside a complete occurrence of it in any file; and no stray writes, each vector run in a fresh copy of the repository root (--root, default the current directory; .git is not copied; at most 256 MiB and 100000 entries) that the emitter must leave as it was (EMIT-STRAY-WRITE). " +
			"--update rewrites each vector's expected/ from the emitter's run, for the author to review; it never touches an expected-failure vector and writes nowhere else. " +
			"The report is RFC 8785 canonical JSON on standard output (harnessReportVersion 1.0.0): one row per vector and check, pass, fail, skip with its reason, or info; vector names, codes, paths and pointers, never a file's contents, a request value or an env value. " +
			"Exit 0 when every row passes, is skipped or is info, 1 when a check fails, 2 when the harness could not run (a refused manifest, vectors not of the layout, an unreadable path, or a bound), and 3 for an unusable command line. " +
			"The emitter is code the repository chose to run, with the user's rights, and is not sandboxed: the copy keeps an honest emitter's mistakes out of the repository, not a hostile one. It follows the accepted RFC; the registry companion's normative text and vectors are not released yet.",
		Example: "jpack emit conformance --manifest tools/emit-db-bootstrap/emitter.json --vectors tools/emit-db-bootstrap/vectors/\n" +
			"jpack emit conformance --manifest tools/emit-db-bootstrap/emitter.json --vectors tools/emit-db-bootstrap/vectors/ --update",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			const name = "emit conformance"
			invocation := func(code, message string) error {
				return a.operational(name, "json", result.ExitInvocation, code, message)
			}
			for _, path := range []string{manifestPath, vectorsPath, root} {
				if strings.Contains(path, "://") || fssecure.IsRemotePath(path) {
					return invocation("JPS-INVOCATION-INPUT", "Use local files.")
				}
			}
			switch {
			case manifestPath == "":
				return invocation("JPS-INVOCATION-INPUT", "Supply --manifest, the emitter manifest.")
			case vectorsPath == "":
				return invocation("JPS-INVOCATION-INPUT", "Supply --vectors, the vector directory.")
			case vectorsPath == "-":
				return invocation("JPS-INVOCATION-INPUT", "--vectors must name a directory.")
			case timeout <= 0:
				return invocation("JPS-INVOCATION-ARGUMENTS", "--timeout must be a positive duration, such as 120s.")
			}
			realRoot, err := emit.RealRoot(root)
			if errors.Is(err, emit.ErrRootReparse) {
				return invocation("JPS-INVOCATION-INPUT", "--root passes through a junction or other reparse point, such as a synced folder's placeholder; name a directory whose path has none.")
			}
			if err != nil {
				return invocation("JPS-INVOCATION-INPUT", "--root must name a directory.")
			}
			vectors, err := filepath.Abs(vectorsPath)
			if err != nil {
				return invocation("JPS-INVOCATION-INPUT", "--vectors must name a directory.")
			}
			finish := func(report *conformance.Report) error {
				data, err := report.Canonical()
				if err != nil {
					return &handledExit{code: result.ExitIO}
				}
				if _, err := a.out.Write(append(data, '\n')); err != nil {
					return &handledExit{code: result.ExitIO}
				}
				if code := report.Exit(); code != 0 {
					return &handledExit{code: code}
				}
				return nil
			}
			data, err := a.readInput(manifestPath, emit.MaxManifestBytes, "emitter manifest")
			switch {
			case errors.Is(err, fssecure.ErrTooLarge):
				return finish(conformance.NewReport().Refuse(emit.NewDiagnostic("EMIT-MANIFEST", emit.Location{Document: emit.DocumentManifest})))
			case err != nil:
				return finish(conformance.NewReport().Refuse(emit.NewDiagnostic("EMIT-PATH-UNREADABLE", emit.Location{Document: emit.DocumentManifest})))
			}
			manifest, diagnostics := emit.ReadManifest(data)
			if manifest == nil {
				return finish(conformance.NewReport().Refuse(diagnostics...))
			}
			interrupt, stop := interruptContext()
			defer stop()
			return finish(conformance.Run(interrupt.ctx, manifest, vectors, conformance.Options{Root: realRoot, Timeout: timeout, Update: update}))
		},
	}
	command.Flags().StringVar(&manifestPath, "manifest", "", "The emitter manifest JSON file (or - for standard input)")
	command.Flags().StringVar(&vectorsPath, "vectors", "", "The vector directory")
	command.Flags().BoolVar(&update, "update", false, "Rewrite each vector's expected/ from the emitter's run")
	command.Flags().DurationVar(&timeout, "timeout", emit.DefaultTimeout, "The wall-clock limit on each emitter run")
	command.Flags().StringVar(&root, "root", ".", "The repository root, which the emitter runs in a fresh copy of")
	return command
}
