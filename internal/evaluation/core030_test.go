package evaluation

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

// These expectations are authored from §§6.7, 8.1, 8.3, independently of the
// evaluator and separately from the named 20-row imported Core 0.3 corpus.
func routedPack(when, escalation, extra string) []byte {
	optional := ""
	if escalation != "" {
		optional += `,"escalation":` + escalation
	}
	if extra != "" {
		optional += `,` + extra
	}
	return []byte(`{"specVersion":"0.3.0","id":"https://example.com/packs/routes","version":"1.0.0","title":"Routes","decision":{"intent":"Review","question":"Review?"},"outcomes":[{"id":"yes","label":"Yes"},{"id":"no","label":"No"}],"rules":[{"id":"one","description":"One","when":` + when + `,"outcome":"yes","onUnknown":"ignore"}]` + optional + `}`)
}

func TestCore030RoutingNormativeCases(t *testing.T) {
	engine := newTestEngine(t)
	const falseCondition = `{"op":"literal","value":false}`
	const direct = `"exceptions":[{"id":"direct","description":"Direct","when":{"op":"literal","value":true},"effect":"escalate","onUnknown":"ignore"}]`
	cases := []struct{ name, when, escalation, extra, want string }{
		{"ordered duplicates", falseCondition, `{"triggers":["no-match"],"routes":[{"target":{"kind":"queue","name":"Z"}},{"target":{"kind":"system","name":"A"},"triggers":["no-match"]},{"target":{"kind":"queue","name":"Z"}}]}`, "", `{"handoff":{"routes":[{"kind":"queue","name":"Z"},{"kind":"system","name":"A"},{"kind":"queue","name":"Z"}],"state":"requested","triggeredBy":["no-match"]},"kind":"unresolved","reasons":["no-match"]}`},
		{"unconditional route cannot request", falseCondition, `{"triggers":["unknown"],"routes":[{"target":{"kind":"queue","name":"Review"}}]}`, "", `{"handoff":{"routes":[],"state":"none"},"kind":"unresolved","reasons":["no-match"]}`},
		{"outcome requests nothing", `{"op":"literal","value":true}`, `{"triggers":["unknown"],"routes":[{"target":{"kind":"queue","name":"Review"}}]}`, "", `{"handoff":{"routes":[],"state":"none"},"kind":"outcome","outcomeId":"yes","reasons":[]}`},
		{"ordinary request selects no route", falseCondition, `{"triggers":["no-match","unknown"],"routes":[{"target":{"kind":"queue","name":"Review"},"triggers":["unknown"]}]}`, "", `{"handoff":{"routes":[],"state":"none"},"kind":"unresolved","reasons":["no-match"]}`},
		{"direct request obeys filter", falseCondition, `{"triggers":["unknown"],"routes":[{"target":{"kind":"queue","name":"Review"},"triggers":["unknown"]}]}`, direct, `{"handoff":{"routes":[],"state":"none"},"kind":"unresolved","reasons":["exception-escalation"]}`},
		{"direct unconditional route", falseCondition, `{"triggers":["unknown"],"routes":[{"target":{"kind":"queue","name":"Review"}}]}`, direct, `{"handoff":{"routes":[{"kind":"queue","name":"Review"}],"state":"requested","triggeredBy":["exception-escalation"]},"kind":"unresolved","reasons":["exception-escalation"]}`},
		{"direct no escalation", falseCondition, "", direct, `{"handoff":{"state":"requested","triggeredBy":["exception-escalation"]},"kind":"unresolved","reasons":["exception-escalation"]}`},
		{"target only", falseCondition, `{"triggers":["no-match"],"target":{"kind":"queue","name":"Review"}}`, "", `{"handoff":{"state":"requested","triggeredBy":["no-match"]},"kind":"unresolved","reasons":["no-match"]}`},
		{"filter matches complete reasons", falseCondition, `{"triggers":["unknown","missing-required-evidence"],"routes":[{"target":{"kind":"queue","name":"Missing"},"triggers":["missing-required-evidence"]},{"target":{"kind":"queue","name":"Unknown"},"triggers":["unknown"]}]}`, `"evidenceRequirements":[{"id":"proof","description":"Proof","required":true}],"exceptions":[{"id":"uncertain","description":"Unknown","when":{"op":"fact","path":"/absent","operator":"equals","value":true},"effect":"escalate","onUnknown":"escalate"}]`, `{"handoff":{"routes":[{"kind":"queue","name":"Missing"},{"kind":"queue","name":"Unknown"}],"state":"requested","triggeredBy":["missing-required-evidence","unknown"]},"kind":"unresolved","reasons":["missing-required-evidence","unknown"]}`},
		{"direct plus retained reason selects filter", falseCondition, `{"triggers":["missing-required-evidence"],"routes":[{"target":{"kind":"queue","name":"Proof"},"triggers":["missing-required-evidence"]}]}`, direct + `,"evidenceRequirements":[{"id":"proof","description":"Proof","required":true}]`, `{"handoff":{"routes":[{"kind":"queue","name":"Proof"}],"state":"requested","triggeredBy":["exception-escalation","missing-required-evidence"]},"kind":"unresolved","reasons":["exception-escalation","missing-required-evidence"]}`},
	}

	// §8.3 keeps the complete pre-filter triggeredBy set even though the pack
	// declares a route for only one of its two retained trigger reasons.
	cases = append(cases, struct{ name, when, escalation, extra, want string }{
		"route filtering does not narrow triggers", falseCondition,
		`{"triggers":["unknown","missing-required-evidence"],"routes":[{"target":{"kind":"queue","name":"Missing"},"triggers":["missing-required-evidence"]}]}`,
		`"evidenceRequirements":[{"id":"proof","description":"Proof","required":true}],"exceptions":[{"id":"uncertain","description":"Unknown","when":{"op":"fact","path":"/absent","operator":"equals","value":true},"effect":"escalate","onUnknown":"escalate"}]`,
		`{"handoff":{"routes":[{"kind":"queue","name":"Missing"}],"state":"requested","triggeredBy":["missing-required-evidence","unknown"]},"kind":"unresolved","reasons":["missing-required-evidence","unknown"]}`,
	})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pack := routedPack(tc.when, tc.escalation, tc.extra)
			var evidence []byte
			if strings.Contains(tc.extra, `"evidenceRequirements"`) {
				evidence = []byte(`{"proof":"absent"}`)
			}
			evaluated, failure := engine.EvaluateWith(pack, []byte(`{}`), evidence, Options{SpecVersion: artifacts.CoreVersion})
			if failure != nil {
				t.Fatalf("evaluation refused: %+v", failure)
			}
			encoded, err := evaluated.Disposition.Canonical()
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != tc.want {
				t.Fatalf("got %s\nwant %s", encoded, tc.want)
			}
			if evaluated.SpecVersion != artifacts.CoreVersion || evaluated.EvaluatorSpecVersion != artifacts.CoreVersion {
				t.Fatalf("contract metadata: %+v", evaluated)
			}
			if evaluated.Disposition.Handoff.Routes != nil && evaluated.HandoffTarget != nil {
				t.Fatal("routed result invented a singular target")
			}
			if _, err := DecodeDispositionWith(encoded, result.Core030EvaluatorContract()); err != nil {
				t.Fatalf("produced illegal disposition: %v", err)
			}
		})
	}
}

