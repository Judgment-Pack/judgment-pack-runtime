# Expense approval

- Id: https\:\/\/example\.com\/judgment\-packs\/expense\-approval
- Version: 0\.1\.0
- Spec version: 0\.3\.0
- Digest: sha256:162061fa8fa7606fd5adb5290ac33e270273a3bf8dcf7ddc687694f9d4041ae1
- View version: 1
- Description: Research example for deciding whether an employee expense may be automatically approved\.

## Decision

- Intent: Determine whether an employee expense may be automatically approved\, rejected\, or sent to a human reviewer\.
- Question: What approval path should this expense follow\?

## Applicability

- fact \"\/expense\/type\" equals \"employee\-expense\"

## Outcomes

| Id | Label | Description |
| --- | --- | --- |
| approve | Approve | The evidence and declared rules support automatic approval\. |
| manual\-review | Manual review | An accountable finance reviewer must decide\. |
| reject | Reject | The expense falls in a prohibited category\. |

## Rules

### Rule 1: prohibited\-category

- Description: Reject expenses in a prohibited category\.
- Outcome: reject
- On unknown: escalate
- Evidence requirement refs:
  - receipt
  - cost\-center
- Source refs:
  - expense\-policy
- Rationale: The example policy prohibits these categories\.
- When:
  - fact \"\/expense\/category\" in \[\"personal\"\,\"cash\-equivalent\"\]

### Rule 2: large\-expense

- Description: Send expenses above the declared threshold to finance review\.
- Outcome: manual\-review
- On unknown: escalate
- Evidence requirement refs:
  - receipt
  - cost\-center
- Source refs:
  - expense\-policy
- Rationale: The example policy requires finance review above 5000\.
- When:
  - fact \"\/expense\/amount\" greater\-than \"5000\"

### Rule 3: ordinary\-expense

- Description: Approve a supported ordinary expense at or below the threshold\.
- Outcome: approve
- On unknown: escalate
- Evidence requirement refs:
  - receipt
  - cost\-center
- Source refs:
  - expense\-policy
- Rationale: Ordinary supported expenses may follow the automatic path\.
- When:
  - all of:
    - fact \"\/expense\/amount\" less\-than\-or\-equal \"5000\"
    - not:
      - fact \"\/expense\/category\" in \[\"personal\"\,\"cash\-equivalent\"\]
    - evidence present receipt
    - evidence present cost\-center

## Exceptions

### Exception 1: active\-investigation

- Description: An expense connected to an active investigation requires human review\.
- Effect: force\-outcome
- Outcome: manual\-review
- On unknown: escalate
- When:
  - fact \"\/expense\/activeInvestigation\" equals true

## Fallback outcome

- Outcome: manual\-review

## Escalation

- Triggers:
  - not\-applicable
  - missing\-required\-evidence
  - unknown
  - conflict
  - no\-match
- Target:
  - Kind: human\-role
  - Name: Finance reviewer
- Message: Review the expense\, cited policy\, missing evidence\, and conflicting rules\.

## Evidence requirements

### Evidence requirement 1: receipt

- Description: An itemized receipt for the expense\.
- Required: true
- Kind: document

### Evidence requirement 2: cost\-center

- Description: A valid cost\-center identifier\.
- Required: true
- Kind: fact

## Sources

### Source 1: expense\-policy

- Title: Example expense policy
- Publisher: Example Corporation
- Published at: 2026\-01\-15
- Locator:
  - Kind: uri
  - Value: https\:\/\/example\.com\/policies\/expenses
- Citation:
  - Location: Section 4 — approval thresholds
  - Excerpt: Expenses above 5000 require finance review\. Prohibited categories must be rejected\.
- Rights: Example content\; not an operational policy\.

## Metadata

- Authors:
  - Judgment Pack Specification contributors
- Created at: 2026\-07\-22T00\:00\:00Z
- License: Apache\-2\.0
