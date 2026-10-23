package mcp

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

func historyRPCPage(t *testing.T, s *Server, args any) result.AuditHistoryPage {
	t.Helper()
	input, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	data := auditRawRPC(t, s, auditHistoryTool, string(input))
	if len(data) > maxAuditResponseBytes+128 {
		t.Fatal("history response exceeded result ceiling")
	}
	var reply struct {
		Result struct {
			IsError bool                    `json:"isError"`
			Page    result.AuditHistoryPage `json:"structuredContent"`
		} `json:"result"`
		Error *rpcError `json:"error"`
	}
	if err := json.Unmarshal(data, &reply); err != nil || reply.Error != nil || reply.Result.IsError {
		t.Fatalf("history call failed: %v %s", err, data)
	}
	page := reply.Result.Page
	if page.OutputVersion != "2" || page.AuditHistoryVersion != "1" || page.Command != "mcp experimental_list_audit_history" || page.Tool.Name != "jpack" || page.Tool.Version == "" {
		t.Fatal("history envelope changed")
	}
	return page
}

func historyRPCDrain(t *testing.T, s *Server, args map[string]any) ([]result.AuditHistoryPage, []byte) {
	t.Helper()
	data := []byte{}
	pages := []result.AuditHistoryPage{}
	cursor := ""
	base := int64(-1)
	for range 20000 {
		page := historyRPCPage(t, s, args)
		if len(pages) > 0 {
			first := pages[0]
			if page.SourceBytes != first.SourceBytes || page.SnapshotBetweenWrites != first.SnapshotBetweenWrites || page.ProjectionBytes != first.ProjectionBytes || page.ProjectionDigest != first.ProjectionDigest {
				t.Fatal("history snapshot metadata changed across chunks")
			}
		}
		if base < 0 {
			base = page.Offset
		}
		if cursor != "" {
			retry := historyRPCPage(t, s, map[string]any{"cursor": cursor, "page_bytes": 1})
			if !reflect.DeepEqual(page, retry) {
				t.Fatal("history latest/final retry differs")
			}
		}
		part, err := base64.StdEncoding.DecodeString(page.DataBase64)
		if err != nil || page.Offset != base+int64(len(data)) {
			t.Fatal("history chunk is not contiguous")
		}
		data = append(data, part...)
		pages = append(pages, page)
		if page.Complete {
			digest := sha256.Sum256(data)
			if page.NextCursor != "" || page.ProjectionBytes != base+int64(len(data)) || base == 0 && page.ProjectionDigest != "sha256:"+hex.EncodeToString(digest[:]) {
				t.Fatal("history document identity differs")
			}
			return pages, data
		}
		cursor = page.NextCursor
		args = map[string]any{"cursor": cursor, "page_bytes": audit.MaxWirePage}
	}
	t.Fatal("history chunk transport did not terminate")
	return nil, nil
}

func TestAuditHistoryToolsOnlyHandAuthoredRows(t *testing.T) {
	data, err := os.ReadFile("testdata/audit-history-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		HistoryVersion string
		Cases          []struct {
			Name, Data, DataHex, Kind, ID string
			Raw                           []string
			Unread                        int
		}
	}
	if json.Unmarshal(data, &fixture) != nil || fixture.HistoryVersion != "1" {
		t.Fatal("invalid history fixture")
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			dir := auditMCPProject(t)
			retained := []byte(row.Data)
			if row.DataHex != "" {
				retained, err = hex.DecodeString(row.DataHex)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(dir, "audit", audit.FileName), retained, 0o600); err != nil {
				t.Fatal(err)
			}
			_, encoded := historyRPCDrain(t, NewServer(nil, nil), map[string]any{"kind": row.Kind, "id": row.ID, "page_bytes": 7})
			var got result.AuditDefinitionHistory
			if json.Unmarshal(encoded, &got) != nil {
				t.Fatal("projection is not one JSON history result")
			}
			want := result.AuditDefinitionHistory{State: "records", Unread: row.Unread, Records: []result.AuditDefinitionRecord{}}
			for _, raw := range row.Raw {
				digest := sha256.Sum256([]byte(raw))
				want.Records = append(want.Records, result.AuditDefinitionRecord{Raw: raw, Digest: "sha256:" + hex.EncodeToString(digest[:])})
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("history differs: %#v want %#v", got, want)
			}
		})
	}
}

