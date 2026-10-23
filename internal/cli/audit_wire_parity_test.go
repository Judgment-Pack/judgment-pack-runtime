package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/mcp"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

func wireVerification(t *testing.T, args map[string]any) result.AuditVerification {
	t.Helper()
	request, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "experimental_verify_audit", "arguments": args},
	})
	if err != nil {
		t.Fatal(err)
	}
	var out, log bytes.Buffer
	if err := mcp.NewServer(nil, nil).Serve(bytes.NewReader(append(request, '\n')), &out, &log); err != nil {
		t.Fatal(err)
	}
	var reply struct {
		Result struct {
			IsError bool                     `json:"isError"`
			Report  result.AuditVerification `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out.Bytes(), &reply); err != nil || reply.Result.IsError {
		t.Fatalf("wire verification failed: %v %s", err, out.String())
	}
	return reply.Result.Report
}

func TestAuditWireFullVerifyReportMatchesCLIExpectAndTrail(t *testing.T) {
	// Hand-authored retained bytes; expected checkpoints use an independent
	// digest of these literal bytes, never output recorded from the verifier.
	line := ` {"recordVersion":"1","kind":"evaluation","trail":"0123456789abcdef0123456789abcdef","sequence":1,"previous":"sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855","pack":{"id":"one"},"at":"2026-10-01T00:00:00Z"} `
	digest := sha256.Sum256([]byte(line))
	checkpoint := `{"checkpointVersion":"1","recordDigest":"sha256:` + hex.EncodeToString(digest[:]) + `","sequence":1,"trail":"0123456789abcdef0123456789abcdef"}`
	for _, row := range []struct {
		name, trail, expect string
		through             int64
		status              string
	}{
		{"valid", line + "\n", "", 0, "valid"},
		{"empty", "", "", 0, "valid"},
		{"held-matched", line + "\n", checkpoint + "\n", 1, "valid"},
		{"held-multiple", line + "\n", " \n" + checkpoint + "\r\n" + checkpoint, 1, "valid"},
		{"held-wrong-trail", line + "\n", strings.Replace(checkpoint, "0123456789abcdef0123456789abcdef", "ffffffffffffffffffffffffffffffff", 1), 1, "invalid"},
		{"held-wrong-digest", line + "\n", strings.Replace(checkpoint, hex.EncodeToString(digest[:]), strings.Repeat("0", 64), 1), 1, "invalid"},
		{"unmet-through", line + "\n", checkpoint, 2, "invalid"},
		{"incomplete", line + "\n\xfftorn", "", 0, "invalid"},
		{"binding-malformed", `{"binding":{"kind":"secret"}}` + "\n", "", 0, "invalid"},
	} {
		t.Run(row.name, func(t *testing.T) {
			config := auditProject(t)
			t.Setenv(project.ConfigEnv, config)
			dir := filepath.Join(filepath.Dir(config), "audit")
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			trail := filepath.Join(dir, audit.FileName)
			if err := os.WriteFile(trail, []byte(row.trail), 0o600); err != nil {
				t.Fatal(err)
			}
			// Sidecars are retained but intentionally untrusted without caller
			// public keys/roots: the same default CLI report must say so.
			for _, name := range []string{audit.SidecarName, audit.StampsName} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte("unread-but-not-trusted\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			for _, supplied := range []bool{false, true} {
				args := map[string]any{}
				cliArgs := []string{"--config", config}
				if supplied {
					cliArgs = []string{"--trail", trail}
					args["trail_base64"] = base64.StdEncoding.EncodeToString([]byte(row.trail))
				}
				if row.expect != "" {
					args["expect"] = row.expect
					cliArgs = append(cliArgs, "--expect", writeDocument(t, "held.jsonl", row.expect))
				}
				if row.through > 0 {
					args["require_checkpoint_through"] = row.through
					if row.through == 1 {
						cliArgs = append(cliArgs, "--require-checkpoint-through", "1")
					} else {
						cliArgs = append(cliArgs, "--require-checkpoint-through", "2")
					}
				}
				code, cli := verification(t, cliArgs...)
				wire := wireVerification(t, args)
				if cli.Status != row.status || wire.Status != row.status || (code == 0) != (row.status != "invalid") {
					t.Fatalf("status differs: CLI:%d %s wire:%s", code, cli.Status, wire.Status)
				}
				if supplied {
					if wire.TrailPath != "(supplied trail)" || wire.SnapshotBetweenWrites {
						t.Fatal("supplied trail label/snapshot claim differs")
					}
					cli.SnapshotBetweenWrites = false
				} else if wire.TrailPath != "audit/"+audit.FileName {
					t.Fatal("wire exposed a host pathname", wire.TrailPath)
				}
				cli.Command, cli.TrailPath = wire.Command, wire.TrailPath
				if !reflect.DeepEqual(cli, wire) {
					want, _ := json.Marshal(cli)
					got, _ := json.Marshal(wire)
					t.Fatalf("full report parity failed supplied:%v\nCLI %s\nMCP %s", supplied, want, got)
				}
			}
		})
	}
}
