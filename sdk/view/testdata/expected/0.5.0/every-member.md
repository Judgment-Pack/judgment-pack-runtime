# Every member of a Core 0\.5\.0 pack

- Id: https\:\/\/example\.invalid\/judgment\-packs\/view\-every\-member
- Version: 2\.0\.0
- Spec version: 0\.5\.0
- Profile: base
- Digest: sha256:ea2e851d11926b00e942ae3be6be3d286b4acb79fd007e8a47453678f8255f1d
- View version: 1
- Description: Invented content for the pack view tests\; it authorizes nothing\.

## Decision

- Intent: Carry every member the schema allows at least once\.
- Question: Does the view carry every member\?
- Extensions:
  - com\.example\.decision\-note: \{\"a\"\:\[1\,\"1\"\,true\,null\]\,\"b\"\:2\}

## Applicability

- any of:
  - fact \"\/request\/kind\" equals \"purchase\"
  - fact \"\/request\/kind\" equals \"lease\"

## Outcomes

| Id | Label | Description |
| --- | --- | --- |
| approve | Approve | The purchase may proceed\. |
| refer | Refer | |
| decline | Decline | |

### Extensions of outcome 1: approve

- com\.example\.colour: \"green\"
- org\.judgmentpack\.annotations:
  - tier: green

### Extensions of outcome 2: refer

- org\.judgmentpack\.annotations:
  - tier: amber

## Rules

### Rule 1: over\-limit

- Description: An amount over the limit with a receipt is referred\.
- Outcome: refer
- On unknown: escalate
- Evidence requirement refs:
  - receipt
  - site\-visit
- Source refs:
  - policy
- Rationale: Section 4\.2 sets the limit\.
- Extensions:
  - com\.example\.rule\-owner: \{\"since\"\:2024\,\"team\"\:\"finance\"\}
  - org\.judgmentpack\.annotations:
    - category:
      - limits
      - approval
    - review\-cycle: monthly
- When:
  - all of:
    - fact \"\/request\/amount\" greater\-than\-or\-equal \"1000\.00\"
    - evidence present receipt
    - not:
      - literal false

### Rule 2: small

- Description: A small purchase is approved\.
- Outcome: approve
- On unknown: ignore
- When:
  - fact \"\/request\/category\" in value set small\-categories

## Exceptions

### Exception 1: auditor\-hold

- Description: An auditor hold declines\.
- Effect: force\-outcome
- Outcome: decline
- On unknown: escalate
- Source refs:
  - policy
- Extensions:
  - com\.example\.exception\-note: \"audit\"
- When:
  - fact \"\/request\/hold\" not\-equals false

### Exception 2: trusted\-vendor

- Description: A trusted vendor skips the limit rule\.
- Effect: suppress\-rule
- Target rule: over\-limit
- On unknown: ignore
- When:
  - fact \"\/vendor\/trusted\" equals true

### Exception 3: new\-vendor

- Description: A new vendor escalates\.
- Effect: escalate
- On unknown: ignore
- When:
  - fact \"\/vendor\/tier\" in \[\"new\"\,0\,false\,null\]

## Value sets

### Value set 1: small\-categories

- Description: Categories a small purchase may be in\.
- Entries:
  - Entry 1: \"stationery\"
    - Description: Paper and pens\.
    - Extensions:
      - org\.judgmentpack\.annotations:
        - code: S1
        - owners:
          - office
  - Entry 2: \"café\" (ASCII `"caf\u00e9"`)
    - Extensions:
      - org\.judgmentpack\.annotations:
        - label: Café `U+FFF9`menu`U+FFFB` (ASCII `"Caf\u00e9 \ufff9menu\ufffb"`)
  - Entry 3: \{\"unit\"\:1\.50\}

### Value set 2: unused

- Entries:
  - Entry 1: 0

## Fallback outcome

- Outcome: decline

## Escalation

- Triggers:
  - unknown
  - conflict
  - no\-match
- Message: Review the purchase by hand\.
- Routes:
  - Route 1:
    - Target:
      - Kind: queue
      - Name: Purchasing queue
    - Triggers:
      - unknown
      - no\-match
  - Route 2:
    - Target:
      - Kind: system
      - Name: Conflict resolver
- Extensions:
  - com\.example\.sla: \{\"hours\"\:24\}

## Evidence requirements

### Evidence requirement 1: receipt

- Description: A receipt for the purchase\.
- Required: true
- Kind: document
- Extensions:
  - com\.example\.retention: \"seven\-years\"

### Evidence requirement 2: site\-visit

- Description: A site visit record\.
- Required: false
- Kind: attestation

## Sources

### Source 1: policy

- Title: Purchasing policy
- Publisher: Finance
- Published at: 2026\-01\-15
- Locator:
  - Kind: repository
  - Value: policies\/purchasing
- Citation:
  - Location: Section 4\.2
  - Excerpt: Purchases over the limit need approval\.
- Rights: Internal use only\.
- Extensions:
  - com\.example\.source\-owner: \"finance\"

## Metadata

- Authors:
  - Purchasing team
  - Finance team
- Created at: 2026\-02\-01T09\:30\:00Z
- License: CC\-BY\-4\.0
- Required extensions:
  - com\.example\.rule\-owner
- Reviews:
  - Review 1:
    - Reviewer: Controller
    - Reviewed at: 2026\-02\-02T10\:00\:00Z
    - Disposition: approved
    - Note: Limits match the policy\.
  - Review 2:
    - Reviewer: Auditor
    - Reviewed at: 2026\-02\-03T11\:00\:00Z
    - Disposition: rejected
- Extensions:
  - com\.example\.metadata\-note: \"draft\"

## Extensions

- com\.example\.top\-level: \{\"y\"\:\[2\,3\]\,\"z\"\:1\}
- org\.judgmentpack\.annotations:
  - domain: purchasing
  - tags:
    - limits
