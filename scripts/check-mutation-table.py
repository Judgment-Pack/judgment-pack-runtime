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
file, and a row whose test `go test -list` does not list in the row's package (for `TestX/sub`,
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
    tracked = subprocess.run(['git', 'ls-files', '-z', '--', '*.go'], capture_output=True, check=True).stdout
    module = [p for p in tracked.decode().split('\0') if p and not p.endswith('_test.go')]
    nested = {os.path.dirname(p) for p in subprocess.run(['git', 'ls-files', '-z', '--', '*go.mod'], capture_output=True,
                                                         check=True).stdout.decode().split('\0') if p and p != 'go.mod'}
    module = [p for p in module if not any(p.startswith(d + '/') for d in nested) and '/testdata/' not in p]
    for path in module:
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


def has_function(path, name):
    text = open(path, encoding='utf-8').read()
    return re.search(r'^func (?:\([^)]*\) )?%s(?:\[[^\]]*\])?\(' % re.escape(name), text, re.M) is not None


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


def subtest_named(body, part):
    """Whether a string literal in body names subtest part as go test
    prints it (spaces become underscores)."""
    for literal in re.findall(r'"((?:[^"\\\n]|\\.)*)"|`([^`]*)`', body):
        text = literal[0] or literal[1]
        if text.replace(' ', '_') == part:
            return True
    return False


def main():
    codes = derive()
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
        site = re.fullmatch(r'([\w./-]+\.go):(\w+)', row['mutant site'])
        if not site or site.group(1).endswith('_test.go') or not os.path.isfile(site.group(1)) \
                or not has_function(site.group(1), site.group(2)):
            problems.append('%s: mutant site %s is no function of a non-test Go file' % (where, row['mutant site']))
        if not row['mutant']:
            problems.append('%s: no mutant' % where)
        package = row['package']
        parent = row['test'].split('/')[0]
        if not re.fullmatch(r'\./[\w./-]+', package) or not os.path.isdir(package) or not re.fullmatch(r'Test\w*', parent):
            problems.append('%s: package %s or test %s is not of the form ./dir and TestX or TestX/sub' % (where, package, row['test']))
            continue
        tests.setdefault(package, {}).setdefault(parent, []).append(where)
        subtests = row['test'].split('/')[1:]
        if subtests:
            body = test_body(package, parent)
            for part in subtests:
                if body is not None and not subtest_named(body, part):
                    problems.append('%s: no string in %s names its subtest %s' % (where, parent, part))
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
