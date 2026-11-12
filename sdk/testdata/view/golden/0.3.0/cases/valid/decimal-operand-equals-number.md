# Decimal operand equality JSON number

- Id: https\:\/\/example\.com\/judgment\-packs\/decimal\-operand\-equals\-number
- Version: 0\.1\.0
- Spec version: 0\.3\.0
- Digest: sha256:beed5604d53c3c4f3086632e0de8348cd2a92b2687c6df4657e8692e7a880c49
- View version: 1

## Decision

- Intent: Exercise decimal operand grammar does not apply to equality\.
- Question: Does an equality comparison accept a JSON number operand\?

## Outcomes

| Id | Label | Description |
| --- | --- | --- |
| accept | Accept | |
| reject | Reject | |

## Rules

### Rule 1: equals\-rule

- Description: Uses a JSON number operand with an equality comparison\.
- Outcome: accept
- On unknown: ignore
- When:
  - fact \"\/amount\" equals 5000
