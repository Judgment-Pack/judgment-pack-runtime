# Contributing

This repository implements a nonnormative tool for the Judgment Pack Specification. The tagged
specification artifacts remain authoritative when implementation behavior disagrees with them.

## Quickstart

**Go 1.24 or newer.** `go.mod` carries a `go 1.24.0` directive, and the requirement is real
rather than conservative: the runtime reads through `os.Root`, a standard-library type
introduced in Go 1.24 — see [Build and try it locally](README.md#build-and-try-it-locally) for
why every read goes through a directory handle.

Build it and check it runs:

```bash
mkdir -p bin
env GO111MODULE=on CGO_ENABLED=0 go build -trimpath -o ./bin/jpack ./cmd/jpack
./bin/jpack version
```

`version` prints the runtime's own version, the specification versions it can serve, and the
provenance of its bundled artifacts — which is the fastest way to confirm a build is wired up.

While iterating, run one package or one test rather than the whole suite:

```bash
env GO111MODULE=on go test ./internal/carrier
env GO111MODULE=on go test ./internal/carrier -run '^TestDuplicateMemberReportsNestedPointer$'
```

The `^...$` anchors matter: `-run` takes a regular expression, so an unanchored name also
matches every test whose name contains it.

Run the full checks below before opening a pull request.

## Before opening a pull request

```bash
env GO111MODULE=on go fmt ./...
env GO111MODULE=on go vet ./...
env GO111MODULE=on go test ./...
env GO111MODULE=on CGO_ENABLED=0 go build -trimpath ./cmd/jpack
```

Changes should include focused tests for human and JSON output, exit status, input limits, and
adversarial behavior. Never add a runtime fetch of mutable specification branches.

Every pull request carries a one-line `Material-decision impact:` declaration, and material
decisions require the recorded cross-vendor review described in
[docs/adr/README.md](docs/adr/README.md#review-of-material-decisions).

Material command or output changes should first describe compatibility, migration, automation,
security, privacy, and authority consequences in an issue. Release version increments and
prereleases follow [VERSIONING.md](VERSIONING.md).

## Scope

The public core may contain generally useful developer commands, offline JPS validation, public
plugin contracts, and open integrations. Proprietary services, billing, enterprise administration,
customer-specific behavior, and private dependencies belong in separate repositories.

## Sign-off and license

Contributions must be signed off with `git commit --signoff`, certifying the Developer Certificate
of Origin 1.1: <https://developercertificate.org/>. Contributions are licensed under Apache-2.0.

Participation is governed by [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).
