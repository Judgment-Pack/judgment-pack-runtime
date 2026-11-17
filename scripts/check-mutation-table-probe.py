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

# The mutant must change what its function holds.
admit = check.function_body('internal/registry/admit.go', 'admitValue')
expect('the mutant site\'s body is found', admit is not None and admit.startswith('func admitValue('))
if admit is not None:
    expect('a mutant of a code its function holds applies',
           check.mutant_problem(admit, 'every `"REG-DOC-DEPTH"` becomes `"REG-DOC-JSON"`') is None)
    expect('a mutant of a code its function does not hold is refused',
           check.mutant_problem(admit, 'every `"REG-DOC-SELECTOR"` becomes `"REG-DOC-JSON"`') is not None)
    expect('a mutant that changes nothing is refused',
           check.mutant_problem(admit, 'every `"REG-DOC-DEPTH"` becomes `"REG-DOC-DEPTH"`') is not None)
    expect('a mutant not of the form is refused', check.mutant_problem(admit, 'REG-DOC-DEPTH becomes REG-DOC-JSON') is not None)
    expect('a quoted code is matched with its closing quote',
           check.mutant_problem('x := "EMIT-REQUEST-VERSION"', 'every `"EMIT-REQUEST"` becomes `"EMIT-OPTIONS"`') is not None)
    expect('an identifier is matched as a whole word',
           check.mutant_problem('x := CodeSizeLimit', 'every `CodeSize` becomes `CodeJSON`') is not None)
    expect('a qualified identifier is not the package\'s own',
           check.mutant_problem('x := other.CodeSize', 'every `CodeSize` becomes `CodeJSON`') is not None)

# The provisional column follows the source's marking.
marks = check.provisional_marks()
expect('the source marks a companion code not provisional', marks.get('EMIT-PROCESS') == 'no')
expect('the source marks a runtime code provisional', marks.get('EMIT-STAGING-LEFTOVER') == 'yes')
expect('the source marks a terraform-file code provisional', marks.get('TF-VALUE-LIMIT') == 'yes')
expect('the source marks FACTS-ID-LIMIT provisional', marks.get('FACTS-ID-LIMIT') == 'yes')
expect('an emitter code is n/a', all(v == 'n/a' for k, v in marks.items() if k.startswith('x-')) and any(k.startswith('x-') for k in marks))
expect('a row that says no for a provisional code is refused', check.provisional_problem('EMIT-STAGING-LEFTOVER', 'no', marks) is not None)
expect('a row that says yes for a companion code is refused', check.provisional_problem('EMIT-PROCESS', 'yes', marks) is not None)
expect('n/a for a code that is not an emitter\'s is refused', check.provisional_problem('REG-DOC-DEPTH', 'n/a', marks) is not None)
expect('a row that matches the source passes', check.provisional_problem('EMIT-PROCESS', 'no', marks) is None)
emit_codes = open('internal/emit/codes.go', encoding='utf-8').read()
unmarked = emit_codes.replace("\t// This runtime's, provisional.\n", "\t// This runtime's.\n", 1)
expect('the probe changes the heading', unmarked != emit_codes)
expect('a section whose heading drops provisional is not provisional',
       check.section_marks('internal/emit/codes.go', 'Codes', unmarked).get('EMIT-STAGING-LEFTOVER') == 'no')
facts_cli = open('internal/cli/terraform_facts.go', encoding='utf-8').read()
expect('a constant\'s comment marks it provisional', 'provisional' in check.const_comments('', facts_cli).get('CodeFactsIDLimit', ''))
plain = facts_cli.replace("this runtime's provisional code", "this runtime's code", 1)
expect('the probe changes the comment', plain != facts_cli)
expect('a constant whose comment drops provisional is not marked', 'provisional' not in check.const_comments('', plain).get('CodeFactsIDLimit', ''))
tf = open('internal/terraformfile/codes.go', encoding='utf-8').read()
expect('a const block\'s comment marks its members', 'provisional' in check.const_comments('', tf).get('CodeValueLimit', ''))

for name in failures:
    print('mutation table probe: failed: ' + name, file=sys.stderr)
print('mutation table probe: %d failed' % len(failures) if failures else 'mutation table probe: every probe passed')
sys.exit(1 if failures else 0)
