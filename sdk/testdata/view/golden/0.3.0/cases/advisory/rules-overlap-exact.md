# Exact rule overlap

- Id: https\:\/\/example\.com\/judgment\-packs\/rules\-overlap\-exact
- Version: 0\.1\.0
- Spec version: 0\.3\.0
- Digest: sha256:3f6469cd8fe550295fe4897fbed569ff117f32e386f73cbb1535fb2a8db7a6a1
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

- Description: Scores of at least 40 are medium\.
- Outcome: medium
- On unknown: escalate
- When:
  - fact \"\/score\" greater\-than\-or\-equal \"40\"

### Rule 2: r\-high

- Description: Scores of at least 70 are high\.
- Outcome: high
- On unknown: escalate
- When:
  - fact \"\/score\" greater\-than\-or\-equal \"70\"
