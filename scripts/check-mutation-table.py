#!/usr/bin/env python3
"""Check docs/registry-mutation-table.md against the code tables it covers.

The code set is read from the source, as the table's scope rule states, never listed here:
the keys of `var Codes` in internal/registry/diag.go, of `var FactsCodes` in
internal/registry/facts.go, of `var Codes` in internal/emit/codes.go and of `var Codes` in
internal/federation/codes.go; the names
`func Codes()` in internal/terraformfile/codes.go returns; every other constant in the module's
non-test Go files whose value is a FACTS- code; and every constant in the non-test Go files of a
cmd/jpack-emit-*/ package whose value is an x- code. A key or a name that is an identifier is
resolved through its package's constants.

Fails on a code with no row or with two, a row whose code no longer exists, a row whose severity
is not the one its code table states, a row whose mutant site names no function of a non-test Go
file, a row whose mutant is not `every A becomes B` with A the row's own code, quoted or as a
constant of the site's package that holds it, in that function's body outside its comments and
as a whole token, and B neither blank nor A, a row whose provisional column is not what the source
marks (see provisional_marks), and a row whose test `go test -list` does not list in the row's package (for `TestX/sub`,
the parent `TestX` must be listed, and a string literal in its body must name `sub` as `go test`
prints it, spaces as underscores). No code is excused from a row. Run from the repository root. Standard library only; it runs
`go test -list`, one package at a time.

The start-up module (startup/) has a table of its own, derived from its source as well (see
startup_identifiers and startup_sites): one row for each function of a non-test Go file that uses
one of its exported error values or exported non-string constants, its refusals and limits. It
fails on such a function with no row or two, a row whose identifier or site is not one, a mutant
that is not `every A becomes B` with A the row's identifier in the function's body and B another
error value (for a refusal) or A+1 or A-1 (for a limit), and a test that does not name A or that
no test file of the module declares on linux, darwin or windows (`go list`); a test this host
builds must be listed by `go test -list` in startup/.
"""
import glob
import os
import re
import subprocess
import sys

TABLE = 'docs/registry-mutation-table.md'
HEADER = ['code', 'severity', 'package', 'test', 'mutant site', 'mutant', 'provisional']
SEVERITIES = {'SeverityError': 'error', 'SeverityWarning': 'warning'}
ENTRY = re.compile(r'^\s*(?:"([A-Z0-9-]+)"|([A-Za-z_]\w*))\s*:\s*\{(Severity\w+),')
CONST_LINE = re.compile(r'^\s*([A-Za-z_]\w*)(?:\s+[A-Za-z_]\w*)?\s*=\s*"([^"\\]*)"\s*(?://.*)?$')
CONST_ONE = re.compile(r'^const\s+([A-Za-z_]\w*)(?:\s+[A-Za-z_]\w*)?\s*=\s*"([^"\\]*)"\s*(?://.*)?$')

problems = []


def refuse(message):
    print('mutation table: ' + message, file=sys.stderr)
    print('mutation table: the code set could not be read', file=sys.stderr)
    sys.exit(2)


def source_files(directory):
    return sorted(f for f in glob.glob(os.path.join(directory, '*.go')) if not f.endswith('_test.go'))


def constants(path):
    """The string constants a Go file declares, single or in const ( ... ) blocks."""
    out = {}
    block = False
    for line in open(path, encoding='utf-8').read().split('\n'):
        if block:
            if line.strip() == ')':
                block = False
                continue
            m = CONST_LINE.match(line)
        else:
            if re.match(r'^\s*const\s*\($', line):
                block = True
                continue
            m = CONST_ONE.match(line.strip())
        if m:
            out[m.group(1)] = m.group(2)
    return out


def all_constants(directory):
    """The string constants of every Go file in directory, tests included."""
    out = {}
    for path in sorted(glob.glob(os.path.join(directory, '*.go'))):
        out.update(constants(path))
    return out

