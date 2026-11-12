# Minimal literal decision

- Id: https\:\/\/example\.com\/judgment\-packs\/minimal\-literal
- Version: 0\.1\.0
- Spec version: 0\.3\.0
- Digest: sha256:64a9161bf0891d3a4203a199efd48db44a9810f2d37d8feda1abe2235bd5d253
- View version: 1

## Decision

- Intent: Provide the smallest focused valid pack\.
- Question: Does the literal rule select accept\?

## Outcomes

| Id | Label | Description |
| --- | --- | --- |
| accept | Accept | |
| reject | Reject | |

## Rules

### Rule 1: literal\-rule

- Description: A true literal condition\.
- Outcome: accept
- On unknown: ignore
- When:
  - literal true
