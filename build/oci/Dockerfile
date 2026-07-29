# The distribution image: the released static binary on scratch, nothing else.
# Built by the release workflow from the same smoke-tested archives every other
# distribution channel ships -- never from source at image-build time -- so the
# binary in the image is byte-identical to the one in the release archive.
#
# The io.modelcontextprotocol.server.name label states this runtime's identity
# in the MCP registry; the registry checks it against server.json when the OCI
# package is published (see build/mcp-registry/server.json.tmpl and ADR-0013).
FROM scratch
ARG TARGETOS
ARG TARGETARCH

COPY bin/${TARGETOS}_${TARGETARCH}/judgment-pack /judgment-pack
COPY CONFORMANCE.md LICENSE NOTICE THIRD_PARTY_NOTICES /

# The launch-directory convention (ADR-0012): mount a project at /project and
# the server reads its jpack.json, root-jailed to the mount.
WORKDIR /project
USER 65532:65532

LABEL io.modelcontextprotocol.server.name="io.github.Judgment-Pack/judgment-pack" \
      org.opencontainers.image.title="judgment-pack" \
      org.opencontainers.image.description="Vendor-neutral reference runtime and CLI for the Judgment Pack Specification (JPS): offline validation, conformance testing, and the experimental evaluator, served over stdio MCP." \
      org.opencontainers.image.source="https://github.com/Judgment-Pack/judgment-pack-runtime" \
      org.opencontainers.image.licenses="Apache-2.0"

ENTRYPOINT ["/judgment-pack"]
CMD ["mcp"]
