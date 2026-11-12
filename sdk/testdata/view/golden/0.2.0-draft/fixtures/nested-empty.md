# Empty members inside members

- Id: https\:\/\/example\.invalid\/judgment\-packs\/view\-nested\-empty
- Version: 0\.1\.0
- Spec version: 0\.2\.0\-draft
- Digest: sha256:01aad56b22746cfad070a43c3c04136cc4b6509e961f17744f01c4aa4244bf60
- View version: 1

## Decision

- Intent: Hold the optional arrays and objects of nested members present and empty\.
- Question: Does the view keep a nested empty member apart from an absent one\?

## Outcomes

| Id | Label | Description |
| --- | --- | --- |
| yes | Yes | |
| no | No | |

## Rules

### Rule 1: ticketed

- Description: A ticket means yes\.
- Outcome: yes
- On unknown: ignore
- Source refs:
  - handbook
- When:
  - evidence present ticket

## Exceptions

### Exception 1: never

- Description: Never applies\.
- Effect: escalate
- On unknown: ignore
- Source refs: *empty*
- Extensions: *empty*
- When:
  - literal false

## Fallback outcome

- Outcome: no

## Escalation

- Triggers:
  - unknown
- Target:
  - Kind: queue
  - Name: Review
- Extensions: *empty*

## Evidence requirements

### Evidence requirement 1: ticket

- Description: A ticket\.
- Required: false
- Extensions: *empty*

## Sources

### Source 1: handbook

- Title: Handbook
- Locator:
  - Kind: other
  - Value: The shelf by the door
- Extensions: *empty*

## Metadata

- Required extensions: *empty*
- Reviews: *empty*
- Extensions: *empty*