func TestCore030ProfilesPrecedeOtherInputsAndPreserveIdentity(t *testing.T) {
	engine := newTestEngine(t)
	pack := routedPack(`{"op":"literal","value":false}`, "", `"profile":"extended"`)
	admitted := engine.AdmitPack(pack)
	wantDigest := sha256.Sum256(pack)
	for _, options := range []Options{{SpecVersion: artifacts.CoreVersion}, {SpecVersion: artifacts.CoreVersion, OversizedInputs: []string{"facts", "evidence"}}, {RFC0008Quantifiers: true}, {RFC0016OutcomeValues: true}, {SpecVersion: artifacts.CoreVersion, RFC0008Quantifiers: true}} {
		_, failure := engine.EvaluateAdmitted(admitted, []byte(`{`), []byte(`{`), options)
		if failure == nil || failure.Code != "profile-unsupported" || failure.Profile != "extended" || failure.Message != "" || failure.ExitCode != result.ExitUnsupported {
			t.Fatalf("profile refusal: %+v", failure)
		}
		_, failure = admitted.CheckFactTypes([]byte(`{`), options)
		if failure == nil || failure.Code != "profile-unsupported" || failure.Profile != "extended" || failure.Message != "" {
			t.Fatalf("advice swallowed refusal: %+v", failure)
		}
	}
	if admitted.packDigest != wantDigest || !bytes.Equal(admitted.pack, pack) {
		t.Fatal("profile negotiation modified identity")
	}
	base := routedPack(`{"op":"literal","value":true}`, "", "")
	explicit := routedPack(`{"op":"literal","value":true}`, "", `"profile":"base"`)
	if engine.AdmitPack(base).packDigest == engine.AdmitPack(explicit).packDigest {
		t.Fatal("implicit profile was inserted before digest")
	}
	shared := engine.AdmitPack(base)
	_, failure := engine.EvaluateAdmitted(shared, []byte(`{}`), nil, Options{SpecVersion: artifacts.CoreVersion, SupportedProfiles: []string{}})
	if failure == nil || failure.Code != "profile-unsupported" || failure.Profile != "base" {
		t.Fatalf("implicit base unsupported: %+v", failure)
	}
	if _, failure = engine.EvaluateAdmitted(shared, []byte(`{}`), nil, Options{SpecVersion: artifacts.CoreVersion}); failure != nil {
		t.Fatalf("profile cache mixed capabilities: %+v", failure)
	}
	if _, failure = engine.EvaluateAdmitted(shared, []byte(`{}`), nil, Options{}); failure == nil || failure.Code != "JPS-EVALUATION-PACK-SPEC-VERSION" {
		t.Fatalf("version cache mixed contracts: %+v", failure)
	}
}

