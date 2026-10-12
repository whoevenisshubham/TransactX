package investigator

import (
	"encoding/json"
	"fmt"
)

// NormalizeProviderToolCall decodes the strict provider-facing schema and
// injects the selected payment from trusted server state. Provider arguments
// never contain actor, role, payment scope, SQL, URL, shell or mutation fields.
func NormalizeProviderToolCall(call ProviderToolCall, selectedPaymentID string) (ToolCall, error) {
	if call.CallID == "" || selectedPaymentID == "" {
		return ToolCall{}, ErrInvalidToolArguments
	}
	switch call.Name {
	case ToolReadPaymentEvidence:
		var arguments struct{}
		if err := decodeToolArguments(call.Arguments, &arguments); err != nil {
			return ToolCall{}, err
		}
		return ToolCall{Name: call.Name, Arguments: mustJSON(struct {
			PaymentId string `json:"paymentId"`
		}{selectedPaymentID})}, nil
	case ToolReadRecoveryAttempts:
		var arguments struct {
			Limit int `json:"limit"`
		}
		if err := decodeToolArguments(call.Arguments, &arguments); err != nil {
			return ToolCall{}, err
		}
		return ToolCall{Name: call.Name, Arguments: mustJSON(struct {
			PaymentId string `json:"paymentId"`
			Limit     int    `json:"limit"`
		}{selectedPaymentID, arguments.Limit})}, nil
	case ToolReadRelatedReconciliation:
		var arguments struct {
			RunId string `json:"runId"`
		}
		if err := decodeToolArguments(call.Arguments, &arguments); err != nil {
			return ToolCall{}, err
		}
		return ToolCall{Name: call.Name, Arguments: mustJSON(struct {
			PaymentId string `json:"paymentId"`
			RunId     string `json:"runId"`
		}{selectedPaymentID, arguments.RunId})}, nil
	case ToolReadTraceContext, ToolReadOperationalWindow, ToolRetrieveRunbook:
		var value map[string]json.RawMessage
		if err := json.Unmarshal(call.Arguments, &value); err != nil {
			return ToolCall{}, fmt.Errorf("%w: %v", ErrInvalidToolArguments, err)
		}
		// Registry strict decoding remains the final schema check.
		return ToolCall{Name: call.Name, Arguments: append(json.RawMessage(nil), call.Arguments...)}, nil
	default:
		return ToolCall{}, ErrUnknownTool
	}
}
