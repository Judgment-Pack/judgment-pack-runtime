# Minimal literal decision

- Id: https\:\/\/example\.com\/judgment\-packs\/minimal\-literal
- Version: 0\.1\.0
- Spec version: 0\.3\.0
- Digest: sha256:4040b56db3da7da28271689119a1ed2de92b4726d1be6d0dd9d27274ecfc747d
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
      - Kind: queue
      - Name: Review
    - Triggers:
      - unknown
  - Route 2:
    - Target:
      - Kind: human\-role
      - Name: Oversight
