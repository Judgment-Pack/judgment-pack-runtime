package conformance

import (
	"errors"
	"slices"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/registry"
)

// Check 5 (references), RFC 0036 §5 as ADR-0099 decides it: for each string
// R at a secret position of the request and each output file, the check fails
// when the file holds a run of at least 32 consecutive characters of R (all
// of R, if R is shorter) that does not lie inside a complete occurrence of
// any string at a secret position of the request. The text's "a complete
// occurrence of R" would fail two whole references of one project, which
// share secret:gcp-sm:projects/<p>/secrets/; the decided reading is to be
// raised on spec #350.
//
// A run of all of a string of at most 32 characters is a complete occurrence
// of it, so only strings longer than 32 can fail, and only strings of 32 or
// more can hold a run that could. A secret position of a
// valid registry holds a secret reference (RFC 0035), which is ASCII, so a
// file's text and its bytes read as Latin-1 hold the same runs of it: the
// scan reads every file's bytes.
//
// The scan is linear in the files' bytes for each string that occurs in them:
// a table of every 32-byte window of every string finds where one starts to
// occur, and a suffix automaton of that string reads only the regions around
// those places, once for the string's complete occurrences and once for its
// runs. Its work is bounded (MaxReferenceWork); past the bound, or
// past MaxReferenceBytes of strings, the harness stops (EMIT-HARNESS-BOUND).

// The scan's bounds.
const (
	// MaxReferenceBytes bounds the distinct strings of 32 bytes or more the
	// secret positions of one request hold, together.
	MaxReferenceBytes = 1 << 20
	// MaxReferenceWork bounds the scan's steps for one vector.
	MaxReferenceWork = 1 << 32
)

// The bounds as the code applies them; a test lowers them.
var (
	maxReferenceBytes       = MaxReferenceBytes
	maxReferenceWork  int64 = MaxReferenceWork
)

// runLength is the shortest run of a string's characters that fails.
const runLength = 32

var errReferenceBound = errors.New("conformance: the references scan is past its bounds")

// target is one distinct string at a secret position, with the first
// position that holds it.
type target struct {
	value       []byte
	environment string
	entry       registry.EntryName
	pointer     string
}

// scanner holds the window table of a set of targets.
type scanner struct {
	targets []target
	// classes maps a window's bytes to the targets that hold it, each once.
	classes map[[runLength]byte][]int32
	// filter has the bit of each window's rolling hash set.
	filter []uint64
	work   int64
	// automata are the targets' suffix automata, each built once, when a
	// file first holds its string, and kept for the request's other files
	// and both passes. Together they are linear in the targets' bytes, which
	// MaxReferenceBytes bounds. builds counts them, for a test.
	automata map[int32]*automaton
	builds   int
}

const (
	hashBase   = 1099511628211
	filterBits = 24
)

// newScanner builds the window table of the strings of runLength or more,
// each once.
func newScanner(strings []target) (*scanner, error) {
	s := &scanner{classes: map[[runLength]byte][]int32{}, filter: make([]uint64, 1<<filterBits/64)}
	seen := map[string]bool{}
	total := 0
	for _, t := range strings {
		if len(t.value) < runLength || seen[string(t.value)] {
			continue
		}
		seen[string(t.value)] = true
		if total += len(t.value); total > maxReferenceBytes {
			return nil, errReferenceBound
		}
		index := int32(len(s.targets))
		s.targets = append(s.targets, t)
		for o := 0; o+runLength <= len(t.value); o++ {
			var window [runLength]byte
			copy(window[:], t.value[o:o+runLength])
			class := s.classes[window]
			if len(class) == 0 || class[len(class)-1] != index {
				s.classes[window] = append(class, index)
			}
			s.mark(hashOf(t.value[o : o+runLength]))
		}
	}
	return s, nil
}

func hashOf(window []byte) uint64 {
	var h uint64
	for _, b := range window {
		h = h*hashBase + uint64(b)
	}
	return h
}

func (s *scanner) mark(h uint64) {
	s.filter[h>>(64-filterBits)/64] |= 1 << (h >> (64 - filterBits) % 64)
}
func (s *scanner) marked(h uint64) bool {
	return s.filter[h>>(64-filterBits)/64]&(1<<(h>>(64-filterBits)%64)) != 0
}

func (s *scanner) spend(steps int) error {
	s.work += int64(steps)
	if s.work > maxReferenceWork {
		return errReferenceBound
	}
	return nil
}

// region is a span of a file around the places a target's windows occur.
type region struct{ start, end, last int }

