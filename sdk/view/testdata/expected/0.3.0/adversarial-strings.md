# \# Heading \| with pipe \`tick\` \<script\>alert\(1\)\<\/script\> \[link\]\(https\:\/\/example\.invalid\) \*\*bold\*\* \_em\_ \~\~strike\~\~ \$x\$ \&amp\; \\ end

- Id: https\:\/\/example\.invalid\/judgment\-packs\/view\-adversarial\-strings\?q\=a\%7Cb\&r\=\_x\_\#frag
- Version: 0\.1\.0
- Spec version: 0\.3\.0
- Digest: sha256:bd8eb141687a4cd386aef4cb592001283a445ea1da016f25c6ade629023c02e8
- View version: 1
- Description: Line one`U+000A`\#\# Injected heading`U+000A`\| a \| b \|`U+000A`\|\-\-\-\|\-\-\-\|`U+000A`\- injected item`U+000D U+000A`\> quote`U+2028`separator`U+2029`paragraph

## Decision

- Intent: &#32;&#32;leading and trailing spaces&#32;&#32;
- Question: Bidi `U+202E`desrever`U+202C` and isolates `U+2066`left`U+2069` and marks `U+200E U+200F U+061C`\, a joiner `U+200D`\, a tab`U+0009`and controls `U+0000 U+001B`\[31m`U+007F U+0085`
- Extensions:
  - com\.example\.order: \{\"a\"\:\"ascii\"\,\"é\"\:\"accent\"\,\"😀\"\:\"emoji\"\,\"ﬁ\"\:\"ligature\"\} (ASCII `{"a":"ascii","\u00e9":"accent","\ud83d\ude00":"emoji","\ufb01":"ligature"}`)

## Outcomes

| Id | Label | Description |
| --- | --- | --- |
| pass | Pass \| fail | A cell \| with \| pipes and a trailing backslash \\ |
| fail | Fail`U+000A`\| injected \| row \| | |

## Rules

### Rule 1: pointers

- Description: Paths that need escaping\: \~0 and \~1\, the empty pointer\, and non\-ASCII names\.
- Outcome: pass
- On unknown: ignore
- Rationale: A long rationale\. A long rationale\. A long rationale\. A long rationale\. A long rationale\. A long rationale\. A long rationale\. A long rationale\. A long rationale\. A long rationale\. A long rationale\. A long rationale\. A long rationale\. A long rationale\. A long rationale\. A long rationale\. A long rationale\. A long rationale\. A long rationale\. A long rationale\. A long rationale\. A long rationale\. A long rationale\. A long rationale\. A long rationale\. A long rationale\. A long rationale\. A long rationale\. A long rationale\. A long rationale\. A long rationale\. A long rationale\. The end\.
- When:
  - all of:
    - fact \"\" equals \{\"whole\"\:\"document\"\,\"\|\"\:\"\`\"\}
    - fact \"\/a\~1b\/\~0c\/é𝔘\" (ASCII `"/a~1b/~0c/\u00e9\ud835\udd18"`) in \[\"x\|y\"\,\"\`z\`\"\,\"\<b\>\"\,1\,false\,null\]
    - fact \"\/\" not\-equals \"\]\(https\:\/\/example\.invalid\)\"

## Fallback outcome

- Outcome: fail

## Escalation

- Triggers:
  - unknown
- Target:
  - Kind: human\-role
  - Name: \<script\>alert\(\'name\'\)\<\/script\>
- Message: \@someone \#123 \:smile\: https\:\/\/example\.invalid\/path\?x\=1\&y\=2 www\.example\.invalid

## Evidence requirements

### Evidence requirement 1: proof

- Description: \!\[image\]\(https\:\/\/example\.invalid\/x\.png\) and \<img src\=x onerror\=alert\(1\)\> and \[ref\]\: https\:\/\/example\.invalid
- Required: true

## Sources

### Source 1: source

- Title: 1\. Looks like a list
- Publisher: \+ also a list
- Locator:
  - Kind: path
  - Value: docs\/a b\/c\|d\.md
- Citation:
  - Location: &#32;&#32;&#32;&#32;four spaces of indent
  - Excerpt: \`\`\``U+000A`fenced`U+000A`\`\`\`
- Rights: \*\*\* \-\-\- \_\_\_

## Metadata

- Authors:
  - Author with \| pipe
  - Author with \`tick\`
- License: \&copy\; \&\#169\; \&\#xA9\;

## Extensions

- com\.example\.html: \"\<div onclick\=\\\"x\\\"\>a\<\/div\>\"
- com\.example\.nested: \{\"deep\"\:\{\"deeper\"\:\[\"`U+202E`\"\,\"\\n\"\,\"\\\\\"\,\"\\\"quoted\\\"\"\]\}\} (ASCII `{"deep":{"deeper":["\u202e","\n","\\","\"quoted\""]}}`)
