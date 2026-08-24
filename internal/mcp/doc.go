// Package mcp exposes this runtime over the Model Context Protocol.
//
// It is a transport adapter only: it maps MCP tool calls onto the existing
// validation, conformance, describe, and evaluation packages and returns their
// versioned results. It opens no network connection and holds no credential --
// an MCP client reaches exactly the same offline core the CLI reaches, over
// JSON-RPC on stdio instead of argv. The validation tools evaluate no
// condition and resolve no outcome; the exceptions are experimental_evaluate,
// experimental_test_packs and experimental_test_graphs (ADR-0007, ADR-0021,
// ADR-0026), which run the evaluation
// package on this runtime's experimental surface. experimental_evaluate is the
// one tool here that can write: in a project whose jpack.json declares an audit
// directory it appends one record per completed non-rehearsal call (ADR-0028),
// through the project's own
// directory handle and nowhere else (ADR-0018); experimental_test_packs runs
// declared instance matrices, experimental_test_graphs runs declared graph
// matrices, and neither writes anything at all, because a matrix row is
// a rehearsal and not a decision. experimental_list_graphs and
// experimental_get_graph serve the graph convention's inventory and documents
// read-only (ADR-0029): they carry the experimental marker for their
// surface's stability, and they evaluate nothing and write nothing. Nothing in this package states a conformance claim: the claim is stated,
// in full and only, in CONFORMANCE.md, and every evaluation payload carries a
// reference to that file beside the contract version applied
// (result.EvaluationClaimReference, result.EvaluatorSpecVersion).
//
// The server speaks MCP's newline-delimited JSON-RPC 2.0 stdio framing directly,
// without a third-party SDK, to keep the runtime's dependency set minimal and
// its build free of a newer-Go-toolchain requirement. It runs as the "mcp"
// subcommand of the single jpack binary.
package mcp
