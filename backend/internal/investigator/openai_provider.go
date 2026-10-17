package investigator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	defaultOpenAIResponsesEndpoint = "https://api.openai.com/v1/responses"
	maxProviderResponseBytes       = 1 << 20
	maxProviderToolArguments       = 16 << 10
)

var (
	ErrProviderRefusal     = errors.New("provider refused the investigation")
	ErrProviderRateLimited = errors.New("provider rate limited the investigation")
	ErrProviderMalformed   = errors.New("provider returned malformed output")
	ErrProviderUnavailable = errors.New("provider is unavailable")
	providerModelPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
)

type OpenAIProviderConfig struct {
	Endpoint string
	APIKey   string
	Model    string
	Client   *http.Client
}

type OpenAIProvider struct {
	endpoint   string
	apiKey     string
	sendAPIKey bool
	model      string
	client     *http.Client
	assets     PromptAssets
}

func NewOpenAIProvider(config OpenAIProviderConfig) (*OpenAIProvider, error) {
	if config.Endpoint == "" {
		config.Endpoint = defaultOpenAIResponsesEndpoint
	}
	if !providerModelPattern.MatchString(config.Model) {
		return nil, errors.New("invalid provider model identifier")
	}
	parsed, err := url.Parse(config.Endpoint)
	if err != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return nil, errors.New("invalid provider endpoint")
	}
	isOpenAI := parsed.Scheme == "https" && parsed.Host == "api.openai.com" && parsed.Path == "/v1/responses"
	host := parsed.Hostname()
	isLocal := parsed.Scheme == "http" && (host == "127.0.0.1" || host == "localhost" || host == "::1")
	if !isOpenAI && !isLocal {
		return nil, errors.New("provider endpoint must be the OpenAI Responses API or explicit loopback HTTP")
	}
	if isOpenAI && strings.TrimSpace(config.APIKey) == "" {
		return nil, errors.New("OpenAI API key is required")
	}
	if config.Client == nil {
		config.Client = &http.Client{Timeout: 25 * time.Second}
	}
	assets, err := LoadPromptAssets()
	if err != nil {
		return nil, err
	}
	return &OpenAIProvider{endpoint: config.Endpoint, apiKey: config.APIKey, sendAPIKey: isOpenAI, model: config.Model, client: config.Client, assets: assets}, nil
}

