package audit

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/fssecure"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

const (
	WireVersion        = "1"
	MaxWireFileBytes   = 64 << 20
	DefaultWirePage    = 64 << 10
	MaxWirePage        = 256 << 10
	MaxWireRecord      = 64 << 10
	MaxWireRecords     = 128
	MaxWireExports     = 8
	WireExportLifetime = 5 * time.Minute
)

// Wire refusals are finite and contain no paths, key settings or retained data.
var (
	ErrWireRead     = errors.New("The configured audit files could not be read as contained regular files.")
	ErrWireLimit    = errors.New("An audit file exceeds the 64 MiB wire snapshot limit; no complete export was produced.")
	ErrWireChanged  = errors.New("The audit snapshot changed; discard this export and start again.")
	ErrWireCursor   = errors.New("The audit cursor is unknown, expired or no longer resumable; start a new export.")
	ErrWireCapacity = errors.New("Eight audit exports are already active; finish one or retry after its five-minute lifetime.")
	ErrWirePage     = errors.New("The audit page_bytes must be an integer from 1 to 262144.")
)

// WireSource carries root-authorized openers in evaluations/signatures/stamps
// order. Authority binds a cursor to the current project declaration; it is
// never returned. Openers must re-resolve their same guarded name on each call.
// A missing file returns fs.ErrNotExist, never an unguarded fallback path.
type WireSource struct {
	Authority string
	Open      [3]func() (*os.File, error)
}

type wireSnapshot struct {
	files  [3]*os.File
	sizes  [3]int64
	locked bool
}

func (s *wireSnapshot) close() {
	for _, file := range s.files {
		if file != nil {
			file.Close()
		}
	}
}

// openWireSnapshot retains the existing trail/sidecar between-writes size
// capture. Stamps remain independently locked, as for CLI verification. When
// evaluations is absent there is no trail lock and the result says so.
func openWireSnapshot(source WireSource) (wireSnapshot, error) {
	optional := func(open func() (*os.File, error)) func() (*os.File, error) {
		return func() (*os.File, error) {
			f, err := open()
			if errors.Is(err, fs.ErrNotExist) {
				return nil, nil
			}
			return f, err
		}
	}
	trail, err := source.Open[0]()
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return wireSnapshot{}, ErrWireRead
	}
	if trail != nil {
		trail.Close()
		files, sizes, locked, err := fssecure.OpenSnapshot(source.Open[0], optional(source.Open[1]), optional(source.Open[2]))
		if err != nil {
			return wireSnapshot{}, ErrWireRead
		}
		return wireSnapshot{files: [3]*os.File{files[0], files[1], files[2]}, sizes: [3]int64{sizes[0], sizes[1], sizes[2]}, locked: locked}, nil
	}
	var snapshot wireSnapshot
	for i := 1; i < 3; i++ {
		file, err := optional(source.Open[i])()
		if err != nil {
			snapshot.close()
			return wireSnapshot{}, ErrWireRead
		}
		snapshot.files[i] = file
		if file != nil {
			info, err := file.Stat()
			if err != nil {
				snapshot.close()
				return wireSnapshot{}, ErrWireRead
			}
			snapshot.sizes[i] = info.Size()
		}
	}
	return snapshot, nil
}

type wireExport struct {
	authority  string
	expires    time.Time
	info       [3]os.FileInfo
	metadata   []result.AuditFile
	blocks     [3][][sha256.Size]byte
	locked     bool
	id         string
	sequence   uint64
	previous   string
	last       *result.AuditPage
	complete   bool
	file       int
	offset     int64
	cache      []byte
	cacheBlock int64
	line       []byte
	lineHash   hash.Hash
	lineOffset int64
	lineBytes  int64
}

// Pager owns at most eight short-lived exports. Each retains fixed block hashes,
// one block cache, one bounded line and the previous response for retry. It holds
// no file descriptors between calls. A mutex serializes cursor advancement.
type Pager struct {
	mu      sync.Mutex
	exports map[string]*wireExport
	now     func() time.Time
}

func NewPager() *Pager {
	return &Pager{exports: make(map[string]*wireExport), now: time.Now}
}

