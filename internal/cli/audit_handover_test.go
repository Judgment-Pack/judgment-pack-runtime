package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// A deliverer polls audit checkpoint --since with the last sequence it handed
// over, gives each new checkpoint to a holder, and the holder's file is what
// audit verify --expect holds the trail to: every record handed over is
// witnessed, and the ones not yet handed over are unwitnessed. Nothing a
// decision does waits for the deliverer, and nothing is written beside the
// trail.
func TestADelivererHandsEachCheckpointToAHolder(t *testing.T) {
	configPath, trail := recordedProject(t, 2)
	holder := filepath.Join(t.TempDir(), "held.jsonl")
	deliver := func(since int64) int64 {
		t.Helper()
		code, stdout, stderr := runTest(t, []string{"audit", "checkpoint", "--config", configPath, "--since", formatInt(since)}, "")
		if code != 0 || stderr != "" {
			t.Fatalf("exit=%d stderr=%q", code, stderr)
		}
		file, err := os.OpenFile(holder, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if _, err := file.WriteString(stdout); err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(strings.TrimSuffix(stdout, "\n"), "\n") {
			if line == "" {
				continue
			}
			checkpoint, err := audit.ParseCheckpoint([]byte(line))
			if err != nil || string(audit.EncodeCheckpoint(checkpoint)) != line+"\n" {
				t.Fatalf("each line is one canonical checkpoint: %q %v", line, err)
			}
			since = checkpoint.Sequence
		}
		return since
	}
	since := deliver(0)
	if since != 2 {
		t.Fatalf("the first poll hands over both records: %d", since)
	}
	facts := writeDocument(t, "facts.json", hardFailFacts)
	if code, _, stderr := runTest(t, []string{"experimental", "evaluate", "--pack-id", "intake", "--config", configPath, "--facts", facts}, ""); code != 0 {
		t.Fatalf("a decision does not wait for a deliverer: exit=%d %q", code, stderr)
	}
	entries, err := os.ReadDir(filepath.Dir(trail))
	if err != nil || len(entries) != 1 || entries[0].Name() != audit.FileName {
		t.Fatalf("nothing is written beside the trail: %v %v", entries, err)
	}
	code, output := verification(t, "--config", configPath, "--expect", holder, "--require-checkpoint-through", "3")
	if code != result.ExitInvalid || output.Required.Status != "unmet" || output.Coverage.Witnessed != 2 || output.Coverage.Unwitnessed != 1 ||
		output.Findings[0].Name != audit.FindingCheckpointCoverageMissing || !endsWithAttempts(output.DoesNotEstablish) {
		t.Fatalf("the third record is unwitnessed until it is handed over: exit=%d %+v %+v %q", code, output.Coverage, output.Findings, output.DoesNotEstablish)
	}
	if code, human, _ := runTest(t, []string{"audit", "verify", "--config", configPath, "--expect", holder, "--require-checkpoint-through", "3"}, ""); code != result.ExitInvalid ||
		!humanEndsWithAttempts(human) {
		t.Fatalf("human output, an unmet requirement: exit=%d %q", code, human)
	}
	if since = deliver(since); since != 3 {
		t.Fatalf("the next poll hands over the new record: %d", since)
	}
	if again := deliver(since); again != since {
		t.Fatal("a poll with nothing new hands over nothing")
	}
	code, output = verification(t, "--config", configPath, "--expect", holder, "--require-checkpoint-through", "3")
	if code != 0 || output.Status != "valid" || output.Held.Supplied != 3 || output.Held.Status != "matched" || output.Coverage.Witnessed != 3 ||
		output.Coverage.Unwitnessed != 0 || output.Required.Status != "met" || output.Coverage.Stamped.Status != "not-checked" {
		t.Fatalf("exit=%d %+v %+v %+v", code, output.Held, output.Coverage, output.Required)
	}
	code, human, _ := runTest(t, []string{"audit", "verify", "--config", configPath, "--expect", holder}, "")
	if code != 0 || !strings.HasPrefix(human, "consistent, and witnessed through the held checkpoint at sequence 3") ||
		!strings.Contains(human, "records: 3 witnessed by a held checkpoint, 0 unwitnessed; stamped: not checked (no --tsa-roots)") {
		t.Fatalf("human output: %q", human)
	}
}

// Rotation writes a hand-over but promotes neither a caller's seed nor its
// trusted-key list. If the caller archives the old trail before promotion, it
// can recover custody with the retained next seed, verify the archive under
// the old list, and start the new trail under a list beginning at that key.
func TestACallerRecoversCustodyAfterMovingARotatedTrail(t *testing.T) {
	skipWhereKeysCannotSign(t)
	firstSeed, firstPublic := generatedKey(t)
	nextSeed, nextPublic := generatedKey(t)
	firstSeedBytes, nextSeedBytes := readFileBytes(t, firstSeed), readFileBytes(t, nextSeed)
	trusted := []string{firstPublic, nextPublic}

	t.Setenv(audit.SigningKeyEnv, firstSeed)
	configPath, trail := recordedProject(t, 1)
	_, held, stderr := runTest(t, []string{"audit", "checkpoint", "--config", configPath}, "")
	if stderr != "" {
		t.Fatalf("checkpoint: %q", stderr)
	}
	heldPath := writeDocument(t, "held.json", held)
	code, stdout, stderr := runTest(t, []string{"audit", "key", "rotate", "--format", "json", "--config", configPath, "--next", nextSeed}, "")
	var rotation result.AuditRotation
	if err := json.Unmarshal([]byte(stdout), &rotation); err != nil || code != 0 || stderr != "" || rotation.At != 1 || rotation.NextPublicKey+"\n" != readFile(t, nextPublic) {
		t.Fatalf("rotation: exit=%d stdout=%q stderr=%q rotation=%+v err=%v", code, stdout, stderr, rotation, err)
	}
	if !bytes.Equal(readFileBytes(t, firstSeed), firstSeedBytes) || !bytes.Equal(readFileBytes(t, nextSeed), nextSeedBytes) ||
		!slices.Equal(trusted, []string{firstPublic, nextPublic}) {
		t.Fatal("rotation promoted or changed the caller's retained seeds or trusted-key list")
	}

	auditDir := filepath.Dir(trail)
	archive := filepath.Join(filepath.Dir(auditDir), "audit-before-custody-promotion")
	if err := os.Rename(auditDir, archive); err != nil {
		t.Fatal(err)
	}
	archivedTrail := filepath.Join(archive, audit.FileName)
	archivedSidecar := filepath.Join(archive, audit.SidecarName)
	code, old := verification(t, "--trail", archivedTrail, "--signatures", archivedSidecar,
		"--expect", heldPath, "--public-key", trusted[0], "--public-key", trusted[1])
	if code != 0 || len(old.Findings) != 0 || old.Held.Status != "matched" || old.Signatures.Rotations != 1 || old.Signatures.KeyInForce != rotation.Next {
		t.Fatalf("archive: exit=%d findings=%+v held=%+v signatures=%+v", code, old.Findings, old.Held, old.Signatures)
	}

	// Promotion is the caller's act after the move: retain the next seed and
	// begin the new trail's trust list with its public half.
	t.Setenv(audit.SigningKeyEnv, nextSeed)
	trusted = []string{nextPublic}
	evaluateOnce(t, configPath)
	code, current := verification(t, "--config", configPath, "--public-key", trusted[0])
	if code != 0 || len(current.Findings) != 0 || current.Coverage.SignedRecords != 1 || current.Signatures.FirstKey != rotation.Next ||
		current.Head == nil || old.Head == nil || current.Head.Trail == old.Head.Trail {
		t.Fatalf("recovered custody: exit=%d findings=%+v coverage=%+v signatures=%+v heads=%+v/%+v", code, current.Findings, current.Coverage, current.Signatures, old.Head, current.Head)
	}
	code, stale := verification(t, "--config", configPath, "--public-key", firstPublic, "--public-key", nextPublic)
	firstKey, err := audit.ParsePublicKey(readFileBytes(t, firstPublic))
	if err != nil {
		t.Fatal(err)
	}
	want := result.AuditFinding{Name: audit.FindingSignatureInvalid, Line: 1,
		Detail: "sidecar line 1 does not verify under key " + audit.KeyID(firstKey) + ", the key in force at sequence 1"}
	if code != result.ExitInvalid || !slices.Equal(stale.Findings, []result.AuditFinding{want}) {
		t.Fatalf("the old list is not the new trail's list: exit=%d findings=%+v", code, stale.Findings)
	}
}

func formatInt(value int64) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

// --since pages by --limit and says when more follow; its JSON payload lists
// the checkpoints with whether more follow; --limit needs --since; and the
// held checkpoints may come from more than one file.
func TestAuditCheckpointSincePagesAndRefusesWhatItCannotList(t *testing.T) {
	configPath, _ := recordedProject(t, 3)
	code, stdout, stderr := runTest(t, []string{"audit", "checkpoint", "--config", configPath, "--since", "0", "--limit", "2"}, "")
	if code != 0 || strings.Count(stdout, "\n") != 2 || !strings.Contains(stderr, "ask again with --since 2") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	code, stdout, _ = runTest(t, []string{"audit", "checkpoint", "--config", configPath, "--since", "1", "--format", "json"}, "")
	var list result.AuditCheckpointList
	if err := json.Unmarshal([]byte(stdout), &list); err != nil || code != 0 || list.Status != "listed" || list.After != 1 || list.More ||
		len(list.Checkpoints) != 2 || list.Checkpoints[0].Sequence != 2 || list.Checkpoints[1].Sequence != 3 {
		t.Fatalf("exit=%d %+v %v", code, list, err)
	}
	code, stdout, _ = runTest(t, []string{"audit", "checkpoint", "--config", configPath, "--since", "3", "--format", "json"}, "")
	if err := json.Unmarshal([]byte(stdout), &list); err != nil || code != 0 || len(list.Checkpoints) != 0 || list.More || !strings.Contains(stdout, `"checkpoints":[]`) {
		t.Fatalf("nothing after the head: exit=%d %q", code, stdout)
	}
	first := filepath.Join(t.TempDir(), "first.jsonl")
	second := filepath.Join(t.TempDir(), "second.jsonl")
	for path, since := range map[string]string{first: "0", second: "2"} {
		_, out, _ := runTest(t, []string{"audit", "checkpoint", "--config", configPath, "--since", since, "--limit", "1"}, "")
		if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if code, output := verification(t, "--config", configPath, "--expect", first, "--expect", second); code != 0 || output.Held.Supplied != 2 || output.Coverage.Checkpointed.Through != 3 {
		t.Fatalf("two files: exit=%d %+v", code, output.Held)
	}
	for name, args := range map[string][]string{
		"--limit without --since": {"audit", "checkpoint", "--config", configPath, "--limit", "2"},
		"a negative --since":      {"audit", "checkpoint", "--config", configPath, "--since", "-1"},
		"a --limit of zero":       {"audit", "checkpoint", "--config", configPath, "--since", "0", "--limit", "0"},
	} {
		t.Run(name, func(t *testing.T) {
			code, stdout, _ := runTest(t, append(args, "--format", "json"), "")
			if code != result.ExitInvocation || !strings.Contains(stdout, `"JPS-INVOCATION-AUDIT-SINCE"`) {
				t.Fatalf("exit=%d %q", code, stdout)
			}
		})
	}
	code, stdout, _ = runTest(t, []string{"audit", "verify", "--config", configPath, "--require-checkpoint-through", "-1", "--format", "json"}, "")
	if code != result.ExitInvocation || !strings.Contains(stdout, `"JPS-INVOCATION-AUDIT-REQUIRE"`) {
		t.Fatalf("a negative requirement: exit=%d %q", code, stdout)
	}
	broken := filepath.Join(t.TempDir(), "broken.jsonl")
	if err := os.WriteFile(broken, append(bytes.Repeat([]byte("\n"), 2), []byte("{}\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ = runTest(t, []string{"audit", "verify", "--config", configPath, "--expect", broken, "--format", "json"}, "")
	if code != result.ExitInvocation || !strings.Contains(stdout, "line 3") {
		t.Fatalf("a held file with a line that is not a checkpoint: exit=%d %q", code, stdout)
	}
}

// A holder's file whose third line is a checkpoint of a later version is
// refused as unsupported, and the refusal names the line.
func TestAHeldCheckpointOfALaterVersionIsNamedByItsLine(t *testing.T) {
	configPath, _ := recordedProject(t, 2)
	_, listing, _ := runTest(t, []string{"audit", "checkpoint", "--config", configPath, "--since", "0"}, "")
	lines := strings.Split(strings.TrimSuffix(listing, "\n"), "\n")
	later := strings.Replace(lines[1], `"checkpointVersion":"1"`, `"checkpointVersion":"2"`, 1)
	held := filepath.Join(t.TempDir(), "held.jsonl")
	if err := os.WriteFile(held, []byte(lines[0]+"\n\n"+later+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := runTest(t, []string{"audit", "verify", "--config", configPath, "--expect", held, "--format", "json"}, "")
	if code != result.ExitUnsupported || !strings.Contains(stdout, `"JPS-AUDIT-CHECKPOINT-VERSION"`) || !strings.Contains(stdout, "line 3") {
		t.Fatalf("exit=%d %q", code, stdout)
	}
	code, human, _ := runTest(t, []string{"audit", "verify", "--config", configPath, "--expect", held}, "")
	if code != result.ExitUnsupported || !strings.HasPrefix(human, "unsupported: ") || !strings.Contains(human, "line 3") {
		t.Fatalf("exit=%d %q", code, human)
	}
}