func TestCore030SelectionIsBoundedAndNotFullAdmission(t *testing.T) {
	for _, raw := range []string{`{}`, `{"specVersion":null}`, `{"specVersion":""}`, `{"specVersion":"0.1.0-draft"}`, `{"specVersion":"0.3"}`, `{"specVersion":"0.3.0","profile":null}`, `{"specVersion":"0.3.0","profile":"future"}`, `{"specVersion":"0.3.0","profile":"extended",`} {
		if _, failure := SelectContract([]byte(raw)); failure == nil {
			t.Errorf("selected invalid contract: %s", raw)
		}
	}
	contract, failure := SelectContract([]byte(`{"specVersion":"0.3.0"}`))
	if failure != nil || contract != result.Core030EvaluatorContract() {
		t.Fatalf("selection is not full admission: %+v %+v", contract, failure)
	}
	_, failure = SelectContract([]byte(`{"specVersion":"0.3.0","profile":"extended","outcomes":false}`))
	if failure == nil || failure.Code != "profile-unsupported" || failure.Profile != "extended" || failure.Message != "" {
		t.Fatalf("selection hid profile: %+v", failure)
	}
}

func TestCore030AndLegacyCorpusRemainSeparatelyScoped(t *testing.T) {
	engine := newTestEngine(t)
	for _, version := range []string{artifacts.EvaluatorDraftVersion, artifacts.CoreVersion} {
		report, failure := engine.RunCorpus(version, "test")
		if failure != nil || report.Status != "passed" || report.Summary.Total != 20 || report.SpecVersion != version {
			t.Fatalf("named 20-row %s corpus: %+v %+v", version, report, failure)
		}
	}
}

func TestCore030DoesNotExtendDraftOptIns(t *testing.T) {
	engine := newTestEngine(t)
	pack := routedPack(`{"op":"literal","value":true}`, "", "")
	for _, options := range []Options{{SpecVersion: artifacts.CoreVersion, RFC0008Quantifiers: true}, {SpecVersion: artifacts.CoreVersion, RFC0016OutcomeValues: true}} {
		if _, failure := engine.EvaluateWith(pack, []byte(`{}`), nil, options); failure == nil || failure.ExitCode != result.ExitInvocation {
			t.Fatalf("extended draft posture: %+v", failure)
		}
	}
}

func TestCore030TargetOnlyCanonicalBytesStayLegacy(t *testing.T) {
	engine := newTestEngine(t)
	pack := routedPack(`{"op":"literal","value":false}`, `{"triggers":["no-match"],"target":{"kind":"queue","name":"Review"}}`, "")
	modern, failure := engine.EvaluateWith(pack, []byte(`{}`), nil, Options{SpecVersion: artifacts.CoreVersion})
	if failure != nil {
		t.Fatal(failure)
	}
	legacy, failure := engine.EvaluateWith(bytes.Replace(pack, []byte(`"0.3.0"`), []byte(`"0.2.0-draft"`), 1), []byte(`{}`), nil, Options{})
	if failure != nil {
		t.Fatal(failure)
	}
	a, _ := modern.Disposition.Canonical()
	b, _ := legacy.Disposition.Canonical()
	if !bytes.Equal(a, b) || !reflect.DeepEqual(modern.HandoffTarget, legacy.HandoffTarget) {
		t.Fatalf("legacy bytes changed: %s %s", a, b)
	}
}

