package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/audit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/carrier"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/evaluation"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/lock"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/project"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// A batch of experimental evaluate (ADR-0085) is many items read as JSON Lines
// and evaluated by one process against one pack, admitted once. Each item is
// one line: one JSON object of exactly {"facts": <facts document>, "id":
// <string>, "evidence": <evidence document>}, facts required. Each result is
// one line: {"line", "id", "run", "exitCode", "result"}, in input order, where
// result is the exact JSON a single experimental evaluate --format json writes
// for that item, exitCode is the code it exits with, and run, on every line of
// a batch that records, is the run id each of the batch's records carries.
const (
	// DefaultBatchMaxItems is --max-items when it is not given.
	DefaultBatchMaxItems = 100000
	// MaxBatchIDBytes bounds an item's id, in bytes of its decoded text.
	MaxBatchIDBytes = 256
	// maxBatchLineBytes bounds one line: a facts document and an evidence
	// document at the byte limit each, an id, and the members' framing. A
	// longer line is refused without being held.
	maxBatchLineBytes = 2*carrier.HardMaxBytes + 64<<10
)

// The codes a batch adds.
const (
	batchInvocationCode = "JPS-INVOCATION-BATCH"
	maxItemsCode        = "JPS-INVOCATION-MAX-ITEMS"
	batchItemCode       = "JPS-INVOCATION-BATCH-ITEM"
	batchItemsCode      = "JPS-RESOURCE-BATCH-ITEMS"
	batchLineCode       = "JPS-RESOURCE-BATCH-LINE-BYTE-LIMIT"
)

// A chunk is the items whose records are appended together, under one hold
// of the trail's lock and one sync, before any of their results is written.
// It closes at batchChunkItems items or batchChunkBytes bytes of input lines,
// whichever comes first, and at the end of the input. They are variables so a
// test can make a chunk small.
var (
	batchChunkItems       = 256
	batchChunkBytes int64 = 32 << 20
)

// batchAppend appends one chunk's records. It is a variable so a test can
// make an append fail after its records were written, as a failed sync or
// signature write does.
var batchAppend = (*audit.Writer).AppendAll

// batchUnconfirmed opens the refusal of an evaluation whose chunk's append
// failed. An append can fail after writing some or all of its lines, so the
// record is not confirmed rather than known not to be written; the single
// run's message follows it, saying what the append reported.
const batchUnconfirmed = "This item's record is not confirmed written, and the batch stopped: the append of its chunk of records failed, and the trail may hold all of that chunk's records, the first of them, or none, under the batch's run id. The append reported: "

// batchRunNote is the line a batch that records writes to standard error
// before it appends its first record, so its run id is known even when it
// ends before writing a result.
const batchRunNote = "note: the records of this batch carry run %s\n"

// batchRun is what one batch needs from the invocation that reached it: the
// checks single evaluate makes once per run, made once.
type batchRun struct {
	evaluator *evaluation.Engine
	pack      []byte
	contract  result.EvaluatorContract
	options   evaluation.Options
	rehearsal bool
	loaded    *project.Project
	writer    *audit.Writer
	unsigned  *project.Unsigned
	reviewed  *bool
	set       *lock.Set
	maxItems  int
}

// batchLine is one line of a batch's output. Run is the batch's run id on
// every line of a batch that records and is absent from one that records
// nothing.
type batchLine struct {
	Line     int64           `json:"line"`
	ID       *string         `json:"id,omitempty"`
	Run      string          `json:"run,omitempty"`
	ExitCode int             `json:"exitCode"`
	Result   json.RawMessage `json:"result"`
}

// batchItem is one item, parsed from its line.
type batchItem struct {
	id               *string
	facts            []byte
	evidence         []byte
	evidenceSupplied bool
}

// pendingResult is one item's result, held until its chunk is recorded.
type pendingResult struct {
	line int64
	id   *string
	// evaluated is the item's evaluation when it produced one; failure is
	// the envelope when it did not.
	evaluated *result.Evaluation
	failure   any
	exitCode  int
	// recorded says the item's record is in the chunk's append.
	recorded bool
}