def package_constants(directory):
    out = {}
    for path in source_files(directory):
        out.update(constants(path))
    return out


def map_table(path, name):
    """{code: severity} from `var NAME = map[string]codeInfo{` in path."""
    text = open(path, encoding='utf-8').read().split('\n')
    start = [i for i, line in enumerate(text) if line == 'var %s = map[string]codeInfo{' % name]
    if len(start) != 1:
        refuse('%s: no single var %s table' % (path, name))
    resolve = package_constants(os.path.dirname(path))
    out = {}
    for line in text[start[0] + 1:]:
        if line == '}':
            return out
        if not line.strip() or line.strip().startswith('//'):
            continue
        m = ENTRY.match(line)
        if not m or m.group(3) not in SEVERITIES:
            refuse('%s: a %s entry is not one line of a key and its severity: %s' % (path, name, line.strip()))
        code = m.group(1) or resolve.get(m.group(2))
        if code is None:
            refuse('%s: %s key %s is no constant of its package' % (path, name, m.group(2)))
        out[code] = SEVERITIES[m.group(3)]
    refuse('%s: the %s table does not end' % (path, name))


def section_marks(path, name, text=None):
    """{code: 'yes' or 'no'} from the comment heading each section of `var NAME = map[string]codeInfo{`
    in path (or in text, its source): a code under a heading that says provisional is provisional,
    any other is not."""
    return {code: 'yes' if 'provisional' in heading else 'no' for code, heading in section_headings(path, name, text).items()}


def section_headings(path, name, text=None):
    """{code: heading}: the comment heading the section of `var NAME = map[string]codeInfo{` in path
    (or in text, its source) that holds each code."""
    text = (open(path, encoding='utf-8').read() if text is None else text).split('\n')
    start = [i for i, line in enumerate(text) if line == 'var %s = map[string]codeInfo{' % name]
    if len(start) != 1:
        refuse('%s: no single var %s table' % (path, name))
    resolve = package_constants(os.path.dirname(path))
    out = {}
    heading, previous = '', ''
    for line in text[start[0] + 1:]:
        if line == '}':
            return out
        stripped = line.strip()
        if stripped.startswith('//'):
            heading = (heading + ' ' if previous.startswith('//') else '') + stripped
        elif stripped:
            m = ENTRY.match(line)
            if m:
                code = m.group(1) or resolve.get(m.group(2))
                out[code] = heading
        previous = stripped
    refuse('%s: the %s table does not end' % (path, name))


def const_comments(path, text=None):
    """{name: comment} for each string constant of a Go file (or of text, its source): the comment
    lines just above its declaration, and for a member of a const ( ... ) block also those just
    above the block."""
    out = {}
    lines = (open(path, encoding='utf-8').read() if text is None else text).split('\n')
    block = None
    for i, line in enumerate(lines):
        above = []
        j = i - 1
        while j >= 0 and lines[j].strip().startswith('//'):
            above.insert(0, lines[j].strip())
            j -= 1
        if block is not None:
            if line.strip() == ')':
                block = None
                continue
            m = CONST_LINE.match(line)
            if m:
                out[m.group(1)] = ' '.join(block + above)
            continue
        if re.match(r'^\s*const\s*\($', line):
            block = above
            continue
        m = CONST_ONE.match(line.strip())
        if m:
            out[m.group(1)] = ' '.join(above)
    return out


