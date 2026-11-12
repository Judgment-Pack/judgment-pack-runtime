# Suppress\-rule exception

- Id: https\:\/\/example\.com\/judgment\-packs\/exception\-suppress\-rule
- Version: 0\.1\.0
- Spec version: 0\.3\.0
- Digest: sha256:e82954a94e68fe513e422e252e519ce53c8191d7bdaf98534f181c42fe92b74d
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