// runBatch evaluates every item of the batch input against the admitted
// pack and writes one result line per item, in input order. Its exit code is
// 0 when every item produced a disposition, 1 when the input was read to its
// end and some item was refused, and 4 when the batch stopped: at the item
// ceiling, at an input that could not be read, at a chunk whose append failed,
// or at output that could not be written. What it reports is a lower bound on
// what it recorded: after a failed append or an interruption the trail may
// hold up to one chunk more, under the run its lines name (ADR-0085 §8).
func (a *App) runBatch(run batchRun, input io.Reader) error {
	command := "experimental evaluate"
	admitted := run.evaluator.AdmitPack(run.pack)
	// The pack half of the preflight is the same for every item: a pack the
	// engine refuses is refused once, before any item is read, with the
	// envelope and exit code single evaluate gives for it.
	if failure := admitted.PackFailure(run.options); failure != nil {
		return a.evaluationFailure(command, "json", failure, run.contract)
	}
	packDigest := audit.Digest(run.pack)
	recording := !run.rehearsal && run.writer != nil
	// Every line of a batch that records names the run its records carry, so
	// a caller can find them in the trail, those an append that failed or an
	// interruption left unreported included (ADR-0085 §8).
	runID := ""
	if recording {
		runID = run.writer.Run()
	}
	noted := false
	reader := newBatchReader(input)
	// A chunk's results are written together, after its records are synced.
	out := bufio.NewWriterSize(a.out, 64<<10)
	encoder := json.NewEncoder(out)
	encoder.SetEscapeHTML(false)

	var pending []pendingResult
	var records []audit.Record
	var pendingBytes int64
	refused := false
	// flush appends the chunk's records and then writes its results. A
	// result is written only after its record is durable. An append that
	// fails may have written some or all of the chunk's records, so each
	// recorded item is reported as not confirmed, with the code a single run
	// reports, and the batch stops.
	flush := func() (int, bool) {
		stop := 0
		if len(records) > 0 {
			// The note precedes every record, so a batch that ends before
			// its first line still named its run. It is said as a note is
			// elsewhere: a standard error that cannot take it fails nothing.
			if !noted {
				noted = true
				_, _ = fmt.Fprintf(a.errOut, batchRunNote, runID)
			}
			if err := batchAppend(run.writer, records); err != nil {
				failure := result.NewOperationalResult(command, "error", audit.FailureCode, batchUnconfirmed+audit.FailureMessageFor(err))
				for index := range pending {
					if pending[index].recorded {
						pending[index].evaluated, pending[index].failure, pending[index].exitCode = nil, failure, result.ExitIO
					}
				}
				stop = result.ExitIO
			}
		}
		var reason *project.Unsigned
		if stop == 0 && !run.rehearsal {
			reason = run.loaded.UnsignedAfter(run.writer, run.unsigned)
		}
		for _, held := range pending {
			var value any = held.failure
			if held.evaluated != nil {
				if reason != nil {
					held.evaluated.Diagnostics = append(held.evaluated.Diagnostics, reason.Diagnostic())
				}
				value = *held.evaluated
			}
			if held.exitCode != result.ExitSuccess {
				refused = true
			}
			if err := writeBatchLine(encoder, held.line, held.id, runID, held.exitCode, value); err != nil {
				return result.ExitIO, true
			}
		}
		if err := out.Flush(); err != nil {
			return result.ExitIO, true
		}
		// Nothing a written chunk held stays reachable from the next one.
		clear(pending)
		clear(records)
		pending, records, pendingBytes = pending[:0], records[:0], 0
		return stop, stop != 0
	}
	// stopWith flushes what is held and writes the line that stopped the
	// batch, unless the flush itself stopped it.
	stopWith := func(line int64, exitCode int, code, message string) error {
		if stop, stopped := flush(); stopped {
			return &handledExit{code: stop}
		}
		if err := writeBatchLine(encoder, line, nil, runID, exitCode, result.NewOperationalResult(command, "error", code, message)); err != nil {
			return &handledExit{code: result.ExitIO}
		}
		if err := out.Flush(); err != nil {
			return &handledExit{code: result.ExitIO}
		}
		return &handledExit{code: exitCode}
	}

	for {
		line, number, tooLong, err := reader.next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return stopWith(number, result.ExitIO, "JPS-INPUT-READ", fmt.Sprintf("The batch input could not be read at line %d; the items before it are reported and no later line is read.", number))
		}
		if number > int64(run.maxItems) {
			return stopWith(number, result.ExitIO, batchItemsCode, fmt.Sprintf("The batch holds more than %d items (--max-items); the first %d are reported and no later line is read.", run.maxItems, run.maxItems))
		}
		held := pendingResult{line: number}
		switch {
		case tooLong:
			held.failure = result.NewOperationalResult(command, "error", batchLineCode, fmt.Sprintf("The line is longer than %d bytes, the longest item a batch reads: a facts and an evidence document at the %d-byte limit each, and an id.", maxBatchLineBytes, carrier.HardMaxBytes))
			held.exitCode = result.ExitIO
		default:
			// The id is echoed for a line that is an item; a line that is
			// not is named by its number alone.
			item, problem := parseBatchItem(line)
			if problem != "" {
				held.failure = result.NewOperationalResult(command, "error", batchItemCode, problem)
				held.exitCode = result.ExitInvocation
				break
			}
			held.id = item.id
			options := run.options
			options.EvidenceSupplied = item.evidenceSupplied
			output, failure := run.evaluator.EvaluateAdmitted(admitted, item.facts, item.evidence, options)
			if failure != nil {
				held.failure = evaluationFailureEnvelope(command, failure, run.contract)
				held.exitCode = failure.ExitCode
				break
			}
			// The payload says what a single evaluate's says about the
			// reviewed set (ADR-0044); a rehearsal consulted none.
			if run.rehearsal {
				output.Rehearsal = true
			} else {
				output.Reviewed = run.reviewed
				if run.reviewed != nil && *run.reviewed {
					output.ReviewedSet = run.set.Provenance()
				}
			}
			if recording {
				record, err := audit.EvaluationRecordFor(output, audit.Inputs{
					Facts:            item.facts,
					Evidence:         item.evidence,
					EvidenceSupplied: item.evidenceSupplied,
				}, nil, packDigest, nil)
				if err != nil {
					held.failure = result.NewOperationalResult(command, "error", audit.FailureCode, audit.FailureMessageFor(err))
					held.exitCode = result.ExitIO
					break
				}
				records = append(records, record)
				held.recorded = true
			}
			held.evaluated = &output
		}
		pending = append(pending, held)
		pendingBytes += int64(len(line))
		// Where nothing is recorded a result waits for nothing, and is
		// written as soon as it exists.
		if !recording || len(pending) >= batchChunkItems || pendingBytes >= batchChunkBytes {
			if stop, stopped := flush(); stopped {
				return &handledExit{code: stop}
			}
		}
	}
	if stop, stopped := flush(); stopped {
		return &handledExit{code: stop}
	}
	if refused {
		return &handledExit{code: result.ExitInvalid}
	}
	return nil
}