def provisional_marks():
    """{code: 'yes', 'no' or 'n/a'} for each code whose provisional status the source marks: a code
    of `var Codes` in internal/emit/codes.go, of `var Codes` in internal/registry/diag.go or of
    `var FactsCodes` in internal/registry/facts.go by its section's heading; a code held by a
    constant (Codes() in internal/terraformfile/codes.go names them, and every other FACTS-
    constant) as provisional when the constant's comment says so, and as not provisional
    otherwise unless a table marks it; an emitter's x- code is n/a. The source marks every code."""
    marks = section_marks('internal/emit/codes.go', 'Codes')
    for path, name in (('internal/registry/diag.go', 'Codes'), ('internal/registry/facts.go', 'FactsCodes'),
                       (FEDERATION_CODES, 'Codes')):
        marks.update(section_marks(path, name))
    held = []
    directory = 'internal/terraformfile'
    comments = {}
    for path in source_files(directory):
        comments.update(const_comments(path))
    resolve = package_constants(directory)
    for name in re.findall(r'\w+', re.search(r'^func Codes\(\) \[\]string \{\n\treturn \[\]string\{([^}]*)\}',
                                             open(os.path.join(directory, 'codes.go'), encoding='utf-8').read(), re.M).group(1)):
        held.append((resolve[name], comments.get(name, '')))
    for path in module_files():
        values = constants(path)
        comments = const_comments(path)
        for name, value in values.items():
            if re.fullmatch(r'FACTS-[A-Z0-9-]+', value):
                held.append((value, comments.get(name, '')))
    for code, comment in held:
        if 'provisional' in comment:
            marks[code] = 'yes'
    for code, comment in held:
        marks.setdefault(code, 'no')
    for code in derive():
        if code.startswith('x-'):
            marks[code] = 'n/a'
    return marks


def provisional_problem(code, value, marks):
    """Why a row's provisional value is not what the source marks for its code, or None. A code
    the source does not mark may be yes or no, never n/a."""
    if code in marks and value != marks[code] or code not in marks and value == 'n/a':
        return 'provisional is %s, the source marks %s' % (value, marks.get(code, 'yes or no'))
    return None


def module_files():
    """The module's non-test Go files, outside nested modules and testdata."""
    tracked = subprocess.run(['git', 'ls-files', '-z', '--', '*.go'], capture_output=True, check=True).stdout
    module = [p for p in tracked.decode().split('\0') if p and not p.endswith('_test.go')]
    nested = {os.path.dirname(p) for p in subprocess.run(['git', 'ls-files', '-z', '--', '*go.mod'], capture_output=True,
                                                         check=True).stdout.decode().split('\0') if p and p != 'go.mod'}
    return [p for p in module if not any(p.startswith(d + '/') for d in nested) and '/testdata/' not in p]


def returned_names(path, name):
    """The codes `func NAME() []string { return []string{...} }` returns, resolved."""
    text = open(path, encoding='utf-8').read()
    m = re.search(r'^func %s\(\) \[\]string \{\n\treturn \[\]string\{([^}]*)\}\n\}' % name, text, re.M)
    if not m:
        refuse('%s: no func %s() returning one []string literal' % (path, name))
    resolve = package_constants(os.path.dirname(path))
    out = []
    for item in (s.strip() for s in m.group(1).split(',')):
        if item not in resolve:
            refuse('%s: %s returns %s, which is no constant of its package' % (path, name, item))
        out.append(resolve[item])
    return out


def derive():
    """{code: severity or None}: None where the source states no severity."""
    codes = {}
    for path, name in (('internal/registry/diag.go', 'Codes'), ('internal/registry/facts.go', 'FactsCodes'),
                       ('internal/emit/codes.go', 'Codes'), (FEDERATION_CODES, 'Codes')):
        codes.update(map_table(path, name))
    for code in returned_names('internal/terraformfile/codes.go', 'Codes'):
        codes.setdefault(code, None)
    for path in module_files():
        for value in constants(path).values():
            if re.fullmatch(r'FACTS-[A-Z0-9-]+', value):
                codes.setdefault(value, None)
    for directory in sorted(glob.glob('cmd/jpack-emit-*/')):
        for value in package_constants(directory).values():
            if re.fullmatch(r'x-[a-z0-9-]+', value):
                codes.setdefault(value, None)
    return codes


FEDERATION_CODES = 'internal/federation/codes.go'


