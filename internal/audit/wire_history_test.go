package audit

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// Independent compatibility oracle transcribes the retained Desk reader's
// exact window, line ordering and typed decode, without invoking Desk code.
func deskHistoryOracle(data []byte, before int64, kind, id string) (result.AuditDefinitionHistory, bool) {
	out := result.AuditDefinitionHistory{State: "records", Records: []result.AuditDefinitionRecord{}}
	end := int64(len(data))
	if before >= 0 {
		end = before
	}
	if end < 0 || end > int64(len(data)) {
		return out, false
	}
	if end == 0 {
		return out, true
	}
	start := end - (4 << 20)
	if start < 0 {
		start = 0
	}
	window := data[start:end]
	if start > 0 {
		at := bytes.IndexByte(window, '\n')
		if at < 0 {
			return out, false
		}
		start += int64(at + 1)
		window = window[at+1:]
	}
	if len(window) > 0 && window[len(window)-1] != '\n' {
		out.Unread++
		at := bytes.LastIndexByte(window, '\n')
		window = window[:at+1]
	}
	position := start + int64(len(window))
	lines := bytes.Split(window, []byte("\n"))
	for n := len(lines) - 2; n >= 0; n-- {
		line := lines[n]
		position -= int64(len(line) + 1)
		var r struct {
			RecordVersion string `json:"recordVersion"`
			Pack          *struct {
				ID string `json:"id"`
			} `json:"pack"`
			Graph *struct {
				ID string `json:"id"`
			} `json:"graph"`
		}
		if len(line) == 0 || !utf8.Valid(line) || json.Unmarshal(line, &r) != nil {
			out.Unread++
			continue
		}
		if r.RecordVersion != "1" {
			out.Unread++
			continue
		}
		if !(kind == "pack" && r.Pack != nil && r.Pack.ID == id || kind == "graph" && r.Graph != nil && r.Graph.ID == id) {
			continue
		}
		digest := sha256.Sum256(line)
		out.Records = append(out.Records, result.AuditDefinitionRecord{Raw: string(line), Digest: "sha256:" + hex.EncodeToString(digest[:])})
		if len(out.Records) == 50 {
			if position > 0 {
				out.Next = &position
			}
			return out, true
		}
	}
	if start > 0 {
		out.Next = &start
	}
	return out, true
}

func historyRecordBytes(size int, unit string) []byte {
	prefix, suffix := `{"recordVersion":"1","pack":{"id":"same"},"v":"`, `"}`
	n := size - len(prefix) - len(suffix)
	return []byte(prefix + strings.Repeat(unit, n/len(unit)) + strings.Repeat("x", n%len(unit)) + suffix)
}

func historyDrain(t *testing.T, p *Pager, source WireSource, declared bool, request HistoryRequest, chunk int) ([]result.AuditHistoryPage, []byte) {
	t.Helper()
	pages := []result.AuditHistoryPage{}
	data := []byte{}
	cursor := ""
	query := &request
	for range 20000 {
		page, err := p.HistoryPage(source, declared, cursor, query, chunk)
		if err != nil {
			t.Fatal(err)
		}
		part, err := base64.StdEncoding.DecodeString(page.DataBase64)
		if err != nil || len(part) > chunk || page.Offset != int64(len(data)) {
			t.Fatal("history range differs", err)
		}
		data = append(data, part...)
		pages = append(pages, page)
		if page.Complete {
			if page.NextCursor != "" {
				t.Fatal("complete history has continuation")
			}
			digest := sha256.Sum256(data)
			if page.ProjectionBytes != int64(len(data)) || page.ProjectionDigest != "sha256:"+hex.EncodeToString(digest[:]) {
				t.Fatal("projection identity changed")
			}
			return pages, data
		}
		cursor = page.NextCursor
		query = nil
	}
	t.Fatal("history transport did not terminate")
	return nil, nil
}

