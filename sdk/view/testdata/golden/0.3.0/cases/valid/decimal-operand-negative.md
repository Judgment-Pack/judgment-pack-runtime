# Negative fractional decimal operand

- Id: https\:\/\/example\.com\/judgment\-packs\/decimal\-operand\-negative
- Version: 0\.1\.0
- Spec version: 0\.3\.0
- Digest: sha256:4895abcf5bacc03809b62d1f8892e01af0918d0806891eefe7d124b75835da24
- View version: 1

## Decision

- Intent: Exercise decimal operand grammar with a conforming negative fraction\.
- Question: Does an ordered comparison accept a negative decimal fraction\?

## Outcomes

| Id | Label | Description |
| --- | --- | --- |
| accept | Accept | |
| reject | Reject | |

## Rules

### Rule 1: ordered\-rule

- Description: Uses a valid negative decimal fraction operand\.
- Outcome: accept
- On unknown: ignore
- When:
  - fact \"\/amount\" greater\-than \"\-0\.5\"
