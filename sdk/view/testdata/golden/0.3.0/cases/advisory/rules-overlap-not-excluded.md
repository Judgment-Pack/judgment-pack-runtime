# Cross\-path rule overlap

- Id: https\:\/\/example\.com\/judgment\-packs\/rules\-overlap\-not\-excluded
- Version: 0\.1\.0
- Spec version: 0\.3\.0
- Digest: sha256:12bb1faeb4626f1636f66ac3e851f36f580aec4e90cd6c5429fdc1ca79683174
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

- Description: Priorities of at least 70 are high\.
- Outcome: high
- On unknown: escalate
- When:
  - fact \"\/priority\" greater\-than\-or\-equal \"70\"
