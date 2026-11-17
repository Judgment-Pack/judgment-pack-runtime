package registry

import (
	"errors"
	"slices"
	"testing"
)

func TestSecretStrings(t *testing.T) {
	registry, kinds := fixture(t)
	result := run(registry, kinds)
	if !result.Report.Valid {
		t.Fatalf("fixture: %+v", result.Report.Diagnostics)
	}
	want := map[string][]SecretString{
		"dev":     {{Entry: EntryName{Kind: "db-role", ID: "billing-api"}, Pointer: "/password", Value: "secret:gcp-sm:projects/example-dev/secrets/billing-db/versions/3"}},
		"staging": {{Entry: EntryName{Kind: "db-role", ID: "billing-api"}, Pointer: "/password", Value: "secret:gcp-sm:projects/example-staging/secrets/billing-db/versions/5"}},
		"prod":    {{Entry: EntryName{Kind: "db-role", ID: "billing-api"}, Pointer: "/password", Value: "secret:gcp-sm:projects/example-prod/secrets/billing-db/versions/7"}},
	}
	for environment, expected := range want {
		got, err := result.SecretStrings(environment, nil)
		if err != nil || !slices.Equal(got, expected) {
			t.Fatalf("%s: %+v %v", environment, got, err)
		}
	}
	// A kind the filter leaves out has no secret strings.
	if got, err := result.SecretStrings("dev", func(kind string) bool { return kind != "db-role" }); err != nil || len(got) != 0 {
		t.Fatalf("filtered: %+v %v", got, err)
	}
	if _, err := result.SecretStrings("qa", nil); err == nil {
		t.Fatal("an unknown environment was read")
	}
	invalid := run(`{}`, kinds)
	if _, err := invalid.SecretStrings("dev", nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid: %v", err)
	}
}
