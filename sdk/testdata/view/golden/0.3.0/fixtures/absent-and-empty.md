# Present but empty members

- Id: https\:\/\/example\.invalid\/judgment\-packs\/view\-absent\-and\-empty
- Version: 0\.1\.0
- Spec version: 0\.3\.0
- Digest: sha256:2ad10d563f8c11c2ef66b21a1500d8469b5c18a5a447a17686cf1a6faa529d51
- View version: 1

## Decision

- Intent: Hold the optional arrays and objects present and empty\.
- Question: Does the view keep empty apart from absent\?
- Extensions: *empty*

## Outcomes

| Id | Label | Description |
| --- | --- | --- |
| yes | Yes | |
| no | No | |

### Extensions of outcome 1: yes

*empty*

## Rules

### Rule 1: always

- Description: Always yes\.
- Outcome: yes
- On unknown: ignore
- Evidence requirement refs: *empty*
- Source refs: *empty*
- When:
  - literal true

## Exceptions

### Exception 1: never

- Description: Never applies\.
- Effect: escalate
- On unknown: ignore
- Source refs: *empty*
- Extensions: *empty*
- When:
  - literal false

## Escalation

- Triggers:
  - unknown
- Routes:
  - Route 1:
    - Target:
      - Kind: queue
      - Name: Review
- Extensions: *empty*

## Evidence requirements

*empty*

## Sources

*empty*

## Metadata

- Required extensions: *empty*
- Reviews: *empty*
- Extensions: *empty*

## Extensions

*empty*
