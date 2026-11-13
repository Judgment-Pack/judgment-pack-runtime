# Outcome severity

- Id: https\:\/\/example\.invalid\/judgment\-packs\/view\-severity
- Version: 1\.0\.0
- Spec version: 0\.5\.0
- Digest: sha256:6195f92041acdf096bde001c120bbfa42c44239f89bf7ba60b1e789315508b59
- View version: 2

## Decision

- Intent: List the severity of each outcome\.
- Question: Which outcome is the most severe\?

## Outcomes

| Id | Label | Severity | Description |
| --- | --- | --- | --- |
| note | Note | low | |
| refer | Refer | high | A reviewer decides\. |
| unstated | Unstated | | |
| wrong\-case | Wrong case | | |
| hold | Hold | high | |
| listed | Listed | | |
| look\-alike | Look\-alike | | |
| stop | Stop | critical | |
| check | Check | medium | |

- By severity, most severe first, as the author states it (annotation vocabulary 1.0.0): stop, refer, hold, check, note
- With no severity level, none stated or a value that is not a level, in document order: unstated, wrong\-case, listed, look\-alike

### Extensions of outcome 1: note

- org\.judgmentpack\.annotations:
  - severity: low

### Extensions of outcome 2: refer

- org\.judgmentpack\.annotations:
  - severity: high

### Extensions of outcome 4: wrong\-case

- org\.judgmentpack\.annotations:
  - severity: High

### Extensions of outcome 5: hold

- org\.judgmentpack\.annotations:
  - severity: high
  - tier: amber

### Extensions of outcome 6: listed

- org\.judgmentpack\.annotations:
  - severity:
    - critical

### Extensions of outcome 7: look\-alike

- org\.judgmentpack\.annotations:
  - severity: critіcal (ASCII `"crit\u0456cal"`)

### Extensions of outcome 8: stop

- org\.judgmentpack\.annotations:
  - severity: critical

### Extensions of outcome 9: check

- com\.example\.colour: \"blue\"
- org\.judgmentpack\.annotations:
  - severity: medium

## Rules

### Rule 1: always

- Description: Always refer\.
- Outcome: refer
- On unknown: escalate
- Extensions:
  - org\.judgmentpack\.annotations:
    - severity: critical
- When:
  - literal true

## Extensions

- org\.judgmentpack\.annotations:
  - severity: low
