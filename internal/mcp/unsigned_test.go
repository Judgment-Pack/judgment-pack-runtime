package mcp

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// signingServerProject lays out the comparable-facts project under
// configVersion "8", keeping a trail, with members added to its audit member,
// points JPACK_CONFIG at it, and returns the pack's bytes, the configuration's
// path and the trail's.
func signingServerProject(t *testing.T, members string) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	pack, err := os.ReadFile(filepath.Join("..", "evaluation", "testdata", "comparable-facts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "packs"), 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, project.DefaultConfigName)
	for path, body := range map[string][]byte{
		filepath.Join(root, "packs", "detector.json"): pack,
		configPath: []byte(`{"configVersion":"8","audit":{"dir":"audit"` + members + `},"packs":{"detector":{"path":"packs/detector.json"}}}`),
	} {
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(project.ConfigEnv, configPath)
	return string(pack), configPath, filepath.Join(root, "audit", audit.FileName)
}

// refusedSeed is a new signing key in a directory its owner's alone, readable
// by every user, so it is refused for where it is. The test is skipped where
// no key signs, as there every key is refused.
func refusedSeed(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(audit.StandInKeyAncestorsForTests(dir))
	seed := make([]byte, 32)
	if _, err := rand.Read(seed); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(dir, "seed")
	if _, err := audit.WriteSeed(keyPath, []byte(hex.EncodeToString(seed)+"\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := audit.LoadSigner(keyPath, nil, nil); errors.Is(err, audit.ErrKeyPrivacyUnchecked) {
		t.Skip("no key signs on this platform: its privacy cannot be checked")
	} else if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(keyPath, 0o644); err != nil {
		t.Fatal(err)
	}
	return keyPath
}

func trailLines(t *testing.T, trail string) int {
	t.Helper()
	data, err := os.ReadFile(trail)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "\n")
}

// experimental_evaluate tells its caller why a recorded call's record is
// unsigned, as the CLI does (#354): the payload is the one the same call
// returns with no key named, with a diagnostics member carrying one
// JPS-AUDIT-UNSIGNED warning, exactly packs validate's reason, in the
// structured content and the text alike; the record is written. A rehearsal
// says nothing. Under requireSigning the call is refused instead, in the
// operational envelope with the refusal's exact message, and nothing is
// recorded; a rehearsal is not refused.
func TestExperimentalEvaluateSaysWhyItsRecordIsUnsigned(t *testing.T) {
	t.Setenv(audit.SigningKeyEnv, "")
	t.Setenv(audit.SigningPublicKeyEnv, "")
	keyPath := refusedSeed(t)
	call := func(t *testing.T, pack string, rehearsal bool) map[string]any {
		t.Helper()
		arguments := map[string]any{"pack": pack, "facts": comparableGood}
		if rehearsal {
			arguments["rehearsal"] = true
		}
		return runServer(t, toolCall(t, 1, "experimental_evaluate", arguments))[0]["result"].(map[string]any)
	}

	pack, _, plainTrail := signingServerProject(t, "")
	plain := call(t, pack, false)
	if plain["isError"] != false || trailLines(t, plainTrail) != 1 {
		t.Fatalf("no key named: %#v", plain)
	}
	baseline := toolText(t, plain)

	pack, _, trail := signingServerProject(t, `,"signingKey":`+jsonText(keyPath))
	want := "The signing key the audit member's signingKey names, " + keyPath + ", is refused, so records are written unsigned: the signing key can be read or written by its group or by other users."
	warned := call(t, pack, false)
	if warned["isError"] != false {
		t.Fatalf("the call is not refused: %#v", warned)
	}
	if text := toolText(t, warned); text != strings.TrimSuffix(baseline, "}")+`,"diagnostics":[{"code":"JPS-AUDIT-UNSIGNED","codeStability":"provisional","layer":"audit","severity":"warning","instancePath":"","message":`+jsonText(want)+`}]}` {
		t.Fatalf("text:\n got %s\nbase %s", text, baseline)
	}
	var structured struct {
		Diagnostics []result.Diagnostic `json:"diagnostics"`
	}
	decodeStructured(t, warned, &structured)
	if len(structured.Diagnostics) != 1 || structured.Diagnostics[0] != result.WarningDiagnostic(project.UnsignedCode, "audit", "", want) {
		t.Fatalf("structured diagnostics: %#v", structured.Diagnostics)
	}
	if trailLines(t, trail) != 1 {
		t.Fatal("the call is recorded")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(trail), audit.SidecarName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("and nothing is signed: %v", err)
	}
	if rehearsed := call(t, pack, true); rehearsed["isError"] != false || strings.Contains(toolText(t, rehearsed), project.UnsignedCode) || trailLines(t, trail) != 1 {
		t.Fatalf("a rehearsal says nothing and records nothing: %#v", rehearsed)
	}

	for name, members := range map[string]string{
		"the key is refused": `,"signingKey":` + jsonText(keyPath) + `,"requireSigning":true`,
		"no key is named":    `,"requireSigning":true`,
	} {
		t.Run(name, func(t *testing.T) {
			pack, configPath, trail := signingServerProject(t, members)
			message := "This evaluation was refused because " + configPath + " requires every decision's record to be signed, and "
			if strings.Contains(members, "signingKey") {
				message += "the signing key the audit member's signingKey names, " + keyPath + ", is refused: the signing key can be read or written by its group or by other users. Make the named key one that signs, or declare the run a rehearsal."
			} else {
				message += "no signing key is named. Name one by the audit member's signingKey or by JPACK_SIGNING_KEY, or declare the run a rehearsal."
			}
			refused := call(t, pack, false)
			if refused["isError"] != true || toolText(t, refused) != message {
				t.Fatalf("refusal: %#v\nwant %q", refused, message)
			}
			// The structured content arrives decoded, so it is compared as
			// the value it is.
			expected, err := json.Marshal(result.NewOperationalResult(evaluateCommand, "error", project.SigningRequiredCode, message))
			if err != nil {
				t.Fatal(err)
			}
			var envelope any
			if err := json.Unmarshal(expected, &envelope); err != nil {
				t.Fatal(err)
			}
			if !jsonEqual(t, refused["structuredContent"], envelope) {
				t.Fatalf("envelope:\n got %#v\nwant %s", refused["structuredContent"], expected)
			}
			if trailLines(t, trail) != 0 {
				t.Fatal("nothing is recorded")
			}
			if rehearsed := call(t, pack, true); rehearsed["isError"] != false || trailLines(t, trail) != 0 {
				t.Fatalf("a rehearsal is not refused: %#v", rehearsed)
			}
		})
	}
}
