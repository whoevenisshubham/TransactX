package investigator

import (
	"encoding/json"
	"sort"
)

type geminiCitationBinding struct {
	EvidenceID string `json:"evidenceId"`
	EntityID   string `json:"entityId"`
}

// State the already-enforced citation/entity distinction in the request and
// narrow generated entity IDs to collected entities. Never coerce a response:
// the canonical grounding code still checks each exact evidence/entity/value pair.
func geminiCitationSchema(base map[string]any, evidence EvidenceBundle) (map[string]any, []geminiCitationBinding, error) {
	bindings := []geminiCitationBinding{{evidence.Payment.EvidenceId, evidence.PaymentId}}
	for _, op := range evidence.Operations {
		bindings = append(bindings, geminiCitationBinding{op.EvidenceId, op.OperationId})
	}
	seen := map[string]bool{}
	entities := []string{}
	for _, binding := range bindings {
		if binding.EvidenceID == "" || binding.EntityID == "" {
			return nil, nil, ErrProviderMalformed
		}
		if !seen[binding.EntityID] {
			entities = append(entities, binding.EntityID)
			seen[binding.EntityID] = true
		}
	}
	sort.Strings(entities)
	raw, err := json.Marshal(base)
	if err != nil {
		return nil, nil, ErrProviderMalformed
	}
	var schema map[string]any
	if json.Unmarshal(raw, &schema) != nil {
		return nil, nil, ErrProviderMalformed
	}
	node := schema
	for _, key := range []string{"properties", "facts", "items", "properties", "assertions", "items", "properties"} {
		child, ok := node[key].(map[string]any)
		if !ok {
			return nil, nil, ErrProviderMalformed
		}
		node = child
	}
	node["entityId"] = map[string]any{"type": "string", "enum": entities, "description": "Copy entityId from citationBindings for this evidenceId. An evidenceId is a citation, never an entityId."}
	return schema, bindings, nil
}
