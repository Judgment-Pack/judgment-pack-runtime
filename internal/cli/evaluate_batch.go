package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
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
// one line: {"line", "id", "exitCode", "result"}, in input order, where result
// is the exact JSON a single experimental evaluate --format json writes for
// that item and exitCode is the code it exits with.
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

// batchLine is one line of a batch's output.
type batchLine struct {
	Line     int64           `json:"line"`
	ID       *string         `json:"id,omitempty"`
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
// end and some item was refused, and otherwise the code of the failure that
// stopped the batch, which is the last line's: the item ceiling (4), an input
// that could not be read (4), a record that could not be written (4), or
// output that could not be written (4).
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
	// result is written only after its record is durable: an append that
	// fails is reported as each recorded item's refusal, the one single
	// evaluate reports, and stops the batch.
	flush := func() (int, bool) {
		stop := 0
		if len(records) > 0 {
			if err := run.writer.AppendAll(records); err != nil {
				failure := result.NewOperationalResult(command, "error", audit.FailureCode, audit.FailureMessageFor(err))
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
			if err := writeBatchLine(encoder, held.line, held.id, held.exitCode, value); err != nil {
				return result.ExitIO, true
			}
		}
		if err := out.Flush(); err != nil {
			return result.ExitIO, true
		}
		pending, records, pendingBytes = pending[:0], records[:0], 0
		return stop, stop != 0
	}
	// stopWith flushes what is held and writes the line that stopped the
	// batch, unless the flush itself stopped it.
	stopWith := func(line int64, exitCode int, code, message string) error {
		if stop, stopped := flush(); stopped {
			return &handledExit{code: stop}
		}
		if err := writeBatchLine(encoder, line, nil, exitCode, result.NewOperationalResult(command, "error", code, message)); err != nil {
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
// encoded.
func writeBatchLine(encoder *json.Encoder, line int64, id *string, exitCode int, value any) error {
	var encoded bytes.Buffer
	if err := writeJSON(&encoded, value, false); err != nil {
		return err
	}
	return encoder.Encode(batchLine{Line: line, ID: id, ExitCode: exitCode, Result: bytes.TrimSuffix(encoded.Bytes(), []byte("\n"))})
}

// parseBatchItem reads one line as an item: one JSON object with the member
// facts and, optionally, id and evidence, each once and spelled exactly, and
// nothing after it. The facts and evidence values are kept as their exact
// bytes, which the engine then admits as it admits a document read from a
// file, so every refusal of those documents is the one single evaluate gives.
// The sentence returned for a line that is not an item names no value from it.
func parseBatchItem(line []byte) (batchItem, string) {
	const shape = `The line must be one JSON object of exactly {"facts": <facts document>}, with optional "id" (a string) and "evidence" (an evidence document), each member once: `
	var item batchItem
	decoder := json.NewDecoder(bytes.NewReader(line))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return item, shape + "it is not a JSON object."
	}
	seen := map[string]bool{}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return item, shape + "it is not a JSON object."
		}
		name, _ := key.(string)
		if name != "facts" && name != "id" && name != "evidence" {
			return item, shape + "it has a member that is not one of these."
		}
		if seen[name] {
			return item, shape + fmt.Sprintf("it has %q twice.", name)
		}
		seen[name] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
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
	}
	if closing, err := decoder.Token(); err != nil || closing != json.Delim('}') {
		return item, shape + "it is not a JSON object."
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return item, shape + "something follows the object on the line."
	}
	if !seen["facts"] {
		return item, shape + `it has no "facts".`
	}
	return item, ""
}

// batchID reads an item's id: a JSON string of well-formed UTF-8, 1 to
// MaxBatchIDBytes bytes once decoded.
func batchID(raw json.RawMessage) (string, string) {
	var id string
	if len(raw) == 0 || raw[0] != '"' || !utf8.Valid(raw) || json.Unmarshal(raw, &id) != nil {
		return "", `its "id" is not a string of well-formed UTF-8.`
	}
	if id == "" || len(id) > MaxBatchIDBytes {
		return "", fmt.Sprintf(`its "id" is not 1 to %d bytes long.`, MaxBatchIDBytes)
	}
	return id, ""
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
			if int64(len(line)+len(chunk)) > maxBatchLineBytes+1 {
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
