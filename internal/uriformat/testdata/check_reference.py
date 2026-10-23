#!/usr/bin/env python3
"""Compare authored URI cases with the pinned, repaired independent reader.

Run with python3 internal/uriformat/testdata/check_reference.py. This imports the
function's AST only, so it needs no mutable schema resolution or vendor writes.
The jsonschema/rfc3986-validator versions are pinned by the retained Spec reader.
"""
import ast
import ipaddress
import json
from pathlib import Path
import re

from rfc3986_validator import URI_RE_COMP

ROOT = Path(__file__).resolve().parents[3]
REFERENCE = ROOT / "internal/policy/official/rfcs/policy-companion-contract/verify.py"
source = ast.parse(REFERENCE.read_text())
function = next(node for node in source.body if isinstance(node, ast.FunctionDef) and node.name == "candidate_uri")
namespace = {"re": re, "ipaddress": ipaddress, "URI_RE_COMP": URI_RE_COMP}
exec(compile(ast.Module(body=[function], type_ignores=[]), str(REFERENCE), "exec"), namespace)
cases = json.loads(Path(__file__).with_name("cases.json").read_text())
for case in cases:
    actual = namespace["candidate_uri"](case["uri"])
    assert actual == case["valid"], (case, actual)
print(f"repaired independent URI reader: {len(cases)}/{len(cases)} authored cases agree")
