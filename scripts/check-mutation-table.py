#!/usr/bin/env python3
"""Check docs/registry-mutation-table.md against the code tables it covers.

The code set is read from the source, as the table's scope rule states, never listed here:
the keys of `var Codes` in internal/registry/diag.go, of `var FactsCodes` in
internal/registry/facts.go and of `var Codes` in internal/emit/codes.go; the names
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
prints it, spaces as underscores). Run from the repository root. Standard library only; it runs
`go test -list`, one package at a time.
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
                out[code] = 'yes' if 'provisional' in heading else 'no'
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
    of `var Codes` in internal/emit/codes.go by its section's heading; a code held by a constant
    (Codes() in internal/terraformfile/codes.go names them, and every other FACTS- constant) as
    provisional when the constant's comment says so; an emitter's x- code is n/a. The source marks
    no other code, and the registry's tables mark none, so their rows are not compared."""
    marks = section_marks('internal/emit/codes.go', 'Codes')
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
            marks.setdefault(code, 'yes')
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
                       ('internal/emit/codes.go', 'Codes')):
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


def cells(line):
    if not (line.startswith('|') and line.endswith('|')):
        return None
    return [c.strip() for c in line[1:-1].split('|')]


def read_table():
    rows = []
    lines = open(TABLE, encoding='utf-8').read().split('\n')
    heads = [i for i, line in enumerate(lines) if cells(line) == HEADER]
    if len(heads) != 1:
        refuse('%s: no single table headed %s' % (TABLE, ' | '.join(HEADER)))
    for number, line in enumerate(lines[heads[0] + 2:], heads[0] + 3):
        row = cells(line)
        if row is None:
            break
        if len(row) != len(HEADER):
            problems.append('%s:%d: %d cells, want %d' % (TABLE, number, len(row), len(HEADER)))
            continue
        rows.append((number, dict(zip(HEADER, (c[1:-1] if len(c) > 1 and c[0] == c[-1] == '`' else c for c in row)))))
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


def go_code(text):
    """text, a piece of Go source, with its comments removed and every string or rune literal
    replaced by a blank, and the interpreted string literals it holds, as written."""
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
            i = n if end < 0 else end + 1
            out.append(' ')
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
    for code in sorted(set(codes) - set(seen)):
        problems.append('%s: %s has no row' % (TABLE, code))
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
    for p in problems:
        print('mutation table: ' + p, file=sys.stderr)
    if problems:
        print('mutation table: %d problems' % len(problems))
    else:
        print('mutation table: %d rows, one for each code; every test is listed' % len(rows))
    sys.exit(1 if problems else 0)


if __name__ == '__main__':
    main()
