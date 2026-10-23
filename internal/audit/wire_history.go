package audit

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

const HistoryReadBytes int64 = 4 << 20

// MaxHistorySourceBytes bounds source sizes and logical offsets to exact JSON
// integers. It does not increase the bounded backward reading window.
const MaxHistorySourceBytes int64 = 9007199254740990

// Valid UTF-8 JSON line bytes expand at most twofold in a JSON string with
// HTML escaping disabled. Fifty digests plus fixed count/offset metadata fit
// within the extra 64 KiB; the source window itself bounds total raw strings.
const MaxHistoryProjectionBytes = 2*HistoryReadBytes + (64 << 10)

var (
	ErrHistorySourceLimit = errors.New("The captured history source is outside the supported address range; no complete result was produced.")
	ErrHistoryArguments   = errors.New("Supply one history kind (pack or graph), an id of 1 to 2048 UTF-8 bytes and an optional nonnegative before offset; use only cursor to continue.")
	ErrHistoryWindow      = errors.New("A record exceeds the 4 MiB history reading window; download the exact trail instead.")
	ErrHistoryBefore      = errors.New("The history before offset is beyond the captured trail; start a new history reading.")
	ErrHistoryProjection  = errors.New("The history projection exceeds its bounded encoding limit; no complete result was produced.")
)

type HistoryRequest struct {
	Kind, ID string
	Before   int64
}

// DefinitionHistory applies the existing Desk reader's window and typed JSON
// selection exactly. In particular, Unicode escape replacement belongs to Go's
// typed decoder here; raw line text and digest are never repaired or reencoded.
func DefinitionHistory(reader io.ReaderAt, size int64, request HistoryRequest) (result.AuditDefinitionHistory, error) {
	out := result.AuditDefinitionHistory{State: "records", Records: []result.AuditDefinitionRecord{}}
	start, end, err := historyRange(size, request.Before)
	if err != nil {
		return out, err
	}
	if end == 0 {
		return out, nil
	}
	data := make([]byte, end-start)
	if _, err := reader.ReadAt(data, start); err != nil {
		return out, ErrWireRead
	}
	return historyWindow(data, start, request)
}

// historyRange validates before allocation or reads, including direct readers
// that do not pass through the pager's argument checks. -1 means omitted.
func historyRange(size, before int64) (start, end int64, err error) {
	if size < 0 || size > MaxHistorySourceBytes {
		return 0, 0, ErrHistorySourceLimit
	}
	if before < -1 {
		return 0, 0, ErrHistoryArguments
	}
	end = size
	if before >= 0 {
		end = before
	}
	if end > size {
		return 0, 0, ErrHistoryBefore
	}
	return max(int64(0), end-HistoryReadBytes), end, nil
}

func historyWindow(data []byte, start int64, request HistoryRequest) (result.AuditDefinitionHistory, error) {
	out := result.AuditDefinitionHistory{State: "records", Records: []result.AuditDefinitionRecord{}}
	if start > 0 {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			return out, ErrHistoryWindow
		}
		start += int64(i + 1)
		data = data[i+1:]
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		out.Unread++
		i := bytes.LastIndexByte(data, '\n')
		data = data[:i+1]
	}
	offset := start + int64(len(data))
	lines := bytes.Split(data, []byte{'\n'})
	for i := len(lines) - 2; i >= 0; i-- {
		line := lines[i]
		offset -= int64(len(line) + 1)
		var record struct {
			RecordVersion string `json:"recordVersion"`
			Pack          *struct {
				ID string `json:"id"`
			} `json:"pack"`
			Graph *struct {
				ID string `json:"id"`
			} `json:"graph"`
		}
		if len(line) == 0 || !utf8.Valid(line) || json.Unmarshal(line, &record) != nil || record.RecordVersion != "1" {
			out.Unread++
			continue
		}
		matches := request.Kind == "pack" && record.Pack != nil && record.Pack.ID == request.ID || request.Kind == "graph" && record.Graph != nil && record.Graph.ID == request.ID
		if !matches {
			continue
		}
		digest := sha256.Sum256(line)
		out.Records = append(out.Records, result.AuditDefinitionRecord{Raw: string(line), Digest: "sha256:" + hex.EncodeToString(digest[:])})
		if len(out.Records) == 50 {
			if offset > 0 {
				out.Next = &offset
			}
			return out, nil
		}
	}
	if start > 0 {
		out.Next = &start
	}
	return out, nil
}

