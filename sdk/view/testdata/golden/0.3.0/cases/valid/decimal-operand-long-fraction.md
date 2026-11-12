# High\-precision decimal operand

- Id: https\:\/\/example\.com\/judgment\-packs\/decimal\-operand\-long\-fraction
- Version: 0\.1\.0
- Spec version: 0\.3\.0
- Digest: sha256:bdc6c57fc27abe14b3376e9a010a577c83e48f250ec280043e8fbbe1f5405b94
- View version: 1

## Decision

- Intent: Exercise decimal operand grammar preserving exact identity beyond float precision\.
- Question: Does an ordered comparison accept a high\-precision decimal operand\?

## Outcomes

| Id | Label | Description |
| --- | --- | --- |
| accept | Accept | |
| reject | Reject | |

## Rules

### Rule 1: ordered\-rule

- Description: Uses a valid high\-precision decimal operand whose identity would change if parsed as a float\.
- Outcome: accept
- On unknown: ignore
- When:
  - fact \"\/amount\" greater\-than \"12345678901234567890\.123456789\"
