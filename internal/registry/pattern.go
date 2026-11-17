package registry

import (
	"errors"
	"fmt"
	"regexp"
	"regexp/syntax"
	"strconv"
	"strings"
	"unicode/utf8"
)

// MaxPatternLength bounds a pattern, in code points (RFC 0034 §2.2).
const MaxPatternLength = 256

// Program bounds (ADR-0093, amendment). RFC 0034 §2.2 admits {n,m} up to
// 1000 in a pattern of 256 code points, and RE2 compiles x{n,m} as m copies
// of x, so one pattern within the subset can compile to a quarter of a
// million instructions, tens of megabytes. A pattern's size is counted on the
// pattern as written, by the registry companion's rules (§3.4, "Program
// size"; patternSizer), before anything is compiled, and a kind is refused
// with REG-KIND-PATTERN-LIMIT at a pattern past MaxPatternInstructions, or at
// one that would take the kind's patterns together past
// MaxKindPatternInstructions.
const (
	MaxPatternInstructions     = 4096
	MaxKindPatternInstructions = 65536
)

// errPatternSize refuses a pattern within the subset whose size, counted
// as written, passes MaxPatternInstructions.
var errPatternSize = errors.New("the pattern's size passes its bound")

// Pattern is a compiled pattern of the companion's subset. Matching is RE2's,
// so it is linear in the input.
type Pattern struct {
	re *regexp.Regexp
	// size is the pattern's size, as patternSizer counts it.
	size int
}

// MatchString reports whether the pattern matches anywhere in s.
func (p *Pattern) MatchString(s string) bool { return p.re.MatchString(s) }

// CompilePattern checks a pattern against the subset of RFC 0034 §2.2 and
// compiles it. The subset is read lexically, because the parsed form of RE2
// cannot tell \w from the class it stands for. Each construct is given the one
// meaning the RFC fixes and translated into RE2:
//
//   - "." matches any code point except U+000A, U+000D, U+2028 and U+2029,
//     ECMAScript's meaning;
//   - "\d" is the ASCII digits;
//   - "$" matches only at the very end of the string, never before a final
//     line feed as Python's does;
//   - "\-" is admitted only inside a class, as ECMAScript's u mode requires.
//
// A pattern whose program would pass MaxPatternInstructions is refused
// before it is compiled.
func CompilePattern(source string) (*Pattern, error) {
	translated, size, err := translatePattern(source)
	if err != nil {
		return nil, err
	}
	re, err := regexp.Compile(translated)
	if err != nil {
		return nil, err
	}
	return &Pattern{re: re, size: size}, nil
}

// patternBudget compiles one kind's patterns and holds them together to
// MaxKindPatternInstructions.
type patternBudget struct {
	used int
}

// compile returns the compiled pattern, or nil and the code that refuses it:
// REG-KIND-PATTERN for one outside the subset, REG-KIND-PATTERN-LIMIT for one
// past MaxPatternInstructions or one that does not fit in what the kind has
// left. A refused pattern is not counted, so each later one is judged on the
// patterns admitted before it, in the order the kind is read.
func (b *patternBudget) compile(source string) (*Pattern, string) {
	translated, size, err := translatePattern(source)
	switch {
	case errors.Is(err, errPatternSize):
		return nil, "REG-KIND-PATTERN-LIMIT"
	case err != nil:
		return nil, "REG-KIND-PATTERN"
	case b.used+size > MaxKindPatternInstructions:
		return nil, "REG-KIND-PATTERN-LIMIT"
	}
	re, err := regexp.Compile(translated)
	if err != nil {
		return nil, "REG-KIND-PATTERN"
	}
	b.used += size
	return &Pattern{re: re, size: size}, ""
}

// patternSizer counts a pattern's size on the pattern as written, by the
// registry companion's rules (§3.4, "Program size"): each literal code
// point, escape, ".", class, "^" and "$" counts 1; a group counts its
// content; "*" counts 2 plus its atom, "+" and "?" 1 plus it; {n} counts n
// times its atom, {n,} 1 plus n times it ({0,} as "*"), and {n,m} m times
// it plus m-n; a sequence counts the sum of its parts, and an alternation
// of k alternatives the sum of its alternatives plus k-1; every construct
// counts at least 1, an empty alternative and x{0} included. It is an upper
// bound, close to it, on the program RE2 compiles. Every count saturates
// just past MaxPatternInstructions, so nested repetitions cannot overflow.
type patternSizer struct {
	frames []sizeFrame
}

// sizeFrame is the count of the pattern, or of one open group.
type sizeFrame struct {
	bars     int // the alternatives closed by "|"
	closed   int // their sizes, each at least 1
	sequence int // the current alternative's parts before pending
	pending  int // the last atom, which a quantifier may follow; 0 when none
}

const sizeLimit = MaxPatternInstructions + 1

func saturate(n int) int { return min(n, sizeLimit) }

