# Nested conditions

- Id: https\:\/\/example\.invalid\/judgment\-packs\/view\-nested\-conditions
- Version: 0\.1\.0
- Spec version: 0\.2\.0\-draft
- Digest: sha256:ee0b895d9cf1988e47cdf03708473116462be01e2e6d22b3b534dc1b5146e9dd
- View version: 1

## Decision

- Intent: Nest every compound condition inside every other\.
- Question: Does the view keep the tree and the order of every operand\?

## Outcomes

| Id | Label | Description |
| --- | --- | --- |
| first | First | |
| second | Second | |
| third | Third | |

## Rules

### Rule 1: zeta

- Description: Declared first although its id sorts last\.
- Outcome: first
- On unknown: escalate
- When:
  - not:
    - not:
      - all of:
        - any of:
          - not:
            - all of:
              - fact \"\/z\" equals \"last\"
              - fact \"\/a\" equals \"first\"
          - evidence present ticket
        - literal true
        - any of:
          - literal false
          - fact \"\/m\" in \[3\,2\,1\]

### Rule 2: alpha

- Description: Declared second although its id sorts first\.
- Outcome: second
- On unknown: ignore
- When:
  - literal true

### Rule 3: mu

- Description: Declared third\.
- Outcome: third
- On unknown: ignore
- When:
  - literal false

## Escalation

- Triggers:
  - unknown
  - conflict
- Target:
  - Kind: queue
  - Name: Nesting review

## Evidence requirements

### Evidence requirement 1: ticket

- Description: A ticket\.
- Required: false