// scan returns the targets whose strings the file holds a failing run of.
func (s *scanner) scan(data []byte) ([]int32, error) {
	if len(s.targets) == 0 || len(data) < runLength {
		return nil, nil
	}
	if err := s.spend(len(data)); err != nil {
		return nil, err
	}
	var top uint64 = 1
	for range runLength - 1 {
		top *= hashBase
	}
	open := map[int32]*region{}
	closed := map[int32][]region{}
	h := hashOf(data[:runLength])
	for p := 0; ; p++ {
		if s.marked(h) {
			var window [runLength]byte
			copy(window[:], data[p:p+runLength])
			class := s.classes[window]
			if err := s.spend(len(class)); err != nil {
				return nil, err
			}
			for _, index := range class {
				n := len(s.targets[index].value)
				start, end := max(0, p+runLength-n), min(len(data), p+n)
				r := open[index]
				switch {
				case r == nil:
					open[index] = &region{start: start, end: end, last: p}
				case p-r.last <= 2*n:
					r.end, r.last = end, p
				default:
					closed[index] = append(closed[index], *r)
					*r = region{start: start, end: end, last: p}
				}
			}
		}
		if p+runLength >= len(data) {
			break
		}
		h = (h-uint64(data[p])*top)*hashBase + uint64(data[p+runLength])
	}
	indices := make([]int32, 0, len(open))
	regions := map[int32][]region{}
	for index, r := range open {
		indices = append(indices, index)
		regions[index] = append(closed[index], *r)
	}
	slices.Sort(indices)
	// The first pass finds the complete occurrences of every string the file
	// holds; a complete occurrence of a string lies inside its regions.
	var covers []span
	for _, index := range indices {
		value := s.targets[index].value
		automaton, err := s.automatonOf(index)
		if err != nil {
			return nil, err
		}
		for _, r := range regions[index] {
			if err := s.spend(r.end - r.start); err != nil {
				return nil, err
			}
			automaton.matches(data[r.start:r.end], func(end, length int) {
				if length == len(value) {
					covers = append(covers, span{start: r.start + end - length, end: r.start + end})
				}
			})
		}
	}
	c := newCoverage(covers)
	// The second pass reads each string's runs against those occurrences.
	var failing []int32
	for _, index := range indices {
		value := s.targets[index].value
		if len(value) <= runLength {
			continue
		}
		automaton, err := s.automatonOf(index)
		if err != nil {
			return nil, err
		}
		for _, r := range regions[index] {
			fails, err := s.failsIn(automaton, data, r, c)
			if err != nil {
				return nil, err
			}
			if fails {
				failing = append(failing, index)
				break
			}
		}
	}
	return failing, nil
}

// span is a complete occurrence of a string in a file: [start, end).
type span struct{ start, end int }

// coverage answers whether a complete occurrence holds a run: the
// occurrences in order of start, and for each the furthest end of it and
// those before it.
type coverage struct {
	starts   []int
	furthest []int
}

func newCoverage(covers []span) coverage {
	slices.SortFunc(covers, func(a, b span) int { return a.start - b.start })
	c := coverage{starts: make([]int, len(covers)), furthest: make([]int, len(covers))}
	reach := 0
	for i, s := range covers {
		reach = max(reach, s.end)
		c.starts[i], c.furthest[i] = s.start, reach
	}
	return c
}

// holds says whether one of the first before occurrences, those that start
// at or before a run, ends at or after the run's end.
func (c coverage) holds(before, end int) bool {
	return before > 0 && c.furthest[before-1] >= end
}

// automaton is the suffix automaton of one string: it reads a text and, at
// each place, gives the length of the longest run that ends there and is a
// substring of the string. It has at most 2n states and 3n edges for a
// string of n bytes, and is built in time linear in n.
type automaton struct {
	length int
	link   []int32
	size   []int32
	// next is the edges, by state and byte; out lists each state's bytes.
	next map[uint64]int32
	out  [][]byte
}

func edge(state int32, b byte) uint64 { return uint64(state)<<8 | uint64(b) }

// automatonOf returns a target's automaton, building it the first time.
func (s *scanner) automatonOf(index int32) (*automaton, error) {
	if a := s.automata[index]; a != nil {
		return a, nil
	}
	a, err := s.automaton(s.targets[index].value)
	if err != nil {
		return nil, err
	}
	if s.automata == nil {
		s.automata = map[int32]*automaton{}
	}
	s.automata[index] = a
	s.builds++
	return a, nil
}

