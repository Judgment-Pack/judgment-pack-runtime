# Force\-outcome exception

- Id: https\:\/\/example\.com\/judgment\-packs\/exception\-force\-outcome
- Version: 0\.1\.0
- Spec version: 0\.2\.0\-draft
- Digest: sha256:3fc6c4f4071dfeddcf332cd899195f6b365d45e432b4ed3f9e01c3383ce62e0a
- View version: 1

## Decision

- Intent: Exercise the force\-outcome exception branch\.
- Question: Should the exception force rejection\?

## Outcomes

| Id | Label | Description |
| --- | --- | --- |
| accept | Accept | |
| reject | Reject | |

## Rules

### Rule 1: base\-rule

- Description: A declared base rule\.
- Outcome: accept
- On unknown: ignore
- When:
  - literal true

## Exceptions

### Exception 1: forced\-rejection

- Description: Forces a declared outcome\.
- Effect: force\-outcome
- Outcome: reject
- On unknown: ignore
- When:
  - literal true
