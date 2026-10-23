package audit

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

func wireFixture(t *testing.T, contents map[string][]byte) (WireSource, string) {
	t.Helper()
	dir := t.TempDir()
	for name, data := range contents {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := fssecure.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	source := WireSource{Authority: "configured-project"}
	for i, name := range []string{FileName, SidecarName, StampsName} {
		source.Open[i] = func() (*os.File, error) { return root.Open(name) }
	}
	return source, dir
}

func wireDrain(t *testing.T, pager *Pager, source WireSource, cursor string, size int) ([]result.AuditPage, map[string][]byte) {
	t.Helper()
	pages := []result.AuditPage{}
	files := map[string][]byte{}
	for range 100000 {
		page, err := pager.Page(source, cursor, size)
		if err != nil {
			t.Fatal(err)
		}
		data, err := base64.StdEncoding.DecodeString(page.DataBase64)
		if err != nil || len(data) > size || len(page.Records) > MaxWireRecords {
			t.Fatalf("bad page bounds: %v", err)
		}
		files[page.File] = append(files[page.File], data...)
		pages = append(pages, page)
		if page.Complete {
			if page.NextCursor != "" {
				t.Fatal("completed export still has a cursor")
			}
			return pages, files
		}
		if page.NextCursor == "" {
			t.Fatal("partial export has no cursor")
		}
		cursor = page.NextCursor
	}
	t.Fatal("export did not finish")
	return nil, nil
}

func TestAuditWirePreservesBytesAndCrossPageMembers(t *testing.T) {
	first := ` { "recordVersion":"1", "pack":{"id":"same"}, "facts":{"exact":"  "}, "extra":[1,true] }` + "\r"
	second := `{"recordVersion":"1","graph":{"id":"same"},"extra":null}`
	evaluations := []byte(first + "\n" + second + "\n\nnot-json\n{\"invalid\":\"\xff\"}\nunfinished")
	sidecar := []byte(" {\"kind\":\"signature\",\"extension\":true} \r\n")
	stamps := []byte("{\"kind\":\"stamp\",\"token\":\"kept\"}\n")
	source, _ := wireFixture(t, map[string][]byte{FileName: evaluations, SidecarName: sidecar, StampsName: stamps})
	pages, files := wireDrain(t, NewPager(), source, "", 7)
	for name, expected := range map[string][]byte{FileName: evaluations, SidecarName: sidecar, StampsName: stamps} {
		if !bytes.Equal(files[name], expected) {
			t.Fatalf("%s exact export differs", name)
		}
	}
	var records []result.AuditPageRecord
	pending := false
	for _, page := range pages {
		pending = pending || page.PendingLine
		if page.File == FileName {
			records = append(records, page.Records...)
		}
	}
	if !pending || len(records) != 6 {
		t.Fatalf("cross-page records: %d, pending:%v", len(records), pending)
	}
	statuses := []string{}
	for _, record := range records {
		statuses = append(statuses, record.Status)
		raw, err := base64.StdEncoding.DecodeString(record.RawLineBase64)
		if err != nil || int64(len(raw)) != record.Bytes {
			t.Fatal("line bytes changed")
		}
		digest := sha256.Sum256(raw)
		if record.Digest != "sha256:"+hex.EncodeToString(digest[:]) {
			t.Fatal("line digest includes a newline or changes whitespace")
		}
	}
	if !reflect.DeepEqual(statuses, []string{"parsed", "parsed", "malformed", "malformed", "malformed", "incomplete"}) {
		t.Fatal(statuses)
	}
	var members map[string]any
	if json.Unmarshal(records[0].Members, &members) != nil || members["recordVersion"] != "1" || members["pack"].(map[string]any)["id"] != "same" || members["extra"] == nil {
		t.Fatal("structured history members lost")
	}
	raw, _ := base64.StdEncoding.DecodeString(records[0].RawLineBase64)
	if string(raw) != first || records[1].Offset != int64(len(first)+1) {
		t.Fatal("history whitespace/CR or byte offsets changed")
	}
}

func TestAuditWireMissingEmptyAndOversizedLines(t *testing.T) {
	oversized := bytes.Repeat([]byte{'x'}, MaxWireRecord+1)
	contents := append(append([]byte{}, oversized...), '\n')
	contents = append(contents, []byte("{\"after\":true}\n")...)
	source, _ := wireFixture(t, map[string][]byte{FileName: contents, SidecarName: {}})
	pages, files := wireDrain(t, NewPager(), source, "", 12000)
	if !bytes.Equal(files[FileName], contents) || !pages[0].Files[1].Present || pages[0].Files[1].Bytes != 0 || pages[0].Files[2].Present {
		t.Fatal("missing/empty/oversized custody differs")
	}
	var records []result.AuditPageRecord
	for _, page := range pages {
		if page.File == FileName {
			records = append(records, page.Records...)
		}
	}
	digest := sha256.Sum256(oversized)
	if len(records) != 2 || records[0].Status != "oversized" || records[0].RawLineBase64 != "" || records[0].Digest != "sha256:"+hex.EncodeToString(digest[:]) || records[1].Status != "parsed" {
		t.Fatal("oversized line hid a later record", records)
	}
	missing, _ := wireFixture(t, nil)
	missingPages, _ := wireDrain(t, NewPager(), missing, "", 1)
	if len(missingPages) != 3 || missingPages[0].SnapshotBetweenWrites {
		t.Fatal("missing trail incorrectly claims between-writes snapshot")
	}
}

func TestAuditWireLineCountBoundDoesNotTruncate(t *testing.T) {
	data := bytes.Repeat([]byte("{}\n"), MaxWireRecords+2)
	source, _ := wireFixture(t, map[string][]byte{FileName: data})
	pages, files := wireDrain(t, NewPager(), source, "", MaxWirePage)
	if len(pages[0].Records) != MaxWireRecords || pages[0].FileComplete || !bytes.Equal(files[FileName], data) {
		t.Fatal("line result limit silently completed or truncated export")
	}
}

func TestAuditWireAppendExclusionAndCursorRetries(t *testing.T) {
	source, dir := wireFixture(t, map[string][]byte{FileName: []byte("{\"v\":1}\n")})
	pager := NewPager()
	first, err := pager.Page(source, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(dir, FileName), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("{\"later\":true}\n")
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	second, err := pager.Page(source, first.NextCursor, 2)
	if err != nil {
		t.Fatal(err)
	}
	retried, err := pager.Page(source, first.NextCursor, 100)
	if err != nil || !reflect.DeepEqual(second, retried) {
		t.Fatal("latest cursor retry was not idempotent", err)
	}
	pages, files := wireDrain(t, pager, source, second.NextCursor, 2)
	prefix, _ := base64.StdEncoding.DecodeString(first.DataBase64 + "")
	part, _ := base64.StdEncoding.DecodeString(second.DataBase64)
	got := append(append(prefix, part...), files[FileName]...)
	if string(got) != "{\"v\":1}\n" {
		t.Fatal("append leaked into fixed prefix", string(got))
	}
	last := pages[len(pages)-1]
	// The cursor which returned the final stamps page remains replayable.
	previous := pages[len(pages)-2].NextCursor
	again, err := pager.Page(source, previous, 1)
	if err != nil || !reflect.DeepEqual(last, again) {
		t.Fatal("final cursor retry differs", err)
	}
	if _, err := pager.Page(source, first.NextCursor, 1); !errors.Is(err, ErrWireCursor) {
		t.Fatal("old cursor was accepted", err)
	}
}

func TestAuditWireChangedFilesNeverComplete(t *testing.T) {
	for _, mutation := range []string{"replace-identical", "shrink", "edit-served", "edit-unserved", "create-missing", "remove-sidecar", "authority"} {
		t.Run(mutation, func(t *testing.T) {
			data := bytes.Repeat([]byte{'a'}, DefaultWirePage+8)
			source, dir := wireFixture(t, map[string][]byte{FileName: data, SidecarName: []byte("{}\n")})
			pager := NewPager()
			first, err := pager.Page(source, "", DefaultWirePage)
			if err != nil {
				t.Fatal(err)
			}
			name := filepath.Join(dir, FileName)
			switch mutation {
			case "replace-identical":
				if err := os.Rename(name, filepath.Join(dir, "old")); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(name, data, 0o600); err != nil {
					t.Fatal(err)
				}
			case "shrink":
				if err := os.Truncate(name, 1); err != nil {
					t.Fatal(err)
				}
			case "edit-served", "edit-unserved":
				at := int64(0)
				if mutation == "edit-unserved" {
					at = DefaultWirePage
				}
				f, err := os.OpenFile(name, os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				_, err = f.WriteAt([]byte{'b'}, at)
				f.Close()
				if err != nil {
					t.Fatal(err)
				}
			case "create-missing":
				if err := os.WriteFile(filepath.Join(dir, StampsName), []byte{}, 0o600); err != nil {
					t.Fatal(err)
				}
			case "remove-sidecar":
				if err := os.Remove(filepath.Join(dir, SidecarName)); err != nil {
					t.Fatal(err)
				}
			case "authority":
				source.Authority = "changed-project"
			}
			cursor := first.NextCursor
			for range 5 {
				page, err := pager.Page(source, cursor, MaxWirePage)
				if errors.Is(err, ErrWireChanged) {
					return
				}
				if err != nil || page.Complete {
					t.Fatalf("changed export succeeded or wrong refusal: %v", err)
				}
				cursor = page.NextCursor
			}
			t.Fatal("changed prefix was never detected")
		})
	}
}

func TestAuditWireCapacityExpiryLimitsAndConcurrentRetry(t *testing.T) {
	source, dir := wireFixture(t, map[string][]byte{FileName: []byte("{}\n")})
	pager := NewPager()
	now := time.Unix(100, 0)
	pager.now = func() time.Time { return now }
	var first result.AuditPage
	for range MaxWireExports {
		var err error
		first, err = pager.Page(source, "", 1)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pager.Page(source, "", 1); !errors.Is(err, ErrWireCapacity) {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	answers := make(chan result.AuditPage, 2)
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			page, err := pager.Page(source, first.NextCursor, 1)
			answers <- page
			errs <- err
		}()
	}
	wg.Wait()
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(<-answers, <-answers) {
		t.Fatal("concurrent retry advanced twice")
	}
	now = now.Add(WireExportLifetime)
	if _, err := pager.Page(source, first.NextCursor, 1); !errors.Is(err, ErrWireCursor) {
		t.Fatal(err)
	}
	for _, size := range []int{0, -1, MaxWirePage + 1} {
		if _, err := pager.Page(source, "", size); err != ErrWirePage {
			t.Fatal(err)
		}
	}
	if err := os.Truncate(filepath.Join(dir, FileName), MaxWireFileBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, err := pager.Page(source, "", 1); err != ErrWireLimit {
		t.Fatal(err)
	}
}

func TestAuditWireCompletedExportsDoNotExhaustCapacity(t *testing.T) {
	source, _ := wireFixture(t, nil)
	pager := NewPager()
	for range MaxWireExports + 1 {
		wireDrain(t, pager, source, "", 1)
	}
	if len(pager.exports) != MaxWireExports {
		t.Fatal("unbounded completed export retention")
	}
}

// A single-byte page slices a verified block cache. It does not hash the file
// again per byte; only capture and finalization visit every block.
func TestAuditWireSmallPagesUseBoundedBlockCache(t *testing.T) {
	source, _ := wireFixture(t, map[string][]byte{FileName: []byte(strings.Repeat("x", DefaultWirePage+10))})
	pager := NewPager()
	first, err := pager.Page(source, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	export := pager.exports[strings.Split(first.NextCursor, ".")[0]]
	cache := &export.cache[0]
	cursor := first.NextCursor
	for range 25 {
		page, err := pager.Page(source, cursor, 1)
		if err != nil {
			t.Fatal(err)
		}
		if &export.cache[0] != cache || len(export.cache) > DefaultWirePage || len(export.line) > MaxWireRecord {
			t.Fatal("block cache was reread or memory grew unbounded")
		}
		cursor = page.NextCursor
	}
}

func TestAuditWireValidRecordSizeBoundary(t *testing.T) {
	prefix, suffix := `{"recordVersion":"1","pack":{"id":"same"},"v":"`, `"}`
	line := prefix + strings.Repeat("x", MaxWireRecord-len(prefix)-len(suffix)) + suffix
	larger := prefix + strings.Repeat("x", MaxWireRecord+1-len(prefix)-len(suffix)) + suffix
	data := []byte(line + "\n" + larger + "\n")
	source, _ := wireFixture(t, map[string][]byte{FileName: data})
	pages, files := wireDrain(t, NewPager(), source, "", DefaultWirePage)
	var records []result.AuditPageRecord
	for _, page := range pages {
		if page.File == FileName {
			records = append(records, page.Records...)
		}
	}
	if !bytes.Equal(files[FileName], data) || len(records) != 2 || records[0].Status != "parsed" || records[0].Bytes != MaxWireRecord || records[1].Status != "oversized" || records[1].Bytes != MaxWireRecord+1 || len(records[1].Members) != 0 {
		t.Fatal("valid record size boundary changed")
	}
}

func TestAuditWireFinalizationRechecksNames(t *testing.T) {
	source, dir := wireFixture(t, map[string][]byte{FileName: []byte("{}\n")})
	pager := NewPager()
	first, err := pager.Page(source, "", DefaultWirePage)
	if err != nil {
		t.Fatal(err)
	}
	second, err := pager.Page(source, first.NextCursor, DefaultWirePage)
	if err != nil {
		t.Fatal(err)
	}
	open := source.Open[0]
	calls := 0
	source.Open[0] = func() (*os.File, error) {
		calls++
		// Each normal snapshot opens the trail three times: probing presence,
		// opening under its lock, then confirming identity. On the final page,
		// two snapshots precede hashing; the seventh open is the post-pass check.
		if calls == 7 {
			name := filepath.Join(dir, FileName)
			if err := os.Rename(name, filepath.Join(dir, "previous")); err != nil {
				return nil, err
			}
			if err := os.WriteFile(name, []byte("{}\n"), 0o600); err != nil {
				return nil, err
			}
		}
		return open()
	}
	page, err := pager.Page(source, second.NextCursor, DefaultWirePage)
	if !errors.Is(err, ErrWireChanged) || page.Complete || calls < 7 {
		t.Fatal("replacement during finalization completed", err, calls)
	}
}
