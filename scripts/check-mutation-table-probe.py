#!/usr/bin/env python3
"""Probes of check-mutation-table.py's rules, on this tree's own tests and source.

Each probe changes a copy of a real test body, function or code table in
memory, or a mutant or provisional value, and asserts the rule refuses it.
Run from the repository root, after the check.
"""
import contextlib
import importlib.util
import io
import os
import sys

# The probe imports the check; it leaves no bytecode in the tree.
sys.dont_write_bytecode = True

spec = importlib.util.spec_from_file_location('check', os.path.join('scripts', 'check-mutation-table.py'))
check = importlib.util.module_from_spec(spec)
spec.loader.exec_module(check)

failures = []


def expect(name, ok):
    if not ok:
        failures.append(name)


codes = check.derive()
names = check.all_constants('./internal/registry')
body = check.test_body('./internal/registry', 'TestAdmission')
expect('the parent test is found', body is not None)
if body is not None:
    case = check.subtest_case(body, 'too_deep')
    expect('the too deep case names REG-DOC-DEPTH', case is not None and check.named_codes(case, codes, names) == {'REG-DOC-DEPTH'})
    # A case changed to expect another code no longer names the row's.
    changed = body.replace('"REG-DOC-DEPTH"},', '"REG-DOC-JSON"},', 1)
    expect('the probe changes the case', changed != body)
    case = check.subtest_case(changed, 'too_deep')
    expect('a too deep case expecting REG-DOC-JSON is refused for REG-DOC-DEPTH',
           case is not None and 'REG-DOC-DEPTH' not in check.named_codes(case, codes, names))
    # The case ends at the next case: the following one's code is not in it.
    case = check.subtest_case(body, 'lone_surrogate')
    expect('a case ends before the next case', case is not None and 'REG-DOC-DEPTH' not in check.named_codes(case, codes, names))
    expect('a renamed subtest is not found', check.subtest_case(body, 'too_deep_gone') is None)
expect('a missing parent test is not found', check.test_body('./internal/registry', 'TestNoSuchTestHere') is None)

# The mutant must change the row's own code, where its function holds it.
admit = check.function_body('internal/registry/admit.go', 'admitValue')
expect('the mutant site\'s body is found', admit is not None and admit.startswith('func admitValue('))
registry_names = check.package_constants('internal/registry')
if admit is not None:
    expect('a mutant of the row\'s code its function holds applies',
           check.mutant_problem(admit, 'every `"REG-DOC-DEPTH"` becomes `"REG-DOC-JSON"`', 'REG-DOC-DEPTH', registry_names) is None)
    expect('a mutant of another code than the row\'s is refused',
           check.mutant_problem(admit, 'every `"REG-DOC-JSON"` becomes `"REG-DOC-SIZE"`', 'REG-DOC-DEPTH', registry_names) is not None)
    expect('a mutant of a code its function does not hold is refused',
           check.mutant_problem(admit, 'every `"REG-DOC-SELECTOR"` becomes `"REG-DOC-JSON"`', 'REG-DOC-SELECTOR', registry_names) is not None)
    expect('a mutant that changes nothing is refused',
           check.mutant_problem(admit, 'every `"REG-DOC-DEPTH"` becomes `"REG-DOC-DEPTH"`', 'REG-DOC-DEPTH', registry_names) is not None)
    expect('a mutant into blanks is refused',
           check.mutant_problem(admit, 'every `"REG-DOC-DEPTH"` becomes ` `', 'REG-DOC-DEPTH', registry_names) is not None)
    expect('a mutant not of the form is refused',
           check.mutant_problem(admit, 'REG-DOC-DEPTH becomes REG-DOC-JSON', 'REG-DOC-DEPTH', registry_names) is not None)
    # The same body with the code only in a comment, a URL in a string kept.
    commented = admit.replace('"REG-DOC-DEPTH"', '"https://example.invalid/x" // was "REG-DOC-DEPTH"')
    expect('a code only in a comment is refused',
           check.mutant_problem(commented, 'every `"REG-DOC-DEPTH"` becomes `"REG-DOC-JSON"`', 'REG-DOC-DEPTH', registry_names) is not None)
    expect('a code in a block comment is refused',
           check.mutant_problem(admit.replace('"REG-DOC-DEPTH"', '/* "REG-DOC-DEPTH" */ at'),
                                'every `"REG-DOC-DEPTH"` becomes `"REG-DOC-JSON"`', 'REG-DOC-DEPTH', registry_names) is not None)
expect('a quoted code is matched as a whole literal',
       check.mutant_problem('x := "EMIT-REQUEST-VERSION"', 'every `"EMIT-REQUEST"` becomes `"EMIT-OPTIONS"`', 'EMIT-REQUEST', {}) is not None)