def missing_rows(codes, seen):
    """The codes of the code set (codes) that no row of the table (seen) holds, as problems: every
    code has a row, and none is excused from one."""
    return ['%s: %s has no row' % (TABLE, code) for code in sorted(set(codes) - set(seen))]


def cells(line):
    if not (line.startswith('|') and line.endswith('|')):
        return None
    return [c.strip() for c in line[1:-1].split('|')]


def read_table(header=HEADER):
    rows = []
    lines = open(TABLE, encoding='utf-8').read().split('\n')
    heads = [i for i, line in enumerate(lines) if cells(line) == header]
    if len(heads) != 1:
        refuse('%s: no single table headed %s' % (TABLE, ' | '.join(header)))
    for number, line in enumerate(lines[heads[0] + 2:], heads[0] + 3):
        row = cells(line)
        if row is None:
            break
        if len(row) != len(header):
            problems.append('%s:%d: %d cells, want %d' % (TABLE, number, len(row), len(header)))
            continue
        rows.append((number, dict(zip(header, (c[1:-1] if len(c) > 1 and c[0] == c[-1] == '`' else c for c in row)))))
    return rows


def function_body(path, name):
    """The source of function name in path, from its func line to the closing brace, or None."""
    text = open(path, encoding='utf-8').read()
    start = re.search(r'^func (?:\([^)]*\) )?%s(?:\[[^\]]*\])?\(' % re.escape(name), text, re.M)
    if not start:
        return None
    end = re.search(r'^}', text[start.end():], re.M)
    return text[start.start():start.end() + (end.end() if end else len(text) - start.end())]


MUTANT = re.compile(r'every `([^`]+)` becomes `([^`]+)`')


def go_code(text, raw=False):
    """text, a piece of Go source, with its comments removed and every string or rune literal
    replaced by a blank, and the interpreted string literals it holds, as written (with raw, the
    raw string literals too)."""
    out, literals, i, n = [], [], 0, len(text)
    while i < n:
        if text.startswith('//', i):
            end = text.find('\n', i)
            i = n if end < 0 else end
            out.append(' ')
        elif text.startswith('/*', i):
            end = text.find('*/', i + 2)
            i = n if end < 0 else end + 2
            out.append(' ')
        elif text[i] in '"\'':
            quote, j = text[i], i + 1
            while j < n and text[j] not in (quote, '\n'):
                j += 2 if text[j] == '\\' else 1
            j = min(j + 1, n)
            if quote == '"':
                literals.append(text[i:j])
            out.append(' ')
            i = j
        elif text[i] == '`':
            end = text.find('`', i + 1)
            j = n if end < 0 else end + 1
            if raw:
                literals.append(text[i:j])
            out.append(' ')
            i = j
        else:
            out.append(text[i])
            i += 1
    return ''.join(out), literals


def mutant_problem(body, mutant, code, resolve):
    """Why the mutant `every A becomes B` cannot apply to a function's body as a mutant of code,
    or None. It is not of that form; A is not the row's own code, quoted ("CODE"), or an
    identifier that resolve (the site package's constants) maps to it; B is blank or A; or A does
    not occur in the body outside its comments, the quoted code as a whole string literal, an
    identifier as a whole word."""
    m = MUTANT.fullmatch(mutant)
    if not m:
        return 'is not of the form every `A` becomes `B`'
    old, new = m.group(1), m.group(2)
    if not new.strip():
        return 'changes %s into nothing' % old
    if old == new:
        return 'changes %s into itself' % old
    source, literals = go_code(body)
    if old.startswith('"'):
        if old != '"%s"' % code:
            return 'changes %s, not the row\'s code "%s"' % (old, code)
        found = old in literals
    elif re.fullmatch(r'[A-Za-z_]\w*', old):
        if resolve.get(old) != code:
            return 'changes %s, which is no constant of its package holding %s' % (old, code)
        found = re.search(r'(?<![\w.])%s(?!\w)' % re.escape(old), source) is not None
    else:
        return 'changes %s, neither the quoted code nor a constant holding it' % old
    if not found:
        return 'changes %s, which its function does not hold outside its comments' % old
    return None


