# Synthetic partial\-trigger routing fixture

- Id: https\:\/\/example\.invalid\/judgment\-packs\/partial\-trigger\-conflict
- Version: 0\.1\.0
- Spec version: 0\.2\.0\-draft
- Digest: sha256:5db6116b3f23922c5e2c2ce7df958fbbdb790c80117219725f154511bf71256e
- View version: 1
- Description: Constructed corpus fixture whose escalation trigger list is a strict subset of the reasons its rules can generate\, so a retained reason can exist without requesting a handoff\. Synthetic content for specification testing\; it authorizes nothing\.

## Decision

- Intent: Exercise the handoff members of the portable disposition where the configured triggers name fewer reasons than resolution retains\.
- Question: Which synthetic review track does this request take\?

## Outcomes

| Id | Label | Description |
| --- | --- | --- |
| track\-a | Track A | The first synthetic routing outcome\. |
| track\-b | Track B | The second synthetic routing outcome\. |

## Rules

### Rule 1: select\-track\-a

- Description: Route to track A when the request carries the track\-A marker\.
- Outcome: track\-a
- On unknown: ignore
- When:
  - fact \"\/flags\/trackA\" equals true

### Rule 2: select\-track\-b

- Description: Route to track B when the request carries the track\-B marker\.
- Outcome: track\-b
- On unknown: ignore
- When:
  - fact \"\/flags\/trackB\" equals true

### Rule 3: hold\-for\-review\-decision

- Description: Route to track A when a review decision has been recorded\; escalate rather than guess when the decision is unknown\.
- Outcome: track\-a
- On unknown: escalate
- When:
  - fact \"\/flags\/reviewDecided\" equals true

## Escalation

- Triggers:
  - conflict
- Target:
  - Kind: human\-role
  - Name: Routing coordinator
- Message: Escalate a routing conflict to the coordinator\. An unknown review decision alone is deliberately not a configured trigger for this synthetic pack\.

## Metadata

- Authors:
  - Judgment Pack Specification contributors
- Created at: 2026\-07\-28T00\:00\:00Z
- License: Apache\-2\.0
