package sdk

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/cli"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/validation"
)

// A fault of this runtime is never a refusal of the caller's input: bundled
// artifacts that fail their integrity check, and any engine failure that
// carries no §8.4 class and is not the profile refusal, wrap ErrInternal.
func TestAnInternalFaultIsNotARefusal(t *testing.T) {
	previous := sharedValidator
	sharedValidator = func() (*validation.Engine, error) { return nil, errors.New("integrity") }
	t.Cleanup(func() { sharedValidator = previous })
	_, err := Load(context.Background(), []byte(reusePack), LoadOptions{})
	var refusal *Refusal
	if !errors.Is(err, ErrInternal) || errors.As(err, &refusal) {
		t.Fatalf("failed integrity check: %v (%T)", err, err)
	}
	if got, want := err.Error(), "sdk: internal fault: JPS-ARTIFACT-INTEGRITY: Bundled JPS artifacts failed their integrity check."; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}

	unclassified := failureError(&evaluation.Failure{Code: "JPS-EVALUATION-INTERNAL", Message: "The validated pack did not decode to a JSON object."})
	if !errors.Is(unclassified, ErrInternal) || errors.As(unclassified, &refusal) {
		t.Fatalf("an unclassified engine failure: %v (%T)", unclassified, unclassified)
	}
	profile := failureError(&evaluation.Failure{Code: "profile-unsupported", Profile: "extended", Phase: "preflight"})
	if !errors.As(profile, &refusal) || errors.Is(profile, ErrInternal) {
		t.Fatalf("the profile refusal: %v (%T)", profile, profile)
	}
}

// Evaluation.JSON is the envelope "jpack experimental evaluate --format json"
// writes for the same inputs outside a project, byte for byte, apart from the
// command member, which names this surface. The CLI runs in-process, from a
// temporary directory with no project in it and JPACK_CONFIG empty.
func TestJSONIsTheCLIEnvelope(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("JPACK_CONFIG", "")
	const routed = `{"specVersion":"0.3.0","id":"https://example.invalid/judgment-packs/sdk-routes","version":"1.0.0","title":"Routes","decision":{"intent":"Review","question":"Review?"},"outcomes":[{"id":"yes","label":"Yes"},{"id":"no","label":"No"}],"rules":[{"id":"one","description":"One <&>","when":{"op":"fact","path":"/score","operator":"equals","value":1},"outcome":"yes","onUnknown":"ignore"}],"escalation":{"triggers":["no-match"],"routes":[{"target":{"kind":"queue","name":"Review <&>"}},{"target":{"kind":"system","name":"Ledger"},"triggers":["no-match"]}]}}`
	rows := []struct {
		name, pack, facts, evidence string
	}{
		{"routed Core 0.3.0 pack", routed, `{"score":2}`, ""},
		{"routed outcome", routed, `{"score":1}`, `{}`},
		{"0.2.0-draft pack", reusePack, `{"flag":true}`, ""},
	}
	for index, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			packFile := filepath.Join(dir, "pack"+string(rune('a'+index))+".json")
			factsFile := filepath.Join(dir, "facts"+string(rune('a'+index))+".json")
			for name, data := range map[string]string{packFile: row.pack, factsFile: row.facts} {
				if err := os.WriteFile(name, []byte(data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			args := []string{"experimental", "evaluate", packFile, "--facts", factsFile, "--format", "json"}
			var evidence []byte
			if row.evidence != "" {
				evidenceFile := filepath.Join(dir, "evidence"+string(rune('a'+index))+".json")
				if err := os.WriteFile(evidenceFile, []byte(row.evidence), 0o600); err != nil {
					t.Fatal(err)
				}
				args = append(args, "--evidence", evidenceFile)
				evidence = []byte(row.evidence)
			}
			var stdout, stderr bytes.Buffer
			if code := cli.Run(args, strings.NewReader(""), &stdout, &stderr); code != 0 || stderr.Len() != 0 {
				t.Fatalf("CLI exit %d, stderr %q", code, stderr.String())
			}
			pack, err := Load(context.Background(), []byte(row.pack), LoadOptions{})
			if err != nil {
				t.Fatal(err)
			}
			evaluated, err := pack.Evaluate(context.Background(), []byte(row.facts), evidence)
			if err != nil {
				t.Fatal(err)
			}
			got, err := evaluated.JSON()
			if err != nil {
				t.Fatal(err)
			}
			const ours, theirs = `"command":"sdk evaluate"`, `"command":"experimental evaluate"`
			if strings.Count(string(got), ours) != 1 || strings.Count(stdout.String(), theirs) != 1 {
				t.Fatalf("command members: %s\n%s", got, stdout.String())
			}
			if want := strings.Replace(stdout.String(), theirs, ours, 1); string(got) != want {
				t.Fatalf("JSON() differs from the CLI\n got %s\nwant %s", got, want)
			}
		})
	}
}
