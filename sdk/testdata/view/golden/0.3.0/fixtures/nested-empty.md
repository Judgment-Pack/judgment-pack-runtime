# Empty members inside members

- Id: https\:\/\/example\.invalid\/judgment\-packs\/view\-nested\-empty
- Version: 0\.1\.0
- Spec version: 0\.3\.0
- Digest: sha256:f52b6ba673a472ad2bafb6767a791f0cdfc85ba506712cc16acdb5706655b4b0
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
- Extensions: *empty*
- When:
  - evidence present ticket

## Exceptions

*empty*

## Fallback outcome

- Outcome: no

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

*empty*
