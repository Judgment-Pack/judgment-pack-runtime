# Minimal literal decision

- Id: https\:\/\/example\.com\/judgment\-packs\/minimal\-literal
- Version: 0\.1\.0
- Spec version: 0\.3\.0
- Digest: sha256:9d6ac048c14114ba610acbe4c54f7e7091eec678567409ae939bddc9bd361a4d
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

## Escalation

- Triggers:
  - unknown
  - conflict
- Routes:
  - Route 1:
    - Target:
      - Kind: system
      - Name: Review
  - Route 2:
    - Target:
      - Kind: system
      - Name: Review
  - Route 3:
    - Target:
      - Kind: system
      - Name: Review
  - Route 4:
    - Target:
      - Kind: system
      - Name: Review
  - Route 5:
    - Target:
      - Kind: system
      - Name: Review
  - Route 6:
    - Target:
      - Kind: system
      - Name: Review
  - Route 7:
    - Target:
      - Kind: system
      - Name: Review
  - Route 8:
    - Target:
      - Kind: system
      - Name: Review
