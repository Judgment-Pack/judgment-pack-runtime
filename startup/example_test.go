package startup_test

import (
	"context"
	"encoding/json"
	"log"
	"log/slog"

	"github.com/Judgment-Pack/judgment-pack-runtime/startup"
)

// A service reads its environment's resolved registry at start, then the
// secret its database role refers to, from where the platform delivered it.
// The bindings name the variable or file the deployment configured for each
// pinned version; nothing here contacts a secret store.
func Example() {
	resolved, err := startup.ReadResolvedFile("registry/resolved/prod.json", "prod")
	if err != nil {
		log.Fatal(err)
	}
	role, ok := resolved.Entry("db-role", "billing-api")
	if !ok {
		log.Fatal("no db-role billing-api in prod")
	}
	var value struct {
		Password string `json:"password"` // a secret reference, not a value
	}
	if err := json.Unmarshal(role.Value, &value); err != nil {
		log.Fatal(err)
	}
	secrets := startup.Bindings{
		Env: map[string]string{
			"secret:gcp-sm:projects/example-prod/secrets/billing-db/versions/7": "BILLING_DB_PASSWORD",
		},
		Files: map[string]string{
			"secret:gcp-sm:projects/example-prod/secrets/billing-api-key/versions/2": "/run/secrets/billing-api-key",
		},
	}
	password, err := secrets.ReadSecret(context.Background(), value.Password)
	if err != nil {
		log.Fatal(err)
	}
	slog.Info("connecting", "role", role.ID, "password", password) // logs password=[secret]
	connect(role.ID, password.Reveal())                            // the one place the bytes are reached
}

// connect stands for the service's own client.
func connect(user string, password []byte) {}
