# Condition branches

- Id: https\:\/\/example\.com\/judgment\-packs\/condition\-branches
- Version: 0\.1\.0
- Spec version: 0\.3\.0
- Digest: sha256:f1bc73ddbf56bd437d1fff63511f4520b3ba5ace72b1cb1f21af06ce4160044e
- View version: 1

## Decision

- Intent: Exercise every Core condition shape\.
- Question: Do the declared conditions support approval\?

## Outcomes

| Id | Label | Description |
| --- | --- | --- |
| accept | Accept | |
| reject | Reject | |

## Rules

### Rule 1: composite\-rule

- Description: Combines literal\, all\, any\, not\, fact\, and evidence\-present nodes\.
- Outcome: accept
- On unknown: escalate
- Evidence requirement refs:
  - attestation
- When:
  - all of:
    - literal true
    - any of:
      - literal false
      - not:
        - literal false
    - fact \"\/request\/region\" in \[\"ca\"\,\"us\"\]
    - evidence present attestation

## Evidence requirements

### Evidence requirement 1: attestation

- Description: A declared attestation\.
- Required: false
- Kind: attestation
