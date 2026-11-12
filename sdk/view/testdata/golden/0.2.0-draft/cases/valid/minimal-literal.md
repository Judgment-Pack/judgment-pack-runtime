# Minimal literal decision

- Id: https\:\/\/example\.com\/judgment\-packs\/minimal\-literal
- Version: 0\.1\.0
- Spec version: 0\.2\.0\-draft
- Digest: sha256:0ea1a0c23cbc43609d6ab458694072ca08f620126b86a35369f1f3afaa98742c
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
