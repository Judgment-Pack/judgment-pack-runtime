# Supported required extension

- Id: https\:\/\/example\.com\/judgment\-packs\/required\-extension\-supported
- Version: 0\.1\.0
- Spec version: 0\.3\.0
- Digest: sha256:64c9739bd50eefe88190f2d979cfd46e4609a75caf474c7e197fc65bfb76ac73
- View version: 1

## Decision

- Intent: Exercise successful required\-extension capability negotiation\.
- Question: Can a capable consumer fully interpret the pack\?

## Outcomes

| Id | Label | Description |
| --- | --- | --- |
| accept | Accept | |
| reject | Reject | |

## Rules

### Rule 1: literal\-rule

- Description: Carries data understood by the declared consumer capability\.
- Outcome: accept
- On unknown: ignore
- Extensions:
  - com\.example\.review\-policy: \{\"reviewMode\"\:\"two\-person\"\}
- When:
  - literal true

## Metadata

- Required extensions:
  - com\.example\.review\-policy
