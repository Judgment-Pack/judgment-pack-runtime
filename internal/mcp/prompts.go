package mcp

import (
	"encoding/json"
	"strings"
)

// The prompts surface (ADR-0008) serves authoring METHOD as static, versioned
// text; the client's model executes it with the client's key. The server never
// calls a model: serving a prompt is read-only, the same class of operation as
// get_schema. Everything here is non-normative guidance — following it does
// not make a pack conformant, and nothing in it is a conformance claim.

type promptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

type promptDefinition struct {
	name        string
	description string
	arguments   []promptArgument
	// build renders the prompt text with the caller's arguments.
	build func(args map[string]string) string
}

func promptDefinitions() []promptDefinition {
	return []promptDefinition{
		{
			name: "author_pack",
			description: "Guide an authoring session: encode one policy decision as a JPS judgment " +
				"pack using the validate/evaluate loop. Non-normative method guidance; the pack it " +
				"helps produce is yours, not the runtime's.",
			arguments: []promptArgument{
				{Name: "policy", Description: "The policy text or a description of the decision to encode.", Required: false},
			},
			build: buildAuthorPack,
		},
		{
			name: "test_pack",
			description: "Guide a logic-testing session: build an instance matrix for a pack you hold " +
				"and probe it with the experimental evaluator. Non-normative method guidance.",
			arguments: []promptArgument{
				{Name: "pack", Description: "The pack under test, as JSON text (optional; you may also reference a file you hold).", Required: false},
			},
			build: buildTestPack,
		},
		{
			name: "fix_pack",
			description: "Guide a repair session: work a non-conformant pack back to valid using the " +
				"validator's diagnostics. Non-normative method guidance.",
			arguments: []promptArgument{
				{Name: "diagnostics", Description: "The diagnostics from a failed validate call (optional).", Required: false},
			},
			build: buildFixPack,
		},
	}
}

const authoringDisclaimer = `(Method guidance from the judgment-pack runtime, non-normative: following it does not make a
pack conformant, and only "spec validate" / the "validate" tool decides conformance. The document
you produce is yours; this runtime stores nothing and decides nothing.)`

func buildAuthorPack(args map[string]string) string {
	var b strings.Builder
	b.WriteString("Encode ONE policy decision as a Judgment Pack (JPS 0.1.0-draft), working in a\n")
	b.WriteString("validate-and-fix loop against the judgment-pack tools.\n\n")
	if policy := strings.TrimSpace(args["policy"]); policy != "" {
		b.WriteString("The policy to encode:\n\n---\n" + policy + "\n---\n\n")
	} else {
		b.WriteString("Ask for the policy text or decision description if you do not already hold it.\n\n")
	}
	b.WriteString(`Work in this order:

1. ORIENT. Call get_schema for the format reference and list_examples for the bundled valid
   fixtures; fetch the one closest to your decision with get_example and use its SHAPE (not its
   content) as scaffolding. Fixtures are version-pinned conformance cases, not templates.

2. SCOPE. One pack = one decision ("may X be done?"), with the outcomes the policy itself names.
   Agent procedure ("confirm before acting") is not a decision; leave it out. If the policy names a
   case that must go to a human, that is the escalation machinery, not an outcome.

3. STRUCTURE for the resolution model. Two rules that fire together naming DIFFERENT outcomes are a
   conflict and the result is unresolved -- there is no rule priority. Two shapes work well:
   - Detector style: every rule detects one outcome (say, a violation -> deny); the opposite
     outcome is reachable only as fallbackOutcome. Simultaneous detections then agree instead of
     conflicting, and every fired rule is a citable reason.
   - Carve-out style: encode "if A then deny, otherwise allow when B" with a force-outcome or
     escalate EXCEPTION for A (exceptions outrank rules) and rules for B.
   Pick one deliberately; mixing affirmative and negative rules invites unresolved conflicts.

4. CONDITIONS. Facts are addressed by RFC 6901 JSON Pointer into one facts document. Ordered
   comparisons (greater-than family) are defined only over decimal STRINGS matching
   -?(0|[1-9][0-9]*)(\.[0-9]+)? -- a JSON number on either side yields unknown, silently. The
   format has no arithmetic, no date/time comparison, and no quantifier over arrays: any such value
   must be prepared upstream and supplied as a fact. Keep a PREPARED-FACTS ledger as you go: every
   fact that is computed or concluded rather than stated by the requester, and for each, whether
   producing it requires applying the policy itself (flag those loudly -- they are decision logic
   living outside your pack).

5. UNCERTAINTY is the point. onUnknown: escalate on a rule means "a missing fact here must stop
   the decision"; onUnknown: ignore means "this rule simply does not fire". Gate rules with an
   always-present boolean condition where you can: conjunction is strong, so a false gate beats an
   unknown sibling and an inapplicable rule stays false instead of escalating. Wire escalation
   triggers to the reasons you actually want handed to a human, and remember an outcome produced by
   fallbackOutcome is blocked by an escalating unknown -- that blocking is usually what you want.

6. EVIDENCE. evidenceRequirements with required: true block every evaluation that arrives without
   an evidence document -- use required: true only for evidence the decision truly cannot proceed
   without, and remember callers may not supply an evidence document at all.

7. LOOP. validate the draft; every diagnostic names its location and the fix; repair and repeat to
   exit 0. Then evaluate against 2-3 realistic facts documents and check the dispositions match
   your intent -- including one probe with a load-bearing fact REMOVED, which should escalate, not
   guess.

8. RECORD. Keep your interpretation decisions (ambiguous policy text, the reading you chose, why)
   and the prepared-facts ledger next to the pack. They are the audit trail of everything the pack
   itself cannot say.

`)
	b.WriteString(authoringDisclaimer)
	return b.String()
}

