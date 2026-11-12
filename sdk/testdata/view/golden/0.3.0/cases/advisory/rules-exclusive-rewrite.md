# Exclusive rule ranges

- Id: https\:\/\/example\.com\/judgment\-packs\/rules\-exclusive\-rewrite
- Version: 0\.1\.0
- Spec version: 0\.3\.0
- Digest: sha256:935a66aed8e2c6cfa8793df8aa695c49c2e0a37e365d001d14a8124789e2d10b
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

- Description: Scores from 40 up to but not including 70 are medium\.
- Outcome: medium
- On unknown: escalate
- When:
  - all of:
    - fact \"\/score\" greater\-than\-or\-equal \"40\"
    - fact \"\/score\" less\-than \"70\"

### Rule 2: r\-high

- Description: Scores of at least 70 are high\.
- Outcome: high
- On unknown: escalate
- When:
  - fact \"\/score\" greater\-than\-or\-equal \"70\"
