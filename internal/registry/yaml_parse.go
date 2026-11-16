package registry

import (
	"sort"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// This file reads RFC 0034 §7's YAML subset: YAML 1.2 block and flow
// collections, plain, quoted and block scalars and comments, in one document,
// with no directive, tag, anchor, alias, merge key or complex key. It is its
// own reader rather than a YAML library (ADR-0094), so nothing outside the
// subset can be read by a library's own rules first.
//
// A refusal that leaves the structure readable (a tag, an anchor, an alias, a
// refused scalar, an empty node, a duplicate key) is recorded and reading
// goes on, so one run reports every such construct. A refusal that leaves the
// structure unreadable stops reading.

type yamlKind uint8

const (
	yamlScalar yamlKind = iota
	yamlMapping
	yamlSequence
)

// yamlNode is one node of the document read, with the offset of its first
// character in the text.
type yamlNode struct {
	at       int
	kind     yamlKind
	value    any // a scalar's value: string, bool, nil or json.Number
	keys     []string
	children []*yamlNode
}

func (n *yamlNode) member(name string) *yamlNode {
	for i, key := range n.keys {
		if key == name {
			return n.children[i]
		}
	}
	return nil
}

func (n *yamlNode) toValue() any {
	switch n.kind {
	case yamlMapping:
		object := make(map[string]any, len(n.keys))
		for i, key := range n.keys {
			object[key] = n.children[i].toValue()
		}
		return object
	case yamlSequence:
		array := make([]any, len(n.children))
		for i, child := range n.children {
			array[i] = child.toValue()
		}
		return array
	}
	return n.value
}

// yamlKey is a mapping key as read, before its text is checked.
type yamlKey struct {
	text  string
	at    int
	plain bool
}

// flowItem is a node read in a flow collection, whose scalar is not yet
// resolved because it may turn out to be a key.
type flowItem struct {
	node      *yamlNode // a collection or an alias; nil for a scalar
	text      string
	at        int
	plain     bool
	multiline bool
}

// blockStart says where a block node begins.
type blockStart uint8

const (
	startFresh     blockStart = iota // at the start of its own line
	startAfterKey                    // after "key:" on the key's line
	startAfterDash                   // after "-" on the entry's line
)

type yamlStop struct{}

type yamlParser struct {
	text        []rune
	pos         int
	lineStarts  []int
	depth       int
	diagnostics *collector
	// refusalLimit is the most refusals recorded before reading stops with
	// REG-YAML-TRUNCATED; 0 is no limit.
	refusalLimit int
}

// maxImportRefusals is the most refusals one import reports. A file within
// the size bound could otherwise hold millions (one per alias in a flow
// sequence), and the report would be larger than the file. Past it, reading
// stops and the report ends with one REG-YAML-TRUNCATED where it stopped.
const maxImportRefusals = 1000

// decodeYAMLText checks the encoding (UTF-8, no byte-order mark, only
// characters YAML allows) and returns the text with every line break as a
// line feed.
func decodeYAMLText(source []byte, diagnostics *collector) ([]rune, bool) {
	text := make([]rune, 0, len(source))
	line, column := 1, 1
	for i := 0; i < len(source); {
		r, size := utf8.DecodeRune(source[i:])
		if r == utf8.RuneError && size <= 1 || !yamlPrintable(r) {
			diagnostics.add("REG-YAML-ENCODING", Location{Document: DocumentYAML, Line: line, Column: column})
			return nil, false
		}
		i += size
		if r == '\r' {
			if i < len(source) && source[i] == '\n' {
				i++
			}
			r = '\n'
		}
		text = append(text, r)
		if r == '\n' {
			line, column = line+1, 1
		} else {
			column++
		}
	}
	return text, true
}

// yamlPrintable is YAML 1.2's c-printable, less the byte-order mark, which
// may appear nowhere in the subset.
func yamlPrintable(r rune) bool {
	switch {
	case r == '\t', r == '\n', r == '\r', r >= 0x20 && r <= 0x7e, r == 0x85, r >= 0xa0 && r <= 0xd7ff, r >= 0x10000 && r <= 0x10ffff:
		return true
	case r >= 0xe000 && r <= 0xfffd:
		return r != 0xfeff
	}
	return false
}

func newYAMLParser(text []rune, diagnostics *collector) *yamlParser {
	p := &yamlParser{text: text, lineStarts: []int{0}, diagnostics: diagnostics, refusalLimit: maxImportRefusals}
	for i, r := range text {
		if r == '\n' {
			p.lineStarts = append(p.lineStarts, i+1)
		}
	}
	return p
}

// parse reads the one document and returns its root, or nil when reading
// stopped.
func (p *yamlParser) parse() (root *yamlNode) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if _, stopped := recovered.(yamlStop); !stopped {
				panic(recovered)
			}
			root = nil
		}
	}()
	p.skipBlankLines()
	for !p.eof() && p.column() == 0 && p.peek(0) == '%' {
		p.report("REG-YAML-DIRECTIVE", p.pos)
		p.skipLine()
		p.skipBlankLines()
	}
	if p.eof() {
		p.stop("REG-YAML-DOCUMENTS", p.pos)
	}
	if p.atMarker("---") {
		marker := p.pos
		p.pos += 3
		if p.lineEndsHere() {
			p.endLine()
			p.skipBlankLines()
			if p.eof() || p.atMarker("---") || p.atMarker("...") {
				p.stop("REG-YAML-EMPTY", marker)
			}
			root = p.parseBlockNode(-1, startFresh, false)
		} else {
			p.skipWhite()
			root = p.parseBlockNode(-1, startAfterKey, false)
		}
	} else if p.atMarker("...") {
		p.stop("REG-YAML-DOCUMENTS", p.pos)
	} else {
		root = p.parseBlockNode(-1, startFresh, false)
	}
	switch {
	case p.eof():
	case p.atMarker("..."):
		p.pos += 3
		p.endLine()
		p.skipBlankLines()
		if !p.eof() {
			p.stop("REG-YAML-DOCUMENTS", p.pos)
		}
	case p.atMarker("---"):
		p.stop("REG-YAML-DOCUMENTS", p.pos)
	default:
		p.stop("REG-YAML-SYNTAX", p.pos)
	}
	return root
}