func TestCore030ExpectedDispositionRejectsMutations(t *testing.T) {
	contract := result.Core030EvaluatorContract()
	valid := `{"kind":"unresolved","reasons":["exception-escalation"],"handoff":{"state":"none","routes":[]}}`
	if _, err := DecodeDispositionWith(json.RawMessage(valid), contract); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"kind":"unresolved","reasons":["unknown"],"handoff":{"state":"requested","triggeredBy":["unknown"],"routes":[]}}`,
		`{"kind":"unresolved","reasons":["unknown"],"handoff":{"state":"none","routes":[{"kind":"queue","name":"Review"}]}}`,
		`{"kind":"unresolved","reasons":["unknown"],"handoff":{"state":"none","routes":null}}`,
		`{"kind":"unresolved","reasons":["unknown"],"handoff":{"state":"requested","triggeredBy":["unknown"],"routes":[null]}}`,
		`{"kind":"unresolved","reasons":["unknown"],"handoff":{"state":"requested","triggeredBy":["unknown"],"routes":[{"kind":"email","name":"Review"}]}}`,
		`{"kind":"unresolved","reasons":["unknown"],"handoff":{"state":"requested","triggeredBy":["unknown"],"routes":[{"kind":"queue","Name":"Review"}]}}`,
		`{"kind":"unresolved","reasons":["unknown"],"handoff":{"state":"requested","triggeredBy":["unknown"],"routes":[{"kind":"queue","name":"Review","extra":true}]}}`,
		`{"kind":"unresolved","reasons":["unknown"],"handoff":{"state":"requested","triggeredBy":["unknown"],"routes":[{"kind":"queue","name":"Review","name":"Other"}]}}`,
		`{"kind":"unresolved","reasons":["exception-escalation"],"handoff":{"state":"none"}}`,
	} {
		if _, err := DecodeDispositionWith(json.RawMessage(raw), contract); err == nil {
			t.Errorf("accepted illegal disposition: %s", raw)
		}
	}
	if _, err := DecodeDisposition(json.RawMessage(valid)); err == nil {
		t.Fatal("legacy decoder inferred version from routes")
	}
	route := `{"kind":"queue","name":"Review"}`
	for _, count := range []int{8, 9, 16, 17} {
		routes := make([]string, count)
		for i := range routes {
			routes[i] = route
		}
		raw := `{"kind":"unresolved","reasons":["unknown"],"handoff":{"state":"requested","triggeredBy":["unknown"],"routes":[` + strings.Join(routes, ",") + `]}}`
		_, err := DecodeDispositionWith(json.RawMessage(raw), contract)
		if (err == nil) != (count <= 8) {
			t.Fatalf("base handoff %d: %v", count, err)
		}
	}
	contract.Profile = "extended"
	if _, err := DecodeDispositionWith(json.RawMessage(valid), contract); err == nil {
		t.Fatal("unsupported extended contract admitted")
	}
}

func TestCore030AdmissionHelpersAndRouteWork(t *testing.T) {
	engine := newTestEngine(t)
	pack := routedPack(`{"op":"literal","value":false}`, `{"triggers":["no-match"],"routes":[{"target":{"kind":"queue","name":"Review"}}]}`, "")
	admitted := engine.AdmitPack(pack)
	if admitted.Admits(nil) || !admitted.AdmitsWith(Options{SpecVersion: artifacts.CoreVersion}) || !engine.AdmitsWith(pack, Options{SpecVersion: artifacts.CoreVersion}) {
		t.Fatal("static admission lost exact contract")
	}
	if admitted.AdmitsWith(Options{SpecVersion: artifacts.CoreVersion, SupportedProfiles: []string{}}) {
		t.Fatal("static admission skipped profiles")
	}
	_, failure := engine.EvaluateAdmitted(admitted, []byte(`{}`), nil, Options{SpecVersion: artifacts.CoreVersion, WorkBudget: 2})
	if failure == nil || failure.Class != result.ClassResourceExhaustion {
		t.Fatalf("route traversal was uncharged: %+v", failure)
	}
	if _, failure = engine.EvaluateAdmitted(admitted, []byte(`{}`), nil, Options{SpecVersion: artifacts.CoreVersion, WorkBudget: 3}); failure != nil {
		t.Fatalf("bounded route budget: %+v", failure)
	}
}
