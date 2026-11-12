# Unsupported required extension

- Id: https\:\/\/example\.com\/judgment\-packs\/required\-extension\-unsupported
- Version: 0\.1\.0
- Spec version: 0\.3\.0
- Digest: sha256:94d73f7b8b1670f4262e5197e1d37909307bd3ffa14c445612eedb40d342ccd5
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