// writeBatchLine writes one result line. value is encoded exactly as single
// evaluate's --format json writes it, without --pretty, and carried as it was
// encoded; run is "" for a batch that records nothing.
func writeBatchLine(encoder *json.Encoder, line int64, id *string, run string, exitCode int, value any) error {
	var encoded bytes.Buffer
	if err := writeJSON(&encoded, value, false); err != nil {
		return err
	}
	return encoder.Encode(batchLine{Line: line, ID: id, Run: run, ExitCode: exitCode, Result: bytes.TrimSuffix(encoded.Bytes(), []byte("\n"))})
}

// parseBatchItem reads one line as an item: one JSON object with the member
// facts and, optionally, id and evidence, each once and spelled exactly as
// written here, with no escape in the name, and nothing after it. The facts
// and evidence values are located, not decoded (itemScanner), and kept as
// their exact bytes, which the engine then admits as it admits a document read
// from a file, so every refusal of those documents, the depth and byte limits
// included, is the one single evaluate gives for the same bytes. The sentence
// returned for a line that is not an item names no value from it.
func parseBatchItem(line []byte) (batchItem, string) {
	const shape = `The line must be one JSON object of exactly {"facts": <facts document>}, with optional "id" (a string) and "evidence" (an evidence document), each member once: `
	var item batchItem
	scanner := itemScanner{line: line}
	scanner.space()
	if !scanner.take('{') {
		return item, shape + "it is not a JSON object."
	}
	seen := map[string]bool{}
	scanner.space()
	if !scanner.take('}') {
		for {
			scanner.space()
			name, ok := scanner.name()
			if !ok {
				return item, shape + "it is not a JSON object."
			}
			if name != "facts" && name != "id" && name != "evidence" {
				return item, shape + "it has a member that is not one of these."
			}
			if seen[name] {
				return item, shape + fmt.Sprintf("it has %q twice.", name)
			}
			seen[name] = true
			scanner.space()
			if !scanner.take(':') {
				return item, shape + "it is not a JSON object."
			}
			scanner.space()
			value, ok := scanner.value()
			if !ok {
				return item, shape + "it is not a JSON object."
			}
			switch name {
			case "facts":
				item.facts = value
			case "evidence":
				item.evidence, item.evidenceSupplied = value, true
			case "id":
				id, problem := batchID(value)
				if problem != "" {
					return item, shape + problem
				}
				item.id = &id
			}
			scanner.space()
			if scanner.take(',') {
				continue
			}
			if scanner.take('}') {
				break
			}
			return item, shape + "it is not a JSON object."
		}
	}
	scanner.space()
	if scanner.at < len(line) {
		return item, shape + "something follows the object on the line."
	}
	if !seen["facts"] {
		return item, shape + `it has no "facts".`
	}
	return item, ""
}