func (s *scanner) automaton(value []byte) (*automaton, error) {
	if err := s.spend(8 * len(value)); err != nil {
		return nil, err
	}
	a := &automaton{length: len(value), link: []int32{-1}, size: []int32{0}, next: map[uint64]int32{}, out: [][]byte{nil}}
	state := func(size, link int32, out []byte) int32 {
		a.size = append(a.size, size)
		a.link = append(a.link, link)
		a.out = append(a.out, out)
		return int32(len(a.size) - 1)
	}
	last := int32(0)
	for _, b := range value {
		current := state(a.size[last]+1, 0, nil)
		p := last
		for p != -1 {
			if _, ok := a.next[edge(p, b)]; ok {
				break
			}
			a.next[edge(p, b)] = current
			a.out[p] = append(a.out[p], b)
			p = a.link[p]
		}
		if p != -1 {
			q := a.next[edge(p, b)]
			if a.size[p]+1 == a.size[q] {
				a.link[current] = q
			} else {
				clone := state(a.size[p]+1, a.link[q], append([]byte(nil), a.out[q]...))
				for _, c := range a.out[q] {
					a.next[edge(clone, c)] = a.next[edge(q, c)]
				}
				for p != -1 && a.next[edge(p, b)] == q {
					a.next[edge(p, b)] = clone
					p = a.link[p]
				}
				a.link[q], a.link[current] = clone, clone
			}
		}
		last = current
	}
	return a, nil
}

// matches calls each with, for every end e of the text in order, the length
// of the longest run ending at e that is a substring of the string.
func (a *automaton) matches(text []byte, each func(end, length int)) {
	state, length := int32(0), 0
	for i, b := range text {
		for state != 0 {
			if _, ok := a.next[edge(state, b)]; ok {
				break
			}
			state = a.link[state]
			length = int(a.size[state])
		}
		if next, ok := a.next[edge(state, b)]; ok {
			state = next
			length++
		} else {
			state, length = 0, 0
		}
		each(i+1, length)
	}
}

// failsIn says whether a region of a file holds a run of at least runLength
// of the string's characters that no complete occurrence of a string holds.
// For each end e whose longest run of the string is m >= runLength, it asks
// whether an occurrence starts at or before e-m and ends at or after e. The
// longest run ending at e holds every run ending there, and e-m never falls
// as e rises, so one pointer into the occurrences serves the region.
func (s *scanner) failsIn(a *automaton, data []byte, r region, c coverage) (bool, error) {
	if err := s.spend(r.end - r.start); err != nil {
		return false, err
	}
	before, _ := slices.BinarySearch(c.starts, r.start)
	fails := false
	a.matches(data[r.start:r.end], func(end, length int) {
		if fails || length < runLength {
			return
		}
		runStart, runEnd := r.start+end-length, r.start+end
		for before < len(c.starts) && c.starts[before] <= runStart {
			before++
		}
		fails = !c.holds(before, runEnd)
	})
	return fails, nil
}

// references is check 5.
func (h *harness) references(run *vectorRun, row Row) (Row, *emit.Diagnostic) {
	switch {
	case h.m.Input == emit.InputFactsContracts:
		row.Status, row.Reason = StatusSkip, reasonFacts
		return row, nil
	case run.refused != nil:
		row.Status, row.Reason = StatusSkip, reasonRefused
		return row, nil
	case !run.v.hasRegistry():
		row.Status, row.Reason = StatusSkip, reasonGivenRequest
		return row, nil
	case !run.succeeded():
		row.Status, row.Reason = StatusSkip, reasonNoFiles
		return row, nil
	}
	bound := func() (Row, *emit.Diagnostic) {
		d := vectorDiagnostic("EMIT-HARNESS-BOUND", run.v.Name, "")
		return row, &d
	}
	secretsOf := func(environments []string) ([]target, error) {
		var out []target
		for _, environment := range environments {
			found, err := run.registry.SecretStrings(environment, h.keep)
			if err != nil {
				return nil, err
			}
			for _, s := range found {
				if !ascii(s.Value) {
					// Not a secret reference, which a valid registry never
					// holds at a secret position; the scan does not guess.
					return nil, errReferenceBound
				}
				out = append(out, target{value: []byte(s.Value), environment: environment, entry: s.Entry, pointer: s.Pointer})
			}
		}
		return out, nil
	}
	var work int64
	var found []Reference
	for i, request := range run.requests {
		environments := []string{request.Environment}
		if h.m.Environments == emit.EnvironmentsAll {
			environments = run.registry.Environments()
		}
		targets, err := secretsOf(environments)
		if err != nil {
			return bound()
		}
		s, err := newScanner(targets)
		if err != nil {
			return bound()
		}
		s.work = work
		for _, file := range run.results[i].Response.Files {
			failing, err := s.scan(file.Data)
			if err != nil {
				return bound()
			}
			for _, index := range failing {
				t := s.targets[index]
				reference := Reference{File: file.Path, Entry: t.entry, Pointer: t.pointer}
				if h.m.Environments == emit.EnvironmentsEach {
					reference.Environment = t.environment
				}
				found = append(found, reference)
			}
		}
		work = s.work
	}
	if len(found) > 0 {
		row.Status, row.Reason, row.References = StatusFail, reasonReferences, bounded(found)
		return row, nil
	}
	row.Status = StatusPass
	return row, nil
}

func ascii(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}
