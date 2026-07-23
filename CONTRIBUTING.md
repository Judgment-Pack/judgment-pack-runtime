# Contributing

This repository implements a nonnormative tool for the Judgment Pack Specification. The tagged
specification artifacts remain authoritative when implementation behavior disagrees with them.

## Before opening a pull request

```bash
env GO111MODULE=on go fmt ./...
env GO111MODULE=on go vet ./...
env GO111MODULE=on go test ./...
env GO111MODULE=on CGO_ENABLED=0 go build -trimpath ./cmd/judgment-pack
```

Changes should include focused tests for human and JSON output, exit status, input limits, and
adversarial behavior. Never add a runtime fetch of mutable specification branches.

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