// position returns the 1-based line and column of an offset.
func (p *yamlParser) position(at int) (int, int) {
	line := sort.Search(len(p.lineStarts), func(i int) bool { return p.lineStarts[i] > at }) - 1
	return line + 1, at - p.lineStarts[line] + 1
}

func (p *yamlParser) location(at int) Location {
	line, column := p.position(at)
	return Location{Document: DocumentYAML, Line: line, Column: column}
}

func (p *yamlParser) report(code string, at int) {
	if p.refusalLimit > 0 && len(p.diagnostics.diagnostics) >= p.refusalLimit {
		p.diagnostics.add("REG-YAML-TRUNCATED", p.location(at))
		panic(yamlStop{})
	}
	p.diagnostics.add(code, p.location(at))
}

func (p *yamlParser) stop(code string, at int) {
	p.report(code, at)
	panic(yamlStop{})
}

func (p *yamlParser) eof() bool { return p.pos >= len(p.text) }

func (p *yamlParser) peek(offset int) rune {
	if i := p.pos + offset; i >= 0 && i < len(p.text) {
		return p.text[i]
	}
	return 0 // the end; a NUL in the text is refused as an encoding error
}

// column is the 0-based column of the current offset.
func (p *yamlParser) column() int {
	_, column := p.position(p.pos)
	return column - 1
}

func isWhite(r rune) bool { return r == ' ' || r == '\t' }

// isBlank is a white space, a line break or the end.
func isBlank(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == 0 }

func isFlowIndicator(r rune) bool { return strings.ContainsRune(",[]{}", r) }

func isBlankOrFlow(r rune) bool { return isBlank(r) || isFlowIndicator(r) }

// isIndicator is YAML's c-indicator.
func isIndicator(r rune) bool { return r != 0 && strings.ContainsRune("-?:,[]{}#&*!|>'\"%@`", r) }

func (p *yamlParser) skipWhite() {
	for isWhite(p.peek(0)) {
		p.pos++
	}
}

func (p *yamlParser) skipLine() {
	for !p.eof() && p.peek(0) != '\n' {
		p.pos++
	}
	if !p.eof() {
		p.pos++
	}
}

// commentHere says whether a comment starts at the current offset: a "#" at
// the start of a line or after white space.
func (p *yamlParser) commentHere() bool {
	return p.peek(0) == '#' && (p.pos == 0 || isBlank(p.text[p.pos-1]))
}

// lineEndsHere says whether only white space and a comment are left on the
// line, without moving.
func (p *yamlParser) lineEndsHere() bool {
	save := p.pos
	defer func() { p.pos = save }()
	p.skipWhite()
	return p.eof() || p.peek(0) == '\n' || p.commentHere()
}

// endLine consumes the rest of the line, which may hold only white space and
// a comment, and its line break.
func (p *yamlParser) endLine() {
	p.skipWhite()
	if p.commentHere() {
		for !p.eof() && p.peek(0) != '\n' {
			p.pos++
		}
	}
	if p.eof() {
		return
	}
	if p.peek(0) != '\n' {
		p.stop("REG-YAML-SYNTAX", p.pos)
	}
	p.pos++
}

