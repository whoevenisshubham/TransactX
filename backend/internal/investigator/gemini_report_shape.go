package investigator

import (
	"encoding/json"
	"errors"
)

// Keep the public failure category stable while retaining a content-free local
// diagnosis. Neither provider strings nor model output enter this error.
type geminiResponseError struct{ reason string }

func (e geminiResponseError) Error() string { return "Gemini response rejected: " + e.reason }
func (e geminiResponseError) Unwrap() error { return ErrProviderMalformed }
func malformedGemini(reason string) error   { return geminiResponseError{reason} }

// DiagnoseGeminiResponse runs the exact canonical decoder without a provider
// request. It returns only fixed rejection codes, never text from the response.
func DiagnoseGeminiResponse(raw []byte, model string, inputLimit, outputLimit int64) string {
	_, err := decodeGeminiResponse(raw, model, inputLimit, outputLimit)
	if err == nil {
		return "valid"
	}
	var detail geminiResponseError
	if errors.As(err, &detail) {
		return detail.reason
	}
	if errors.Is(err, ErrProviderRefusal) {
		return "provider_refusal"
	}
	return "decode_failed"
}

func validateGeminiReportShape(raw []byte) string {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return "report_json"
	}
	assets, err := LoadPromptAssets()
	if err != nil {
		return "report_schema_unavailable"
	}
	return matchReportShape(value, assets.ReportSchema)
}

// This is the deliberately small structural subset used by the embedded report
// schema, not a general JSON Schema engine. Unsupported schema keywords fail
// closed. Semantic citations and financial assertions remain GroundGeneratedReport's job.
func matchReportShape(value any, schema map[string]any) string {
	for key := range schema {
		switch key {
		case "type", "properties", "required", "additionalProperties", "items", "enum":
		default:
			return "report_schema_unsupported"
		}
	}
	types := []any{schema["type"]}
	if union, ok := schema["type"].([]any); ok {
		types = union
	}
	typeOK := false
	for _, kind := range types {
		switch kind {
		case "null":
			typeOK = typeOK || value == nil
		case "string":
			_, ok := value.(string)
			typeOK = typeOK || ok
		case "boolean":
			_, ok := value.(bool)
			typeOK = typeOK || ok
		case "object":
			_, ok := value.(map[string]any)
			typeOK = typeOK || ok
		case "array":
			_, ok := value.([]any)
			typeOK = typeOK || ok
		default:
			return "report_schema_unsupported"
		}
	}
	if !typeOK {
		return "report_schema_type"
	}
	if choices, ok := schema["enum"].([]any); ok {
		matched := false
		for _, choice := range choices {
			if actual, ok := value.(string); ok && actual == choice {
				matched = true
			}
		}
		if !matched {
			return "report_schema_enum"
		}
	}
	if object, ok := value.(map[string]any); ok {
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			return "report_schema_unsupported"
		}
		if required, ok := schema["required"].([]any); ok {
			for _, field := range required {
				name, ok := field.(string)
				if !ok {
					return "report_schema_unsupported"
				}
				if _, exists := object[name]; !exists {
					return "report_required_fields"
				}
			}
		}
		for field, child := range object {
			property, exists := properties[field]
			if !exists {
				if schema["additionalProperties"] == false {
					return "report_schema_unknown_field"
				}
				continue
			}
			childSchema, ok := property.(map[string]any)
			if !ok {
				return "report_schema_unsupported"
			}
			if reason := matchReportShape(child, childSchema); reason != "" {
				return reason
			}
		}
	}
	if array, ok := value.([]any); ok {
		itemSchema, ok := schema["items"].(map[string]any)
		if !ok {
			return "report_schema_unsupported"
		}
		for _, child := range array {
			if reason := matchReportShape(child, itemSchema); reason != "" {
				return reason
			}
		}
	}
	return ""
}

// Emit only fixed local codes; never copy provider-controlled strings to logs.
func geminiFinishFailure(finish string) string {
	switch finish {
	case "MAX_TOKENS":
		return "finish_max_tokens"
	case "MALFORMED_FUNCTION_CALL":
		return "finish_malformed_function_call"
	case "UNEXPECTED_TOOL_CALL":
		return "finish_unexpected_tool_call"
	case "OTHER":
		return "finish_other"
	default:
		return "finish_not_stop"
	}
}