// captureHistory retains only a bounded window while checking its original
// bytes and guarded identity. The resulting logical document is immutable;
// continuations do not reopen the source or reinterpret a later append.
func captureHistory(source WireSource, declared bool, request HistoryRequest) ([]byte, int64, bool, error) {
	out := result.AuditDefinitionHistory{State: "no-trail", Records: []result.AuditDefinitionRecord{}}
	size := int64(0)
	locked := false
	if declared {
		files, sizes, held, err := fssecure.OpenSnapshot(source.Open[0])
		if errors.Is(err, fs.ErrNotExist) {
			out.State = "no-records"
		} else if err != nil {
			return nil, 0, false, ErrWireRead
		} else {
			defer files[0].Close()
			size, locked = sizes[0], held
			start, end, err := historyRange(size, request.Before)
			if err != nil {
				return nil, 0, false, err
			}
			data := make([]byte, end-start)
			if len(data) > 0 {
				if _, err := files[0].ReadAt(data, start); err != nil {
					return nil, 0, false, ErrWireRead
				}
			}
			identity, err := files[0].Stat()
			if err != nil {
				return nil, 0, false, ErrWireRead
			}
			current, currentSizes, _, err := fssecure.OpenSnapshot(source.Open[0])
			if err != nil {
				return nil, 0, false, ErrWireChanged
			}
			defer current[0].Close()
			named, err := current[0].Stat()
			if err != nil || !os.SameFile(identity, named) || currentSizes[0] < size {
				return nil, 0, false, ErrWireChanged
			}
			hash := sha256.New()
			n, err := io.CopyBuffer(hash, io.NewSectionReader(current[0], start, end-start), make([]byte, 32<<10))
			digest := sha256.Sum256(data)
			if err != nil || n != int64(len(data)) || !bytes.Equal(hash.Sum(nil), digest[:]) {
				return nil, 0, false, ErrWireChanged
			}
			out, err = historyWindow(data, start, request)
			if err != nil {
				return nil, 0, false, err
			}
		}
	}
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(out); err != nil || int64(encoded.Len()) > MaxHistoryProjectionBytes {
		return nil, 0, false, ErrHistoryProjection
	}
	return encoded.Bytes(), size, locked, nil
}

// HistoryPage shares export capacity, lifetime, authority binding and retry
// discipline with raw listing, but refuses cursors belonging to raw exports.
func (p *Pager) HistoryPage(source WireSource, declared bool, cursor string, request *HistoryRequest, pageBytes int) (result.AuditHistoryPage, error) {
	if pageBytes < 1 || pageBytes > MaxWirePage {
		return result.AuditHistoryPage{}, ErrWirePage
	}
	if cursor == "" {
		if request == nil || (request.Kind != "pack" && request.Kind != "graph") || len(request.ID) == 0 || len(request.ID) > 2048 || !utf8.ValidString(request.ID) || request.Before < -1 {
			return result.AuditHistoryPage{}, ErrHistoryArguments
		}
	} else if request != nil {
		return result.AuditHistoryPage{}, ErrHistoryArguments
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	p.expire(now)
	var e *wireExport
	if cursor != "" {
		parts := strings.Split(cursor, ".")
		if len(parts) != 2 || len(parts[0]) != 64 || len(parts[1]) > 20 {
			return result.AuditHistoryPage{}, ErrWireCursor
		}
		sequence, err := strconv.ParseUint(parts[1], 10, 64)
		e = p.exports[parts[0]]
		if err != nil || e == nil || e.history == nil || strconv.FormatUint(sequence, 10) != parts[1] {
			return result.AuditHistoryPage{}, ErrWireCursor
		}
		if e.authority != source.Authority {
			delete(p.exports, e.id)
			return result.AuditHistoryPage{}, ErrWireChanged
		}
		if cursor == e.previous && e.historyLast != nil {
			return *e.historyLast, nil
		}
		if e.complete || sequence != e.sequence {
			return result.AuditHistoryPage{}, ErrWireCursor
		}
	} else {
		if err := p.makeRoom(); err != nil {
			return result.AuditHistoryPage{}, err
		}
		data, size, locked, err := captureHistory(source, declared, *request)
		if err != nil {
			return result.AuditHistoryPage{}, err
		}
		var token [32]byte
		_, _ = rand.Read(token[:])
		digest := sha256.Sum256(data)
		e = &wireExport{
			authority: source.Authority, expires: now.Add(WireExportLifetime), id: hex.EncodeToString(token[:]),
			locked: locked, history: data, historyDigest: "sha256:" + hex.EncodeToString(digest[:]), historySourceBytes: size,
		}
	}
	end := min(int64(len(e.history)), e.offset+int64(pageBytes))
	page := result.AuditHistoryPage{
		OutputVersion: result.OutputVersion, Tool: result.CurrentTool(), Command: "mcp experimental_list_audit_history", AuditHistoryVersion: "1",
		SnapshotBetweenWrites: e.locked, SourceBytes: e.historySourceBytes, ProjectionBytes: int64(len(e.history)), ProjectionDigest: e.historyDigest,
		Offset: e.offset, DataBase64: base64.StdEncoding.EncodeToString(e.history[e.offset:end]), Complete: end == int64(len(e.history)),
	}
	e.offset = end
	e.previous = cursor
	e.sequence++
	e.complete = page.Complete
	if !page.Complete {
		page.NextCursor = e.id + "." + strconv.FormatUint(e.sequence, 10)
	}
	e.historyLast = &page
	p.exports[e.id] = e
	return page, nil
}