// skipBlankLines moves from the start of a line past blank and comment lines
// to the first character of the next line with content. A tab in that line's
// indentation is refused: YAML indents with spaces.
func (p *yamlParser) skipBlankLines() {
	for !p.eof() {
		tab := -1
		for isWhite(p.peek(0)) {
			if p.peek(0) == '\t' && tab < 0 {
				tab = p.pos
			}
			p.pos++
		}
		switch {
		case p.eof():
			return
		case p.peek(0) == '\n', p.peek(0) == '#':
			p.skipLine()
			continue
		}
		if tab >= 0 {
			p.stop("REG-YAML-SYNTAX", tab)
		}
		return
	}
}

func (p *yamlParser) finishLine() {
	p.endLine()
	p.skipBlankLines()
}

// atMarker says whether a document marker ("---" or "...") starts a line here.
func (p *yamlParser) atMarker(marker string) bool {
	if p.pos+3 > len(p.text) || p.column() != 0 || string(p.text[p.pos:p.pos+3]) != marker {
		return false
	}
	return isBlank(p.peek(3))
}

// onlySpacesLeft says whether only spaces are left before the end of the
// file, without moving.
func (p *yamlParser) onlySpacesLeft() bool {
	for _, r := range p.text[p.pos:] {
		if r != ' ' {
			return false
		}
	}
	return true
}

func (p *yamlParser) atAnyMarker() bool { return p.atMarker("---") || p.atMarker("...") }

func (p *yamlParser) enter(kind yamlKind, at int) *yamlNode {
	p.depth++
	if p.depth > MaxDepth {
		p.stop("REG-DOC-DEPTH", at)
	}
	return &yamlNode{at: at, kind: kind}
}

func (p *yamlParser) leave() { p.depth-- }

// skipProperties records and steps over node properties: a tag ("!") or an
// anchor ("&") is outside the subset.
func (p *yamlParser) skipProperties(flow bool) {
	for p.peek(0) == '!' || p.peek(0) == '&' {
		code := "REG-YAML-TAG"
		if p.peek(0) == '&' {
			code = "REG-YAML-ANCHOR"
		}
		p.report(code, p.pos)
		for !isBlank(p.peek(0)) && !(flow && isFlowIndicator(p.peek(0))) {
			p.pos++
		}
		p.skipWhite()
	}
}

// skipAlias records and steps over an alias ("*name"), which is outside the
// subset, and returns a null node in its place.
func (p *yamlParser) skipAlias(flow bool) *yamlNode {
	at := p.pos
	p.report("REG-YAML-ANCHOR", at)
	for !isBlank(p.peek(0)) && !(flow && isFlowIndicator(p.peek(0))) {
		p.pos++
	}
	return &yamlNode{at: at}
}

// keyFollows says whether an implicit key's ":" follows, after white space on
// the same line, and leaves the offset at the ":" when it does.
func (p *yamlParser) keyFollows() bool {
	save := p.pos
	p.skipWhite()
	if p.peek(0) == ':' && isBlank(p.peek(1)) {
		return true
	}
	p.pos = save
	return false
}

// parseBlockNode reads a node in block context. owner is the indentation of
// the structure that holds it (-1 for the root): a line that continues the
// node must be indented more. On return the offset is at the first character
// of the next line with content, or at the end.
func (p *yamlParser) parseBlockNode(owner int, start blockStart, tabbed bool) *yamlNode {
	// Properties alone on their line; the node follows on a later line.
	// This is a loop, not a recursion: lines of properties do not nest, and
	// any number of them must not deepen the stack.
	for p.skipProperties(false); p.lineEndsHere(); p.skipProperties(false) {
		at := p.pos
		p.finishLine()
		if p.eof() || p.atAnyMarker() || p.column() <= owner {
			p.report("REG-YAML-EMPTY", at)
			return &yamlNode{at: at}
		}
		start, tabbed = startFresh, false
	}
	at := p.pos
	c, next := p.peek(0), p.peek(1)
	switch {
	case c == '-' && isBlank(next):
		if start == startAfterKey || tabbed {
			p.stop("REG-YAML-SYNTAX", at)
		}
		return p.parseBlockSequence(p.column())
	case (c == '?' || c == ':') && isBlank(next):
		// An explicit key, or an empty one.
		p.stop("REG-YAML-KEY", at)
	case c == '|' || c == '>':
		return p.parseBlockScalar(owner)
	case c == '[' || c == '{':
		node := p.parseFlowCollection(owner)
		if p.keyFollows() {
			p.stop("REG-YAML-KEY", at)
		}
		p.finishLine()
		return node
	case c == '*':
		node := p.skipAlias(false)
		p.finishLine()
		return node
	case c == '"' || c == '\'':
		column := p.column()
		text, multiline := p.parseQuoted(owner)
		if p.keyFollows() {
			if multiline {
				p.stop("REG-YAML-KEY", at)
			}
			if start == startAfterKey || tabbed {
				p.stop("REG-YAML-SYNTAX", at)
			}
			return p.parseBlockMapping(column, &yamlKey{text: text, at: at})
		}
		p.finishLine()
		return &yamlNode{at: at, value: text}
	case isIndicator(c) && c != '-' && c != '?' && c != ':':
		p.stop("REG-YAML-SYNTAX", at)
	}
	column := p.column()
	text := p.scanPlainLine(false)
	if p.keyFollows() {
		if start == startAfterKey || tabbed {
			p.stop("REG-YAML-SYNTAX", at)
		}
		return p.parseBlockMapping(column, &yamlKey{text: text, at: at, plain: true})
	}
	text, _ = p.continuePlain(owner, text, false)
	if p.keyFollows() {
		// An implicit key spanning lines.
		p.stop("REG-YAML-KEY", at)
	}
	p.finishLine()
	return p.plainNode(text, at)
}

