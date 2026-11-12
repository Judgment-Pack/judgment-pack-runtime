# Synthetic direct\-escalation fixture without a handoff configuration

- Id: https\:\/\/example\.invalid\/judgment\-packs\/direct\-exception\-escalation
- Version: 0\.1\.0
- Spec version: 0\.2\.0\-draft
- Digest: sha256:cd7e36c4b4e67a33edd93cc099d4637304be8c27c7c5c56e6980fba8ebbf14c6
- View version: 1
- Description: Constructed corpus fixture carrying an escalate exception and no escalation object\, so a direct handoff request has no Core\-defined destination\. Synthetic content for specification testing\; it authorizes nothing\.

## Decision

- Intent: Exercise a direct exception escalation in a pack that supplies no handoff configuration\.
- Question: Should this synthetic request be approved\, held\, or handed off\?

## Outcomes

| Id | Label | Description |
| --- | --- | --- |
| approve | Approve | The synthetic request proceeds\. |
| hold | Hold | The synthetic request waits\. |

## Rules

### Rule 1: approve\-when\-ready

- Description: Approve the request once it is marked ready\.
- Outcome: approve
- On unknown: ignore
- When:
  - fact \"\/request\/ready\" equals true

### Rule 2: hold\-when\-not\-ready

- Description: Hold the request while it is marked not ready\.
- Outcome: hold
- On unknown: ignore
- When:
  - fact \"\/request\/ready\" equals false

## Exceptions

### Exception 1: escalate\-flagged\-request

- Description: A flagged request is handed to a person regardless of readiness\; this pack declares no handoff configuration\, so the request names no destination\.
- Effect: escalate
- On unknown: ignore
- When:
  - fact \"\/request\/flagged\" equals true

## Metadata

- Authors:
  - Judgment Pack Specification contributors
- Created at: 2026\-07\-28T00\:00\:00Z
- License: Apache\-2\.0
