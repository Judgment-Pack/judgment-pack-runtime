# Compatible overlapping rules

- Id: https\:\/\/example\.com\/judgment\-packs\/rules\-overlap\-same\-outcome
- Version: 0\.1\.0
- Spec version: 0\.3\.0
- Digest: sha256:8325e3f74254c165d1f7c4b420fa770ca499af24e3e1d0ccfe5187770bd9985e
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

### Rule 1: r\-medium\-score

- Description: Scores of at least 40 are medium\.
- Outcome: medium
- On unknown: escalate
- When:
  - fact \"\/score\" greater\-than\-or\-equal \"40\"

### Rule 2: r\-medium\-high\-score

- Description: Scores of at least 70 are also medium\.
- Outcome: medium
- On unknown: escalate
- When:
  - fact \"\/score\" greater\-than\-or\-equal \"70\"