// itemScanner locates the parts of an item's line without decoding the
// documents in it, so it has no depth of nesting of its own to refuse: what a
// document's depth is, and every other question about the document, is the
// engine's. A value is delimited by the JSON grammar's quotes and brackets: a
// string runs to its closing quote, an object or an array to the bracket that
// closes the last one open, of either kind, and anything else to the next
// whitespace or structural character. Whether the bytes so delimited are a
// JSON document is then the engine's to say, as it is for a file's.
type itemScanner struct {
	line []byte
	at   int
}

// space passes JSON whitespace.
func (s *itemScanner) space() {
	for s.at < len(s.line) {
		switch s.line[s.at] {
		case ' ', '\t', '\r', '\n':
			s.at++
		default:
			return
		}
	}
}

// take passes the byte want when it is next.
func (s *itemScanner) take(want byte) bool {
	if s.at < len(s.line) && s.line[s.at] == want {
		s.at++
		return true
	}
	return false
}

// text passes a string from its opening quote through its closing one, a
// backslash escaping the byte after it.
func (s *itemScanner) text() bool {
	s.at++
	for s.at < len(s.line) {
		switch s.line[s.at] {
		case '\\':
			s.at += 2
			continue
		case '"':
			s.at++
			return true
		}
		s.at++
	}
	return false
}

// name passes a member name and answers it as it is written between its
// quotes, escapes undecoded, so only a name spelled out byte for byte is one
// of an item's.
func (s *itemScanner) name() (string, bool) {
	if s.at >= len(s.line) || s.line[s.at] != '"' {
		return "", false
	}
	start := s.at
	if !s.text() {
		return "", false
	}
	return string(s.line[start+1 : s.at-1]), true
}

// value passes one value and answers its bytes, or false when no value
// starts here or the line ends inside one.
func (s *itemScanner) value() ([]byte, bool) {
	if s.at >= len(s.line) {
		return nil, false
	}
	start := s.at
	switch s.line[s.at] {
	case '"':
		if !s.text() {
			return nil, false
		}
		return s.line[start:s.at], true
	case '{', '[':
		open := 0
		for s.at < len(s.line) {
			switch s.line[s.at] {
			case '"':
				if !s.text() {
					return nil, false
				}
				continue
			case '{', '[':
				open++
			case '}', ']':
				open--
			}
			s.at++
			if open == 0 {
				return s.line[start:s.at], true
			}
		}
		return nil, false
	case '}', ']', ',', ':':
		return nil, false
	}
	for s.at < len(s.line) && !structural(s.line[s.at]) {
		s.at++
	}
	return s.line[start:s.at], true
}

// structural says whether b ends a value that is not a string, an object or
// an array.
func structural(b byte) bool {
	switch b {
	case ' ', '\t', '\r', '\n', '{', '}', '[', ']', ',', ':', '"':
		return true
	}
	return false
}

