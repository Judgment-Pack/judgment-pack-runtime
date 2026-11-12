# Condition branches

- Id: https\:\/\/example\.com\/judgment\-packs\/condition\-branches
- Version: 0\.1\.0
- Spec version: 0\.2\.0\-draft
- Digest: sha256:d288e61c920dda439228ccb07b95c6590182216288d3bb93b394d61fc62a32a6
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