def test_body(package, name):
    """The source of test function name in package's test files, or None."""
    for file in sorted(os.listdir(package)):
        if not file.endswith('_test.go'):
            continue
        text = open(os.path.join(package, file), encoding='utf-8').read()
        start = re.search(r'^func %s\(' % re.escape(name), text, re.M)
        if start:
            end = re.search(r'^}', text[start.end():], re.M)
            return text[start.start():start.end() + (end.end() if end else len(text))]
    return None


LITERAL = re.compile(r'"((?:[^"\\\n]|\\.)*)"|`([^`]*)`')


def subtest_case(body, part):
    """The lines of the case in body whose string literal names subtest part
    as go test prints it (spaces become underscores), or None: from that
    line to the next line at its indentation that starts another case, or
    to the first line indented less."""
    lines = body.split('\n')
    for i, line in enumerate(lines):
        if not any((a or b).replace(' ', '_') == part for a, b in LITERAL.findall(line)):
            continue
        indent = len(line) - len(line.lstrip())
        j = i + 1
        while j < len(lines):
            text = lines[j]
            if text.strip():
                depth = len(text) - len(text.lstrip())
                if depth < indent or depth == indent and text.lstrip()[0] in '{"`':
                    break
            j += 1
        return '\n'.join(lines[i:j])
    return None


def named_codes(text, codes, names):
    """The codes text names: as a whole token inside a string literal (a
    literal may hold more, as "EMIT-OPTIONS@/x" does), or by the name of a Go
    constant (names maps each to its value) that holds one."""
    found = set()
    for a, b in LITERAL.findall(text):
        for token in re.findall(r'[A-Za-z][A-Za-z0-9]*(?:-[A-Za-z0-9]+)+', a or b):
            if token in codes:
                found.add(token)
    for name, value in names.items():
        if value in codes and re.search(r'\b%s\b' % re.escape(name), text):
            found.add(value)
    return found

# The start-up module (startup/, a nested module) reports no codes: its refusals are exported
# error values and its limits exported constants, under the module's 1.0 promise. Its rows are
# in a table of their own, one for each function of a non-test Go file of startup/ that uses one.
STARTUP = 'startup'
STARTUP_HEADER = ['identifier', 'test', 'mutant site', 'mutant']
STARTUP_GOOS = ('linux', 'darwin', 'windows')
FUNC = re.compile(r'^func (?:\([^)]*\) )?([A-Za-z_]\w*)(?:\[[^\]]*\])?\(', re.M)


def startup_sources():
    """{path: text} of the start-up module's non-test Go files, every build constraint's."""
    return {p: open(p, encoding='utf-8').read() for p in source_files(STARTUP)}


def declarations(text, keyword):
    """[(name, rest)] for each top-level `keyword` (var or const) declaration in text, single or
    a member of a `keyword ( ... )` block: its name and the rest of its line."""
    out, block = [], False
    for line in text.split('\n'):
        if block:
            if line == ')':
                block = False
            elif re.match(r'^\t[A-Za-z_]', line):
                m = re.match(r'^\t([A-Za-z_]\w*)(.*)$', line)
                out.append((m.group(1), m.group(2)))
            continue
        if re.match(r'^%s\s*\($' % keyword, line):
            block = True
            continue
        m = re.match(r'^%s\s+([A-Za-z_]\w*)(.*)$' % keyword, line)
        if m:
            out.append((m.group(1), m.group(2)))
    return out


