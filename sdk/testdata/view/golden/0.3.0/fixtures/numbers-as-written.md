# Numbers and decimal strings as written

- Id: https\:\/\/example\.invalid\/judgment\-packs\/view\-numbers\-as\-written
- Version: 0\.1\.0
- Spec version: 0\.3\.0
- Digest: sha256:2688dfe1141c8e7ad96bd4c145d6164b222ac7f9b70c5e61a4de4ea13c516f4d
- View version: 1

## Decision

- Intent: Hold numbers in several spellings of the same value\.
- Question: Does the view keep each number\'s spelling\?

## Outcomes

| Id | Label | Description |
| --- | --- | --- |
| match | Match | |
| other | Other | |

## Rules

### Rule 1: spellings

- Description: Each comparison value is written its own way\.
- Outcome: match
- On unknown: escalate
- Extensions:
  - com\.example\.weights: \{\"high\"\:1E\-3\,\"low\"\:2\.500\}
- When:
  - any of:
    - fact \"\/a\" equals 1\.50
    - fact \"\/b\" equals 1e2
    - fact \"\/c\" equals 1E\+2
    - fact \"\/d\" equals \-0
    - fact \"\/e\" equals 0\.000
    - fact \"\/f\" equals 123456789012345678901234567890
    - fact \"\/g\" not\-equals \-1\.5e\-10
    - fact \"\/h\" in \[1\.0\,\"1\.0\"\,1\,\"1\"\]
    - fact \"\/i\" greater\-than \"1000\.50\"
    - fact \"\/j\" less\-than \"\-0\.0\"
    - fact \"\/k\" greater\-than\-or\-equal \"0\.10\"
    - fact \"\/l\" equals \"1\.50\"

## Fallback outcome

- Outcome: other

## Escalation

- Triggers:
  - unknown
- Target:
  - Kind: queue
  - Name: Number review
- Message: A spelling could not be compared\.
