package registry

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// MaxPatternLength bounds a pattern, in code points (RFC 0034 §2.2).
const MaxPatternLength = 256

// Pattern is a compiled pattern of the companion's subset. Matching is RE2's,
// so it is linear in the input.
type Pattern struct {
	re *regexp.Regexp
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
func CompilePattern(source string) (*Pattern, error) {
	if !utf8.ValidString(source) {
		return nil, fmt.Errorf("not UTF-8")
	}
	runes := []rune(source)
	if len(runes) > MaxPatternLength {
		return nil, fmt.Errorf("longer than %d code points", MaxPatternLength)
	}
	var out strings.Builder
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
				return nil, fmt.Errorf("^ is allowed only first")
			}
			out.WriteString(`^`)
			canQuantify, quantified = false, false
		case '$':
			if i != last {
				return nil, fmt.Errorf("$ is allowed only last")
			}
			out.WriteString(`\z`)
			canQuantify, quantified = false, false
		case '\\':
			if i == last {
				return nil, fmt.Errorf("trailing backslash")
			}
			i++
			c := runes[i]
			switch {
			case c == 'd':
				out.WriteString(`[0-9]`)
			case strings.ContainsRune(`.\/()[]{}*+?|^$`, c):
				out.WriteString(regexp.QuoteMeta(string(c)))
			default:
				return nil, fmt.Errorf("escape outside the subset")
			}
			canQuantify, quantified = true, false
		case '.':
			out.WriteString(`[^\n\r\x{2028}\x{2029}]`)
			canQuantify, quantified = true, false
		case '[':
			end, class, err := readClass(runes, i)
			if err != nil {
				return nil, err
			}
			out.WriteString(class)
			i = end
			canQuantify, quantified = true, false
		case '(':
			if i < last && runes[i+1] == '?' {
				return nil, fmt.Errorf("group modifiers are outside the subset")
			}
			depth++
			out.WriteString(`(?:`)
			canQuantify, quantified = false, false
		case ')':
			if depth == 0 {
				return nil, fmt.Errorf("unbalanced )")
			}
			depth--
			out.WriteString(`)`)
			canQuantify, quantified = true, false
		case '|':
			out.WriteString(`|`)
			canQuantify, quantified = false, false
		case '*', '+', '?':
			if !canQuantify || quantified {
				return nil, fmt.Errorf("quantifier without an atom, or lazy or possessive")
			}
			out.WriteRune(r)
			canQuantify, quantified = false, true
		case '{':
			end, err := readBraces(runes, i)
			if err != nil {
				return nil, err
			}
			if !canQuantify || quantified {
				return nil, fmt.Errorf("quantifier without an atom")
			}
			out.WriteString(string(runes[i : end+1]))
			i = end
			canQuantify, quantified = false, true
		case '}', ']':
			return nil, fmt.Errorf("unescaped %c", r)
		default:
			out.WriteString(regexp.QuoteMeta(string(r)))
			canQuantify, quantified = true, false
		}
	}
	if depth != 0 {
		return nil, fmt.Errorf("unbalanced (")
	}
	re, err := regexp.Compile(out.String())
	if err != nil {
		return nil, err
	}
	return &Pattern{re: re}, nil
}

// readBraces reads {n}, {n,} or {n,m} at start, with n <= m <= 1000.
func readBraces(runes []rune, start int) (int, error) {
	closing := -1
	for j := start + 1; j < len(runes); j++ {
		if runes[j] == '}' {
			closing = j
			break
		}
	}
	if closing < 0 {
		return 0, fmt.Errorf("unterminated {")
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
		return 0, fmt.Errorf("malformed {n,m}")
	}
	if hasComma && high != "" {
		if m, ok := parse(high); !ok || m < n {
			return 0, fmt.Errorf("malformed {n,m}")
		}
	}
	return closing, nil
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