// batchID reads an item's id: a JSON string of well-formed UTF-8 whose
// escapes each name a Unicode character, 1 to MaxBatchIDBytes bytes once
// decoded. An escaped half of a surrogate pair without its other half is
// refused rather than taken as U+FFFD, which is not the id the line gave.
func batchID(raw []byte) (string, string) {
	var id string
	if len(raw) == 0 || raw[0] != '"' || !utf8.Valid(raw) || escapesLoneSurrogate(raw) || json.Unmarshal(raw, &id) != nil {
		return "", `its "id" is not a string of well-formed UTF-8 whose every escape names a Unicode character.`
	}
	if id == "" || len(id) > MaxBatchIDBytes {
		return "", fmt.Sprintf(`its "id" is not 1 to %d bytes long.`, MaxBatchIDBytes)
	}
	return id, ""
}

// escapesLoneSurrogate says whether a JSON string's text escapes half of a
// UTF-16 surrogate pair that no escape of the other half follows at once.
// Text that is not a JSON string at all is json.Unmarshal's to refuse.
func escapesLoneSurrogate(text []byte) bool {
	for index := 0; index < len(text); index++ {
		if text[index] != '\\' {
			continue
		}
		index++
		if index >= len(text) || text[index] != 'u' {
			continue
		}
		unit, ok := escapedUnit(text, index+1)
		index += 4
		if !ok || !utf16.IsSurrogate(unit) {
			continue
		}
		if index+2 < len(text) && text[index+1] == '\\' && text[index+2] == 'u' {
			if low, ok := escapedUnit(text, index+3); ok && utf16.DecodeRune(unit, low) != unicode.ReplacementChar {
				index += 6
				continue
			}
		}
		return true
	}
	return false
}

// escapedUnit reads the four hexadecimal digits at text[at:] as one UTF-16
// code unit.
func escapedUnit(text []byte, at int) (rune, bool) {
	if at+4 > len(text) {
		return 0, false
	}
	unit, err := strconv.ParseUint(string(text[at:at+4]), 16, 16)
	if err != nil {
		return 0, false
	}
	return rune(unit), true
}

// batchReader reads a batch's input a line at a time, holding at most
// maxBatchLineBytes of any line. A line is the bytes before a newline, or the
// bytes after the last newline when there are any.
type batchReader struct {
	reader *bufio.Reader
	number int64
}

func newBatchReader(input io.Reader) *batchReader {
	return &batchReader{reader: bufio.NewReaderSize(input, 64<<10)}
}

// next returns the next line, its 1-based number, and whether it was longer
// than maxBatchLineBytes, in which case it is read to its end and not held.
// It returns io.EOF when no line is left, and any other read error with the
// number of the line it stopped in.
func (r *batchReader) next() ([]byte, int64, bool, error) {
	var line []byte
	tooLong := false
	started := false
	for {
		chunk, err := r.reader.ReadSlice('\n')
		if len(chunk) > 0 && !started {
			started = true
			r.number++
		}
		if !tooLong {
			// The newline that ends a line is not part of it, and a last
			// line without one is bounded as every other line is.
			length := int64(len(line) + len(chunk))
			if err == nil {
				length--
			}
			if length > maxBatchLineBytes {
				tooLong, line = true, nil
			} else {
				line = append(line, chunk...)
			}
		}
		switch {
		case err == nil:
			return bytes.TrimSuffix(line, []byte("\n")), r.number, tooLong, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			if !started {
				return nil, r.number, false, io.EOF
			}
			return line, r.number, tooLong, nil
		default:
			if !started {
				r.number++
			}
			return nil, r.number, false, err
		}
	}
}

// openBatch opens the batch input: standard input for "-", recorded as read
// as readInput records it, or one regular file an operator named.
func (a *App) openBatch(argument string) (io.Reader, func(), error) {
	if argument == "-" {
		if file, ok := a.in.(*os.File); ok {
			if info, err := file.Stat(); err == nil && info.Mode().IsRegular() {
				a.inputs = append(a.inputs, inputFile{info: info, what: "the batch input, read from standard input"})
			}
		}
		return a.in, func() {}, nil
	}
	file, err := a.openInput(argument, "the batch input")
	if err != nil {
		return nil, nil, err
	}
	return file, func() { file.Close() }, nil
}
