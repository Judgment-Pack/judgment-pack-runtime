# Synthetic decimal\-threshold fee fixture

- Id: https\:\/\/example\.invalid\/judgment\-packs\/decimal\-threshold\-fee
- Version: 0\.1\.0
- Spec version: 0\.2\.0\-draft
- Digest: sha256:e1c164cd11be03d6cffd3ec56a8056024c713244a0fcfccd54527d6cafe0a7f6
- View version: 1
- Description: Constructed corpus fixture whose rules order a decimal\-string amount against a threshold and key an exemption to an exact declared string\, so ordered comparison and equality are exercised separately\. Synthetic content for specification testing\; it authorizes nothing\.

## Decision

- Intent: Exercise ordered comparison of two decimal strings\, the deliberate absence of decimal\-aware equality\, and the refusal to coerce a JSON number into an ordered comparison\.
- Question: Which synthetic fee band applies to this declared amount\?

## Outcomes

| Id | Label | Description |
| --- | --- | --- |
| standard\-fee | Standard fee | The declared amount is at or above the threshold\. |
| reduced\-fee | Reduced fee | The declared amount is below the threshold\. |
| exempt | Exempt | The declared amount matches the exempt declaration exactly\. |

## Rules

### Rule 1: standard\-fee\-at\-or\-above\-threshold

- Description: Charge the standard fee when the declared amount is at or above the threshold of 1000\.
- Outcome: standard\-fee
- On unknown: escalate
- Rationale: Ordered comparison is by mathematical value\, so 1000\.00 is not above 1000 and 999\.99 is not at or above it\.
- When:
  - fact \"\/request\/amount\" greater\-than\-or\-equal \"1000\"

### Rule 2: reduced\-fee\-below\-threshold

- Description: Charge the reduced fee when the declared amount is below the threshold of 1000\.
- Outcome: reduced\-fee
- On unknown: escalate
- Rationale: An amount that is neither at or above nor below the threshold is not a decimal string this pack can order\, and escalating is preferred to guessing\.
- When:
  - fact \"\/request\/amount\" less\-than \"1000\"

### Rule 3: exempt\-declared\-amount

- Description: Exempt the declared amount when it is submitted as exactly the string 1000\.
- Outcome: exempt
- On unknown: ignore
- Rationale: Equality is type\-preserving JSON equality and is deliberately not decimal\-aware\, so a declaration of 1000\.00 is not exempt even though it is neither greater nor less than 1000\.
- When:
  - fact \"\/request\/amount\" equals \"1000\"

## Escalation

- Triggers:
  - unknown
- Target:
  - Kind: queue
  - Name: Fee review queue
- Message: Escalate an amount that cannot be ordered against the threshold\, including an amount submitted as a JSON number rather than a decimal string\.

## Metadata

- Authors:
  - Judgment Pack Specification contributors
- Created at: 2026\-07\-28T00\:00\:00Z
- License: Apache\-2\.0
