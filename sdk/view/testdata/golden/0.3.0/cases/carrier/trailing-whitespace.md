# Minimal literal decision

- Id: https\:\/\/example\.com\/judgment\-packs\/minimal\-literal
- Version: 0\.1\.0
- Spec version: 0\.3\.0
- Digest: sha256:3cba6f6e09221c8adb02c27db2e3c32b1f6de832e7293ddab3efa7985745264e
- View version: 1

## Decision

- Intent: Provide the smallest focused valid pack\.
- Question: Does the literal rule select accept\?

## Outcomes

| Id | Label | Description |
| --- | --- | --- |
| accept | Accept | |
| reject | Reject | |

## Rules

### Rule 1: literal\-rule

- Description: A true literal condition\.
- Outcome: accept
- On unknown: ignore
- When:
  - literal true
