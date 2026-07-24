// Package mcp exposes this runtime over the Model Context Protocol.
//
// It is a transport adapter only: it maps MCP tool calls onto the existing
// validation, conformance, and describe packages and returns their versioned
// results. It evaluates no condition, resolves no outcome, opens no network
// connection, and holds no credential -- an MCP client reaches exactly the same
// offline core the CLI reaches, over JSON-RPC on stdio instead of argv.
//
// The server speaks MCP's newline-delimited JSON-RPC 2.0 stdio framing directly,
// without a third-party SDK, to keep the runtime's dependency set minimal and
// its build free of a newer-Go-toolchain requirement. It runs as the "mcp"
// subcommand of the single judgment-pack binary.
package mcp