def startup_identifiers(texts=None):
    """{name: 'refusal' or 'limit'}: each exported variable of the start-up module, which must be
    an errors.New value (a refusal), and each exported constant whose value is not a string
    literal (a limit). Refuses an exported variable of any other form."""
    out = {}
    for path, text in sorted((startup_sources() if texts is None else texts).items()):
        for name, rest in declarations(text, 'var'):
            if not name[0].isupper():
                continue
            if not re.match(r'^\s*=\s*errors\.New\(', rest):
                refuse('%s: the exported variable %s is not an errors.New value' % (path, name))
            out[name] = 'refusal'
        for name, rest in declarations(text, 'const'):
            if name[0].isupper() and not re.match(r'^(?:\s+[A-Za-z_]\w*)?\s*=\s*"', rest):
                out[name] = 'limit'
    return out


def functions(text):
    """[(name, source)] for each top-level function or method of a Go file's text."""
    out = []
    for m in FUNC.finditer(text):
        line_end = text.find('\n', m.end())
        line_end = len(text) if line_end < 0 else line_end
        if text[:line_end].rstrip().endswith('}'):
            out.append((m.group(1), text[m.start():line_end]))
            continue
        end = re.search(r'^}', text[m.end():], re.M)
        out.append((m.group(1), text[m.start():m.end() + (end.end() if end else len(text) - m.end())]))
    return out


def startup_sites(identifiers, texts=None):
    """{(name, 'path:function')}: each function of the module that uses an identifier, outside its
    comments and string literals and as a whole word."""
    sites = set()
    for path, text in sorted((startup_sources() if texts is None else texts).items()):
        for function, body in functions(text):
            code, _ = go_code(body)
            for name in identifiers:
                if re.search(r'(?<![\w.])%s(?!\w)' % re.escape(name), code):
                    sites.add((name, '%s:%s' % (path, function)))
    return sites


def startup_mutant_problem(body, mutant, name, identifiers):
    """Why the mutant cannot apply to a function's body as a mutant of the row's identifier, or
    None. It must be `every A becomes B`, A the row's identifier and in the body outside its
    comments as a whole word; for a refusal B is another refusal, for a limit `A+1` or `A-1`."""
    m = MUTANT.fullmatch(mutant)
    if not m:
        return 'is not of the form every `A` becomes `B`'
    old, new = m.group(1), m.group(2)
    if old != name:
        return 'changes %s, not the row\'s %s' % (old, name)
    if identifiers.get(name) == 'refusal' and (new == old or identifiers.get(new) != 'refusal'):
        return 'changes %s into %s, which is not another refusal of the module' % (old, new)
    if identifiers.get(name) == 'limit' and new not in (old + '+1', old + '-1'):
        return 'changes %s into %s, not %s+1 or %s-1' % (old, new, old, old)
    if re.search(r'(?<![\w.])%s(?!\w)' % re.escape(old), go_code(body)[0]) is None:
        return 'changes %s, which its function does not hold outside its comments' % old
    return None