// parseBlockMapping reads a block mapping whose keys are at column, the first
// key already read.
func (p *yamlParser) parseBlockMapping(column int, first *yamlKey) *yamlNode {
	node := p.enter(yamlMapping, first.at)
	defer p.leave()
	seen := map[string]bool{}
	key := first
	for {
		if key == nil {
			key = p.parseBlockKey()
		}
		colon := p.pos
		p.pos++
		if colon-key.at > 1024 {
			// YAML bounds an implicit key at 1024 characters.
			p.report("REG-YAML-KEY", key.at)
		}
		name := p.keyName(key)
		if seen[name] {
			p.report("REG-DOC-DUPLICATE", key.at)
		}
		seen[name] = true
		node.keys = append(node.keys, name)
		node.children = append(node.children, p.parseMappingValue(column, colon))
		key = nil
		if p.eof() || p.atAnyMarker() || p.column() < column {
			return node
		}
		if p.column() > column {
			p.stop("REG-YAML-SYNTAX", p.pos)
		}
	}
}

func (p *yamlParser) parseMappingValue(column, colon int) *yamlNode {
	p.skipWhite()
	if !p.lineEndsHere() {
		return p.parseBlockNode(column, startAfterKey, false)
	}
	p.finishLine()
	switch {
	case p.eof() || p.atAnyMarker():
	case p.column() > column:
		return p.parseBlockNode(column, startFresh, false)
	case p.column() == column && p.peek(0) == '-' && isBlank(p.peek(1)):
		// A mapping's value may be a sequence at the key's own indentation.
		return p.parseBlockSequence(column)
	}
	p.report("REG-YAML-EMPTY", colon)
	return &yamlNode{at: colon}
}

// parseBlockKey reads a key of a block mapping after its first, at the
// mapping's column, and leaves the offset at its ":".
func (p *yamlParser) parseBlockKey() *yamlKey {
	p.skipProperties(false)
	at := p.pos
	c, next := p.peek(0), p.peek(1)
	switch {
	case p.lineEndsHere():
		p.stop("REG-YAML-SYNTAX", at)
	case c == '-' && isBlank(next):
		p.stop("REG-YAML-SYNTAX", at)
	case (c == '?' || c == ':') && isBlank(next), c == '[', c == '{':
		p.stop("REG-YAML-KEY", at)
	case c == '*':
		p.stop("REG-YAML-ANCHOR", at)
	case c == '"' || c == '\'':
		text, multiline := p.parseQuoted(-1)
		if multiline {
			p.stop("REG-YAML-KEY", at)
		}
		if !p.keyFollows() {
			p.stop("REG-YAML-SYNTAX", p.pos)
		}
		return &yamlKey{text: text, at: at}
	case isIndicator(c) && c != '-' && c != '?' && c != ':':
		p.stop("REG-YAML-SYNTAX", at)
	}
	text := p.scanPlainLine(false)
	if !p.keyFollows() {
		p.stop("REG-YAML-SYNTAX", p.pos)
	}
	return &yamlKey{text: text, at: at, plain: true}
}

// keyName checks a key and returns its text: a plain key must read as a
// string, and the merge key is refused.
func (p *yamlParser) keyName(key *yamlKey) string {
	if key.plain {
		if code := plainKeyCode(key.text); code != "" {
			p.report(code, key.at)
		}
	}
	return key.text
}

// parseBlockSequence reads a block sequence whose "-" indicators are at
// column.
func (p *yamlParser) parseBlockSequence(column int) *yamlNode {
	node := p.enter(yamlSequence, p.pos)
	defer p.leave()
	for {
		dash := p.pos
		p.pos++
		tabbed := false
		for isWhite(p.peek(0)) {
			tabbed = tabbed || p.peek(0) == '\t'
			p.pos++
		}
		var item *yamlNode
		if p.lineEndsHere() {
			p.finishLine()
			if !p.eof() && !p.atAnyMarker() && p.column() > column {
				item = p.parseBlockNode(column, startFresh, false)
			} else {
				p.report("REG-YAML-EMPTY", dash)
				item = &yamlNode{at: dash}
			}
		} else {
			// A tab before a compact collection would leave its indentation
			// undefined; parseBlockNode refuses that case.
			item = p.parseBlockNode(column, startAfterDash, tabbed)
		}
		node.children = append(node.children, item)
		if p.eof() || p.atAnyMarker() || p.column() < column {
			return node
		}
		if p.column() > column {
			p.stop("REG-YAML-SYNTAX", p.pos)
		}
		if p.peek(0) != '-' || !isBlank(p.peek(1)) {
			return node
		}
	}
}

