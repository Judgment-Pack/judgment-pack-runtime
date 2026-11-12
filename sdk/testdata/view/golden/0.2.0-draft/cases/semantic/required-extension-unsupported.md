# Unsupported required extension

- Id: https\:\/\/example\.com\/judgment\-packs\/required\-extension\-unsupported
- Version: 0\.1\.0
- Spec version: 0\.2\.0\-draft
- Digest: sha256:c125b6a5d1b4aed591d3ec5fbff340af2bc61f07fed4a030b0f0de564061b327
- View version: 1

## Decision

- Intent: Exercise required\-extension capability negotiation\.
- Question: Does the consumer report its missing capability\?

## Outcomes

| Id | Label | Description |
| --- | --- | --- |
| accept | Accept | |
| reject | Reject | |

## Rules

### Rule 1: literal\-rule

- Description: Carries data whose semantics require extension support\.
- Outcome: accept
- On unknown: ignore
- Extensions:
  - com\.example\.review\-policy: \{\"reviewMode\"\:\"two\-person\"\}
- When:
  - literal true

## Metadata

- Required extensions:
  - com\.example\.review\-policy
