# One\-item in operand

- Id: https\:\/\/example\.com\/judgment\-packs\/in\-operand\-one\-item
- Version: 0\.1\.0
- Spec version: 0\.3\.0
- Digest: sha256:2e9241beefb57d8aa24614a7add1969d4527ed9fd0b09e09332da68934f8b287
- View version: 1

## Decision

- Intent: Exercise the membership\-operand shape\.
- Question: Is a one\-item membership operand accepted\?

## Outcomes

| Id | Label | Description |
| --- | --- | --- |
| accept | Accept | |
| reject | Reject | |

## Rules

### Rule 1: membership\-rule

- Description: Uses a single\-item array satisfying the membership minimum\.
- Outcome: accept
- On unknown: escalate
- When:
  - fact \"\/category\" in \[\"one\-category\"\]
