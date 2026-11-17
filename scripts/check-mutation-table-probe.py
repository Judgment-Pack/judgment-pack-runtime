#!/usr/bin/env python3
"""Probes of check-mutation-table.py's rules, on this tree's own tests and source.

Each probe changes a copy of a real test body, function or code table in
memory, or a mutant or provisional value, and asserts the rule refuses it.
Run from the repository root, after the check.
"""
import importlib.util
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
expect('the source marks a terraform-file code provisional', marks.get('TF-VALUE-LIMIT') == 'yes')
expect('the source marks FACTS-ID-LIMIT not provisional', marks.get('FACTS-ID-LIMIT') == 'no')
expect('the source marks a registry code not provisional', marks.get('REG-DOC-DEPTH') == 'no' and marks.get('REG-YAML-SYNTAX') == 'no')
expect('the source marks a FACTS- code a registry table lists not provisional', marks.get('FACTS-DOCUMENT-LIMIT') == 'no')
expect('the source marks every code', set(check.derive()) <= set(marks))
expect('an emitter code is n/a', all(v == 'n/a' for k, v in marks.items() if k.startswith('x-')) and any(k.startswith('x-') for k in marks))
expect('a row that says no for a provisional code is refused', check.provisional_problem('TF-VALUE-LIMIT', 'no', marks) is not None)
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
expect('a const block\'s comment marks its members', 'provisional' in check.const_comments('', tf).get('CodeValueLimit', ''))

for name in failures:
    print('mutation table probe: failed: ' + name, file=sys.stderr)
print('mutation table probe: %d failed' % len(failures) if failures else 'mutation table probe: every probe passed')
sys.exit(1 if failures else 0)
