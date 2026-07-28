// Package mcp exposes this runtime over the Model Context Protocol.
//
// It is a transport adapter only: it maps MCP tool calls onto the existing
// validation, conformance, describe, and evaluation packages and returns their
// versioned results. It opens no network connection and holds no credential --
// an MCP client reaches exactly the same offline core the CLI reaches, over
// JSON-RPC on stdio instead of argv. The validation tools evaluate no
// condition and resolve no outcome; the one exception is experimental_evaluate
// (ADR-0007), which runs the evaluation package on this runtime's experimental
// surface and carries its §3.4.1 conformance claim and version scope in every
// payload (result.EvaluationClaim, CONFORMANCE.md).
//
// The server speaks MCP's newline-delimited JSON-RPC 2.0 stdio framing directly,
// without a third-party SDK, to keep the runtime's dependency set minimal and
// its build free of a newer-Go-toolchain requirement. It runs as the "mcp"
// subcommand of the single judgment-pack binary.
package mcp
