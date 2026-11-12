# Synthetic data request intake triage

- Id: https\:\/\/example\.invalid\/judgment\-packs\/data\-request\-intake\-triage
- Version: 0\.1\.0
- Spec version: 0\.2\.0\-draft
- Digest: sha256:5bdf53e5c283b5a7c7a04373cfa66a488b021c1e098254424c0de7a963ea8aca
- View version: 1
- Description: Invented intake\-triage policy for a fictional data platform team\: route an incoming data request to proceed\, clarify\/return\, or decline\/redirect once completeness and appropriateness have been assessed\. Synthetic content for specification testing\; it authorizes nothing\.

## Decision

- Intent: Triage an incoming data request into exactly one intake outcome once the completeness of the submission and the appropriateness of the request have been assessed\.
- Question: Should this data request proceed\, be returned for clarification\, or be declined and redirected\?

## Applicability

- fact \"\/request\/type\" in \[\"new\-data\-pipeline\"\,\"pipeline\-change\"\,\"dataset\-onboarding\"\,\"one\-time\-extract\"\,\"data\-access\"\,\"reporting\-feed\"\]

## Outcomes

| Id | Label | Description |
| --- | --- | --- |
| proceed | Proceed | Complete and appropriate\: accept into the delivery queue\, record the accepted scope\, and hand off to the build process\. |
| clarify\-return | Clarify \/ return | Substantially valid but incomplete or not yet evaluable\: return with the specific enumerated gaps\, all at once\. |
| decline\-redirect | Decline \/ redirect | Fails an appropriateness criterion that more information will not fix\: decline with a clear reason and\, where possible\, a redirect or alternative\. |

## Rules

### Rule 1: decline\-hard\-appropriateness\-failure

- Description: Decline when the request fails an appropriateness criterion that more information cannot fix\, such as no sponsored purpose\, consumers not authorized for the data classification\, a data\-residency breach\, duplication of an existing asset\, or the wrong delivery mechanism\.
- Outcome: decline\-redirect
- On unknown: escalate
- Evidence requirement refs:
  - intake\-form
- Source refs:
  - intake\-standard
- Rationale: The cited standard treats a hard appropriateness failure as a decline\, not a clarification\.
- When:
  - fact \"\/request\/appropriateness\" equals \"hard\-fail\"

### Rule 2: clarify\-incomplete\-or\-not\-evaluable

- Description: Return for clarification when mandatory fields are missing or ambiguous\, when approvals or triggered reviews are merely pending\, or when appropriateness cannot yet be judged\.
- Outcome: clarify\-return
- On unknown: escalate
- Evidence requirement refs:
  - intake\-form
- Source refs:
  - intake\-standard
- Rationale: The cited standard returns substantially valid but not yet decidable requests with enumerated gaps\.
- When:
  - any of:
    - fact \"\/request\/completeness\" equals \"incomplete\"
    - fact \"\/request\/appropriateness\" in \[\"pending\"\,\"not\-evaluable\"\]

### Rule 3: proceed\-complete\-and\-appropriate

- Description: Proceed when every mandatory intake field is complete\, every appropriateness criterion passes\, and the mandatory evidence is present\.
- Outcome: proceed
- On unknown: escalate
- Evidence requirement refs:
  - intake\-form
  - sponsor\-endorsement
- Source refs:
  - intake\-standard
- Rationale: The cited standard accepts complete and appropriate requests into the delivery queue\.
- When:
  - all of:
    - fact \"\/request\/completeness\" equals \"complete\"
    - fact \"\/request\/appropriateness\" equals \"pass\"
    - evidence present intake\-form
    - evidence present sponsor\-endorsement

## Exceptions

### Exception 1: embargoed\-information\-to\-unauthorized

- Description: Embargoed material non\-public information reaching recipients outside the authorized set\, or ahead of public release\, is a hard decline\: restrict to the authorized set and escalate to the disclosure office\.
- Effect: force\-outcome
- Outcome: decline\-redirect
- On unknown: escalate
- When:
  - fact \"\/request\/embargoedInformationToUnauthorizedRecipients\" equals true

## Fallback outcome

- Outcome: clarify\-return

## Escalation

- Triggers:
  - not\-applicable
  - missing\-required\-evidence
  - unknown
  - conflict
  - no\-match
- Target:
  - Kind: human\-role
  - Name: Intake reviewer
- Message: Escalate to the intake reviewer\: enumerate the missing intake fields or unresolved appropriateness criteria\, note any conflicting rules\, and route embargoed\-information questions to the disclosure office\.

## Evidence requirements

### Evidence requirement 1: intake\-form

- Description: The submitted request carrying every mandatory intake field\.
- Required: true
- Kind: document

### Evidence requirement 2: sponsor\-endorsement

- Description: A named accountable business sponsor at or above the minimum seniority threshold\.
- Required: true
- Kind: attestation

### Evidence requirement 3: sensitive\-data\-approvals

- Description: Data\-owner\, privacy\, and\/or legal approvals where sensitive or personal data is present\.
- Required: false
- Kind: attestation

## Sources

### Source 1: intake\-standard

- Title: Example data request intake standard
- Publisher: Example Data Governance Office
- Published at: 2026\-01\-15
- Locator:
  - Kind: uri
  - Value: https\:\/\/example\.invalid\/standards\/data\-request\-intake
- Citation:
  - Location: Section 6 — Triage decision and outcomes
  - Excerpt: The reviewer assigns exactly one outcome\: proceed when complete and appropriate\; clarify\/return when substantially valid but incomplete or not yet evaluable\; decline\/redirect when an appropriateness criterion fails in a way more information will not fix\.
- Rights: Invented content for specification testing\; not an operational policy\.

## Metadata

- Authors:
  - Judgment Pack Specification contributors
- Created at: 2026\-07\-27T00\:00\:00Z
- License: Apache\-2\.0