func (z *patternSizer) top() *sizeFrame {
	if len(z.frames) == 0 {
		z.frames = append(z.frames, sizeFrame{})
	}
	return &z.frames[len(z.frames)-1]
}

func (f *sizeFrame) flush() {
	f.sequence = saturate(f.sequence + f.pending)
	f.pending = 0
}

func (f *sizeFrame) total() int {
	f.flush()
	return max(1, saturate(f.closed+max(1, f.sequence)+f.bars))
}

// atom counts an atom of size n, which a quantifier may follow.
func (z *patternSizer) atom(n int) {
	frame := z.top()
	frame.flush()
	frame.pending = n
}

// anchor counts "^" or "$", which no quantifier follows.
func (z *patternSizer) anchor() {
	frame := z.top()
	frame.flush()
	frame.sequence = saturate(frame.sequence + 1)
}

func (z *patternSizer) bar() {
	frame := z.top()
	frame.flush()
	frame.closed = saturate(frame.closed + max(1, frame.sequence))
	frame.sequence = 0
	frame.bars++
}

func (z *patternSizer) open() {
	z.top().flush()
	z.frames = append(z.frames, sizeFrame{})
}

// close ends a group, which is then the atom of its enclosing count.
func (z *patternSizer) close() {
	content := z.top().total()
	z.frames = z.frames[:len(z.frames)-1]
	z.atom(content)
}

// quantify applies a quantifier to the last atom: "*", "+" or "?", or "{"
// with n and m as readBraces returns them (m is -1 for {n,}).
func (z *patternSizer) quantify(quantifier rune, n, m int) {
	frame := z.top()
	atom := frame.pending
	var size int
	switch {
	case quantifier == '*' || quantifier == '{' && m == -1 && n == 0:
		size = 2 + atom
	case quantifier == '+' || quantifier == '?':
		size = 1 + atom
	case m == -1:
		size = 1 + n*atom
	default:
		size = m*atom + m - n
	}
	frame.pending = max(1, saturate(size))
}

// size is the whole pattern's size, once every group is closed.
func (z *patternSizer) size() int { return z.top().total() }

// translatePattern checks a pattern against the subset, translates it into
// RE2 and parses it, and returns the translation and its size, counted on
// the pattern as written. A pattern outside the subset, Go's refusal of a
// nested repetition past 1000 among it, is refused whatever its size; one
// within it whose size passes MaxPatternInstructions is refused with
// errPatternSize.
func translatePattern(source string) (string, int, error) {
	translated, size, err := translateSubset(source)
	if err != nil {
		return "", 0, err
	}
	if _, err := syntax.Parse(translated, syntax.Perl); err != nil {
		return "", 0, err
	}
	if size > MaxPatternInstructions {
		return "", 0, errPatternSize
	}
	return translated, size, nil
}

// translateSubset reads a pattern lexically against the subset, and returns
// its RE2 translation and its size (patternSizer).
func translateSubset(source string) (string, int, error) {
	if !utf8.ValidString(source) {
		return "", 0, fmt.Errorf("not UTF-8")
	}
	runes := []rune(source)
	if len(runes) > MaxPatternLength {
		return "", 0, fmt.Errorf("longer than %d code points", MaxPatternLength)
	}
	var out strings.Builder
	var sizer patternSizer
	depth := 0
	// canQuantify is true right after an atom; quantified is true right after
	// a quantifier, where another quantifier would be lazy or possessive.
	canQuantify, quantified := false, false
	last := len(runes) - 1
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch r {
		case '^':
			if i != 0 {
				return "", 0, fmt.Errorf("^ is allowed only first")
			}
			out.WriteString(`^`)
			sizer.anchor()
			canQuantify, quantified = false, false
		case '$':
			if i != last {
				return "", 0, fmt.Errorf("$ is allowed only last")
			}
			out.WriteString(`\z`)
			sizer.anchor()
			canQuantify, quantified = false, false
		case '\\':
			if i == last {
				return "", 0, fmt.Errorf("trailing backslash")
			}
			i++
			c := runes[i]
			switch {
			case c == 'd':
				out.WriteString(`[0-9]`)
			case strings.ContainsRune(`.\/()[]{}*+?|^$`, c):
				out.WriteString(regexp.QuoteMeta(string(c)))
			default:
				return "", 0, fmt.Errorf("escape outside the subset")
			}
			sizer.atom(1)
			canQuantify, quantified = true, false
		case '.':
			out.WriteString(`[^\n\r\x{2028}\x{2029}]`)
			sizer.atom(1)
			canQuantify, quantified = true, false
		case '[':
			end, class, err := readClass(runes, i)
			if err != nil {
				return "", 0, err
			}
			out.WriteString(class)
			sizer.atom(1)
			i = end
			canQuantify, quantified = true, false
		case '(':
			if i < last && runes[i+1] == '?' {
				return "", 0, fmt.Errorf("group modifiers are outside the subset")
			}
			depth++
			out.WriteString(`(?:`)
			sizer.open()
			canQuantify, quantified = false, false
		case ')':
			if depth == 0 {
				return "", 0, fmt.Errorf("unbalanced )")
			}
			depth--
			out.WriteString(`)`)
			sizer.close()
			canQuantify, quantified = true, false
		case '|':
			out.WriteString(`|`)
			sizer.bar()
			canQuantify, quantified = false, false
		case '*', '+', '?':
			if !canQuantify || quantified {
				return "", 0, fmt.Errorf("quantifier without an atom, or lazy or possessive")
			}
			out.WriteRune(r)
			sizer.quantify(r, 0, 0)
			canQuantify, quantified = false, true
		case '{':
			end, n, m, err := readBraces(runes, i)
			if err != nil {
				return "", 0, err
			}
			if !canQuantify || quantified {
				return "", 0, fmt.Errorf("quantifier without an atom")
			}
			out.WriteString(string(runes[i : end+1]))
			sizer.quantify('{', n, m)
			i = end
			canQuantify, quantified = false, true
		case '}', ']':
			return "", 0, fmt.Errorf("unescaped %c", r)
		default:
			out.WriteString(regexp.QuoteMeta(string(r)))
			sizer.atom(1)
			canQuantify, quantified = true, false
		}
	}
	if depth != 0 {
		return "", 0, fmt.Errorf("unbalanced (")
	}
	return out.String(), sizer.size(), nil
}

