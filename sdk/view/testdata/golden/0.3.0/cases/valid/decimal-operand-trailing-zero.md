# Decimal operand with significant trailing zeroes

- Id: https\:\/\/example\.com\/judgment\-packs\/decimal\-operand\-trailing\-zero
- Version: 0\.1\.0
- Spec version: 0\.3\.0
- Digest: sha256:aba324dae9f7bad5cc25deb9d43d10f2a61ad61fcf618464d3c94410f96df8ec
- View version: 1

## Decision

- Intent: Exercise decimal operand grammar with significant trailing zeroes\.
- Question: Does an ordered comparison accept a decimal operand with trailing zeroes\?

## Outcomes

| Id | Label | Description |
| --- | --- | --- |
| accept | Accept | |
| reject | Reject | |

## Rules

### Rule 1: ordered\-rule

- Description: Uses a valid decimal operand whose scale carries a trailing zero\.
- Outcome: accept
- On unknown: ignore
- When:
  - fact \"\/amount\" greater\-than \"0\.10\"