func (provider *OpenAIProvider) Generate(ctx context.Context, request GenerationRequest) (GenerationResult, error) {
	if request.PromptVersion != provider.assets.Version || request.Evidence.PaymentId == "" || strings.TrimSpace(request.Question) == "" {
		return GenerationResult{}, errors.New("invalid generation request")
	}
	if request.InputTokenLimit < 1 || request.InputTokenLimit > 8000 || request.OutputTokenLimit < 1 || request.OutputTokenLimit > 1200 || request.ToolRoundsRemaining < 0 || request.ToolRoundsRemaining > 2 {
		return GenerationResult{}, errors.New("generation bounds exceed configured limits")
	}
	body, err := provider.requestBody(request)
	if err != nil {
		return GenerationResult{}, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.endpoint, bytes.NewReader(body))
	if err != nil {
		return GenerationResult{}, ErrProviderUnavailable
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	if provider.sendAPIKey {
		httpRequest.Header.Set("Authorization", "Bearer "+provider.apiKey)
	}
	response, err := provider.client.Do(httpRequest)
	if err != nil {
		if ctx.Err() != nil {
			return GenerationResult{}, ctx.Err()
		}
		return GenerationResult{}, ErrProviderUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusTooManyRequests {
		return GenerationResult{}, ErrProviderRateLimited
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return GenerationResult{}, ErrProviderUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxProviderResponseBytes+1))
	if err != nil || len(raw) > maxProviderResponseBytes {
		return GenerationResult{}, ErrProviderMalformed
	}
	return decodeOpenAIResponse(raw)
}

func (provider *OpenAIProvider) RequiresCostReservation() bool { return provider.sendAPIKey }

func (provider *OpenAIProvider) requestBody(request GenerationRequest) ([]byte, error) {
	packet := struct {
		Question      string         `json:"question"`
		PromptVersion string         `json:"promptVersion"`
		Evidence      EvidenceBundle `json:"evidence"`
		Runbooks      []RunbookChunk `json:"runbooks"`
	}{request.Question, request.PromptVersion, request.Evidence, request.Chunks}
	packetJSON, err := json.Marshal(packet)
	if err != nil {
		return nil, err
	}
	input := []any{map[string]any{
		"role":    "user",
		"content": []any{map[string]any{"type": "input_text", "text": string(packetJSON)}},
	}}
	for _, item := range request.Continuation {
		if len(item) == 0 || len(item) > maxProviderResponseBytes || !json.Valid(item) {
			return nil, errors.New("invalid provider continuation")
		}
		input = append(input, item)
	}
	for _, output := range request.ToolOutputs {
		if output.CallID == "" || output.Name == "" || len(output.Output) == 0 || len(output.Output) > 64<<10 || !json.Valid(output.Output) {
			return nil, errors.New("invalid provider tool output")
		}
		input = append(input, map[string]any{
			"type": "function_call_output", "call_id": output.CallID, "output": string(output.Output),
		})
	}
	estimatedBytes := len(provider.assets.Prompt) + len(packetJSON)
	for _, item := range request.Continuation {
		estimatedBytes += len(item)
	}
	for _, output := range request.ToolOutputs {
		estimatedBytes += len(output.Output) + len(output.CallID) + len(output.Name)
	}
	// Exact provider tokenization is model-specific. Two UTF-8 bytes per token
	// is a deliberately conservative request-side estimate for this bounded,
	// mostly-JSON packet; the durable reservation uses the full configured cap.
	if (estimatedBytes+1)/2 > request.InputTokenLimit {
		return nil, errors.New("generation packet exceeds conservative input token limit")
	}
	body := map[string]any{
		"model":               provider.model,
		"instructions":        provider.assets.Prompt,
		"input":               input,
		"max_output_tokens":   request.OutputTokenLimit,
		"parallel_tool_calls": false,
		"store":               false,
		"text": map[string]any{"format": map[string]any{
			"type": "json_schema", "name": "transactx_investigation_report", "strict": true, "schema": provider.assets.ReportSchema,
		}},
	}
	if request.ToolRoundsRemaining > 0 {
		body["tools"] = providerToolDefinitions()
		body["tool_choice"] = "auto"
	} else {
		body["tool_choice"] = "none"
	}
	return json.Marshal(body)
}

func decodeOpenAIResponse(raw []byte) (GenerationResult, error) {
	var response struct {
		ID     string            `json:"id"`
		Model  string            `json:"model"`
		Status string            `json:"status"`
		Output []json.RawMessage `json:"output"`
		Usage  struct {
			InputTokens  *int64 `json:"input_tokens"`
			OutputTokens *int64 `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &response); err != nil || response.ID == "" || response.Status != "completed" || len(response.Output) == 0 {
		return GenerationResult{}, ErrProviderMalformed
	}
	result := GenerationResult{
		ProviderRequestID: response.ID,
		ProviderModel:     response.Model,
		Usage:             Usage{InputTokens: response.Usage.InputTokens, OutputTokens: response.Usage.OutputTokens},
		Continuation:      append([]json.RawMessage(nil), response.Output...),
	}
	var outputText string
	for _, item := range response.Output {
		var kind struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(item, &kind); err != nil {
			return GenerationResult{}, ErrProviderMalformed
		}
		switch kind.Type {
		case "function_call":
			var call struct {
				CallID    string `json:"call_id"`
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}
			if err := json.Unmarshal(item, &call); err != nil || call.CallID == "" || call.Name == "" || len(call.Arguments) > maxProviderToolArguments || !json.Valid([]byte(call.Arguments)) {
				return GenerationResult{}, ErrProviderMalformed
			}
			result.ToolCalls = append(result.ToolCalls, ProviderToolCall{CallID: call.CallID, Name: call.Name, Arguments: json.RawMessage(call.Arguments)})
		case "message":
			var message struct {
				Content []struct {
					Type    string `json:"type"`
					Text    string `json:"text"`
					Refusal string `json:"refusal"`
				} `json:"content"`
			}
			if err := json.Unmarshal(item, &message); err != nil {
				return GenerationResult{}, ErrProviderMalformed
			}
			for _, content := range message.Content {
				if content.Type == "refusal" || content.Refusal != "" {
					return GenerationResult{}, ErrProviderRefusal
				}
				if content.Type == "output_text" {
					if outputText != "" {
						return GenerationResult{}, ErrProviderMalformed
					}
					outputText = content.Text
				}
			}
		}
	}
	if len(result.ToolCalls) > 0 {
		if outputText != "" || len(result.ToolCalls) > 8 {
			return GenerationResult{}, ErrProviderMalformed
		}
		return result, nil
	}
	if outputText == "" {
		return GenerationResult{}, ErrProviderMalformed
	}
	decoder := json.NewDecoder(strings.NewReader(outputText))
	decoder.DisallowUnknownFields()
	var report InvestigationReport
	if err := decoder.Decode(&report); err != nil {
		return GenerationResult{}, ErrProviderMalformed
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return GenerationResult{}, ErrProviderMalformed
	}
	result.Report = &report
	return result, nil
}

func providerToolDefinitions() []map[string]any {
	object := func(properties map[string]any, required ...string) map[string]any {
		return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
	}
	function := func(name, description string, parameters map[string]any) map[string]any {
		return map[string]any{"type": "function", "name": name, "description": description, "strict": true, "parameters": parameters}
	}
	return []map[string]any{
		function(ToolReadPaymentEvidence, "Re-read the already-authorized selected payment evidence.", object(map[string]any{})),
		function(ToolReadRecoveryAttempts, "Read at most 20 recovery attempts for the selected payment.", object(map[string]any{"limit": map[string]any{"type": "integer"}}, "limit")),
		function(ToolReadRelatedReconciliation, "Read a reconciliation run ID already listed in selected evidence.", object(map[string]any{"runId": map[string]any{"type": "string"}}, "runId")),
		function(ToolReadTraceContext, "Read a trace ID already listed in selected evidence.", object(map[string]any{"traceId": map[string]any{"type": "string"}}, "traceId")),
		function(ToolReadOperationalWindow, "Read a bounded completed window for a bank or execution target in selected evidence.", object(map[string]any{
			"entityType": map[string]any{"type": "string", "enum": []string{"bank", "execution_target"}},
			"entityId":   map[string]any{"type": "string"}, "from": map[string]any{"type": "string"},
			"to": map[string]any{"type": "string"}, "limit": map[string]any{"type": "integer"},
		}, "entityType", "entityId", "from", "to", "limit")),
		function(ToolRetrieveRunbook, "Retrieve approved runbook chunks with a bounded lexical query.", object(map[string]any{
			"query": map[string]any{"type": "string"}, "tags": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"topK": map[string]any{"type": "integer"},
		}, "query", "tags", "topK")),
	}
}

var _ Provider = (*OpenAIProvider)(nil)
