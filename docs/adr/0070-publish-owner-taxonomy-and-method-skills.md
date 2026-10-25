---
status: proposed
date: 2026-10-25
deciders:
---

# Publish truthful owner taxonomy and digest-addressed method skills

## Context and problem statement

Clients currently infer tool effects and duplicate the Runtime's method prompts.
Issue #329 requests owner metadata and six method skills. Its initial read-only
assumption conflicts with experimental_evaluate's existing opt-in audit appends
(ADR-0018/0028); metadata cannot remove that effect or authorize a call.

Material-decision impact: public-surface, documented-claim, security, dependency.
The authorized same-vendor review exception is an exception to the normal vendor
requirement; it supplies no claim of cross-vendor review or conformance.

## Decision outcome

Publish operation/effect and optional checkKind/experimental directly in tool
_meta, matching Agent ADR-0027. A source-owned closed declaration table covers
every served name. The tools/list path refuses missing or invalid declarations;
there is no name/configuration fallback. Existing names, descriptions, schemas,
prompt text and handlers remain unchanged. Standard annotations derive only from
effect: pure/read are readOnly and idempotent, not destructive/openWorld; act is
destructive/openWorld, not readOnly/idempotent. Hints and taxonomy grant nothing.

Evaluation declares evaluate/act for its complete capability, even if one call
would be a rehearsal or a project currently disables audit. Test matrices remain
check/test with pure or read effects because their handlers enforce no writes.
The Agent foundation currently refuses Runtime act metadata until its canonical
call path has exact host-bound approval/pause/resume. That integration must retain
existing evaluation functionality before release; metadata alone is insufficient.

Advertise MCP resources/list and resources/read for six method definitions:
author_pack, fix_pack, test_pack, present_pack, explain_disposition, author_graph.
All existing prompts remain. Instructions are exactly each prompt builder's
empty-argument render; project text and host state are never incorporated.
The JSON definition includes kind method, publisher judgment-pack-runtime,
version "1", tools, dependencies, resources, digest, profile and uri alongside
name/description/instructions. Version 1 identifies this publication revision;
content is selected by the digest, not a mutable version-only alias.
Each URI is judgment-pack://methods/1/<name>/<64-lowercase-digest-hex>.
Only exact currently served URIs resolve; an old content identity is never
redirected to different bytes after a binary upgrade. Each definition is at most
64 KiB; six definitions bound discovery and reads. No subscriptions or pagination.

The digest is Agent's exact skillDigest: SHA-256 prefixed sha256: over canonical
JSON {kind,name,description,instructions,tools,resources,dependencies}, omitting
dependencies only for empty tools. Each dependency is {name,taxonomy} in the
tools order, binding metadata as well as instructions. Object members sort by
UTF-16 code units; strings use minimal ECMAScript escapes. Existing internal/jcs
supports this strings/booleans/arrays/objects value space without an Agent module
dependency. Hand-authored canonical fixtures are shared with a TS consumer.
Publisher/version/profile/URI are fixed or derived publication metadata, not
additional author-controlled digest inputs. Changed dependency metadata changes
the digest and URI; hosts must verify these bytes and recompute the profile.

Profiles are the strongest effect and ordered union of declared Runtime operation
families. Unknown or withheld references refuse publication, never default to a
weaker profile. Optional branches are included. Thus author_pack, test_pack,
present_pack and explain_disposition include evaluate/act; present_pack tells a
client to run an evaluation if its audience requests one. fix_pack includes the
project matrix rerun and is read/check. This truthfully departs from the issue
comment's read/check-only assumption without rewriting its existing instructions.
The author_graph method is read/discover/check for its Runtime references. Its
optional terminal prerequisite is explicit in a named digest-bound skill resource.
That profile cannot classify or grant external terminal operations. An importer
must refuse unresolved external prerequisites and dependencies, never silently
turn terminal instructions into Runtime read authority.

## Consequences

Resource publication does not cause Agent import, default catalogue admission or
approval. Agent #220 needs explicit provenance, exact digest/profile verification,
external-prerequisite disposition and existing offer/approval gates. Skill text
grants no tool and cannot widen review/work presets. This producer alone does not
complete #329 end to end. No normative Spec surface, historical artifact, project
write behavior, dependency package, standards claim or credential handling changes.