func TestAuditHistoryExactDeskWindowAndUnicode(t *testing.T) {
	for _, row := range []struct {
		name            string
		data            []byte
		id              string
		before          int64
		matches, unread int
	}{
		{"empty", nil, "same", -1, 0, 0},
		{"unicode-selection-and-unread", []byte("{\"recordVersion\":\"1\",\"pack\":{\"id\":\"\\ud800\"}}\r\n" + `{"recordVersion":"1","pack":{"id":"other"}}` + "\n" + `{"recordVersion":"2","pack":{"id":"other"}}` + "\n" + "{\"recordVersion\":\"1\",\"pack\":{\"id\":\"\xff\"}}\n" + `{"recordVersion":"1","pack":{"id":"same"},"graph":{"id":3}}` + "\npartial"), "�", -1, 1, 4},
		{"paired-surrogate", []byte(`{"RecordVersion":"1","Pack":{"ID":"\ud83d\ude00"}}` + "\n"), "😀", -1, 1, 0},
		{"case-and-duplicate-order", []byte(`{"recordVersion":"1","RecordVersion":"2","pack":{"id":"same"}}` + "\n" + `{"recordVersion":"2","recordVersion":"1","Pack":{"ID":"same"}}` + "\n"), "same", -1, 1, 1},
		{"64KiB-plus", append(historyRecordBytes((64<<10)+1, "x"), '\n'), "same", -1, 1, 0},
		{"exact-four-MiB-window", append(historyRecordBytes((4<<20)-1, "x"), '\n'), "same", -1, 1, 0},
		{"one-byte-over-window", append(historyRecordBytes(4<<20, "x"), '\n'), "same", -1, 0, 0},
		{"partial-at-before", []byte(`{"recordVersion":"1","pack":{"id":"same"}}` + "\npartial"), "same", 5, 0, 1},
	} {
		t.Run(row.name, func(t *testing.T) {
			want, ok := deskHistoryOracle(row.data, row.before, "pack", row.id)
			if !ok {
				t.Fatal("bad oracle fixture")
			}
			if len(want.Records) != row.matches || want.Unread != row.unread {
				t.Fatal("authored expectation differs", len(want.Records), want.Unread)
			}
			source, _ := wireFixture(t, map[string][]byte{FileName: row.data})
			pages, encoded := historyDrain(t, NewPager(), source, true, HistoryRequest{Kind: "pack", ID: row.id, Before: row.before}, MaxWirePage)
			var got result.AuditDefinitionHistory
			if json.Unmarshal(encoded, &got) != nil || !reflect.DeepEqual(got, want) {
				t.Fatal("logical history differs")
			}
			if pages[0].SourceBytes != int64(len(row.data)) {
				t.Fatal("source size differs")
			}
			if row.name == "one-byte-over-window" && (got.Next == nil || *got.Next != int64(len(row.data))) {
				t.Fatal("Desk's nonprogress next edge was changed")
			}
		})
	}
}

func TestAuditHistoryFiftyNextAndWindowRefusals(t *testing.T) {
	data := []byte{}
	firstBytes := 0
	for n := 0; n < 51; n++ {
		line := []byte(fmt.Sprintf(`{"recordVersion":"1","pack":{"id":"same"},"n":%d}`+"\n", n))
		if n == 0 {
			firstBytes = len(line)
		}
		data = append(data, line...)
	}
	source, _ := wireFixture(t, map[string][]byte{FileName: data})
	_, encoded := historyDrain(t, NewPager(), source, true, HistoryRequest{Kind: "pack", ID: "same", Before: -1}, 23)
	var got result.AuditDefinitionHistory
	json.Unmarshal(encoded, &got)
	if len(got.Records) != 50 || got.Next == nil || *got.Next != int64(firstBytes) || got.Unread != 0 {
		t.Fatal("50-record boundary changed")
	}
	want, _ := deskHistoryOracle(data, -1, "pack", "same")
	if !reflect.DeepEqual(got, want) {
		t.Fatal("newest-first ordering changed")
	}
	if _, err := DefinitionHistory(bytes.NewReader(data), int64(len(data)), HistoryRequest{Kind: "pack", ID: "same", Before: int64(len(data)) + 1}); err != ErrHistoryBefore {
		t.Fatal(err)
	}
	long := bytes.Repeat([]byte{'x'}, (4<<20)+1)
	if _, err := DefinitionHistory(bytes.NewReader(long), int64(len(long)), HistoryRequest{Kind: "pack", ID: "same", Before: -1}); err != ErrHistoryWindow {
		t.Fatal(err)
	}
}