// blockLine is one line of a block scalar's content, its indentation removed.
type blockLine struct {
	text     []rune
	empty    bool
	hasBreak bool
}

// parseBlockScalar reads a literal ("|") or folded (">") block scalar with
// its chomping and indentation indicators.
func (p *yamlParser) parseBlockScalar(owner int) *yamlNode {
	at := p.pos
	folded := p.peek(0) == '>'
	p.pos++
	chomp, indicator := rune(0), 0
	for i := 0; i < 2; i++ {
		switch c := p.peek(0); {
		case (c == '-' || c == '+') && chomp == 0:
			chomp = c
			p.pos++
		case c >= '1' && c <= '9' && indicator == 0:
			indicator = int(c - '0')
			p.pos++
		}
	}
	if !isBlank(p.peek(0)) {
		p.stop("REG-YAML-SYNTAX", p.pos)
	}
	p.endLine()

	indent := owner + indicator
	if indicator == 0 {
		indent = p.detectIndentation(owner)
	}
	var lines []blockLine
	for !p.eof() && !p.atAnyMarker() {
		lineStart := p.pos
		spaces := 0
		for spaces < indent && p.peek(0) == ' ' {
			p.pos++
			spaces++
		}
		if p.peek(0) == '\n' {
			lines = append(lines, blockLine{empty: true})
			p.pos++
			continue
		}
		if p.eof() || spaces < indent && p.onlySpacesLeft() {
			// The file ends here, or ends after spaces on this line: an
			// empty last line with no line break, which adds nothing.
			p.pos = len(p.text)
			break
		}
		if spaces < indent {
			// A less indented line ends the scalar; one that holds only
			// white space with a tab is neither content nor an empty line.
			p.skipWhite()
			if p.peek(0) == '\n' || p.eof() {
				p.stop("REG-YAML-SYNTAX", lineStart)
			}
			p.pos = lineStart
			break
		}
		start := p.pos
		for !p.eof() && p.peek(0) != '\n' {
			p.pos++
		}
		line := blockLine{text: p.text[start:p.pos], hasBreak: !p.eof()}
		if line.hasBreak {
			p.pos++
		}
		lines = append(lines, line)
	}
	p.skipBlankLines()
	return &yamlNode{at: at, value: foldBlockLines(lines, folded, chomp)}
}

// detectIndentation returns a block scalar's content indentation from its
// first line with content, refusing a leading empty line indented more. When
// no line with content is indented more than owner, the content is empty.
func (p *yamlParser) detectIndentation(owner int) int {
	widest, widestAt := 0, -1
	for q := p.pos; q < len(p.text); {
		spaces := 0
		for q+spaces < len(p.text) && p.text[q+spaces] == ' ' {
			spaces++
		}
		if q+spaces < len(p.text) && p.text[q+spaces] == '\n' {
			if spaces > widest {
				widest, widestAt = spaces, q
			}
			q += spaces + 1
			continue
		}
		if q+spaces >= len(p.text) {
			// Spaces up to the end of the file: an empty last line, which
			// counts toward the indentation as any empty line does.
			return max(owner+1, widest, spaces)
		}
		if spaces <= owner {
			return max(owner+1, widest)
		}
		if widest > spaces {
			p.stop("REG-YAML-SYNTAX", widestAt)
		}
		return spaces
	}
	return max(owner+1, widest)
}

