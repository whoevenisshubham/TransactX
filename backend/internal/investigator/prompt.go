package investigator

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

const InvestigatorPromptVersion = "tx.investigator.prompt.v1"

//go:embed prompts/investigator_v1.txt prompts/investigation_report.schema.json
var promptFiles embed.FS

type PromptAssets struct {
	Version      string
	Prompt       string
	PromptSHA256 string
	ReportSchema map[string]any
}

func LoadPromptAssets() (PromptAssets, error) {
	prompt, err := promptFiles.ReadFile("prompts/investigator_v1.txt")
	if err != nil {
		return PromptAssets{}, err
	}
	schemaBytes, err := promptFiles.ReadFile("prompts/investigation_report.schema.json")
	if err != nil {
		return PromptAssets{}, err
	}
	var schema map[string]any
	if err := json.Unmarshal(schemaBytes, &schema); err != nil {
		return PromptAssets{}, fmt.Errorf("decode investigation report schema: %w", err)
	}
	digest := sha256.Sum256(prompt)
	return PromptAssets{
		Version: InvestigatorPromptVersion, Prompt: string(prompt),
		PromptSHA256: hex.EncodeToString(digest[:]), ReportSchema: schema,
	}, nil
}