func TestAuditHistoryProjectionEscapeAndMetadataBounds(t *testing.T) {
	for _, unit := range []string{`\\`, "\u2028", "<"} {
		data := append(historyRecordBytes((4<<20)-1, unit), '\n')
		source, _ := wireFixture(t, map[string][]byte{FileName: data})
		_, encoded := historyDrain(t, NewPager(), source, true, HistoryRequest{Kind: "pack", ID: "same", Before: -1}, MaxWirePage)
		if int64(len(encoded)) > MaxHistoryProjectionBytes {
			t.Fatal("projection bound exceeded")
		}
		if unit != "<" && len(encoded) < 2*len(data)-1024 {
			t.Fatal("fixture did not exercise near-maximal string escaping", len(encoded))
		}
		var history result.AuditDefinitionHistory
		if json.Unmarshal(encoded, &history) != nil || len(history.Records) != 1 || history.Records[0].Raw != string(data[:len(data)-1]) {
			t.Fatal("escaped projection changed raw bytes")
		}
	}
	data := []byte{}
	id := strings.Repeat("i", 2048)
	for range 50 {
		data = append(data, []byte(`{"recordVersion":"1","pack":{"id":"`+id+`"}}`+"\n")...)
	}
	source, _ := wireFixture(t, map[string][]byte{FileName: data})
	_, encoded := historyDrain(t, NewPager(), source, true, HistoryRequest{Kind: "pack", ID: id, Before: -1}, MaxWirePage)
	var history result.AuditDefinitionHistory
	json.Unmarshal(encoded, &history)
	if len(history.Records) != 50 || int64(len(encoded)) > 2*int64(len(data))+64<<10 {
		t.Fatal("50-record metadata budget changed")
	}
}

