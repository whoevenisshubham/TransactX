package operations

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// TestSchemaCompliance_AgainstV2Schema verifies that our Go DTO structs define
// all required JSON fields specified in the shared contract schema v2.schema.json.
func TestSchemaCompliance_AgainstV2Schema(t *testing.T) {
	schemaPaths := []string{
		"../../../docs/next-gen/execution-pack/docs/next-gen/contracts/v2.schema.json",
		"../../../../TransactX_Execution_Pack/TransactX_Execution_Pack/docs/next-gen/contracts/v2.schema.json",
		"../../docs/next-gen/contracts/v2.schema.json",
	}

	var schemaData []byte
	var err error
	for _, p := range schemaPaths {
		schemaData, err = os.ReadFile(p)
		if err == nil {
			break
		}
	}
	if err != nil {
		t.Skip("v2.schema.json not accessible from test path, skipping schema contract test")
	}

	var schemaRoot struct {
		Defs map[string]struct {
			Properties map[string]any `json:"properties"`
			Required   []string       `json:"required"`
		} `json:"$defs"`
	}

	if err := json.Unmarshal(schemaData, &schemaRoot); err != nil {
		t.Fatalf("failed to unmarshal v2.schema.json: %v", err)
	}

	typeMapping := map[string]any{
		"Worklist":              Worklist{},
		"WorklistItem":          WorklistItem{},
		"EvidenceBundle":        EvidenceBundle{},
		"PaymentSnapshot":       PaymentSnapshot{},
		"Route":                 RouteSnapshot{},
		"Operation":             OperationSnapshot{},
		"Transition":            TransitionSnapshot{},
		"RecoverySummary":       RecoverySnapshot{},
		"RecoveryAttemptsPage":  RecoveryAttemptsPage{},
		"RecoveryAttempt":       RecoveryAttemptItem{},
		"RecoveryRecheckResult": RecoveryRecheckResult{},
	}

	for defName, dtoInstance := range typeMapping {
		def, ok := schemaRoot.Defs[defName]
		if !ok {
			t.Errorf("definition %s missing from schema $defs", defName)
			continue
		}

		dtoFields := getJSONTags(reflect.TypeOf(dtoInstance))

		// Verify every required field in schema is present in DTO
		for _, req := range def.Required {
			if !dtoFields[req] {
				t.Errorf("DTO for %s missing required schema field: %q", defName, req)
			}
		}

		// Verify every property in schema is present in DTO
		for prop := range def.Properties {
			if !dtoFields[prop] {
				t.Errorf("DTO for %s missing schema property: %q", defName, prop)
			}
		}
	}
}

func getJSONTags(t reflect.Type) map[string]bool {
	tags := make(map[string]bool)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("json")
		if tag != "" && tag != "-" {
			parts := strings.Split(tag, ",")
			tags[parts[0]] = true
		}
	}
	return tags
}
