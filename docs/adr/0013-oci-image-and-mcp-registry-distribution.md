---
status: accepted
date: 2026-07-28
deciders: Brian Jin
---

# Distribute the released binary as an OCI image and publish it to the MCP registry

## Context and problem statement

Reaching an MCP client today means a PATH install from a release archive, which every client
guide documents but non-technical users stumble on, and which registry-aware clients (VS Code,
Docker Desktop, Claude Desktop, registry browsers) increasingly bypass in favor of installing a
server by name. The runtime needs distribution channels those clients can consume without
changing anything about what the runtime is: local execution, keyless, network-free, no HTTP
surface (ADR-0004).

## Decision drivers

- Hassle-free integration across many clients without per-client installation guides.
- The release pipeline's existing shape: package without publishing, smoke-test the archives on
  every OS, attest, then publish behind a human-gated environment. New channels must not weaken
  that chain or introduce a second build of the binary.
- ADR-0004: no hosted execution; a distribution channel must not become a service.
- Registry identity: the official MCP registry verifies OCI ownership through the
  `io.modelcontextprotocol.server.name` annotation and grants the `io.github.Judgment-Pack/*`
  namespace through GitHub OIDC.

## Considered options

- OCI image on GHCR plus a `server.json` in the official MCP registry (this ADR).
- MCPB bundles for one-click desktop install — deferred, not rejected; it adds a manifest
  format and signing story of its own and can follow as a separate decision.
- GoReleaser's built-in docker/registry publishing — rejected for now: it runs at package time,
  before the smoke tests and the human gate, inverting the pipeline's order.

## Decision outcome

Chosen option: post-gate release jobs build a `scratch` image **from the published release
archives** — never a rebuild, and never the expiring run artifact — push it to
`ghcr.io/judgment-pack/judgment-pack` under the immutable version tag only (existing version tags
refused rather than overwritten), smoke-test the pushed image by digest **anonymously on native
runners of both architectures** — an unauthenticated pull is the only proof the package is
publicly visible, and a native arm64 run is the only execution the arm64 manifest gets — and only
after that passing run attest the digest, fast-forward `latest` to it (non-prereleases only), and
publish a rendered `server.json` (OCI package, stdio transport) to the official MCP registry
under `io.github.Judgment-Pack/judgment-pack` via GitHub OIDC. Everything user-facing — the
attestation, `latest`, the registry entry — postdates a passing execution of the image.

### Consequences

- Good, because the binary in the image is byte-identical to the archived one; one artifact,
  many channels. The archives and the image carry **separate attestations** (the image's digest
  attestation does not embed the archive attestation as verified material); both chain to the
  same workflow run.
- Good, because a container is still local execution with the client's own key — ADR-0004's
  refusal of a hosted API is untouched.
- Good, because registry-aware clients can now install by name; `docker run -i` becomes a
  supported MCP command for clients without a PATH install.
- Bad, because the registry entry and image tags are release-time state that can drift from the
  repository if a release is yanked or re-cut; the release checklist gains steps.
- Bad, because `mcp-publisher` and the registry schema are young and may change; the publish job
  pins the publisher by sha256, CI validates the rendered template against the vendored schema,
  and the first release after this ADR must watch that job.
- Accepted deliberately: a prerelease is published to the registry so an rc exercises the
  registry job, and the registry — which orders by SemVer with no prerelease notion — will show
  it as newest until the final follows. The GitHub release and the image `latest` tag never
  move for a prerelease.
- Revisit when MCPB bundles are added, or if the registry's verification model changes.

## More information

- `build/oci/Dockerfile`, `build/mcp-registry/server.json.tmpl`, the `publish-image` and
  `publish-mcp-registry` jobs in `.github/workflows/release.yml`.
- [MCP registry package types](https://modelcontextprotocol.io/registry/package-types),
  [official registry requirements](https://github.com/modelcontextprotocol/registry/blob/main/docs/reference/server-json/official-registry-requirements.md).
- ADR-0003 (MCP surface), ADR-0004 (no HTTP API), ADR-0012 (jpack.json convention, the
  `/project` workdir contract).
