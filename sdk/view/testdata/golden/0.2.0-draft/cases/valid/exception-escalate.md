# Escalate exception

- Id: https\:\/\/example\.com\/judgment\-packs\/exception\-escalate
- Version: 0\.1\.0
- Spec version: 0\.2\.0\-draft
- Digest: sha256:c753fb5c7a91554b372e3927a9b5f8729f441a4378c25acd9f2bafc7e1d380db
- View version: 1

## Decision

- Intent: Exercise the escalate exception branch\.
- Question: Should the exception require escalation\?

## Outcomes

| Id | Label | Description |
| --- | --- | --- |
| accept | Accept | |
| reject | Reject | |

## Rules

### Rule 1: base\-rule

- Description: A declared base rule\.
- Outcome: accept
- On unknown: ignore
- When:
  - literal true

## Exceptions

### Exception 1: escalation

- Description: Requires escalation without a target rule or outcome\.
- Effect: escalate
- On unknown: ignore
- When:
  - literal true
