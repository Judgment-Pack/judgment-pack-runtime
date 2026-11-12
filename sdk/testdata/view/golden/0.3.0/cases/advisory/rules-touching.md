# Touching rule ranges

- Id: https\:\/\/example\.com\/judgment\-packs\/rules\-touching
- Version: 0\.1\.0
- Spec version: 0\.3\.0
- Digest: sha256:37b2bcae9a2cb4e4cc7787f5f1fedf1c2a41b26f8072dcee5b064b08ec10e3ec
- View version: 1

## Decision

- Intent: Assign a score band\.
- Question: Which score band applies\?

## Outcomes

| Id | Label | Description |
| --- | --- | --- |
| medium | Medium | |
| high | High | |

## Rules

### Rule 1: r\-medium

- Description: Scores below 70 are medium\.
- Outcome: medium
- On unknown: escalate
- When:
  - fact \"\/score\" less\-than \"70\"

### Rule 2: r\-high

- Description: Scores of at least 70 are high\.
- Outcome: high
- On unknown: escalate
- When:
  - fact \"\/score\" greater\-than\-or\-equal \"70\"