func TestAuditHistoryLargeProjectionAppendAndBefore(t *testing.T) {
	dir := auditMCPProject(t)
	prefix, suffix := `{"recordVersion":"1","pack":{"id":"same"},"v":"`, `"}`
	n := (4 << 20) - 1 - len(prefix) - len(suffix)
	raw := prefix + strings.Repeat(`\\`, n/2) + strings.Repeat("x", n%2) + suffix
	trail := filepath.Join(dir, "audit", audit.FileName)
	if err := os.WriteFile(trail, []byte(raw+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewServer(nil, nil)
	first := historyRPCPage(t, s, map[string]any{"kind": "pack", "id": "same", "page_bytes": 17})
	file, err := os.OpenFile(trail, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.WriteString(`{"recordVersion":"1","pack":{"id":"same"},"later":true}` + "\n")
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	_, rest := historyRPCDrain(t, s, map[string]any{"cursor": first.NextCursor, "page_bytes": audit.MaxWirePage})
	part, _ := base64.StdEncoding.DecodeString(first.DataBase64)
	encoded := append(part, rest...)
	digest := sha256.Sum256(encoded)
	if first.ProjectionDigest != "sha256:"+hex.EncodeToString(digest[:]) || first.ProjectionBytes != int64(len(encoded)) {
		t.Fatal("large projection identity changed")
	}
	var got result.AuditDefinitionHistory
	if json.Unmarshal(encoded, &got) != nil || len(got.Records) != 1 || got.Records[0].Raw != raw || first.SourceBytes != 4<<20 || int64(len(encoded)) > audit.MaxHistoryProjectionBytes {
		t.Fatal("large immutable projection differs")
	}
	// A fresh before=original-end query matches the same exact prior window.
	_, fresh := historyRPCDrain(t, NewServer(nil, nil), map[string]any{"kind": "pack", "id": "same", "before": 4 << 20, "page_bytes": audit.MaxWirePage})
	if !bytes.Equal(encoded, fresh) {
		t.Fatal("before-offset projection changed")
	}
}

func TestAuditHistoryStrictArgumentsAndAuthority(t *testing.T) {
	dir := auditMCPProject(t)
	if err := os.WriteFile(filepath.Join(dir, "audit", audit.FileName), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range []string{`{}`, `null`, `[]`, `{"kind":"pack"}`, `{"kind":"other","id":"secret"}`, `{"kind":"pack","id":null}`, `{"kind":"pack","id":""}`, `{"kind":"pack","id":"same","before":null}`, `{"kind":"pack","id":"same","before":-1}`, `{"kind":"pack","id":"same","before":1e0}`, `{"kind":"pack","id":"same","page_bytes":0}`, `{"cursor":"secret","kind":"pack","id":"same"}`, `{"cursor":"secret","before":0}`, `{"cursor":null}`, `{"path":"secret"}`, `{"kind":"pack","kind":"pack","id":"same"}`} {
		s := NewServer(nil, nil)
		answer, size := auditRPC(t, s, auditHistoryTool, args)
		if answer["isError"] != true || size > 1024 || strings.Contains(toolText(t, answer), "secret") {
			t.Fatal("history refusal differs", answer)
		}
		if s.toolAuditHistory(json.RawMessage(args)).(map[string]any)["isError"] != true {
			t.Fatal("standalone history arguments were not strict")
		}
	}
	if NewServer(nil, nil).toolAuditHistory(json.RawMessage(`{"kind":"pack","id":"\ud800"}`)).(map[string]any)["isError"] != true {
		t.Fatal("standalone argument repaired Unicode")
	}
	for _, args := range []map[string]any{{"kind": "pack", "id": strings.Repeat("i", 2049)}, {"kind": "pack", "id": "same", "before": 1}, {"kind": "pack", "id": "same", "page_bytes": 262145}} {
		encoded, _ := json.Marshal(args)
		answer, size := auditRPC(t, NewServer(nil, nil), auditHistoryTool, string(encoded))
		if answer["isError"] != true || size > 1024 {
			t.Fatal("history bound refusal differs", answer)
		}
	}
	s := NewServer(nil, nil)
	first := historyRPCPage(t, s, map[string]any{"kind": "pack", "id": "same", "page_bytes": 1})
	config := filepath.Join(dir, project.DefaultConfigName)
	content, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, append(content, ' '), 0o600); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]any{"cursor": first.NextCursor})
	answer, _ := auditRPC(t, s, auditHistoryTool, string(args))
	if answer["isError"] != true || toolText(t, answer) != audit.ErrWireChanged.Error() {
		t.Fatal("history authority change was not refused", answer)
	}
}

func TestAuditHistoryAdvertisedArguments(t *testing.T) {
	for _, definition := range toolDefinitions() {
		if definition["name"] != auditHistoryTool {
			continue
		}
		schema := definition["inputSchema"].(map[string]any)
		properties := schema["properties"].(map[string]any)
		if schema["additionalProperties"] != false || schema["type"] != "object" || len(properties) != 5 {
			t.Fatal("history argument envelope changed")
		}
		for name, kind := range map[string]string{"kind": "string", "id": "string", "before": "integer", "cursor": "string", "page_bytes": "integer"} {
			if properties[name].(map[string]any)["type"] != kind {
				t.Fatal("missing/changed history argument", name)
			}
		}
		if !reflect.DeepEqual(properties["kind"].(map[string]any)["enum"], []string{"pack", "graph"}) || properties["id"].(map[string]any)["maxLength"] != 2048 || properties["before"].(map[string]any)["maximum"] != int64(9007199254740990) || properties["cursor"].(map[string]any)["maxLength"] != 85 || properties["page_bytes"].(map[string]any)["maximum"] != 262144 {
			t.Fatal("history advertised bounds changed")
		}
		return
	}
	t.Fatal("history tool missing")
}

func TestAuditHistoryContainedRegularFileGuards(t *testing.T) {
	for _, shape := range []string{"outside-dir", "linked-file", "directory-file", "absent-project"} {
		t.Run(shape, func(t *testing.T) {
			dir := auditMCPProject(t)
			trail := filepath.Join(dir, "audit", audit.FileName)
			switch shape {
			case "outside-dir":
				if err := os.WriteFile(filepath.Join(dir, project.DefaultConfigName), []byte(`{"configVersion":"3","audit":{"dir":"../private"},"packs":{}}`), 0o600); err != nil {
					t.Fatal(err)
				}
			case "linked-file":
				target := filepath.Join(dir, "private-secret")
				if err := os.WriteFile(target, []byte("private-content"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, trail); err != nil {
					t.Skip(err)
				}
			case "directory-file":
				if err := os.Mkdir(trail, 0o700); err != nil {
					t.Fatal(err)
				}
			case "absent-project":
				t.Setenv(project.ConfigEnv, filepath.Join(dir, "absent.json"))
			}
			answer, size := auditRPC(t, NewServer(nil, nil), auditHistoryTool, `{"kind":"pack","id":"same"}`)
			if answer["isError"] != true || size > 1024 || strings.Contains(toolText(t, answer), "private-content") || strings.Contains(toolText(t, answer), dir) {
				t.Fatal("history filesystem guard differs", answer)
			}
		})
	}
}

func TestAuditHistoryNoTrailAndRejectedResponseCleanup(t *testing.T) {
	dir := auditMCPProject(t)
	config := filepath.Join(dir, project.DefaultConfigName)
	if err := os.WriteFile(config, []byte(`{"configVersion":"3","packs":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, data := historyRPCDrain(t, NewServer(nil, nil), map[string]any{"kind": "pack", "id": "same"})
	var got result.AuditDefinitionHistory
	json.Unmarshal(data, &got)
	if got.State != "no-trail" {
		t.Fatal(got)
	}
	// Inject overlarge trusted build metadata to reach the generic result
	// refusal path, which ordinary bounded projection chunks cannot exceed.
	version := result.CLIVersion
	defer func() { result.CLIVersion = version }()
	result.CLIVersion = strings.Repeat("<", maxAuditResponseBytes/8)
	s := NewServer(nil, nil)
	for range audit.MaxWireExports + 1 {
		answer, _ := auditRPC(t, s, auditHistoryTool, `{"kind":"pack","id":"same","page_bytes":1}`)
		if answer["isError"] != true || !strings.Contains(toolText(t, answer), "4 MiB") {
			t.Fatal("history result refusal differs", answer)
		}
	}
	result.CLIVersion = version
	historyRPCPage(t, s, map[string]any{"kind": "pack", "id": "same", "page_bytes": 1})
}

func TestAuditHistorySparseSourceWindows(t *testing.T) {
	for _, size := range []int64{64 << 20, (64 << 20) + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			dir := auditMCPProject(t)
			name := filepath.Join(dir, "audit", audit.FileName)
			f, err := os.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			// Only Truncate creates the hole; writes are bounded authored suffixes.
			if err := f.Truncate(size); err != nil {
				t.Fatal(err)
			}
			older := " " + `{"recordVersion":"1","pack":{"id":"same"},"n":1}` + "\r"
			newer := "\t" + `{"recordVersion":"1","pack":{"id":"same"},"n":2}` + " "
			nonmatch := `{"recordVersion":"1","pack":{"id":"other"}}`
			tail := []byte("\n" + older + "\n" + nonmatch + "\nbad\n" + newer + "\npartial")
			olderEnd := int64(8 << 20)
			oldTail := []byte("\n" + older + "\n")
			for _, w := range []struct {
				end  int64
				data []byte
			}{{olderEnd, oldTail}, {size, tail}} {
				if _, err := f.WriteAt(w.data, w.end-int64(len(w.data))); err != nil {
					t.Fatal(err)
				}
			}
			check := func(before *int64, sourceBytes int64, raws []string, unread int, next *int64) []byte {
				t.Helper()
				args := map[string]any{"kind": "pack", "id": "same", "page_bytes": 17}
				if before != nil {
					args["before"] = *before
				}
				pages, encoded := historyRPCDrain(t, NewServer(nil, nil), args)
				want := result.AuditDefinitionHistory{State: "records", Records: []result.AuditDefinitionRecord{}, Unread: unread, Next: next}
				for _, raw := range raws {
					digest := sha256.Sum256([]byte(raw))
					want.Records = append(want.Records, result.AuditDefinitionRecord{Raw: raw, Digest: "sha256:" + hex.EncodeToString(digest[:])})
				}
				var got result.AuditDefinitionHistory
				if json.Unmarshal(encoded, &got) != nil || !reflect.DeepEqual(got, want) || pages[0].SourceBytes != sourceBytes {
					t.Fatalf("sparse history: %#v want %#v source=%d", got, want, pages[0].SourceBytes)
				}
				return encoded
			}
			next := size - int64(len(tail)) + 1
			captured := check(nil, size, []string{newer, older}, 2, &next)
			oldNext := olderEnd - int64(len(oldTail)) + 1
			check(&olderEnd, size, []string{older}, 0, &oldNext)
			zero := int64(0)
			check(&zero, size, nil, 0, nil)
			for _, before := range []int64{size + 1, audit.MaxHistorySourceBytes} {
				answer, _ := auditRPC(t, NewServer(nil, nil), auditHistoryTool, fmt.Sprintf(`{"kind":"pack","id":"same","before":%d}`, before))
				if answer["isError"] != true || toolText(t, answer) != audit.ErrHistoryBefore.Error() {
					t.Fatal(answer)
				}
			}
			answer, _ := auditRPC(t, NewServer(nil, nil), auditHistoryTool, `{"kind":"pack","id":"same","before":9007199254740991}`)
			if answer["isError"] != true || toolText(t, answer) != audit.ErrHistoryArguments.Error() {
				t.Fatal(answer)
			}
			if size > audit.MaxWireFileBytes {
				for _, tool := range []string{auditListTool, auditVerifyTool} {
					answer, n := auditRPC(t, NewServer(nil, nil), tool, `{}`)
					if answer["isError"] != true || n > 1024 || toolText(t, answer) != audit.ErrWireLimit.Error() {
						t.Fatal("raw/verify source gate changed", answer)
					}
				}
			}
			if _, err := f.WriteAt([]byte("\nappended\n"), size); err != nil {
				t.Fatal(err)
			}
			fresh := check(&size, size+10, []string{newer, older}, 2, &next)
			if !bytes.Equal(captured, fresh) {
				t.Fatal("fresh old-end selection changed after append")
			}
		})
	}
}
