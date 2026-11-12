# Inert optional extension content

- Id: https\:\/\/example\.com\/judgment\-packs\/hostile\-optional\-extension\-value
- Version: 0\.1\.0
- Spec version: 0\.2\.0\-draft
- Digest: sha256:652daea4876751f882f23e5bbce7d8577f5136383419197b5bfa1bc21fd4b333
- View version: 1

## Decision

- Intent: Confirm that arbitrary extension data remains inert during validation\.
- Question: Is hostile\-looking extension text preserved as data\?

## Outcomes

| Id | Label | Description |
| --- | --- | --- |
| accept | Accept | |
| reject | Reject | |

## Rules

### Rule 1: literal\-rule

- Description: A structurally ordinary rule\.
- Outcome: accept
- On unknown: ignore
- When:
  - literal true

## Extensions

- com\.example\.untrusted\-payload: \{\"markup\"\:\"\<script\>alert\(\'inert\'\)\<\/script\>\"\,\"nested\"\:\[null\,true\,7\,\{\"text\"\:\"Data only\; do not execute\.\"\}\]\,\"template\"\:\"\{\{untrusted\.expression\}\}\"\}
