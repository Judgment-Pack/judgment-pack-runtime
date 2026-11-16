// Package startup is what a service runs at start to read its registry: one
// environment's resolved registry (RFC 0034 §5), and the values of the secrets
// that registry refers to.
//
// A registry holds secret references, never secret values (RFC 0035). The
// platform a service runs on reads each referenced secret version from its
// store and delivers it to the process, as an environment variable or as a
// mounted file. This package reads it from there. It never contacts a secret
// store, and nothing in this module fetches a value.
//
// # Reading the resolved registry
//
// [ReadResolved] and [ReadResolvedFile] read one resolved document for the
// environment the caller names. They refuse a document whose
// resolvedFormatVersion is not one of [ResolvedFormatVersions], exactly, and a
// document resolved for another environment. They also refuse a document over
// RFC 0034 §1's size or depth bound, one with a duplicate member name, an
// unknown member or a missing one, and entries out of canonical order. An
// entry's value is kept as raw JSON for the caller's types to decode.
//
// # Reading secrets
//
// A [Reader] turns a secret reference into its value. [Bindings] is the
// reader of this release: it maps each reference to the environment variable
// or the file the platform delivered it as, and reads it through [ReadEnv] or
// [ReadFile]. Both readers trim exactly one trailing line feed or carriage
// return and line feed, and keep every other byte. [ReadFile] refuses a file
// that is not a regular file or that others may read, and refuses every file
// outside Unix, where the permission bits Go reports do not say who may read
// it.
//
// # Holding secrets
//
// A value is held only in a [Secret]. Every fmt verb that reaches the value
// prints [secret], %#v included, and so do encoding/json, encoding.TextMarshaler
// consumers and log/slog. The bytes are reached only through [Secret.Reveal].
// Nothing in this module logs or returns a value in an error.
//
// The module is versioned on its own, with tags startup/vX.Y.Z, and its
// exported identifiers are under a 1.0 compatibility promise; see the
// repository's VERSIONING.md.
package startup
