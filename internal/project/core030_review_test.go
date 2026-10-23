package project

import (
	"errors"
	"strings"
	"testing"
)

func TestCore030ReviewProjectPreflightPrecedesRowsAndMatrixReads(t *testing.T) {
	legacy := strings.Replace(core030Pack, `"specVersion":"0.3.0"`, `"specVersion":"0.2.0-draft"`, 1)
	legacy = strings.Replace(legacy, `"routes":[{"target":{"kind":"queue","name":"Z"}},{"target":{"kind":"system","name":"A"}},{"target":{"kind":"queue","name":"Z"}}]`, `"target":{"kind":"queue","name":"Z"}`, 1)
	extended := `{"specVersion":"0.3.0","profile":"extended"}`
	for _, test := range []struct{ name, second, want string }{
		{"mixed", legacy, "JPS-MATRIX-MIXED-CONTRACT"},
		{"later unsupported profile", extended, "profile-unsupported"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := writeProject(t, `{"configVersion":"1","packs":{"a":{"path":"a.json","matrix":"a.matrix"},"b":{"path":"b.json","matrix":"missing.matrix"}}}`, map[string]string{"a.json": core030Pack, "b.json": test.second, "a.matrix": core030Matrix})
			project, failure := Load(path)
			if failure != nil {
				t.Fatal(failure)
			}
			defer project.Close()
			// One ordinary row would exceed this injected report budget. A nil engine
			// also makes any attempt to evaluate before the refusal fail the test.
			project.dispositionReportBudget = 1
			output, failure := project.Test(nil, "", "test")
			if failure == nil || failure.Code != test.want || len(output.Packs) != 0 {
				t.Fatalf("preflight: %+v, %+v", output, failure)
			}
			if test.want == "profile-unsupported" && (failure.Profile != "extended" || failure.Message != "") {
				t.Fatal(failure)
			}
		})
	}
}

func TestCore030ReviewProjectProfilePrecedesBrokenMatrix(t *testing.T) {
	pack := `{"specVersion":"0.3.0","profile":"extended"}`
	path := writeProject(t, `{"configVersion":"1","packs":{"a":{"path":"pack.json","matrix":"missing.matrix"}}}`, map[string]string{"pack.json": pack})
	project, failure := Load(path)
	if failure != nil {
		t.Fatal(failure)
	}
	defer project.Close()
	_, failure = project.Test(nil, "", "test")
	if failure == nil || failure.Code != "profile-unsupported" || failure.Profile != "extended" || failure.Message != "" {
		t.Fatal(failure)
	}
	_, err := TestSnapshot(nil, []byte(pack), []byte("{"), "test")
	if !errors.As(err, &failure) || failure.Code != "profile-unsupported" || failure.Profile != "extended" || failure.Message != "" {
		t.Fatal(err)
	}
	// The complete bounded root scan must still refuse malformed JSON rather
	// than accepting an unsupported-profile prefix.
	_, err = TestSnapshot(nil, []byte(strings.TrimSuffix(pack, "}")), []byte("{"), "test")
	if err == nil || errors.As(err, &failure) {
		t.Fatalf("malformed pack became profile refusal: %v", err)
	}
}

func TestCore030ReviewProjectExecutionReadEnforcesPreflightContract(t *testing.T) {
	run := &matrixRun{}
	if _, failure := run.selectPackContract([]byte(core030Pack)); failure != nil {
		t.Fatal(failure)
	}
	legacy := strings.Replace(core030Pack, `"specVersion":"0.3.0"`, `"specVersion":"0.2.0-draft"`, 1)
	legacy = strings.Replace(legacy, `"routes":[{"target":{"kind":"queue","name":"Z"}},{"target":{"kind":"system","name":"A"}},{"target":{"kind":"queue","name":"Z"}}]`, `"target":{"kind":"queue","name":"Z"}`, 1)
	if _, failure := run.selectPackContract([]byte(legacy)); failure == nil || failure.Code != "JPS-MATRIX-MIXED-CONTRACT" {
		t.Fatal("changed read escaped preflight", failure)
	}
	if run.contract.Version() != "0.3.0" {
		t.Fatal("changed read replaced selected contract")
	}
}