// foldBlockLines joins a block scalar's lines: literally, or folding a line
// break between two lines that do not begin with white space into a space;
// then chomps the final line break and trailing empty lines.
func foldBlockLines(lines []blockLine, folded bool, chomp rune) string {
	first, last := -1, -1
	for i, line := range lines {
		if !line.empty {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	var b strings.Builder
	if first < 0 {
		if chomp == '+' {
			b.WriteString(strings.Repeat("\n", len(lines)))
		}
		return b.String()
	}
	startsWhite := func(text []rune) bool { return len(text) > 0 && isWhite(text[0]) }
	b.WriteString(strings.Repeat("\n", first))
	b.WriteString(string(lines[first].text))
	previous, empty := lines[first], 0
	for _, line := range lines[first+1 : last+1] {
		if line.empty {
			empty++
			continue
		}
		switch {
		case folded && !startsWhite(previous.text) && !startsWhite(line.text) && empty == 0:
			b.WriteByte(' ')
		case folded && !startsWhite(previous.text) && !startsWhite(line.text):
			b.WriteString(strings.Repeat("\n", empty))
		default:
			b.WriteString(strings.Repeat("\n", empty+1))
		}
		b.WriteString(string(line.text))
		previous, empty = line, 0
	}
	if chomp != '-' && lines[last].hasBreak {
		b.WriteByte('\n')
	}
	if chomp == '+' {
		b.WriteString(strings.Repeat("\n", len(lines)-1-last))
	}
	return b.String()
}

// parseQuoted reads a single- or double-quoted scalar and says whether it
// spans lines. A line it continues on must be indented more than owner.
func (p *yamlParser) parseQuoted(owner int) (string, bool) {
	at := p.pos
	quote := p.peek(0)
	p.pos++
	var b strings.Builder
	multiline := false
	for {
		c := p.peek(0)
		switch {
		case p.eof():
			p.stop("REG-YAML-SYNTAX", at)
		case c == '\'' && quote == '\'' && p.peek(1) == '\'':
			b.WriteRune('\'')
			p.pos += 2
		case c == quote:
			p.pos++
			return b.String(), multiline
		case c == '\\' && quote == '"' && p.peek(1) == '\n':
			p.pos += 2
			multiline = true
			p.foldFlowLines(owner, &b, true)
		case c == '\\' && quote == '"':
			p.escape(&b)
		case isWhite(c) || c == '\n':
			start := p.pos
			p.skipWhite()
			if p.peek(0) != '\n' {
				b.WriteString(string(p.text[start:p.pos]))
				continue
			}
			p.pos++
			multiline = true
			p.foldFlowLines(owner, &b, false)
		default:
			b.WriteRune(c)
			p.pos++
		}
	}
}

// foldFlowLines is at the start of the line after a line break in a quoted
// scalar, whose trailing white space is already dropped. It drops the leading
// white space of the lines that follow, writes a line feed for each empty one
// or else one space (none after an escaped line break), and checks the
// indentation of the line the scalar continues on.
func (p *yamlParser) foldFlowLines(owner int, b *strings.Builder, escaped bool) {
	breaks := 0
	for {
		if p.atAnyMarker() {
			p.stop("REG-YAML-SYNTAX", p.pos)
		}
		lineStart, spaces := p.pos, 0
		for p.peek(0) == ' ' {
			p.pos++
			spaces++
		}
		p.skipWhite()
		if p.peek(0) == '\n' {
			breaks++
			p.pos++
			continue
		}
		if !p.eof() && spaces <= owner {
			p.stop("REG-YAML-SYNTAX", lineStart)
		}
		break
	}
	switch {
	case breaks > 0:
		b.WriteString(strings.Repeat("\n", breaks))
	case !escaped:
		b.WriteByte(' ')
	}
}

var yamlEscapes = map[rune]rune{
	'0': 0, 'a': 7, 'b': 8, 't': 9, '\t': 9, 'n': 10, 'v': 11, 'f': 12, 'r': 13, 'e': 27,
	' ': ' ', '"': '"', '/': '/', '\\': '\\', 'N': 0x85, '_': 0xa0, 'L': 0x2028, 'P': 0x2029,
}

// escape reads one escape sequence of a double-quoted scalar. A surrogate
// pair of \u escapes is one character, as in JSON; a lone surrogate is
// REG-DOC-STRING, as it is in JSON.
func (p *yamlParser) escape(b *strings.Builder) {
	at := p.pos
	p.pos++
	c := p.peek(0)
	p.pos++
	if r, ok := yamlEscapes[c]; ok && c != 0 {
		b.WriteRune(r)
		return
	}
	width := map[rune]int{'x': 2, 'u': 4, 'U': 8}[c]
	code, ok := p.hex(width)
	if !ok {
		p.stop("REG-YAML-SYNTAX", at)
	}
	if code >= 0xd800 && code <= 0xdbff && c == 'u' && p.peek(0) == '\\' && p.peek(1) == 'u' {
		save := p.pos
		p.pos += 2
		if low, ok := p.hex(4); ok && low >= 0xdc00 && low <= 0xdfff {
			b.WriteRune(utf16.DecodeRune(rune(code), rune(low)))
			return
		}
		p.pos = save
	}
	switch {
	case code >= 0xd800 && code <= 0xdfff:
		p.report("REG-DOC-STRING", at)
		b.WriteRune(utf8.RuneError)
	case code > 0x10ffff:
		p.stop("REG-YAML-SYNTAX", at)
	default:
		b.WriteRune(rune(code))
	}
}

func (p *yamlParser) hex(width int) (int, bool) {
	if width == 0 {
		return 0, false
	}
	code := 0
	for i := 0; i < width; i++ {
		c := p.peek(0)
		var digit int
		switch {
		case c >= '0' && c <= '9':
			digit = int(c - '0')
		case c >= 'a' && c <= 'f':
			digit = int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			digit = int(c-'A') + 10
		default:
			return 0, false
		}
		code = code*16 + digit
		p.pos++
	}
	return code, true
}

// scanPlainLine reads the rest of a plain scalar's line: up to a ":" before
// white space (or, in flow context, before a flow indicator), a comment, a
// flow indicator in flow context, or the line's end. It returns the text
// without trailing white space and leaves the offset after it.
func (p *yamlParser) scanPlainLine(flow bool) string {
	start, end := p.pos, p.pos
	for !p.eof() {
		c := p.peek(0)
		if c == '\n' || c == ':' && (isBlank(p.peek(1)) || flow && isFlowIndicator(p.peek(1))) || flow && isFlowIndicator(c) {
			break
		}
		if isWhite(c) {
			if p.peek(1) == '#' {
				break
			}
			p.pos++
			continue
		}
		p.pos++
		end = p.pos
	}
	p.pos = end
	return string(p.text[start:end])
}

// continuePlain reads the lines a plain scalar continues on: lines indented
// more than owner that do not start a comment or a document marker. A single
// line break folds into a space, and each empty line is a line feed.
func (p *yamlParser) continuePlain(owner int, text string, flow bool) (string, bool) {
	var b strings.Builder
	b.WriteString(text)
	multiline := false
	for {
		save := p.pos
		p.skipWhite()
		if p.peek(0) != '\n' {
			p.pos = save
			break
		}
		p.pos++
		breaks, continues := 0, false
		for !p.eof() {
			if p.atAnyMarker() {
				break
			}
			spaces := 0
			for p.peek(0) == ' ' {
				p.pos++
				spaces++
			}
			p.skipWhite()
			c := p.peek(0)
			if c == '\n' {
				breaks++
				p.pos++
				continue
			}
			continues = !p.eof() && c != '#' && spaces > owner &&
				!(c == ':' && (isBlank(p.peek(1)) || flow && isFlowIndicator(p.peek(1)))) && !(flow && isFlowIndicator(c))
			break
		}
		if !continues {
			p.pos = save
			break
		}
		multiline = true
		if breaks == 0 {
			b.WriteByte(' ')
		} else {
			b.WriteString(strings.Repeat("\n", breaks))
		}
		b.WriteString(p.scanPlainLine(flow))
	}
	return b.String(), multiline
}

func (p *yamlParser) plainNode(text string, at int) *yamlNode {
	value, code := resolvePlain(text)
	if code != "" {
		p.report(code, at)
	}
	return &yamlNode{at: at, value: value}
}

// parseFlowCollection reads a flow sequence or mapping. owner is the
// indentation of the block structure that holds it: every line it continues
// on must be indented more.
func (p *yamlParser) parseFlowCollection(owner int) *yamlNode {
	at := p.pos
	kind, closer := yamlSequence, ']'
	if p.peek(0) == '{' {
		kind, closer = yamlMapping, '}'
	}
	node := p.enter(kind, at)
	defer p.leave()
	p.pos++
	seen := map[string]bool{}
	for {
		p.flowSkip(owner)
		if p.peek(0) == closer {
			p.pos++
			return node
		}
		if kind == yamlSequence {
			p.flowSequenceEntry(owner, node)
		} else {
			p.flowMappingEntry(owner, node, seen)
		}
		p.flowSkip(owner)
		switch p.peek(0) {
		case ',':
			p.pos++
		case closer:
			p.pos++
			return node
		default:
			p.stop("REG-YAML-SYNTAX", p.pos)
		}
	}
}

// flowSkip steps over white space, comments and line breaks inside a flow
// collection, refusing a line with content indented no more than owner.
func (p *yamlParser) flowSkip(owner int) {
	for {
		p.skipWhite()
		if p.commentHere() {
			for !p.eof() && p.peek(0) != '\n' {
				p.pos++
			}
		}
		if p.peek(0) != '\n' {
			return
		}
		p.pos++
		if p.atAnyMarker() {
			p.stop("REG-YAML-SYNTAX", p.pos)
		}
		lineStart, spaces := p.pos, 0
		for p.peek(0) == ' ' {
			p.pos++
			spaces++
		}
		p.skipWhite()
		if !p.eof() && p.peek(0) != '\n' && !p.commentHere() && spaces <= owner {
			p.stop("REG-YAML-SYNTAX", lineStart)
		}
	}
}

// parseFlowNode reads one node in flow context; a scalar is left unresolved,
// since it may be a key.
func (p *yamlParser) parseFlowNode(owner int) flowItem {
	p.skipProperties(true)
	at := p.pos
	c, next := p.peek(0), p.peek(1)
	switch {
	case p.eof():
		p.stop("REG-YAML-SYNTAX", at)
	case c == '[' || c == '{':
		return flowItem{node: p.parseFlowCollection(owner), at: at}
	case c == '"' || c == '\'':
		text, multiline := p.parseQuoted(owner)
		return flowItem{text: text, at: at, multiline: multiline}
	case c == '*':
		return flowItem{node: p.skipAlias(true), at: at}
	case c == '?' || c == ':':
		// An explicit key, or a value with no key. YAML 1.2 lets a plain
		// scalar start with "?b" or ":b" here, but libyaml, and so PyYAML
		// and go-yaml, read the indicator: {?b: c} is {b: c} to them.
		p.stop("REG-YAML-KEY", at)
	case isIndicator(c) && !(c == '-' && !isBlankOrFlow(next)):
		p.stop("REG-YAML-SYNTAX", at)
	}
	text := p.scanPlainLine(true)
	text, multiline := p.continuePlain(owner, text, true)
	return flowItem{text: text, at: at, plain: true, multiline: multiline}
}

func (p *yamlParser) itemValue(item flowItem) *yamlNode {
	switch {
	case item.node != nil:
		return item.node
	case item.plain:
		return p.plainNode(item.text, item.at)
	}
	return &yamlNode{at: item.at, value: item.text}
}

// flowColon says whether a key's ":" follows on the same line, and steps
// past it. After a quoted key it may be followed by the value at once, as in
// JSON; after a plain key it must be followed by white space, or by ",", "]"
// or "}" for an empty value, or it would have been part of the key. A plain
// key's ":" right before "[" or "{" is refused: YAML 1.2 gives a value right
// after the ":" only to a quoted key (or a flow collection, never a key here).
func (p *yamlParser) flowColon(item flowItem) (int, bool) {
	save := p.pos
	p.skipWhite()
	if p.peek(0) == ':' && (!item.plain || isBlankOrFlow(p.peek(1))) {
		colon := p.pos
		if next := p.peek(1); item.plain && (next == '[' || next == '{') {
			p.stop("REG-YAML-SYNTAX", colon+1)
		}
		p.pos++
		return colon, true
	}
	p.pos = save
	return 0, false
}

// flowValue reads the value after a ":" in flow context.
func (p *yamlParser) flowValue(owner, colon int) *yamlNode {
	p.flowSkip(owner)
	if c := p.peek(0); c == ',' || c == '}' || c == ']' {
		p.report("REG-YAML-EMPTY", colon)
		return &yamlNode{at: colon}
	}
	return p.itemValue(p.parseFlowNode(owner))
}

// flowKey checks a flow node used as a key: a scalar on one line that reads
// as a string.
func (p *yamlParser) flowKey(item flowItem) string {
	if item.node != nil || item.multiline {
		p.stop("REG-YAML-KEY", item.at)
	}
	return p.keyName(&yamlKey{text: item.text, at: item.at, plain: item.plain})
}

// flowSequenceEntry reads one entry of a flow sequence: a node, or a single
// "key: value" pair, which is a mapping of one member.
func (p *yamlParser) flowSequenceEntry(owner int, sequence *yamlNode) {
	item := p.parseFlowNode(owner)
	colon, pair := p.flowColon(item)
	if !pair {
		sequence.children = append(sequence.children, p.itemValue(item))
		return
	}
	if colon-item.at > 1024 {
		p.report("REG-YAML-KEY", item.at)
	}
	mapping := p.enter(yamlMapping, item.at)
	defer p.leave()
	mapping.keys = []string{p.flowKey(item)}
	mapping.children = []*yamlNode{p.flowValue(owner, colon)}
	sequence.children = append(sequence.children, mapping)
}

// flowMappingEntry reads one "key: value" member of a flow mapping. The key
// and its ":" must be on one line.
func (p *yamlParser) flowMappingEntry(owner int, mapping *yamlNode, seen map[string]bool) {
	at := p.pos
	if c := p.peek(0); c == '[' || c == '{' {
		p.stop("REG-YAML-KEY", at)
	}
	item := p.parseFlowNode(owner)
	name := p.flowKey(item)
	var value *yamlNode
	if colon, ok := p.flowColon(item); ok {
		value = p.flowValue(owner, colon)
	} else {
		p.flowSkip(owner)
		switch p.peek(0) {
		case ':':
			p.stop("REG-YAML-KEY", item.at)
		case ',', '}':
			p.report("REG-YAML-EMPTY", item.at)
			value = &yamlNode{at: item.at}
		default:
			p.stop("REG-YAML-SYNTAX", p.pos)
		}
	}
	if seen[name] {
		p.report("REG-DOC-DUPLICATE", item.at)
	}
	seen[name] = true
	mapping.keys = append(mapping.keys, name)
	mapping.children = append(mapping.children, value)
}
