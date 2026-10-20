#!/usr/bin/env python3
"""Validate the MCP registry server.json template, rendered at a sample version, against the
vendored schema. The schema copy is the one the release workflow's pinned mcp-publisher
enforces; bump both together (ADR-0013). Needs jsonschema (see dev.json setup)."""
import json
import sys

import jsonschema

template = open('build/mcp-registry/server.json.tmpl', encoding='utf-8').read()
doc = json.loads(template.replace('__VERSION__', '9.9.9-rc.1'))
schema = json.load(open('build/mcp-registry/server.schema-2025-12-11.json', encoding='utf-8'))
errors = [
    '/'.join(map(str, e.path)) + ' - ' + e.message
    for e in jsonschema.Draft202012Validator(schema).iter_errors(doc)
]
# Server-side rules the registry enforces beyond its schema, learned from live rejections
# (v0.5.0-rc.1): an OCI package carries its version in the identifier tag and must not have a
# version member.
for i, package in enumerate(doc.get('packages', [])):
    if package.get('registryType') == 'oci':
        if 'version' in package:
            errors.append(f'packages/{i} - OCI packages must not have a version member')
        if ':' not in package.get('identifier', ''):
            errors.append(f'packages/{i} - OCI identifier must carry the version as its tag')
for e in errors:
    print('INVALID:', e, file=sys.stderr)
if not errors:
    print('registry template: valid')
sys.exit(1 if errors else 0)
