package investigator

import (
	"encoding/json"
	"testing"
)

func TestGeminiRequestSeparatesEntityAndEvidenceIDs(t *testing.T) {
	e := loadFixture(t, "healthy.json").Evidence
	assets, err := LoadPromptAssets()
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(assets.ReportSchema)
	schema, bindings, err := geminiCitationSchema(assets.ReportSchema, e)
	if err != nil {
		t.Fatal(err)
	}
	if bindings[0].EvidenceID != e.Payment.EvidenceId || bindings[0].EntityID != e.PaymentId || bindings[0].EntityID == bindings[0].EvidenceID {
		t.Fatal("ambiguous payment assertion binding")
	}
	if len(bindings) != 1+len(e.Operations) {
		t.Fatal("missing collected operation bindings")
	}
	node := schema
	for _, key := range []string{"properties", "facts", "items", "properties", "assertions", "items", "properties", "entityId"} {
		node = node[key].(map[string]any)
	}
	entities := node["enum"].([]string)
	found := false
	for _, id := range entities {
		if id == e.PaymentId {
			found = true
		}
		for _, binding := range bindings {
			if id == binding.EvidenceID {
				t.Fatal("citation ID admitted as an entity")
			}
		}
	}
	if !found || node["type"] != "string" {
		t.Fatal("scoped entity restriction missing")
	}
	props := schema["properties"].(map[string]any)
	for name, maximum := range map[string]int{"facts": 3, "inferences": 1, "recommendations": 1} {
		if props[name].(map[string]any)["maxItems"] != maximum {
			t.Fatalf("unbounded response section %s", name)
		}
	}
	factProps := props["facts"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
	for _, field := range []string{"assertions", "evidenceIds"} {
		if factProps[field].(map[string]any)["maxItems"] != 1 {
			t.Fatalf("unbounded fact %s", field)
		}
	}
	after, _ := json.Marshal(assets.ReportSchema)
	if string(before) != string(after) {
		t.Fatal("shared canonical schema mutated")
	}
	// A later request with another trusted snapshot cannot inherit previous IDs.
	e.PaymentId = "other-payment"
	_, next, err := geminiCitationSchema(assets.ReportSchema, e)
	if err != nil || next[0].EntityID != "other-payment" {
		t.Fatal("per-request scope retained stale identity")
	}
}
