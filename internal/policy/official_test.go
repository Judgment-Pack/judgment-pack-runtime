package policy

import (
	"bytes"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/artifacts"
)

func officialRead(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := official.ReadFile("official/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestOfficialDraftProvenanceAndTampering(t *testing.T) {
	if err := VerifyOfficialArtifacts(); err != nil {
		t.Fatal(err)
	}
	manifest := mustMap(t, officialRead(t, "provenance.json"))
	if manifest["sourceRelease"] != DraftRelease || manifest["sourceRevision"] != DraftRevision || manifest["sourceArchiveSHA256"] != "ce9bac7fc4a9e2658146a540966aec1e935e7624ba5a7ad70ff074319078b6b9" || len(array(manifest["files"])) != 387 {
		t.Fatal("official archive provenance changed")
	}
	files := fstest.MapFS{}
	if err := fs.WalkDir(official, "official", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			raw, err := official.ReadFile(name)
			if err != nil {
				return err
			}
			files[name] = &fstest.MapFile{Data: raw}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"provenance.json", "schema/policy/1.0.0/policy.schema.json", "conformance/policy/1.0.0/vectors/manifest.json", "conformance/policy/1.0.0/examples/refunds/narrative.txt"} {
		t.Run(name, func(t *testing.T) {
			path := "official/" + name
			original := files[path]
			files[path] = &fstest.MapFile{Data: append(bytes.Clone(original.Data), '\n')}
			if err := verifyOfficialArtifacts(files); err == nil {
				t.Fatal("changed published bytes accepted")
			}
			delete(files, path)
			if err := verifyOfficialArtifacts(files); err == nil {
				t.Fatal("missing published bytes accepted")
			}
			files[path] = original
		})
	}
	files["official/unpublished.json"] = &fstest.MapFile{Data: []byte("{}")}
	if err := verifyOfficialArtifacts(files); err == nil {
		t.Fatal("unmanifested source accepted")
	}
}

func TestOfficialDraftPromotionPreservesRetainedInputs(t *testing.T) {
	for _, directory := range []string{"examples", "vectors/documents"} {
		if err := fs.WalkDir(candidate, "candidate/"+directory, func(name string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			relative := strings.TrimPrefix(name, "candidate/")
			if !bytes.Equal(candidateRead(t, relative), officialRead(t, "conformance/policy/1.0.0/"+relative)) {
				t.Errorf("promotion rewrote retained input %s", relative)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	core, err := artifacts.Load(artifacts.EvaluatorDraftVersion)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := core.Schema()
	if err != nil || !bytes.Equal(raw, officialRead(t, "schema/policy/1.0.0/consumer-core-0.2.0-draft.schema.json")) {
		t.Fatal("official frozen consumer schema differs from unchanged bundled Core")
	}
}

func TestOfficialDraftContractAndSchemaIdentities(t *testing.T) {
	r, exit := ContractFormat("contract", CandidateVersion)
	if exit != 0 || r.SpecRevision != DraftRevision || r.Specification != DraftURL || r.Profile != "jps-policy-companion" || r.Maturity != "draft" || !r.Experimental || r.Capabilities.FullDocumentConformance || r.Capabilities.ApprovalVerification || r.Capabilities.ExecutionAuthority || r.Capabilities.LiveAuthority || r.Capabilities.NetworkAcquisition {
		t.Fatal("incorrect official draft contract", r, exit)
	}
	if !bytes.Equal(r.CandidateCapabilities, officialRead(t, "conformance/policy/1.0.0/capabilities.json")) || r.CandidateResult != nil || r.Capabilities.ValidationScope != "closed-draft-document-and-local-references" || r.Capabilities.CheckScope != "draft-supplied-exact-bytes-anchors-and-scoped-core-references" {
		t.Fatal("incorrect capabilities or full-check scope")
	}
	for _, kind := range []string{"policy", "governing-adoption"} {
		schema, exit := Schema("schema", kind, CandidateVersion)
		if exit != 0 || schema.Resource == nil {
			t.Fatal(schema, exit)
		}
		upstreamID := draftSchemaPrefix + kind + ".schema.json"
		upstream := mustMap(t, officialRead(t, "schema/policy/1.0.0/"+kind+".schema.json"))
		derived := mustMap(t, []byte(schema.Resource.Text))
		if upstream["$id"] != upstreamID || schema.Resource.SourceID != upstreamID || derived["$id"] != "urn:judgmentpack:runtime:experimental-policy:"+DraftRelease+":"+kind+":authoring-schema:1" || strings.Contains(schema.Resource.Text, "urn:jps:proposal:") {
			t.Fatal("official and derived schema identities confused")
		}
	}
	// Historical reports and source pins stay distinct from the new release.
	legacy := mustMap(t, mustJSON(t, Contract("contract")))
	for _, key := range []string{"formatVersion", "candidateCapabilities", "candidateResult", "specRelease"} {
		if _, present := legacy[key]; present {
			t.Fatal("prototype report extended", key)
		}
	}
	for _, version := range []string{"1.0", "1.0.0-draft.1", "policy-v1.0.0-draft.1", "2.0.0"} {
		if _, exit := ContractFormat("contract", version); exit == 0 {
			t.Fatal("unsupported selector accepted", version)
		}
	}
	old := mustMap(t, candidateRead(t, "capabilities.json"))
	current := mustMap(t, r.CandidateCapabilities)
	if old["maturity"] != "proposed-draft" || current["maturity"] != "draft" || reflect.DeepEqual(old, current) {
		t.Fatal("historical candidate metadata was reclassified")
	}
}
