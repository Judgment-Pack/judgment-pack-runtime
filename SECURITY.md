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

Local input files are opened without following a final symlink where the operating system supports
that primitive, then checked as the same regular file before reading. Suite descendants are also
checked for traversal and symlinks. These checks do not make a directory safe against a different
process that can concurrently rename or replace its ancestors; run untrusted suites from a
directory whose ownership and write permissions you control.

Commercial repositories must not be runtime dependencies of this public core and must not override
the behavior of `judgment-pack spec` commands.
