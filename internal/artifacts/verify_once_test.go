package artifacts

import "testing"

// Each bundle is verified once per process (#357), and each Load still hands
// out a Set of the caller's own: what one caller does to its lock's file list
// is not what the next Load returns.
func TestEachBundleIsVerifiedOnceAndEachSetIsTheCallers(t *testing.T) {
	for _, version := range SupportedVersions() {
		first, err := Load(version)
		if err != nil {
			t.Fatalf("%s: %v", version, err)
		}
		before := verifications.Load()
		second, err := Load(version)
		if err != nil {
			t.Fatalf("%s: %v", version, err)
		}
		if verified := verifications.Load() - before; verified != 0 {
			t.Fatalf("%s: verified %d more times", version, verified)
		}
		want := second.Lock().Files[0].SHA256
		first.Lock().Files[0].SHA256 = "changed"
		third, err := Load(version)
		if err != nil {
			t.Fatalf("%s: %v", version, err)
		}
		if second.Lock().Files[0].SHA256 != want || third.Lock().Files[0].SHA256 != want {
			t.Fatalf("%s: one Set's lock changed another's", version)
		}
	}
	if _, err := Load("9.9.9"); err == nil {
		t.Fatal("an unsupported version is still refused")
	}
}