func TestAuditHistorySharedCapacityCursorKindsAndSnapshot(t *testing.T) {
	source, dir := wireFixture(t, map[string][]byte{FileName: []byte(`{"recordVersion":"1","pack":{"id":"same"}}` + "\n")})
	p := NewPager()
	request := &HistoryRequest{Kind: "pack", ID: "same", Before: -1}
	raw, err := p.Page(source, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	first, err := p.HistoryPage(source, true, "", request, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Page(source, first.NextCursor, 1); err != ErrWireCursor {
		t.Fatal("history cursor accepted as raw", err)
	}
	if _, err := p.HistoryPage(source, true, raw.NextCursor, nil, 1); err != ErrWireCursor {
		t.Fatal("raw cursor accepted as history", err)
	}
	if _, err := p.Page(source, raw.NextCursor, 1); err != nil {
		t.Fatal("wrong-kind refusal invalidated raw cursor", err)
	}
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("changed-after-capture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := p.HistoryPage(source, true, first.NextCursor, nil, 23)
	if err != nil {
		t.Fatal(err)
	}
	again, err := p.HistoryPage(source, true, first.NextCursor, nil, MaxWirePage)
	if err != nil || !reflect.DeepEqual(second, again) {
		t.Fatal("history retry differs", err)
	}
	// Cached source answer remains immutable after overwrite; authority still
	// binds every continuation and invalidates only that export when changed.
	changed := source
	changed.Authority = "different"
	if _, err := p.HistoryPage(changed, true, second.NextCursor, nil, 1); err != ErrWireChanged {
		t.Fatal(err)
	}
	p = NewPager()
	now := time.Unix(1, 0)
	p.now = func() time.Time { return now }
	for n := 0; n < MaxWireExports; n++ {
		if n%2 == 0 {
			if _, err := p.Page(source, "", 1); err != nil {
				t.Fatal(err)
			}
		} else {
			if _, err := p.HistoryPage(source, true, "", request, 1); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := p.Page(source, "", 1); err != ErrWireCapacity {
		t.Fatal(err)
	}
	if _, err := p.HistoryPage(source, true, "", request, 1); err != ErrWireCapacity {
		t.Fatal(err)
	}
	now = now.Add(WireExportLifetime)
	if _, err := p.HistoryPage(source, true, "", request, 1); err != nil {
		t.Fatal(err)
	}
}

func TestAuditHistoryMissingStatesAndSourceLimit(t *testing.T) {
	source, dir := wireFixture(t, nil)
	for _, declared := range []bool{false, true} {
		_, data := historyDrain(t, NewPager(), source, declared, HistoryRequest{Kind: "pack", ID: "same", Before: -1}, MaxWirePage)
		var got result.AuditDefinitionHistory
		json.Unmarshal(data, &got)
		want := "no-trail"
		if declared {
			want = "no-records"
		}
		if got.State != want || len(got.Records) != 0 {
			t.Fatal(got)
		}
	}
	file, err := os.Create(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	err = file.Truncate(MaxWireFileBytes + 1)
	if err == nil {
		// A valid record tail with no LF still refuses by the window policy.
		tail := []byte(`{"recordVersion":"1","pack":{"id":"same"}}`)
		_, err = file.WriteAt(tail, int64(MaxWireFileBytes+1-len(tail)))
	}
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewPager().HistoryPage(source, true, "", &HistoryRequest{Kind: "pack", ID: "same", Before: -1}, 1)
	if !errors.Is(err, ErrHistoryWindow) {
		t.Fatal(err)
	}
}

func TestAuditHistoryCaptureRechecksWindowAndIdentity(t *testing.T) {
	for _, mutation := range []string{"edit", "replace", "shrink"} {
		t.Run(mutation, func(t *testing.T) {
			data := []byte(`{"recordVersion":"1","pack":{"id":"same"}}` + "\n")
			source, dir := wireFixture(t, map[string][]byte{FileName: data})
			open := source.Open[0]
			calls := 0
			source.Open[0] = func() (*os.File, error) {
				calls++
				if calls == 3 {
					name := filepath.Join(dir, FileName)
					switch mutation {
					case "edit":
						changed := bytes.ReplaceAll(data, []byte("same"), []byte("else"))
						if err := os.WriteFile(name, changed, 0o600); err != nil {
							return nil, err
						}
					case "replace":
						if err := os.Rename(name, filepath.Join(dir, "old")); err != nil {
							return nil, err
						}
						if err := os.WriteFile(name, data, 0o600); err != nil {
							return nil, err
						}
					case "shrink":
						if err := os.Truncate(name, 0); err != nil {
							return nil, err
						}
					}
				}
				return open()
			}
			p := NewPager()
			_, err := p.HistoryPage(source, true, "", &HistoryRequest{Kind: "pack", ID: "same", Before: -1}, 1)
			if err != ErrWireChanged || len(p.exports) != 0 {
				t.Fatal("changed capture was published", err)
			}
		})
	}
}

func TestAuditHistoryWaitsForTrailAndSerializesRetry(t *testing.T) {
	source, dir := wireFixture(t, map[string][]byte{FileName: []byte(`{"recordVersion":"1","pack":{"id":"same"}`)})
	root, err := fssecure.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	started := make(chan bool, 1)
	release := make(chan struct{})
	done := make(chan error, 1)
	var once sync.Once
	defer once.Do(func() { close(release) })
	go func() {
		err, _ := root.AppendExistingLockedThen(FileName, func(state fssecure.AppendState) ([]byte, error) {
			started <- state.Locked
			<-release
			return []byte("}\n"), nil
		}, nil)
		done <- err
	}()
	if locked := <-started; !locked {
		once.Do(func() { close(release) })
		<-done
		t.Skip("cooperative locks unavailable")
	}
	type answer struct {
		page result.AuditHistoryPage
		err  error
	}
	read := make(chan answer, 1)
	p := NewPager()
	go func() {
		page, err := p.HistoryPage(source, true, "", &HistoryRequest{Kind: "pack", ID: "same", Before: -1}, 1)
		read <- answer{page, err}
	}()
	select {
	case <-read:
		t.Fatal("history returned during cooperating trail append")
	case <-time.After(75 * time.Millisecond):
	}
	once.Do(func() { close(release) })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	got := <-read
	if got.err != nil || !got.page.SnapshotBetweenWrites {
		t.Fatal("locked history capture differs", got.err)
	}
	responses := make(chan answer, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			page, err := p.HistoryPage(source, true, got.page.NextCursor, nil, 23)
			responses <- answer{page, err}
		}()
	}
	wg.Wait()
	first, second := <-responses, <-responses
	if first.err != nil || second.err != nil || !reflect.DeepEqual(first.page, second.page) {
		t.Fatal("concurrent history retry advanced twice")
	}
}

// offsetHistoryReader supplies one authored tail without allocating its logical
// prefix. Every read must fit the expected backward window.
type offsetHistoryReader struct {
	start int64
	tail  []byte
	bytes int64
	calls int
}

func (r *offsetHistoryReader) ReadAt(p []byte, off int64) (int, error) {
	r.calls++
	r.bytes += int64(len(p))
	if off < r.start || off+int64(len(p)) > r.start+HistoryReadBytes {
		return 0, fmt.Errorf("read outside window: %d + %d", off, len(p))
	}
	clear(p)
	tailStart := r.start + HistoryReadBytes - int64(len(r.tail))
	for i := range p {
		at := off + int64(i) - tailStart
		if at >= 0 && at < int64(len(r.tail)) {
			p[i] = r.tail[at]
		}
	}
	return len(p), nil
}

func TestAuditHistorySourceAddressRange(t *testing.T) {
	raw := " " + `{"recordVersion":"1","pack":{"id":"same"}}` + "\r"
	tail := []byte("\n" + raw + "\npartial")
	reader := &offsetHistoryReader{start: MaxHistorySourceBytes - HistoryReadBytes, tail: tail}
	got, err := DefinitionHistory(reader, MaxHistorySourceBytes, HistoryRequest{Kind: "pack", ID: "same", Before: -1})
	next := MaxHistorySourceBytes - int64(len(tail)) + 1
	digest := sha256.Sum256([]byte(raw))
	want := result.AuditDefinitionHistory{State: "records", Records: []result.AuditDefinitionRecord{{Raw: raw, Digest: "sha256:" + hex.EncodeToString(digest[:])}}, Unread: 1, Next: &next}
	if err != nil || !reflect.DeepEqual(got, want) || reader.calls != 1 || reader.bytes != HistoryReadBytes {
		t.Fatalf("ceiling window: %#v %v; reads %d/%d", got, err, reader.calls, reader.bytes)
	}
	encoded, err := json.Marshal(got)
	if err != nil || !bytes.Contains(encoded, []byte(fmt.Sprintf(`"next":%d`, next))) {
		t.Fatal("high next lost integer precision", string(encoded), err)
	}
	for _, row := range []struct {
		size, before int64
		want         error
	}{
		{MaxHistorySourceBytes + 1, -1, ErrHistorySourceLimit},
		{MaxHistorySourceBytes + 1, 0, ErrHistorySourceLimit},
		{-1, -1, ErrHistorySourceLimit},
		{10, 11, ErrHistoryBefore},
		{10, -2, ErrHistoryArguments},
		{MaxHistorySourceBytes, MaxHistorySourceBytes + 1, ErrHistoryBefore},
	} {
		reader := &offsetHistoryReader{}
		if _, err := DefinitionHistory(reader, row.size, HistoryRequest{Before: row.before}); err != row.want || reader.calls != 0 {
			t.Fatalf("range %d/%d read or wrong refusal: %v calls=%d", row.size, row.before, err, reader.calls)
		}
	}
}

func TestAuditHistoryLargeCaptureRechecksWindow(t *testing.T) {
	source, dir := wireFixture(t, nil)
	name := filepath.Join(dir, FileName)
	f, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	size := int64(MaxWireFileBytes + 1)
	tail := []byte("\n" + `{"recordVersion":"1","pack":{"id":"same"}}` + "\n")
	if err := f.Truncate(size); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if _, err := f.WriteAt(tail, size-int64(len(tail))); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	open := source.Open[0]
	calls := 0
	source.Open[0] = func() (*os.File, error) {
		calls++
		if calls == 3 {
			f, err := os.OpenFile(name, os.O_WRONLY, 0)
			if err != nil {
				return nil, err
			}
			_, err = f.WriteAt([]byte("X"), size-3)
			f.Close()
			if err != nil {
				return nil, err
			}
		}
		return open()
	}
	p := NewPager()
	if _, err := p.HistoryPage(source, true, "", &HistoryRequest{Kind: "pack", ID: "same", Before: -1}, 1); err != ErrWireChanged || len(p.exports) != 0 {
		t.Fatal("large changed capture published", err)
	}
}