expect('a quoted code inside a longer literal is refused',
       check.mutant_problem('x := "see \\"EMIT-REQUEST\\""', 'every `"EMIT-REQUEST"` becomes `"EMIT-OPTIONS"`', 'EMIT-REQUEST', {}) is not None)
read = check.function_body('internal/terraformfile/read.go', 'read')
tf_names = check.package_constants('internal/terraformfile')
expect('the identifier site\'s body is found', read is not None)
if read is not None:
    expect('a mutant of a constant holding the row\'s code applies',
           check.mutant_problem(read, 'every `CodeSize` becomes `CodeJSON`', 'TF-SIZE', tf_names) is None)
    expect('a mutant of a constant holding another code is refused',
           check.mutant_problem(read, 'every `CodeJSON` becomes `CodeFormat`', 'TF-SIZE', tf_names) is not None)
    expect('an identifier that is no constant is refused',
           check.mutant_problem(read, 'every `limit` becomes `CodeJSON`', 'TF-SIZE', tf_names) is not None)
expect('an identifier is matched as a whole word (a prefix is refused)',
       check.mutant_problem('x := CodeSizeLimit', 'every `CodeSize` becomes `CodeJSON`', 'TF-SIZE', {'CodeSize': 'TF-SIZE'}) is not None)
expect('a qualified identifier is not the package\'s own',
       check.mutant_problem('x := other.CodeSize', 'every `CodeSize` becomes `CodeJSON`', 'TF-SIZE', {'CodeSize': 'TF-SIZE'}) is not None)
expect('an identifier only in a comment is refused',
       check.mutant_problem('x := 1 // CodeSize', 'every `CodeSize` becomes `CodeJSON`', 'TF-SIZE', {'CodeSize': 'TF-SIZE'}) is not None)

# The provisional column follows the source's marking.
marks = check.provisional_marks()
expect('the source marks a companion code not provisional', marks.get('EMIT-PROCESS') == 'no')
expect('the source marks a code the companion took from this runtime not provisional', marks.get('EMIT-STAGING-LEFTOVER') == 'no')
expect('the source marks a terraform-file code, released on the registry line, not provisional', marks.get('TF-VALUE-LIMIT') == 'no')
expect('the source marks FACTS-ID-LIMIT not provisional', marks.get('FACTS-ID-LIMIT') == 'no')
expect('the source marks a registry code not provisional', marks.get('REG-DOC-DEPTH') == 'no' and marks.get('REG-YAML-SYNTAX') == 'no')
expect('the source marks a FACTS- code a registry table lists not provisional', marks.get('FACTS-DOCUMENT-LIMIT') == 'no')
expect('the source marks every code', set(check.derive()) <= set(marks))
expect('an emitter code is n/a', all(v == 'n/a' for k, v in marks.items() if k.startswith('x-')) and any(k.startswith('x-') for k in marks))
expect('a row that says yes for a terraform-file code is refused', check.provisional_problem('TF-VALUE-LIMIT', 'yes', marks) is not None)
expect('a row that says no for a provisional code is refused',
       check.provisional_problem('TF-VALUE-LIMIT', 'no', dict(marks, **{'TF-VALUE-LIMIT': 'yes'})) is not None)
expect('a row that says yes for a companion code is refused', check.provisional_problem('EMIT-PROCESS', 'yes', marks) is not None)
expect('a row that says yes for a code the companion took is refused', check.provisional_problem('EMIT-STAGING-LEFTOVER', 'yes', marks) is not None)
expect('a row that says yes for FACTS-ID-LIMIT is refused', check.provisional_problem('FACTS-ID-LIMIT', 'yes', marks) is not None)
expect('n/a for a code that is not an emitter\'s is refused', check.provisional_problem('REG-DOC-DEPTH', 'n/a', marks) is not None)
expect('a row that matches the source passes', check.provisional_problem('EMIT-PROCESS', 'no', marks) is None)
expect('a row that says yes for a registry code is refused', check.provisional_problem('REG-YAML-SYNTAX', 'yes', marks) is not None)
expect('a row that says yes for a FACTS- code a registry table lists is refused',
       check.provisional_problem('FACTS-CHECK-LIMIT', 'yes', marks) is not None)