def startup_problems(rows, identifiers, sites, texts=None):
    """The problems of the start-up table's rows, and {test: [where]} for the listing: a row whose
    identifier is no refusal or limit of the module, whose site does not use it, a second row for
    one site, a mutant that does not apply, a test the module's test files do not declare or that
    does not name the identifier, and a site with no row."""
    texts = startup_sources() if texts is None else texts
    out, seen, tests = [], {}, {}
    for number, row in rows:
        name, site = row['identifier'], row['mutant site']
        where = '%s:%d: %s' % (TABLE, number, name)
        if name not in identifiers:
            out.append('%s: no exported error value or limit of %s/ is named so' % (where, STARTUP))
            continue
        if (name, site) not in sites:
            out.append('%s: mutant site %s is no function of %s/ that uses it' % (where, site, STARTUP))
            continue
        if (name, site) in seen:
            out.append('%s: a second row for %s (the first is line %d)' % (where, site, seen[name, site]))
            continue
        seen[name, site] = number
        path, function = site.rsplit(':', 1)
        body = dict(functions(texts[path]))[function]
        why = startup_mutant_problem(body, row['mutant'], name, identifiers)
        if why:
            out.append('%s: the mutant %s' % (where, why))
        parent = row['test'].split('/')[0]
        if not re.fullmatch(r'Test\w*(?:/\w[^/]*)*', row['test']):
            out.append('%s: test %s is not of the form TestX or TestX/sub' % (where, row['test']))
            continue
        test = test_body(STARTUP, parent)
        if test is None:
            out.append('%s: no func %s in the test files of %s/' % (where, parent, STARTUP))
            continue
        scope = test
        for part in row['test'].split('/')[1:]:
            scope = subtest_case(scope, part)
            if scope is None:
                out.append('%s: no string in %s names its subtest %s' % (where, parent, part))
                break
        if scope is None:
            continue
        if re.search(r'(?<![\w.])%s(?!\w)' % re.escape(name), go_code(test)[0]) is None:
            out.append('%s: %s does not name %s outside its comments' % (where, parent, name))
        tests.setdefault(parent, []).append(where)
    for name, site in sorted(sites - set(seen)):
        out.append('%s: %s in %s has no row' % (TABLE, name, site))
    for name in sorted(set(identifiers) - {n for n, _ in sites}):
        out.append('%s: %s is used in no function of %s/, so no mutant can show a test holds it' % (TABLE, name, STARTUP))
    return out, tests


def startup_listing(tests):
    """The problems of the start-up rows' tests: each must be declared in a test file of the module
    on linux, darwin or windows (go list), and one declared in a file this host builds must be
    listed by go test -list here. A test outside Unix is listed by its file, not run here."""
    out = []
    if not tests:
        return out
    files = {}
    for goos in STARTUP_GOOS:
        listed = subprocess.run(['go', 'list', '-f', '{{join .TestGoFiles "\\n"}}', '.'], cwd=STARTUP, capture_output=True,
                                text=True, env=dict(os.environ, GOOS=goos))
        if listed.returncode != 0:
            out.append('go list (GOOS=%s) in %s/ failed: %s' % (goos, STARTUP, (listed.stderr or listed.stdout).strip()))
            return out
        files[goos] = set(listed.stdout.split())
    host = subprocess.run(['go', 'env', 'GOOS'], cwd=STARTUP, capture_output=True, text=True).stdout.strip()
    here = []
    for name, wheres in sorted(tests.items()):
        declared = [f for f in sorted(os.listdir(STARTUP)) if f.endswith('_test.go') and
                    re.search(r'^func %s\(t \*testing\.T\) \{' % re.escape(name),
                              open(os.path.join(STARTUP, f), encoding='utf-8').read(), re.M)]
        if not declared or not any(declared[0] in files[goos] for goos in STARTUP_GOOS):
            out.extend('%s: %s is no test of a test file of %s/ on %s' % (w, name, STARTUP, ', '.join(STARTUP_GOOS)) for w in wheres)
        elif declared[0] in files.get(host, set()):
            here.append(name)
    if here:
        listed = subprocess.run(['go', 'test', '-list', '^(' + '|'.join(here) + ')$', '.'], cwd=STARTUP, capture_output=True,
                                text=True)
        if listed.returncode != 0:
            out.append('go test -list in %s/ failed: %s' % (STARTUP, (listed.stderr or listed.stdout).strip()))
            return out
        found = set(listed.stdout.split('\n'))
        for name in here:
            if name not in found:
                out.extend('%s: go test -list does not list %s in %s/' % (w, name, STARTUP) for w in tests[name])
    return out


