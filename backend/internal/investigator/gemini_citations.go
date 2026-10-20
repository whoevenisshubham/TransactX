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

	// Request a bounded selection rather than enumerating the entire snapshot.
	// This is a request-only narrowing; canonical shape and grounding stay strict.
	properties := schema["properties"].(map[string]any)
	for name, maximum := range map[string]int{"facts": 3, "inferences": 1, "recommendations": 1} {
		properties[name].(map[string]any)["maxItems"] = maximum
	}
	facts := properties["facts"].(map[string]any)
	facts["description"] = "Only the most relevant directly grounded facts; do not enumerate intermediate operations. Keep prose brief."
	factFields := facts["items"].(map[string]any)["properties"].(map[string]any)
	factFields["assertions"].(map[string]any)["maxItems"] = 1
	factFields["evidenceIds"].(map[string]any)["maxItems"] = 1
	properties["inferences"].(map[string]any)["description"] = "Use an empty array unless an inference is needed to answer the question."
	properties["recommendations"].(map[string]any)["description"] = "Use an empty array for a settled payment unless the question calls for a further check."
	return schema, bindings, nil
}