diag = open('internal/registry/diag.go', encoding='utf-8').read()
diag_marked = diag.replace('\t// Step 8: bounds.\n', '\t// Step 8: bounds, provisional.\n', 1)
expect('the probe changes a registry heading', diag_marked != diag)
diag_sections = check.section_marks('internal/registry/diag.go', 'Codes', diag_marked)
expect('a registry section whose heading says provisional is provisional', diag_sections.get('REG-BOUND-ENTRIES') == 'yes')
expect('a registry code above that heading is not', diag_sections.get('REG-DOC-DEPTH') == 'no')
emit_codes = open('internal/emit/codes.go', encoding='utf-8').read()
marked = emit_codes.replace('\t"EMIT-STAGING-LEFTOVER":', "\t// This runtime's, provisional.\n\t\"EMIT-STAGING-LEFTOVER\":", 1)
expect('the probe adds a heading', marked != emit_codes)
sections = check.section_marks('internal/emit/codes.go', 'Codes', marked)
expect('a section whose heading says provisional is provisional', sections.get('EMIT-STAGING-LEFTOVER') == 'yes')
expect('a code above that heading is not', sections.get('EMIT-INTERRUPTED') == 'no')
expect('the next heading ends that section', sections.get('EMIT-STRAY-WRITE') == 'no')
unmarked = marked.replace("\t// This runtime's, provisional.\n", "\t// This runtime's.\n", 1)
expect('a section whose heading drops provisional is not provisional',
       check.section_marks('internal/emit/codes.go', 'Codes', unmarked).get('EMIT-STAGING-LEFTOVER') == 'no')
facts_cli = open('internal/cli/terraform_facts.go', encoding='utf-8').read()
expect('a constant\'s comment that does not say provisional does not mark it', 'provisional' not in check.const_comments('', facts_cli).get('CodeFactsIDLimit', 'provisional'))
provisional = facts_cli.replace("is the registry companion's code", "is this runtime's provisional code", 1)
expect('the probe changes the comment', provisional != facts_cli)
expect('a constant\'s comment marks it provisional', 'provisional' in check.const_comments('', provisional).get('CodeFactsIDLimit', ''))
tf = open('internal/terraformfile/codes.go', encoding='utf-8').read()
expect('a const block\'s released comment does not mark its members', 'provisional' not in check.const_comments('', tf).get('CodeValueLimit', 'provisional'))
tf_marked = tf.replace('They are part of the registry line from v0.52.0', 'They are provisional', 1)
expect('the probe changes the const block\'s comment', tf_marked != tf)
expect('a const block\'s comment marks its members', 'provisional' in check.const_comments('', tf_marked).get('CodeValueLimit', ''))

# The start-up module's rows: its refusals and limits, read from its source.
ids = check.startup_identifiers()
expect('an exported error value is a refusal', ids.get('ErrWorldReadable') == 'refusal')
expect('an error value of a file built outside Unix is a refusal', ids.get('ErrPermissionsUnknown') == 'refusal')
expect('an exported numeric constant is a limit', ids.get('MaxSecretBytes') == 'limit' and ids.get('MaxResolvedDepth') == 'limit')
expect('a string constant is not a limit', 'Redacted' not in ids)
expect('an unexported constant is not a limit', 'openNoWait' not in ids)
texts = check.startup_sources()
reader = texts['startup/reader.go']
added = reader.replace('\tErrNoBinding = errors.New(', '\tErrProbe = errors.New("probe")\n\tErrNoBinding = errors.New(', 1)
expect('the probe adds an error value', added != reader)
added_ids = check.startup_identifiers(dict(texts, **{'startup/reader.go': added}))
expect('an added error value is a refusal', added_ids.get('ErrProbe') == 'refusal')
odd = reader.replace('\tErrNoBinding = errors.New(', '\tErrProbe = fmt.Errorf("probe")\n\tErrNoBinding = errors.New(', 1)
with contextlib.redirect_stderr(io.StringIO()):
    try:
        check.startup_identifiers(dict(texts, **{'startup/reader.go': odd}))
        refused = False
    except SystemExit:
        refused = True
expect('an exported variable that is not an errors.New value is refused', refused)
sites = check.startup_sites(ids)
expect('a refusal is found in each function that uses it',
       {('ErrLimit', 'startup/reader.go:admitValue'), ('ErrLimit', 'startup/resolved.go:ReadResolved'),
        ('ErrLimit', 'startup/resolved.go:scan')} <= sites)
expect('a refusal of a file built outside Unix has its site',
       ('ErrPermissionsUnknown', 'startup/permissions_other.go:checkPermissions') in sites)
expect('a method that uses a refusal is a site', ('ErrNotDecodable', 'startup/secret.go:UnmarshalText') in sites)
commented = reader.replace('return Secret{}, fmt.Errorf("%w: %s", ErrNotSet, name)', 'return Secret{}, nil // ErrNotSet', 1)
expect('the probe moves a refusal into a comment', commented != reader)
expect('an identifier only in a comment is no site',
       ('ErrNotSet', 'startup/reader.go:ReadEnv') not in check.startup_sites(ids, dict(texts, **{'startup/reader.go': commented})))
