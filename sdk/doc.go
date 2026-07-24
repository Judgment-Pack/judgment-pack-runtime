// Package sdk is the reserved home for a public, in-process Go composition API
// over the judgment-pack runtime -- the seam that lets a Go program embed
// validation directly instead of invoking the binary.
//
// It is distinct from a client library in another language: non-Go consumers
// reach the runtime over a wire protocol (the future HTTP or MCP adapters) or
// by invoking the binary, not by importing this package.
//
// Stability: experimental. While the project is pre-1.0 this package exports
// nothing and carries no compatibility guarantee; do not depend on it yet.
//
// This package is a scaffold and currently has no implementation.
package sdk
