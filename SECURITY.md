# Security policy

## Support status

The CLI is pre-1.0 and provides no security, compatibility, or support service-level guarantee.
Only versions listed on GitHub Releases are supported artifacts. It must not be used as the sole
control for consequential production decisions.

## Reporting a vulnerability

Do not open a public issue for a vulnerability that could expose document content, bypass a
validation layer, cause resource exhaustion, execute untrusted content, escape a suite directory,
forge artifact provenance, or confuse document conformance with authorization.

Use GitHub private vulnerability reporting or a private security advisory when available. If that
is unavailable, contact the judgment-pack-runtime maintainers privately. Include a minimal synthetic
reproduction, affected command and version, likely impact, and any suggested mitigation. Never
include customer packs, credentials, or proprietary data.

## Security boundary

Every pack, suite, manifest, path, filename, extension value, and diagnostic input is untrusted.
The public core must remain offline during validation. It does not fetch locators, execute pack
content, load plugins, invoke subprocesses, or infer permission to act.

Resource-limit failures are operational errors rather than document-invalid results. Passing
validation establishes only the carrier, structural, and semantic document layers reported in the
result.

The evaluator bounds the work an admitted input can require, not only the input's size: an evaluation
above the documented evaluation-work limit is refused with the JPS §8.4 `resource-exhaustion` error and
no disposition, never processed partway. Both of that class's §10 limits — the work limit and the
collection-size bound — are stated in the README's
[experimental evaluation section](README.md#the-two-10-limits-of-the-claimed-class).

The runtime writes in exactly two ways, and neither happens on its own. An operator can ask for a
copy of a bundled schema or example with `--write <target>`, which creates that one file at the
pathname the operator named and refuses to overwrite an existing one. A project can ask to be told
what its packs decided by declaring an `audit` directory in its `jpack.json`
([ADR-0018](docs/adr/0018-opt-in-evaluation-audit-trail.md)); with that member, each completed
evaluation on the CLI's `experimental evaluate`, `experimental graph evaluate`, and the MCP
`experimental_evaluate` tool appends one record to one file in that directory. Nothing else is
created, named, overwritten, or deleted anywhere.

The append is bounded by the directory handle held open on the configuration's own directory and
refuses every escape a read refuses — an absolute or traversing path, a path leaving the root
through a symlinked component, a final component that is a symlink (checked before the open as
well as after it, so a refused append creates nothing), anything that is not a regular file — and
no pathname is handed to a caller to open. That guarantee is about path resolution and symlinks. It
does not extend to a hardlinked alias, which is a second name for one inode and invisible to every
path-based check: the append refuses a trail file with more than one link where the platform
reports the count, but making such an alias requires write access inside the project directory,
which is the same trust domain as editing the packs themselves.

The records deliberately contain the facts and evidence documents that were evaluated: they are the
project's own trail, written where the project asked, and they are not diagnostics. Human
diagnostics remain sanitized and value-free, and a failed append is reported as an input/output
failure carrying no value at all. The trail file's mode is set to owner-only on every append. The
audit directory's mode is set when this runtime creates it and not afterward: a directory that was
already there keeps the mode the project gave it, and its access control is the project's. The file
grows without bound: its retention and whether it belongs in version control are the project's,
exactly as for the packs beside it.

Local input files are opened without following a final symlink where the operating system supports
that primitive, then checked as the same regular file before reading. Suite descendants are also
checked for traversal and symlinks. These checks do not make a directory safe against a different
process that can concurrently rename or replace its ancestors; run untrusted suites from a
directory whose ownership and write permissions you control.

Commercial repositories must not be runtime dependencies of this public core and must not override
the behavior of `jpack spec` commands.
