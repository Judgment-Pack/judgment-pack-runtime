package mcp

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
	"unicode/utf8"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

func auditMCPProject(t *testing.T) string {
	t.Helper()
	dir := bindingProject(t)
	if err := os.Mkdir(filepath.Join(dir, "audit"), 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func auditRPC(t *testing.T, server *Server, name, args string) (map[string]any, int) {
	t.Helper()
	var output, log bytes.Buffer
	if err := server.Serve(strings.NewReader(rawToolCall(t, 1, name, args)), &output, &log); err != nil {
		t.Fatal(err)
	}
	if _, unpaired := carrier.UnpairedSurrogateEscape(output.Bytes()); unpaired || !utf8.Valid(output.Bytes()) {
		t.Fatal("audit response emitted malformed Unicode")
	}
	var reply struct {
		Result map[string]any `json:"result"`
		Error  *rpcError      `json:"error"`
	}
	if err := json.Unmarshal(output.Bytes(), &reply); err != nil || reply.Error != nil {
		t.Fatalf("RPC failed: %v %s", err, output.String())
	}
	return reply.Result, output.Len()
}

func auditPageRPC(t *testing.T, server *Server, args string) result.AuditPage {
	t.Helper()
	answer, size := auditRPC(t, server, auditListTool, args)
	if answer["isError"] != false {
		t.Fatal(answer)
	}
	if size > maxAuditResponseBytes+128 {
		t.Fatal("serialized response limit exceeded")
	}
	data, err := json.Marshal(answer["structuredContent"])
	if err != nil {
		t.Fatal(err)
	}
	var page result.AuditPage
	if err := json.Unmarshal(data, &page); err != nil {
		t.Fatal(err)
	}
	if page.OutputVersion != "2" || page.AuditWireVersion != "1" || page.Tool.Name != "jpack" || page.Tool.Version == "" || page.Command != "mcp experimental_list_audit" {
		t.Fatal("audit wire envelope changed")
	}
	return page
}

type historyLine struct{ Raw, Digest string }

// This small Runtime-owned compatibility reader models Desk's retained history
// selection: exact UTF-8 version-1 lines, pack/graph identity, newest first,
// digest excluding only LF. Expected rows below are independently authored.
func fileHistory(data []byte, kind, id string) []historyLine {
	rows := []historyLine{}
	lines := bytes.Split(data, []byte{'\n'})
	for i := len(lines) - 2; i >= 0; i-- {
		line := lines[i]
		var members map[string]any
		if !utf8.Valid(line) || json.Unmarshal(line, &members) != nil || members["recordVersion"] != "1" {
			continue
		}
		identity, ok := members[kind].(map[string]any)
		if !ok || identity["id"] != id {
			continue
		}
		digest := sha256.Sum256(line)
		rows = append(rows, historyLine{string(line), "sha256:" + hex.EncodeToString(digest[:])})
	}
	return rows
}

func structuredHistory(t *testing.T, records []result.AuditPageRecord, kind, id string) []historyLine {
	t.Helper()
	rows := []historyLine{}
	for i := len(records) - 1; i >= 0; i-- {
		record := records[i]
		if record.Status != "parsed" {
			continue
		}
		var members map[string]any
		if json.Unmarshal(record.Members, &members) != nil {
			t.Fatal("unreadable structured object")
		}
		identity, ok := members[kind].(map[string]any)
		if !ok || identity["id"] != id || members["recordVersion"] != "1" {
			continue
		}
		// No JSONL parsing here: history selects structured members, retaining
		// separately carried exact bytes and the Runtime's exact line digest.
		raw, err := base64.StdEncoding.DecodeString(record.RawLineBase64)
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, historyLine{string(raw), record.Digest})
	}
	return rows
}

func TestAuditWireContractRowsDownloadAndHistoryFromToolsAlone(t *testing.T) {
	fixture, err := os.ReadFile("testdata/audit-wire-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases struct {
		Version string `json:"auditWireVersion"`
		Cases   []struct {
			Name, Evaluations, EvaluationsHex string
			Statuses                          []string
		}
	}
	if err := json.Unmarshal(fixture, &cases); err != nil || cases.Version != audit.WireVersion {
		t.Fatal(err)
	}
	for _, row := range cases.Cases {
		t.Run(row.Name, func(t *testing.T) {
			dir := auditMCPProject(t)
			data := []byte(row.Evaluations)
			if row.EvaluationsHex != "" {
				var err error
				data, err = hex.DecodeString(row.EvaluationsHex)
				if err != nil {
					t.Fatal(err)
				}
			}
			contents := map[string][]byte{
				audit.FileName:    data,
				audit.SidecarName: []byte(" {\"kind\":\"signature\",\"all\":[1,2]} \r\n"),
				audit.StampsName:  []byte("{\"kind\":\"stamp\",\"unknown\":true}\npartial"),
			}
			for name, bytes := range contents {
				if err := os.WriteFile(filepath.Join(dir, "audit", name), bytes, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			server := NewServer(nil, nil)
			files := map[string][]byte{}
			records := []result.AuditPageRecord{}
			args := `{"page_bytes":7}`
			for range 1000 {
				page := auditPageRPC(t, server, args)
				raw, err := base64.StdEncoding.DecodeString(page.DataBase64)
				if err != nil || page.Offset != int64(len(files[page.File])) {
					t.Fatal("noncontiguous wire export", err)
				}
				files[page.File] = append(files[page.File], raw...)
				if page.File == audit.FileName {
					records = append(records, page.Records...)
				}
				for _, file := range page.Files {
					if file.Name != audit.FileName && file.Name != audit.SidecarName && file.Name != audit.StampsName {
						t.Fatal("unexpected file")
					}
					digest := sha256.Sum256(contents[file.Name])
					if !file.Present || file.Bytes != int64(len(contents[file.Name])) || file.Digest != "sha256:"+hex.EncodeToString(digest[:]) {
						t.Fatal("captured file metadata differs from exact bytes")
					}
				}
				if page.Complete {
					break
				}
				encoded, _ := json.Marshal(map[string]any{"page_bytes": 7, "cursor": page.NextCursor})
				args = string(encoded)
			}
			for name, expected := range contents {
				if !bytes.Equal(files[name], expected) {
					t.Fatalf("%s download differs", name)
				}
			}
			statuses := []string{}
			for _, record := range records {
				statuses = append(statuses, record.Status)
			}
			if !reflect.DeepEqual(statuses, row.Statuses) {
				t.Fatalf("statuses %v != %v", statuses, row.Statuses)
			}
			for _, kind := range []string{"pack", "graph"} {
				got := structuredHistory(t, records, kind, "same")
				want := fileHistory(data, kind, "same")
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("%s history differs: %#v %#v", kind, got, want)
				}
			}
		})
	}
}

func TestAuditToolsStrictStandaloneAndTransportArguments(t *testing.T) {
	dir := auditMCPProject(t)
	if err := os.WriteFile(filepath.Join(dir, "audit", audit.FileName), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{auditListTool, auditVerifyTool} {
		for _, args := range []string{`null`, `[]`, `1`, `"secret"`, `{"private-signing-key-path":"secret"}`, `{"cursor":null}`, `{"expect":null}`, `{"page_bytes":null}`, `{"page_bytes":1.0}`, `{"page_bytes":1e2}`, `{"page_bytes":0}`, `{"page_bytes":262145}`, `{"page_bytes":true}`, `{"require_checkpoint_through":-1}`, `{"require_checkpoint_through":9007199254740991}`, `{"expect":""}`, `{"expect":"secret"}`, `{"trail_base64":"secret!"}`, `{"trail_base64":null}`, `{"trail_base64":"e30=\n"}`, `{"cursor":""}`, `{"cursor":"secret","cursor":"secret"}`, `{"CURSOR":"secret"}`} {
			t.Run(tool+args, func(t *testing.T) {
				server := NewServer(nil, nil)
				answer, size := auditRPC(t, server, tool, args)
				if answer["isError"] != true || size > 1024 || strings.Contains(toolText(t, answer), "secret") || strings.Contains(toolText(t, answer), "private-signing") {
					t.Fatal("unbounded or disclosing refusal", answer)
				}
				var direct any
				if tool == auditListTool {
					direct = server.toolListAudit(json.RawMessage(args))
				} else {
					direct = server.toolVerifyAudit(json.RawMessage(args))
				}
				if direct.(map[string]any)["isError"] != true {
					t.Fatal("standalone accepted invalid arguments", args)
				}
			})
		}
	}
}

func TestAuditToolsProjectAuthorityAndFileGuards(t *testing.T) {
	for _, shape := range []string{"absent", "broken", "directory-config", "no-audit", "outside-dir", "outside-link", "file-link", "directory-file", "missing-trail"} {
		t.Run(shape, func(t *testing.T) {
			dir := auditMCPProject(t)
			config := filepath.Join(dir, project.DefaultConfigName)
			trail := filepath.Join(dir, "audit", audit.FileName)
			external := filepath.Join(t.TempDir(), "private")
			if err := os.WriteFile(external, []byte("private-secret-bytes"), 0o600); err != nil {
				t.Fatal(err)
			}
			switch shape {
			case "absent":
				t.Setenv(project.ConfigEnv, filepath.Join(dir, "absent.json"))
			case "broken":
				if err := os.WriteFile(config, []byte("{broken-private-secret"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "directory-config":
				t.Setenv(project.ConfigEnv, dir)
			case "no-audit":
				if err := os.WriteFile(config, []byte(`{"configVersion":"3","packs":{}}`), 0o600); err != nil {
					t.Fatal(err)
				}
			case "outside-dir":
				if err := os.WriteFile(config, []byte(`{"configVersion":"3","audit":{"dir":"../private"},"packs":{}}`), 0o600); err != nil {
					t.Fatal(err)
				}
			case "outside-link":
				if err := os.Remove(filepath.Join(dir, "audit")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Dir(external), filepath.Join(dir, "audit")); err != nil {
					t.Skip(err)
				}
			case "file-link":
				if err := os.Symlink(external, trail); err != nil {
					t.Skip(err)
				}
			case "directory-file":
				if err := os.Mkdir(trail, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			server := NewServer(nil, nil)
			for _, tool := range []string{auditListTool, auditVerifyTool} {
				answer, size := auditRPC(t, server, tool, `{}`)
				wantError := shape != "missing-trail" || tool == auditVerifyTool
				if answer["isError"] != wantError || size > 4096 {
					t.Fatalf("%s %s: %v", shape, tool, answer)
				}
				encoded, _ := json.Marshal(answer)
				if strings.Contains(string(encoded), "private-secret") || strings.Contains(string(encoded), external) {
					t.Fatal("project refusal disclosed private material")
				}
			}
		})
	}
}

func TestAuditWireSerializedResponseBudgetIncludesBothRepresentations(t *testing.T) {
	dir := auditMCPProject(t)
	// JSON HTML escaping amplifies members independently of the two base64
	// fields, and the text representation itself is escaped a second time.
	line := []byte(`{"v":"` + strings.Repeat("<", audit.MaxWireRecord-9) + `"}` + "\n")
	if err := os.WriteFile(filepath.Join(dir, "audit", audit.FileName), bytes.Repeat(line, 4), 0o600); err != nil {
		t.Fatal(err)
	}
	answer, size := auditRPC(t, NewServer(nil, nil), auditListTool, `{"page_bytes":262144}`)
	if size > maxAuditResponseBytes+128 {
		t.Fatal("serialized budget failed")
	}
	if answer["isError"] != true || !strings.Contains(toolText(t, answer), "4 MiB") {
		t.Fatal("amplified response should refuse", size, answer["isError"])
	}
	if boundedAuditResult(map[string]any{"large": strings.Repeat("x", maxAuditResponseBytes)}).(map[string]any)["isError"] != true {
		t.Fatal("response refusal not enforced")
	}
}

func TestAuditWireToolDefinitionsAndSuppliedEmptyTrail(t *testing.T) {
	dir := auditMCPProject(t)
	if err := os.WriteFile(filepath.Join(dir, "audit", audit.FileName), []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	tools := map[string]bool{}
	for _, definition := range toolDefinitions() {
		tools[definition["name"].(string)] = true
		name := definition["name"].(string)
		if name != auditListTool && name != auditVerifyTool {
			continue
		}
		schema := definition["inputSchema"].(map[string]any)
		if schema["type"] != "object" || schema["additionalProperties"] != false {
			t.Fatal("audit argument envelope weakened")
		}
		properties := schema["properties"].(map[string]any)
		if name == auditListTool {
			if len(properties) != 2 || properties["cursor"].(map[string]any)["maxLength"] != 85 || properties["page_bytes"].(map[string]any)["maximum"] != 262144 {
				t.Fatal("listing schema differs from wire contract")
			}
		} else {
			if len(properties) != 3 || properties["expect"].(map[string]any)["maxLength"] != 1048576 || properties["trail_base64"].(map[string]any)["maxLength"] != 11184812 || properties["require_checkpoint_through"].(map[string]any)["maximum"] != int64(9007199254740990) {
				t.Fatal("verification schema differs from wire contract")
			}
		}
	}
	if !tools[auditListTool] || !tools[auditVerifyTool] {
		t.Fatal("audit tools not discoverable")
	}
	for _, args := range []string{`{}`, `{"trail_base64":""}`} {
		answer, _ := auditRPC(t, NewServer(nil, nil), auditVerifyTool, args)
		if answer["isError"] != false || answer["structuredContent"].(map[string]any)["status"] != "valid" {
			t.Fatal(answer)
		}
	}
}

func TestAuditWireIgnoresPrivateKeyAndTimestampSettings(t *testing.T) {
	dir := auditMCPProject(t)
	config := filepath.Join(dir, project.DefaultConfigName)
	configuration, err := json.Marshal(map[string]any{
		"configVersion": "6", "packs": map[string]any{},
		"audit": map[string]any{"dir": "audit", "signingKey": filepath.Join(t.TempDir(), "private-key-must-not-be-read"), "timestampAuthority": "https://authority.invalid/must-not-be-fetched"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, configuration, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JPACK_SIGNING_KEY", "secret-private-key-setting")
	if err := os.WriteFile(filepath.Join(dir, "audit", audit.FileName), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := NewServer(nil, nil)
	for _, tool := range []string{auditListTool, auditVerifyTool} {
		answer, _ := auditRPC(t, server, tool, `{}`)
		encoded, _ := json.Marshal(answer)
		if answer["isError"] != false || strings.Contains(string(encoded), "private-key") || strings.Contains(string(encoded), "authority.invalid") {
			t.Fatal("audit reader consulted or disclosed private configuration", answer)
		}
	}
}

func TestAuditWireRejectsFinalSymlinksEvenInsideProject(t *testing.T) {
	for _, name := range []string{audit.FileName, audit.SidecarName, audit.StampsName} {
		t.Run(name, func(t *testing.T) {
			dir := auditMCPProject(t)
			if name != audit.FileName {
				if err := os.WriteFile(filepath.Join(dir, "audit", audit.FileName), []byte("{}\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(dir, "retained-private"), []byte("private-secret-bytes"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(dir, "retained-private"), filepath.Join(dir, "audit", name)); err != nil {
				t.Skip(err)
			}
			answer, _ := auditRPC(t, NewServer(nil, nil), auditListTool, `{}`)
			if answer["isError"] != true || strings.Contains(toolText(t, answer), "private-secret") {
				t.Fatal(answer)
			}
		})
	}
}

func TestAuditWireDecodedInputBoundsRefuseWhole(t *testing.T) {
	dir := auditMCPProject(t)
	trail := filepath.Join(dir, "audit", audit.FileName)
	if err := os.WriteFile(trail, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(trail, audit.MaxWireFileBytes+1); err != nil {
		t.Fatal(err)
	}
	server := NewServer(nil, nil)
	for _, tool := range []string{auditListTool, auditVerifyTool} {
		answer, size := auditRPC(t, server, tool, `{}`)
		if answer["isError"] != true || size > 1024 {
			t.Fatal("configured file limit not enforced", answer)
		}
	}
	for _, args := range []map[string]any{
		{"expect": strings.Repeat(" ", maxAuditExpectBytes+1)},
		{"trail_base64": base64.StdEncoding.EncodeToString(make([]byte, maxAuditSuppliedBytes+1))},
	} {
		encoded, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		answer, size := auditRPC(t, server, auditVerifyTool, string(encoded))
		if answer["isError"] != true || size > 1024 {
			t.Fatal("supplied data limit not enforced", answer)
		}
	}
}
