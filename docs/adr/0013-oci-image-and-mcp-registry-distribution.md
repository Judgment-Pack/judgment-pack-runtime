---
status: proposed
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
  `io.modelcontextprotocol.server.name` annotation and grants the `io.github.judgment-pack/*`
  namespace through GitHub OIDC.

## Considered options

- OCI image on GHCR plus a `server.json` in the official MCP registry (this ADR).
- MCPB bundles for one-click desktop install — deferred, not rejected; it adds a manifest
  format and signing story of its own and can follow as a separate decision.
- GoReleaser's built-in docker/registry publishing — rejected for now: it runs at package time,
  before the smoke tests and the human gate, inverting the pipeline's order.

## Decision outcome

Chosen option: post-gate release jobs build a `scratch` image **from the smoke-tested release
archives** — never a rebuild — push it to `ghcr.io/judgment-pack/judgment-pack` (linux
amd64/arm64, version tag, `latest` only for non-prereleases), attest the image digest, smoke-test
the pushed image by digest, and then publish a rendered `server.json` (OCI package, stdio
transport) to the official MCP registry under `io.github.judgment-pack/judgment-pack` via
GitHub OIDC.

### Consequences

- Good, because the binary in the image is byte-identical to the archived one; one artifact,
  many channels, one attestation chain.
- Good, because a container is still local execution with the client's own key — ADR-0004's
  refusal of a hosted API is untouched.
- Good, because registry-aware clients can now install by name; `docker run -i` becomes a
  supported MCP command for clients without a PATH install.
- Bad, because the registry entry and image tags are release-time state that can drift from the
  repository if a release is yanked or re-cut; the release checklist gains steps.
- Bad, because `mcp-publisher` and the registry schema are young and may change; the publish job
  pins the publisher by sha256 and the first release after this ADR must watch that job.
- Revisit when MCPB bundles are added, or if the registry's verification model changes.

## More information

- `build/oci/Dockerfile`, `build/mcp-registry/server.json.tmpl`, the `publish-image` and
  `publish-mcp-registry` jobs in `.github/workflows/release.yml`.
- [MCP registry package types](https://modelcontextprotocol.io/registry/package-types),
  [official registry requirements](https://github.com/modelcontextprotocol/registry/blob/main/docs/reference/server-json/official-registry-requirements.md).
- ADR-0003 (MCP surface), ADR-0004 (no HTTP API), ADR-0012 (jpack.json convention, the
  `/project` workdir contract).
