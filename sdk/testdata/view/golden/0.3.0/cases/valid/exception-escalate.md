# Escalate exception

- Id: https\:\/\/example\.com\/judgment\-packs\/exception\-escalate
- Version: 0\.1\.0
- Spec version: 0\.3\.0
- Digest: sha256:409b4016f35dd3e7e9ea43c6c8272750fb4ad0d6aedb2f2377e501022e778717
- View version: 1

## Decision

- Intent: Exercise the escalate exception branch\.
- Question: Should the exception require escalation\?

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

### Exception 1: escalation

- Description: Requires escalation without a target rule or outcome\.
- Effect: escalate
- On unknown: ignore
- When:
  - literal true