func buildTestPack(args map[string]string) string {
	var b strings.Builder
	b.WriteString("Probe the LOGIC of a judgment pack you hold, using the experimental_evaluate tool.\n")
	b.WriteString("Validation proved the pack well-formed; this session tests whether it decides what its\n")
	b.WriteString("author intended. The evaluator's surface is experimental and may change; its conformance\n")
	b.WriteString("claim is stated, in full and only, in CONFORMANCE.md, and whatever it says there says\n")
	b.WriteString("nothing about whether this pack decides well. Only a pack declaring specVersion\n")
	b.WriteString("0.2.0-draft is evaluated (JPS §11: re-declaring is one edit, the specVersion string).\n\n")
	if pack := strings.TrimSpace(args["pack"]); pack != "" {
		b.WriteString("The pack under test:\n\n---\n" + pack + "\n---\n\n")
	}
	b.WriteString(`Build an instance matrix -- one facts document per row -- and evaluate each:

1. One instance per declared outcome, engineered so exactly that outcome's rule fires.
2. A conflict probe: facts that make two rules with DIFFERENT outcomes true at once. Expect
   unresolved with reason "conflict" -- if you get an outcome instead, the rules cannot actually
   fire together and your probe is wrong, or the pack shape differs from what you think.
3. An unknown probe per escalating rule: omit one fact that rule needs. Expect unresolved with
   reason "unknown" and a handoff if the trigger is wired. If the fallback outcome appears instead,
   the rule has onUnknown: ignore -- check that is intended.
4. A missing-evidence probe if the pack declares required evidence: evaluate without an evidence
   document. Expect unresolved with reason "missing-required-evidence".
5. A not-applicable probe if the pack declares applicability: facts outside scope. Expect a
   not-applicable result, not an outcome.
6. A forced-outcome probe per exception: facts that satisfy the exception AND a rule it should
   override. Expect the exception's outcome with the rule skipped.
7. An ordered-comparison probe if any rule compares magnitudes: supply the value as a JSON number
   instead of a decimal string. Expect unknown behavior per the rule's onUnknown -- this catches
   the most common silent authoring mistake.

Read each disposition fully: kind, outcomeId, reasons, handoff, and the trace (which rules fired,
which were skipped). A divergence between expectation and disposition is either a pack bug or an
intended escalation -- decide which, fix the pack if the former, and re-run the whole matrix after
any change. Keep the matrix with the pack; it is the pack's regression suite.

`)
	b.WriteString(authoringDisclaimer)
	return b.String()
}

func buildFixPack(args map[string]string) string {
	var b strings.Builder
	b.WriteString("Repair a judgment pack that failed validation, using the validator's diagnostics.\n\n")
	if diagnostics := strings.TrimSpace(args["diagnostics"]); diagnostics != "" {
		b.WriteString("The diagnostics:\n\n---\n" + diagnostics + "\n---\n\n")
	} else {
		b.WriteString("Run the validate tool on the pack first; work from its diagnostics.\n\n")
	}
	b.WriteString(`Each diagnostic is self-sufficient: a code, the JSON Pointer location, and a message that
names the offending value and what would be accepted. Work them in order:

1. Carrier diagnostics (malformed or duplicate-member JSON) first -- nothing else is trustworthy
   until the document parses cleanly.
2. Structural diagnostics next. Common causes: an unknown member (typo or misplaced field), a
   condition "op" outside the allowed set, an ordered-comparison operand that is a JSON number
   instead of a decimal string, an exception whose members do not match its effect (force-outcome
   needs "outcome", suppress-rule needs "targetRule", escalate needs neither).
3. Semantic diagnostics last. These are almost always dangling references: a rule outcome,
   fallbackOutcome, exception target, evidenceRequirementRef, or sourceRef that names no declared
   id. The message lists the declared ids -- fix the reference or add the declaration, then check
   for others of the same kind before re-validating.

Re-validate after each batch; repeat to exit 0. An "unsupported" result is different from invalid:
it means a required extension this consumer does not support, which no edit to the pack fixes.
After reaching valid, re-run whatever instance matrix exists for the pack -- a repair that changes
ids or structure can change behavior, and validity does not test logic.

`)
	b.WriteString(authoringDisclaimer)
	return b.String()
}

// listPrompts is the prompts/list payload.
func listPrompts() []map[string]any {
	defs := promptDefinitions()
	out := make([]map[string]any, 0, len(defs))
	for _, def := range defs {
		out = append(out, map[string]any{
			"name":        def.name,
			"description": def.description,
			"arguments":   def.arguments,
		})
	}
	return out
}

// getPrompt renders one prompt with the caller's arguments.
func getPrompt(rawParams json.RawMessage) (map[string]any, *rpcError) {
	var params struct {
		Name      string            `json:"name"`
		Arguments map[string]string `json:"arguments"`
	}
	if err := json.Unmarshal(rawParams, &params); err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: "Invalid prompts/get parameters."}
	}
	for _, def := range promptDefinitions() {
		if def.name != params.Name {
			continue
		}
		if params.Arguments == nil {
			params.Arguments = map[string]string{}
		}
		return map[string]any{
			"description": def.description,
			"messages": []map[string]any{
				{
					"role":    "user",
					"content": map[string]any{"type": "text", "text": def.build(params.Arguments)},
				},
			},
		}, nil
	}
	return nil, &rpcError{Code: codeInvalidParams, Message: "Unknown prompt: " + params.Name}
}
