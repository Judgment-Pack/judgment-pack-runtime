package evaluation

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
	"github.com/Judgment-Pack/judgment-pack-runtime/internal/result"
)

func TestCoreURIAdmissionUsesGrammarAndRetainsExactIdentity(t *testing.T) {
	engine := newTestEngine(t)
	for _, version := range []string{artifacts.EvaluatorDraftVersion, artifacts.CoreVersion} {
		set, err := artifacts.Load(version)
		if err != nil {
			t.Fatal(err)
		}
		base, err := set.Case("valid/minimal-literal.json")
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			id    string
			valid bool
		}{
			{"urn:test:[x]", false}, {"https://example.invalid/[x]", false},
			{"https://%65xample.invalid/a", true}, {"foo://[v1.a]/", true},
			{"FOO://[Vf.a]:99999/a%5bb?x=/a?b#c/d?e", true},
			{"urn:test:foo\n", false},
		} {
			t.Run(version+"/"+tc.id, func(t *testing.T) {
				id, _ := json.Marshal(tc.id)
				pack := bytes.Replace(base, []byte(`"https://example.com/judgment-packs/minimal-literal"`), id, 1)
				before := bytes.Clone(pack)
				admitted := engine.AdmitPack(pack)
				options := Options{SpecVersion: version}
				if admitted.AdmitsWith(options) != tc.valid {
					t.Fatal("admission predicate differs")
				}
				evaluated, failure := engine.EvaluateAdmitted(admitted, []byte(`{}`), nil, options)
				if !tc.valid {
					if failure == nil || failure.Code != "JPS-EVALUATION-PACK-NOT-CONFORMANT" || failure.Class != result.ClassPackNotConformant {
						t.Fatalf("invalid URI evaluation result %+v", failure)
					}
				} else if failure != nil || evaluated.PackID != tc.id || evaluated.Status != "evaluated" {
					t.Fatalf("valid URI evaluation result %+v %+v", evaluated, failure)
				}
				if !bytes.Equal(pack, before) || !bytes.Equal(admitted.pack, before) || admitted.packDigest != sha256.Sum256(before) {
					t.Fatal("URI assertion changed retained bytes or binding digest")
				}
			})
		}
	}
}
