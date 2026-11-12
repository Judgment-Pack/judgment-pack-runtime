# Suppress\-rule exception

- Id: https\:\/\/example\.com\/judgment\-packs\/exception\-suppress\-rule
- Version: 0\.1\.0
- Spec version: 0\.2\.0\-draft
- Digest: sha256:c0e280cf8b9e569fc5417efc0ab73a35c5e9438db3b9c48f4dfa63b468522072
- View version: 1

## Decision

- Intent: Exercise the suppress\-rule exception branch\.
- Question: Should the base rule be suppressed\?

## Outcomes

| Id | Label | Description |
| --- | --- | --- |
| accept | Accept | |
| reject | Reject | |

## Rules

### Rule 1: base\-rule

- Description: The declared suppression target\.
- Outcome: accept
- On unknown: ignore
- When:
  - literal true

## Exceptions

### Exception 1: suppression

- Description: Suppresses the declared base rule\.
- Effect: suppress\-rule
- Target rule: base\-rule
- On unknown: ignore
- When:
  - literal true