// readBraces reads {n}, {n,} or {n,m} at start, with n <= m <= 1000, and
// returns its end, n and m: m is n for {n} and -1 for {n,}.
func readBraces(runes []rune, start int) (int, int, int, error) {
	closing := -1
	for j := start + 1; j < len(runes); j++ {
		if runes[j] == '}' {
			closing = j
			break
		}
	}
	if closing < 0 {
		return 0, 0, 0, fmt.Errorf("unterminated {")
	}
	body := string(runes[start+1 : closing])
	low, high, hasComma := strings.Cut(body, ",")
	parse := func(text string) (int, bool) {
		if text == "" || len(text) > 4 || strings.TrimLeft(text, "0123456789") != "" {
			return 0, false
		}
		if len(text) > 1 && text[0] == '0' {
			return 0, false
		}
		value, err := strconv.Atoi(text)
		return value, err == nil && value <= 1000
	}
	n, ok := parse(low)
	if !ok {
		return 0, 0, 0, fmt.Errorf("malformed {n,m}")
	}
	m := n
	switch {
	case hasComma && high == "":
		m = -1
	case hasComma:
		if m, ok = parse(high); !ok || m < n {
			return 0, 0, 0, fmt.Errorf("malformed {n,m}")
		}
	}
	return closing, n, m, nil
}

// readClass reads a bracket class at start and returns its RE2 form, with
// every member written as an explicit code point.
func readClass(runes []rune, start int) (int, string, error) {
	var out strings.Builder
	out.WriteByte('[')
	i := start + 1
	if i < len(runes) && runes[i] == '^' {
		out.WriteByte('^')
		i++
	}
	type item struct {
		r     rune
		digit bool
	}
	read := func() (item, error) {
		r := runes[i]
		if r == '\\' {
			if i+1 >= len(runes) {
				return item{}, fmt.Errorf("trailing backslash")
			}
			c := runes[i+1]
			i += 2
			if c == 'd' {
				return item{digit: true}, nil
			}
			if strings.ContainsRune(`.-\/()[]{}*+?|^$`, c) {
				return item{r: c}, nil
			}
			return item{}, fmt.Errorf("escape outside the subset")
		}
		if r == '[' {
			return item{}, fmt.Errorf("unescaped [ inside a class")
		}
		i++
		return item{r: r}, nil
	}
	members := 0
	for {
		if i >= len(runes) {
			return 0, "", fmt.Errorf("unterminated class")
		}
		if runes[i] == ']' {
			if members == 0 {
				return 0, "", fmt.Errorf("empty class")
			}
			out.WriteByte(']')
			return i, out.String(), nil
		}
		for _, pair := range []string{"--", "&&", "~~", "||"} {
			if i+1 < len(runes) && string(runes[i:i+2]) == pair {
				return 0, "", fmt.Errorf("%s inside a class", pair)
			}
		}
		low, err := read()
		if err != nil {
			return 0, "", err
		}
		members++
		// A range: low, an unescaped "-", and a high that is not the class end.
		if i+1 < len(runes) && runes[i] == '-' && runes[i+1] != ']' {
			i++
			high, err := read()
			if err != nil {
				return 0, "", err
			}
			if low.digit || high.digit || high.r < low.r {
				return 0, "", fmt.Errorf("malformed range")
			}
			fmt.Fprintf(&out, `\x{%x}-\x{%x}`, low.r, high.r)
			continue
		}
		if low.digit {
			out.WriteString(`0-9`)
		} else {
			fmt.Fprintf(&out, `\x{%x}`, low.r)
		}
	}
}
