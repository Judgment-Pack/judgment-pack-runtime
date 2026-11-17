#!/usr/bin/env python3
"""Probes of check-mutation-table.py's test rules, on this tree's own tests.

Each probe changes a copy of a real test body in memory and asserts the rule
refuses it. Run from the repository root, after the check.
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

for name in failures:
    print('mutation table probe: failed: ' + name, file=sys.stderr)
print('mutation table probe: %d failed' % len(failures) if failures else 'mutation table probe: every probe passed')
sys.exit(1 if failures else 0)
