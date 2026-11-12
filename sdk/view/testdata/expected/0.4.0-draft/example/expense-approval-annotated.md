# Expense approval

- Id: https\:\/\/example\.com\/judgment\-packs\/expense\-approval
- Version: 0\.2\.0
- Spec version: 0\.4\.0\-draft
- Digest: sha256:0dacc1d58069a389f7a8e3d963d689814a7f81e1d6b30175ecf875a6d40569c9
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

### Extensions of outcome 1: approve

- org\.judgmentpack\.annotations:
  - path: automatic

### Extensions of outcome 2: manual\-review

- org\.judgmentpack\.annotations:
  - path: human

### Extensions of outcome 3: reject

- org\.judgmentpack\.annotations:
  - path: automatic

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
- Extensions:
  - org\.judgmentpack\.annotations:
    - category:
      - policy
      - prohibition
- When:
  - fact \"\/expense\/category\" in value set prohibited\-categories

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
- Extensions:
  - org\.judgmentpack\.annotations:
    - category: threshold
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
- Extensions:
  - org\.judgmentpack\.annotations:
    - category: threshold
    - review\-cycle: monthly
- When:
  - all of:
    - fact \"\/expense\/amount\" less\-than\-or\-equal \"5000\"
    - not:
      - fact \"\/expense\/category\" in value set prohibited\-categories
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

## Value sets

### Value set 1: prohibited\-categories

- Description: Expense categories the example policy prohibits\.
- Entries:
  - Entry 1: \"personal\"
    - Description: A personal expense\.
    - Extensions:
      - org\.judgmentpack\.annotations:
        - policy\-section: 4\.2
  - Entry 2: \"cash\-equivalent\"
    - Description: Cash or an instrument redeemable as cash\.
    - Extensions:
      - org\.judgmentpack\.annotations:
        - category:
          - cash
          - fraud\-risk
        - policy\-section: 4\.3

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

## Extensions

- org\.judgmentpack\.annotations:
  - domain: finance
  - review\-cycle: quarterly