def main():
    codes = derive()
    marks = provisional_marks()
    rows = read_table()
    seen = {}
    tests = {}
    for number, row in rows:
        where = '%s:%d: %s' % (TABLE, number, row['code'])
        code = row['code']
        if code in seen:
            problems.append('%s: a second row (the first is line %d)' % (where, seen[code]))
            continue
        seen[code] = number
        if code not in codes:
            problems.append('%s: no code table defines this code' % where)
            continue
        stated = codes[code]
        if row['severity'] not in ('error', 'warning') or stated is not None and row['severity'] != stated:
            problems.append('%s: severity %s, its table says %s' % (where, row['severity'], stated or 'error or warning'))
        if row['provisional'] not in ('yes', 'no', 'n/a'):
            problems.append('%s: provisional is %s, not yes, no or n/a' % (where, row['provisional']))
        elif provisional_problem(code, row['provisional'], marks):
            problems.append('%s: %s' % (where, provisional_problem(code, row['provisional'], marks)))
        site = re.fullmatch(r'([\w./-]+\.go):(\w+)', row['mutant site'])
        body = None
        if site and not site.group(1).endswith('_test.go') and os.path.isfile(site.group(1)):
            body = function_body(site.group(1), site.group(2))
        if body is None:
            problems.append('%s: mutant site %s is no function of a non-test Go file' % (where, row['mutant site']))
        if not row['mutant']:
            problems.append('%s: no mutant' % where)
        elif body is not None:
            why = mutant_problem(body, row['mutant'], code, package_constants(os.path.dirname(site.group(1))))
            if why:
                problems.append('%s: the mutant %s' % (where, why))
        package = row['package']
        parent = row['test'].split('/')[0]
        if not re.fullmatch(r'\./[\w./-]+', package) or not os.path.isdir(package) or not re.fullmatch(r'Test\w*', parent):
            problems.append('%s: package %s or test %s is not of the form ./dir and TestX or TestX/sub' % (where, package, row['test']))
            continue
        tests.setdefault(package, {}).setdefault(parent, []).append(where)
        body = test_body(package, parent)
        if body is None:
            problems.append('%s: no func %s in the test files of %s' % (where, parent, package))
            continue
        # The row's test must name its code. A subtest's case that names any
        # code of the line must name this one; a case that names none (its
        # code is fixed after the table) leaves it to the test's body.
        names = all_constants(package)
        scope = body
        for part in row['test'].split('/')[1:]:
            scope = subtest_case(scope, part)
            if scope is None:
                problems.append('%s: no string in %s names its subtest %s' % (where, parent, part))
                break
        if scope is None:
            continue
        in_case = named_codes(scope, codes, names)
        if in_case and code not in in_case:
            problems.append('%s: the case %s names %s, not this code' % (where, row['test'], ', '.join(sorted(in_case))))
        elif not in_case and code not in named_codes(body, codes, names):
            problems.append('%s: %s does not name the code, as a literal or a constant holding it' % (where, parent))
    problems.extend(missing_rows(codes, seen))
    for package in sorted(tests):
        names = sorted(tests[package])
        listed = subprocess.run(['go', 'test', '-list', '^(' + '|'.join(names) + ')$', package], capture_output=True, text=True)
        if listed.returncode != 0:
            problems.append('go test -list %s failed: %s' % (package, (listed.stderr or listed.stdout).strip()))
            continue
        found = set(listed.stdout.split('\n'))
        for name in names:
            if name not in found:
                for where in tests[package][name]:
                    problems.append('%s: go test -list does not list %s in %s' % (where, name, package))
    identifiers = startup_identifiers()
    startup_rows = read_table(STARTUP_HEADER)
    found, startup_tests = startup_problems(startup_rows, identifiers, startup_sites(identifiers))
    problems.extend(found)
    problems.extend(startup_listing(startup_tests))
    for p in problems:
        print('mutation table: ' + p, file=sys.stderr)
    if problems:
        print('mutation table: %d problems' % len(problems))
    else:
        print('mutation table: %d rows, one for each code, and %d start-up rows, one for each function '
              'that uses a refusal or limit; every test is listed' % (len(rows), len(startup_rows)))
    sys.exit(1 if problems else 0)


if __name__ == '__main__':
    main()
