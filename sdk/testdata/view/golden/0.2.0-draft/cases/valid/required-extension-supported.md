# Supported required extension

- Id: https\:\/\/example\.com\/judgment\-packs\/required\-extension\-supported
- Version: 0\.1\.0
- Spec version: 0\.2\.0\-draft
- Digest: sha256:99d33ee6aad8fab95faee743bee2fb6760cde26aaa475a4451b0a06d15536646
- View version: 1

## Decision

- Intent: Exercise successful required\-extension capability negotiation\.
- Question: Can a capable consumer fully interpret the pack\?

## Outcomes

| Id | Label | Description |
| --- | --- | --- |
| accept | Accept | |
| reject | Reject | |

## Rules

### Rule 1: literal\-rule

- Description: Carries data understood by the declared consumer capability\.
- Outcome: accept
- On unknown: ignore
- Extensions:
  - com\.example\.review\-policy: \{\"reviewMode\"\:\"two\-person\"\}
- When:
  - literal true

## Metadata

- Required extensions:
  - com\.example\.review\-policy
