# Valid surrogate pair 😀

- Id: https\:\/\/example\.com\/judgment\-packs\/valid\-surrogate\-pair
- Version: 0\.1\.0
- Spec version: 0\.3\.0
- Digest: sha256:417cfa002cae26575484f4161583d2d2daedc4570cfe17c71c7a803adeee08b5
- View version: 1

## Decision

- Intent: Exercise acceptance of a valid surrogate pair escape\.
- Question: Is a valid surrogate pair accepted\?

## Outcomes

| Id | Label | Description |
| --- | --- | --- |
| accept | Accept | |
| reject | Reject | |

## Rules

### Rule 1: literal\-rule

- Description: A structurally minimal rule\.
- Outcome: accept
- On unknown: ignore
- When:
  - literal true
