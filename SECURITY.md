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

The runtime writes in exactly three ways, and none happens on its own. An operator can ask for a
copy of a bundled schema or example with `--write <target>`, which creates that one file at the
pathname the operator named and refuses to overwrite an existing one. A project can ask to be told
what its packs decided by declaring an `audit` directory in its `jpack.json`
([ADR-0018](docs/adr/0018-opt-in-evaluation-audit-trail.md)); with that member, each completed
evaluation on the CLI's `experimental evaluate`, `experimental graph evaluate`, and the MCP
`experimental_evaluate` tool — a declared rehearsal excepted ([ADR-0028](docs/adr/0028-declare-an-evaluation-a-rehearsal.md)) — appends one record to one file in that directory. And an operator can
run `packs lock` ([ADR-0019](docs/adr/0019-reviewed-set-lock.md)), which generates one file beside
the configuration declaring the digests of the documents the project reviewed. Nothing else is
created, named, overwritten, or deleted anywhere.

By default the reviewed-set lock is evidence, not a control. With one in place the deciding surfaces refuse to
evaluate declared law whose bytes differ from it — a declared rehearsal excepted (ADR-0028),
which evaluates without consulting it and records nothing — which turns a silent edit into a refusal and an
explicit re-lock; it does not and cannot stop an editor with write access to the tree, because
`packs lock` is available to whatever can edit a pack and a runtime cannot tell an amendment from
tampering. Treat it as a record that an amendment happened, and place the deciding party outside the
law's write domain if you need more than that. A project can make it a control for some callers:
with `requireReviewed` (configVersion `"4"`, [ADR-0044](docs/adr/0044-say-and-require-the-reviewed-set.md))
the deciding surfaces also refuse every run that applies a draft and every run while no lock exists,
declared rehearsals excepted. That binds a caller who neither chooses which configuration a run
reads nor can edit it or its lock, such as an agent limited to the tools of an MCP server someone
else launched. It does not bind whoever chooses the configuration (`--config`, `JPACK_CONFIG`, the
working directory, the server's launch): naming another configuration applies that configuration's
rules, and naming a file that is not there means no project and no requirement. Nor does it bind anything that can edit `jpack.json` or the lock: turning
the member off is an edit like any other, which the lock records as drift for declared runs.

The append is bounded by the directory handle held open on the configuration's own directory and
refuses every escape a read refuses — an absolute or traversing path, a path leaving the root
through a symlinked component, a final component that is a symlink, anything that is not a regular
file — and no pathname is handed to a caller to open. That guarantee is about path resolution and
symlinks. It does not extend to a hardlinked alias, which is a second name for one inode and
invisible to every path-based check: the append refuses a trail file with more than one link where
the platform reports the count, but making such an alias requires write access inside the project
directory, which is the same trust domain as editing the packs themselves.

What "nothing else is created" covers, precisely: a refusal reached before or at the open creates
nothing. An existing trail is opened without `O_CREATE`, and a trail that is not there yet is
created exclusively, so a name that appears in between — a symlink most of all — loses the race
rather than being followed into existence. A failure *after* a successful create, on the other
hand, leaves the file there, empty or partly written; an appender cannot un-append. That is why
every record carries a run id and a graph run's composite is its commit marker: a reader tells a
complete run from an abandoned one by that rule, stated in `internal/audit`'s package
documentation, and not by trusting the writer to have been atomic.

The trail is chained unless the project's configuration says `"chain": false`
([ADR-0047](docs/adr/0047-make-a-decision-record-defensible.md)): each record carries the trail's
identity, its line number, and the SHA-256 of the exact bytes of the line before it, and the first
chained record after earlier lines commits to all of them at once. The writer takes an exclusive
advisory lock on the trail file for the read, the numbering, the write and the syncs, so cooperating
writers never chain two records to one line; the lock does nothing against a process that writes
without it. It refuses to chain after an incomplete last line, and it appends unchained only where
the platform or file system offers no lock at all. What the links give is consistency between lines,
not authenticated history: an edit to any line but the last breaks a link, but anyone who can write
the trail can edit its last line, cut it short, or rewrite it from any line on with its links
recomputed, and the result is as consistent as the original. Only a commitment to the trail held
outside the operator's reach, covering the lines in question, shows that (ADR-0047 §2a).

`jpack audit verify` checks the links over the trail's exact bytes and exits 1 on any failed check.
Without `--expect` it reports the integrity of one supplied chain and says what that does not
establish. With `--expect`, a checkpoint `jpack audit checkpoint` printed earlier and that someone
other than the operator kept, it also fails a trail cut short before the checkpoint's record, or one
with another identity or another record there; the lines after that record stay unauthenticated. A
checkpoint is only as independent as whoever holds it: one kept by the operator protects nothing
against the operator. Time-stamped checkpoints are not built yet (#208), and neither are signatures
(#209). `jpack audit repair` removes and rewrites nothing: after a write that did not complete it
keeps the damaged bytes in place as a line of their own and appends a discontinuity record naming
their digest, and `verify` then reports the trail as segments, never as intact across the break. A
repair is something an operator can run at will, so a discontinuity says a break was acknowledged,
not why; a checkpoint covering the lines before it is what shows they were not changed.

`jpack audit checkpoint --since <sequence>` lists every new checkpoint for a deliverer to hand to a
holder outside the operator's control. The runtime keeps no record of what was handed over, because
any record it kept would be the operator's to rewrite, and recording a decision never waits for a
hand-over: a record is appended first and reported as unwitnessed until a held checkpoint covers it.
What the runtime guarantees is narrower than "a handed-over checkpoint cannot be undone": a
checkpoint is a function of its record's bytes, so the same record always gives the same checkpoint,
and a different checkpoint for one trail and sequence exists only if the trail was rewritten.
Nothing stops an operator from rewriting the trail and issuing new checkpoints to someone who never
held the old ones. What the operator cannot do is change a holder's copy, and `jpack audit verify
--expect <held>` fails any rewrite of the records a held checkpoint covers. The protection is
exactly as good as the holder's independence and retention, and it says nothing about records after
the last checkpoint the holder kept.

The records deliberately contain the facts and evidence documents that were evaluated: they are the
project's own trail, written where the project asked, and they are not diagnostics. Human
diagnostics remain sanitized and value-free, and a failed append is reported as an input/output
failure carrying no value at all. **On unix** the trail file's mode is set to owner-only on every
append. **On Windows a file mode is not access control**: Go's `Chmod` there sets only the
read-only attribute and does not restrict the DACL, so what may read a trail is whatever the
containing directory's ACL allows, and a project recording facts and evidence on Windows should
place its audit directory where that ACL is already what it wants. The audit directory's mode is
set when this runtime creates it and not afterward, on every platform: a directory that was already
there keeps the mode the project gave it, and its access control is the project's. The file grows
without bound: its retention and whether it belongs in version control are the project's, exactly
as for the packs beside it.

Local input files are opened without following a final symlink where the operating system supports
that primitive, then checked as the same regular file before reading. Suite descendants are also
checked for traversal and symlinks. These checks do not make a directory safe against a different
process that can concurrently rename or replace its ancestors; run untrusted suites from a
directory whose ownership and write permissions you control.

Commercial repositories must not be runtime dependencies of this public core and must not override
the behavior of `jpack spec` commands.