// Page transports the captured prefixes in fixed file order. Initial hashing,
// block reads and final revalidation each take one linear pass. Identity and
// minimum size are checked per call. A previously served block changed in place
// may only be detected at finalization: intermediate pages are provisional and
// must be discarded if the export never completes successfully.
func (p *Pager) Page(source WireSource, cursor string, pageBytes int) (result.AuditPage, error) {
	if pageBytes < 1 || pageBytes > MaxWirePage {
		return result.AuditPage{}, ErrWirePage
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	for id, export := range p.exports {
		if !now.Before(export.expires) {
			delete(p.exports, id)
		}
	}
	var export *wireExport
	if cursor != "" {
		parts := strings.Split(cursor, ".")
		if len(parts) != 2 || len(parts[0]) != 64 || len(parts[1]) > 20 {
			return result.AuditPage{}, ErrWireCursor
		}
		sequence, err := strconv.ParseUint(parts[1], 10, 64)
		export = p.exports[parts[0]]
		if err != nil || export == nil || strconv.FormatUint(sequence, 10) != parts[1] {
			return result.AuditPage{}, ErrWireCursor
		}
		if export.authority != source.Authority {
			delete(p.exports, export.id)
			return result.AuditPage{}, ErrWireChanged
		}
		// Only the latest answered cursor is replayable. Its immutable response
		// does not reread files or advance the export a second time.
		if cursor == export.previous && export.last != nil {
			return *export.last, nil
		}
		if export.complete || sequence != export.sequence {
			return result.AuditPage{}, ErrWireCursor
		}
	} else if len(p.exports) >= MaxWireExports {
		// Completed exports retain their last response until expiry, but never
		// prevent a new export from using an otherwise free active slot.
		oldest := ""
		for id, e := range p.exports {
			if e.complete && (oldest == "" || e.expires.Before(p.exports[oldest].expires)) {
				oldest = id
			}
		}
		if oldest == "" {
			return result.AuditPage{}, ErrWireCapacity
		}
		delete(p.exports, oldest)
	}
	snapshot, err := openWireSnapshot(source)
	if err != nil {
		if export != nil {
			delete(p.exports, export.id)
		}
		return result.AuditPage{}, err
	}
	defer snapshot.close()
	if export == nil {
		export, err = captureWire(snapshot, source.Authority, now)
		if err != nil {
			return result.AuditPage{}, err
		}
		var token [32]byte
		_, _ = rand.Read(token[:])
		export.id = hex.EncodeToString(token[:])
	}
	fail := func(err error) (result.AuditPage, error) {
		delete(p.exports, export.id)
		return result.AuditPage{}, err
	}
	if err := export.checkIdentity(snapshot); err != nil {
		return fail(err)
	}
	i, offset := export.file, export.offset
	data, records, err := export.readPage(snapshot.files[i], pageBytes)
	if err != nil {
		return fail(err)
	}
	fileComplete := export.offset == export.metadata[i].Bytes
	complete := fileComplete && i == 2
	// Reopening catches a name replaced while its old handle was read.
	current, err := openWireSnapshot(source)
	if err != nil {
		return fail(ErrWireChanged)
	}
	defer current.close()
	if err := export.checkIdentity(current); err != nil {
		return fail(err)
	}
	if complete {
		if err := export.checkAllBlocks(current); err != nil {
			return fail(err)
		}
		// Final hashing can take longer than a byte page. Check the names
		// once more afterwards so replacement during that pass is refused.
		final, err := openWireSnapshot(source)
		if err != nil {
			return fail(ErrWireChanged)
		}
		err = export.checkIdentity(final)
		final.close()
		if err != nil {
			return fail(err)
		}
	}
	page := result.AuditPage{
		OutputVersion: result.OutputVersion, Tool: result.CurrentTool(), Command: "mcp experimental_list_audit",
		AuditWireVersion: WireVersion, Files: append([]result.AuditFile(nil), export.metadata...),
		SnapshotBetweenWrites: export.locked, File: export.metadata[i].Name, Offset: offset,
		DataBase64: base64.StdEncoding.EncodeToString(data), Records: records, PendingLine: export.lineBytes > 0,
		FileComplete: fileComplete, Complete: complete,
	}
	export.previous = cursor
	export.sequence++
	export.complete = complete
	if !complete {
		if fileComplete {
			export.file++
			export.offset, export.lineOffset, export.lineBytes = 0, 0, 0
			export.cache, export.line = nil, nil
			export.cacheBlock = -1
			export.lineHash = sha256.New()
		}
		page.NextCursor = export.id + "." + strconv.FormatUint(export.sequence, 10)
	}
	export.last = &page
	p.exports[export.id] = export
	return page, nil
}

func captureWire(snapshot wireSnapshot, authority string, now time.Time) (*wireExport, error) {
	export := &wireExport{authority: authority, expires: now.Add(WireExportLifetime), locked: snapshot.locked, cacheBlock: -1, lineHash: sha256.New()}
	for i, name := range []string{FileName, SidecarName, StampsName} {
		meta := result.AuditFile{Name: name, Present: snapshot.files[i] != nil, Bytes: snapshot.sizes[i]}
		if meta.Bytes > MaxWireFileBytes {
			return nil, ErrWireLimit
		}
		if meta.Present {
			var err error
			export.info[i], err = snapshot.files[i].Stat()
			if err != nil {
				return nil, ErrWireRead
			}
			full := sha256.New()
			for offset := int64(0); offset < meta.Bytes; offset += DefaultWirePage {
				data := make([]byte, min(int64(DefaultWirePage), meta.Bytes-offset))
				if _, err := snapshot.files[i].ReadAt(data, offset); err != nil {
					return nil, ErrWireRead
				}
				export.blocks[i] = append(export.blocks[i], sha256.Sum256(data))
				_, _ = full.Write(data)
			}
			meta.Digest = "sha256:" + hex.EncodeToString(full.Sum(nil))
		}
		export.metadata = append(export.metadata, meta)
	}
	return export, nil
}

func (e *wireExport) checkIdentity(snapshot wireSnapshot) error {
	for i, meta := range e.metadata {
		file := snapshot.files[i]
		if meta.Present != (file != nil) {
			return ErrWireChanged
		}
		if file == nil {
			continue
		}
		info, err := file.Stat()
		if err != nil || !os.SameFile(e.info[i], info) || info.Size() < meta.Bytes {
			return ErrWireChanged
		}
	}
	return nil
}

func (e *wireExport) block(file *os.File, i int, block int64) ([]byte, error) {
	offset := block * DefaultWirePage
	data := make([]byte, min(int64(DefaultWirePage), e.metadata[i].Bytes-offset))
	if _, err := file.ReadAt(data, offset); err != nil || sha256.Sum256(data) != e.blocks[i][block] {
		return nil, ErrWireChanged
	}
	return data, nil
}

func (e *wireExport) checkAllBlocks(snapshot wireSnapshot) error {
	for i, blocks := range e.blocks {
		for block := range blocks {
			if _, err := e.block(snapshot.files[i], i, int64(block)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *wireExport) readPage(file *os.File, limit int) ([]byte, []result.AuditPageRecord, error) {
	data := make([]byte, 0, limit)
	records := []result.AuditPageRecord{}
	for len(data) < limit && e.offset < e.metadata[e.file].Bytes && len(records) < MaxWireRecords {
		block := e.offset / DefaultWirePage
		if e.cacheBlock != block {
			var err error
			e.cache, err = e.block(file, e.file, block)
			if err != nil {
				return nil, nil, err
			}
			e.cacheBlock = block
		}
		remaining := e.cache[e.offset%DefaultWirePage:]
		n := min(len(remaining), limit-len(data))
		remaining = remaining[:n]
		if newline := bytes.IndexByte(remaining, '\n'); newline >= 0 {
			remaining = remaining[:newline+1]
		}
		data = append(data, remaining...)
		e.offset += int64(len(remaining))
		newline := remaining[len(remaining)-1] == '\n'
		linePart := remaining
		if newline {
			linePart = linePart[:len(linePart)-1]
		}
		_, _ = e.lineHash.Write(linePart)
		e.lineBytes += int64(len(linePart))
		if e.lineBytes <= MaxWireRecord {
			e.line = append(e.line, linePart...)
		} else {
			e.line = nil
		}
		if newline {
			records = append(records, e.finishLine(false))
			e.lineOffset = e.offset
		}
	}
	if e.offset == e.metadata[e.file].Bytes && e.lineBytes > 0 {
		records = append(records, e.finishLine(true))
	}
	return data, records, nil
}

func (e *wireExport) finishLine(incomplete bool) result.AuditPageRecord {
	record := result.AuditPageRecord{Offset: e.lineOffset, Bytes: e.lineBytes, Digest: "sha256:" + hex.EncodeToString(e.lineHash.Sum(nil)), Status: "malformed"}
	if e.lineBytes > MaxWireRecord {
		record.Status = "oversized"
	} else {
		record.RawLineBase64 = base64.StdEncoding.EncodeToString(e.line)
		trimmed := bytes.TrimSpace(e.line)
		if len(trimmed) > 0 && trimmed[0] == '{' && utf8.Valid(e.line) && json.Valid(e.line) {
			if _, unpaired := carrier.UnpairedSurrogateEscape(e.line); !unpaired {
				record.Status, record.Members = "parsed", append(json.RawMessage(nil), e.line...)
			}
		}
	}
	if incomplete {
		record.Status, record.Members = "incomplete", nil
	}
	e.line, e.lineBytes = nil, 0
	e.lineHash.Reset()
	return record
}
