package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// ExecutionTargetConfig defines an explicit execution target and its associated health and execution endpoints.
// Fields:
//   candidateID: unique candidate route identity (e.g. "direct", "rail-b")
//   executionTargetID: switch-level execution route/rail identity (e.g. "RAIL-A", "RAIL-B")
//   sourceBank: logical source bank code (e.g. "BANK-A")
//   destinationBank: logical destination bank code (e.g. "BANK-B")
//   endpoint: required health probe endpoint associated with ExecutionTargetID
//   sourceEndpoint: optional source execution endpoint (falls back to source bank default adapter)
//   destinationEndpoint: optional destination execution endpoint (falls back to destination bank default adapter)
type ExecutionTargetConfig struct {
	CandidateID         string `json:"candidateId"`
	ExecutionTargetID   string `json:"executionTargetId"`
	SourceBank          string `json:"sourceBank"`
	DestinationBank     string `json:"destinationBank"`
	Endpoint            string `json:"endpoint"`
	SourceEndpoint      string `json:"sourceEndpoint,omitempty"`
	DestinationEndpoint string `json:"destinationEndpoint,omitempty"`
}

func (c *ExecutionTargetConfig) UnmarshalJSON(data []byte) error {
	type alias struct {
		CandidateIDCamel string `json:"candidateId"`
		CandidateIDSnake string `json:"candidate_id"`
		TargetIDCamel    string `json:"executionTargetId"`
		TargetIDSnake    string `json:"execution_target_id"`
		SrcBankCamel     string `json:"sourceBank"`
		SrcBankSnake     string `json:"source_bank"`
		DstBankCamel     string `json:"destinationBank"`
		DstBankSnake     string `json:"destination_bank"`
		Endpoint         string `json:"endpoint"`
		SrcEndpointCamel string `json:"sourceEndpoint"`
		SrcEndpointSnake string `json:"source_endpoint"`
		DstEndpointCamel string `json:"destinationEndpoint"`
		DstEndpointSnake string `json:"destination_endpoint"`
	}
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	c.CandidateID = firstNonEmpty(a.CandidateIDCamel, a.CandidateIDSnake)
	c.ExecutionTargetID = firstNonEmpty(a.TargetIDCamel, a.TargetIDSnake)
	c.SourceBank = firstNonEmpty(a.SrcBankCamel, a.SrcBankSnake)
	c.DestinationBank = firstNonEmpty(a.DstBankCamel, a.DstBankSnake)
	c.Endpoint = a.Endpoint
	c.SourceEndpoint = firstNonEmpty(a.SrcEndpointCamel, a.SrcEndpointSnake)
	c.DestinationEndpoint = firstNonEmpty(a.DstEndpointCamel, a.DstEndpointSnake)
	return nil
}

type Config struct {
	Address               string
	DatabaseURL           string
	JWTSecret             string
	JWTIssuer             string
	JWTLifetime           time.Duration
	DefaultBankCode       string
	DevelopmentMode       bool
	RoutingMode           string
	RoutingStaticBaseline string
	ExecutionTargets      []ExecutionTargetConfig
}

func Load() (Config, error) {
	developmentMode, err := strconv.ParseBool(getEnv("APP_DEVELOPMENT", "false"))
	if err != nil {
		return Config{}, fmt.Errorf("APP_DEVELOPMENT must be true or false: %w", err)
	}

	jwtLifetime, err := time.ParseDuration(getEnv("JWT_LIFETIME", "15m"))
	if err != nil || jwtLifetime <= 0 {
		return Config{}, errors.New("JWT_LIFETIME must be a positive duration")
	}

	routingMode := strings.ToUpper(strings.TrimSpace(getEnv("ROUTING_MODE", "ADAPTIVE")))
	if routingMode != "STATIC" && routingMode != "ADAPTIVE" {
		return Config{}, fmt.Errorf("ROUTING_MODE must be STATIC or ADAPTIVE: %q", routingMode)
	}

	routingStaticBaseline := strings.TrimSpace(os.Getenv("ROUTING_STATIC_BASELINE"))

	executionTargets, err := ParseExecutionTargetConfig(os.Getenv("ROUTING_TARGETS"))
	if err != nil {
		return Config{}, fmt.Errorf("ROUTING_TARGETS invalid: %w", err)
	}

	cfg := Config{
		Address:               getEnv("APP_ADDR", ":8080"),
		DatabaseURL:           getEnv("DATABASE_URL", ""),
		JWTSecret:             os.Getenv("JWT_SECRET"),
		JWTIssuer:             getEnv("JWT_ISSUER", "transactx-api"),
		JWTLifetime:           jwtLifetime,
		DefaultBankCode:       getEnv("DEFAULT_BANK_CODE", "BANK-DEV-001"),
		DevelopmentMode:       developmentMode,
		RoutingMode:           routingMode,
		RoutingStaticBaseline: routingStaticBaseline,
		ExecutionTargets:      executionTargets,
	}
	if !developmentMode && len([]byte(cfg.JWTSecret)) < 32 {
		return Config{}, errors.New("JWT_SECRET must contain at least 32 bytes outside development mode")
	}
	return cfg, nil
}