rows = check.read_table(check.STARTUP_HEADER)
found, tests = check.startup_problems(rows, ids, sites)
expect('the start-up rows have no problem', found == [])
expect('a row is missing', any('has no row' in p for p in check.startup_problems(rows[1:], ids, sites)[0]))
expect('a refusal with no site is refused',
       any('used in no function' in p for p in check.startup_problems(rows, added_ids, sites)[0]))


def changed(index, **cells):
    copy = list(rows)
    copy[index] = (copy[index][0], dict(copy[index][1], **cells))
    return check.startup_problems(copy, ids, sites)[0]


expect('a row naming no identifier of the module is refused', changed(0, identifier='ErrNoSuchThing') != [])
expect('a row whose site does not use its identifier is refused',
       changed(0, **{'mutant site': 'startup/reader.go:ReadEnv'}) != [])
expect('a second row for one site is refused', check.startup_problems(rows + rows[:1], ids, sites)[0] != [])
expect('a test that does not name the identifier is refused', changed(0, test='TestReadResolvedFile') != [])
expect('a test the module does not declare is refused', changed(0, test='TestNoSuchTestHere') != [])
admit = dict(check.functions(reader))['admitValue']
expect('a refusal mutant into another refusal applies',
       check.startup_mutant_problem(admit, 'every `ErrEmpty` becomes `ErrLimit`', 'ErrEmpty', ids) is None)
expect('a refusal mutant into a limit is refused',
       check.startup_mutant_problem(admit, 'every `ErrEmpty` becomes `MaxSecretBytes`', 'ErrEmpty', ids) is not None)
expect('a refusal mutant into itself is refused',
       check.startup_mutant_problem(admit, 'every `ErrEmpty` becomes `ErrEmpty`', 'ErrEmpty', ids) is not None)
expect('a mutant of another identifier than the row\'s is refused',
       check.startup_mutant_problem(admit, 'every `ErrLimit` becomes `ErrEmpty`', 'ErrEmpty', ids) is not None)
expect('a limit mutant by one applies',
       check.startup_mutant_problem(admit, 'every `MaxSecretBytes` becomes `MaxSecretBytes-1`', 'MaxSecretBytes', ids) is None)
expect('a limit mutant by two is refused',
       check.startup_mutant_problem(admit, 'every `MaxSecretBytes` becomes `MaxSecretBytes+2`', 'MaxSecretBytes', ids) is not None)
expect('a limit mutant into a refusal is refused',
       check.startup_mutant_problem(admit, 'every `MaxSecretBytes` becomes `ErrLimit`', 'MaxSecretBytes', ids) is not None)
expect('a mutant of an identifier its function does not hold is refused',
       check.startup_mutant_problem(admit, 'every `ErrNotSet` becomes `ErrEmpty`', 'ErrNotSet', ids) is not None)
expect('a test in a file built only outside Unix is declared', check.startup_listing({'TestReadFileRefusesEveryFileOutsideUnix': ['x']}) == [])
expect('a test no file declares is refused', check.startup_listing({'TestNoSuchTestHere': ['x']}) != [])

# Pending codes (registry 1.1.0's classes not built yet): no row, named by no non-test file outside
# their table, and none left once no class is.
federation_codes = open(check.FEDERATION_CODES, encoding='utf-8').read()
pending = check.pending_codes(federation_codes)
classes = check.pending_classes()
expect('the pending codes are the LOCK- codes', pending != set() and all(c.startswith('LOCK-') for c in pending))
expect('no FED- code is pending', not any(c.startswith('FED-') for c in pending))
expect('the tree holds its pending codes', check.pending_problems(pending, classes, {}, {check.FEDERATION_CODES: federation_codes}) == [])
lock = sorted(pending)[0]
expect('a row for a pending code is refused', check.pending_problems(pending, classes, {lock: 1}, {}) != [])
expect('a file that raises a pending code is refused',
       check.pending_problems(pending, classes, {}, {'internal/federation/lock.go': 'func f() { add("%s", at) }' % lock}) != [])
expect('a pending code in a comment is not raised',
       check.pending_problems(pending, classes, {}, {'internal/federation/lock.go': '// add("%s")' % lock}) == [])
expect('pending codes with no class left to build are refused', check.pending_problems(pending, [], {}, {}) != [])
unmarked = federation_codes.replace('pending', 'built')
expect('a section heading that does not say pending holds no pending code', check.pending_codes(unmarked) == set())
with contextlib.redirect_stderr(io.StringIO()):
    try:
        check.pending_classes('var pendingClasses []string')
        refused = False
    except SystemExit:
        refused = True
expect('a pendingClasses list that cannot be read is a refusal, not an empty list', refused)

for name in failures:
    print('mutation table probe: failed: ' + name, file=sys.stderr)
print('mutation table probe: %d failed' % len(failures) if failures else 'mutation table probe: every probe passed')
sys.exit(1 if failures else 0)
