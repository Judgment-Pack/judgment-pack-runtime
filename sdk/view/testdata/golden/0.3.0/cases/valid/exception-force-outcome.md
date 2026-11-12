# Force\-outcome exception

- Id: https\:\/\/example\.com\/judgment\-packs\/exception\-force\-outcome
- Version: 0\.1\.0
- Spec version: 0\.3\.0
- Digest: sha256:531daae21d71ddb1c72e480df2bafec9c0ec16a8a43996cfab3e1f1af04b34ad
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
