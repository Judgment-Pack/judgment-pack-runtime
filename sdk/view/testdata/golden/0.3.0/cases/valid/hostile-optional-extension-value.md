# Inert optional extension content

- Id: https\:\/\/example\.com\/judgment\-packs\/hostile\-optional\-extension\-value
- Version: 0\.1\.0
- Spec version: 0\.3\.0
- Digest: sha256:61fc9a6a0c14107714001be9ef2663e9a8c7b59bdba32576f95b5a7bed670d79
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
