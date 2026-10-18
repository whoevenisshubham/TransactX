package demoenrich

import (
	"reflect"
	"testing"
)

func TestEnrichmentManifestIsStableScopedAndBounded(t *testing.T) {
	c := Config{ExpectedProject: "fixture-project", ExpectedDatabase: "fixture_demo"}
	a, b := Plan(c), Plan(c)
	if !reflect.DeepEqual(a, b) || len(a) != 12 {
		t.Fatal("manifest changed across resume")
	}
	keys := map[string]bool{}
	var amount int64
	for _, entry := range a {
		if keys[entry.Key] || entry.AmountPaise < 1 || entry.AmountPaise > 200 {
			t.Fatal("unbounded/duplicate manifest")
		}
		keys[entry.Key] = true
		amount += entry.AmountPaise
	}
	if amount != 1530 {
		t.Fatalf("total paise=%d", amount)
	}
	c.ExpectedProject = "different-project"
	if reflect.DeepEqual(a, Plan(c)) {
		t.Fatal("cross-project identities reused")
	}
	if !CanEnrich(28, 978, 12) || CanEnrich(29, 979, 12) || CanEnrich(0, 990, 1) {
		t.Fatal("visitor reserve was not respected")
	}
}
func TestEnrichmentTargetAndAcknowledgement(t *testing.T) {
	c := Config{DatabaseURL: "postgres://fixture:fixture@ep-fixture.neon.tech/fixture_demo?sslmode=verify-full", ExpectedDatabase: "fixture_demo", ExpectedHost: "ep-fixture.neon.tech", ExpectedProject: "fixture-project", APIOrigin: "https://api.example.test", SourceSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	if c.Validate() != nil {
		t.Fatal("valid read-only target rejected")
	}
	c.Apply = true
	if c.Validate() == nil {
		t.Fatal("apply accepted without ACK")
	}
	c.Ack = "ENRICH SYNTHETIC DEMO fixture-project/fixture_demo"
	if c.Validate() != nil {
		t.Fatal("exact acknowledgement rejected")
	}
	for _, bad := range []string{"postgres://u:p@127.0.0.1/source?sslmode=disable", "postgres://u:p@ep-other.neon.tech/fixture_demo?sslmode=require", "postgres://u:p@ep-fixture.neon.tech/postgres?sslmode=require", "postgres://u:p@ep-fixture-pooler.neon.tech/fixture_demo?sslmode=require"} {
		x := c
		x.DatabaseURL = bad
		if x.Validate() == nil {
			t.Fatal("protected/mismatched database accepted")
		}
	}
}
