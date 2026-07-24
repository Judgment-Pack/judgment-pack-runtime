// Package mcp will expose this runtime over the Model Context Protocol.
//
// It is a transport adapter only: it maps MCP tool and resource calls onto the
// existing validation, conformance, and describe packages and returns their
// versioned results. It evaluates no condition, resolves no outcome, and adds
// no judgment behavior of its own -- an MCP client reaches exactly the same
// core the CLI reaches, over JSON-RPC instead of argv.
//
// The adapter is intended to run as an "mcp" subcommand of the single
// judgment-pack binary rather than as a separate program.
//
// This package is a scaffold and currently has no implementation.
package mcp