func ParseExecutionTargetConfig(raw string) ([]ExecutionTargetConfig, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}

	if strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "{") {
		if strings.HasPrefix(trimmed, "{") {
			var single ExecutionTargetConfig
			if err := json.Unmarshal([]byte(trimmed), &single); err != nil {
				return nil, fmt.Errorf("failed to parse execution target JSON object: %w", err)
			}
			if err := validateExecutionTargetConfig(single); err != nil {
				return nil, err
			}
			return []ExecutionTargetConfig{single}, nil
		}
		var list []ExecutionTargetConfig
		if err := json.Unmarshal([]byte(trimmed), &list); err != nil {
			return nil, fmt.Errorf("failed to parse execution targets JSON array: %w", err)
		}
		if len(list) == 0 {
			return nil, errors.New("execution targets JSON array cannot be empty")
		}
		for i, item := range list {
			if err := validateExecutionTargetConfig(item); err != nil {
				return nil, fmt.Errorf("target[%d] invalid: %w", i, err)
			}
		}
		return list, nil
	}

	// Delimited format: entries separated by ';'
	entries := strings.Split(trimmed, ";")
	result := make([]ExecutionTargetConfig, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		var target ExecutionTargetConfig
		if strings.Contains(entry, "|") {
			parts := strings.Split(entry, "|")
			if len(parts) < 5 {
				return nil, fmt.Errorf("malformed delimited target config (expected at least 5 pipe-separated fields): %q", entry)
			}
			target.CandidateID = strings.TrimSpace(parts[0])
			target.ExecutionTargetID = strings.TrimSpace(parts[1])
			target.SourceBank = strings.TrimSpace(parts[2])
			target.DestinationBank = strings.TrimSpace(parts[3])
			target.Endpoint = strings.TrimSpace(parts[4])
			if len(parts) >= 6 {
				target.SourceEndpoint = strings.TrimSpace(parts[5])
			}
			if len(parts) >= 7 {
				target.DestinationEndpoint = strings.TrimSpace(parts[6])
			}
		} else {
			parts := strings.Split(entry, ":")
			if len(parts) < 5 {
				return nil, fmt.Errorf("malformed delimited target config (expected candidate:target:sourceBank:destBank:endpoint): %q", entry)
			}
			target.CandidateID = strings.TrimSpace(parts[0])
			target.ExecutionTargetID = strings.TrimSpace(parts[1])
			target.SourceBank = strings.TrimSpace(parts[2])
			target.DestinationBank = strings.TrimSpace(parts[3])
			target.Endpoint = strings.TrimSpace(strings.Join(parts[4:], ":"))
		}
		if err := validateExecutionTargetConfig(target); err != nil {
			return nil, err
		}
		result = append(result, target)
	}
	if len(result) == 0 {
		return nil, errors.New("no valid execution targets found in configuration")
	}
	return result, nil
}

func validateExecutionTargetConfig(item ExecutionTargetConfig) error {
	if strings.TrimSpace(item.CandidateID) == "" {
		return errors.New("candidate_id is required")
	}
	if strings.TrimSpace(item.ExecutionTargetID) == "" {
		return errors.New("execution_target_id is required")
	}
	if strings.TrimSpace(item.SourceBank) == "" {
		return errors.New("source_bank is required")
	}
	if strings.TrimSpace(item.DestinationBank) == "" {
		return errors.New("destination_bank is required")
	}
	endpoint := strings.TrimSpace(item.Endpoint)
	if endpoint == "" {
		return errors.New("endpoint (health target endpoint) is required")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("invalid endpoint URL: %q", endpoint)
	}

	sourceEndpoint := strings.TrimSpace(item.SourceEndpoint)
	if sourceEndpoint != "" {
		su, err := url.Parse(sourceEndpoint)
		if err != nil || su.Scheme == "" || su.Host == "" {
			return fmt.Errorf("invalid source_endpoint URL: %q", sourceEndpoint)
		}
	}

	destEndpoint := strings.TrimSpace(item.DestinationEndpoint)
	if destEndpoint != "" {
		du, err := url.Parse(destEndpoint)
		if err != nil || du.Scheme == "" || du.Host == "" {
			return fmt.Errorf("invalid destination_endpoint URL: %q", destEndpoint)
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
