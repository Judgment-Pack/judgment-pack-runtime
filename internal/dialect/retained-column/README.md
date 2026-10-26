# Retained-column installation state

No RC2 resources are installed here. The selected CLI/MCP reader still reports
its historical Draft RFC0029 identity, preserved in `testdata/retained-column-draft`.

After immutable RC2 publication and verification, the maintainer import tool can
materialize `official/` with exact upstream paths and provenance. A separate
reviewed source change must pin that manifest, install the exact supported tuple,
and switch the selector/report identity. The release check refuses unpinned
resources and disagreement between installed and selected identities. Importing
files alone grants no support, producer authority or conformance claim.
