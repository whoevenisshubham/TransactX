package chaos

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

type ScenarioType string

const (
	ScenarioTypeBankOutage       ScenarioType = "BANK_OUTAGE"
	ScenarioTypeLatency          ScenarioType = "LATENCY"
	ScenarioTypeTransientDrop    ScenarioType = "TRANSIENT_DROP"
	ScenarioTypeTransient        ScenarioType = "TRANSIENT"
	ScenarioTypeMessageDrop      ScenarioType = "MESSAGE_DROP"
	ScenarioTypePartition        ScenarioType = "TEMPORARY_PARTITION"
)

type EventType string

const (
	EventTypeChaosStarted EventType = "CHAOS_STARTED"
	EventTypeChaosStopped EventType = "CHAOS_STOPPED"
	EventTypeChaosReset   EventType = "CHAOS_RESET"
	EventTypeChaosExpired EventType = "CHAOS_EXPIRED"
)

const (
	ExecutionModeSimulation = "SIMULATION"

	DefaultDuration = 30 * time.Second
	MinDuration     = 100 * time.Millisecond
	MaxDuration     = 10 * time.Minute

	MinLatency = 1 * time.Millisecond
	MaxLatency = 30 * time.Second
)

var (
	ErrInvalidScenarioID = errors.New("scenario ID is invalid")
	ErrInvalidTargetID   = errors.New("target ID is invalid")
	ErrInvalidScenario   = errors.New("unsupported scenario type")
	ErrInvalidParameters = errors.New("invalid chaos scenario parameters")
	ErrScenarioNotFound  = errors.New("chaos scenario not found")
	ErrScenarioInactive  = errors.New("chaos scenario is not active")
	ErrTargetConflict    = errors.New("target already has an active chaos scenario")
	ErrTargetNotFound    = errors.New("target is not a configured execution or health target")

	// Domain simulation errors injected at the communication seam
	ErrBankOutage        = errors.New("chaos simulation: bank endpoint unavailable")
	ErrNetworkPartition  = errors.New("chaos simulation: network partition to target")
	ErrTransientDrop     = errors.New("chaos simulation: message dropped transiently")
)

type ScenarioParameters struct {
	DurationMs   int64   `json:"durationMs,omitempty"`
	LatencyMs    int64   `json:"latencyMs,omitempty"`
	DropCount    int     `json:"dropCount,omitempty"`
	DropRate     float64 `json:"dropRate,omitempty"`
	ErrorMessage string  `json:"errorMessage,omitempty"`
}

func (p *ScenarioParameters) Validate(st ScenarioType) error {
	if p.DurationMs <= 0 {
		p.DurationMs = int64(DefaultDuration / time.Millisecond)
	}
	duration := time.Duration(p.DurationMs) * time.Millisecond
	if duration < MinDuration || duration > MaxDuration {
		return errors.New("duration must be between 100ms and 10m")
	}

	switch st {
	case ScenarioTypeBankOutage:
		// No mandatory extra parameters
	case ScenarioTypeLatency:
		if p.LatencyMs <= 0 {
			return errors.New("latencyMs must be greater than 0")
		}
		lat := time.Duration(p.LatencyMs) * time.Millisecond
		if lat < MinLatency || lat > MaxLatency {
			return errors.New("latencyMs must be between 1ms and 30s")
		}
	case ScenarioTypeTransientDrop, ScenarioTypeTransient, ScenarioTypeMessageDrop:
		if p.DropCount < 0 {
			return errors.New("dropCount cannot be negative")
		}
		if p.DropRate < 0 || p.DropRate > 1.0 {
			return errors.New("dropRate must be between 0.0 and 1.0")
		}
		if p.DropCount == 0 && p.DropRate == 0 {
			// default to dropping all while active if neither specified
			p.DropRate = 1.0
		}
	case ScenarioTypePartition:
		// No mandatory extra parameters
	default:
		return ErrInvalidScenario
	}
	return nil
}

type ChaosScenario struct {
	ID          uuid.UUID          `json:"id"`
	ScenarioID  string             `json:"scenarioId"`
	Type        ScenarioType       `json:"type"`
	TargetID    string             `json:"targetId"`
	Parameters  ScenarioParameters `json:"parameters"`
	StartedAt   time.Time          `json:"startedAt"`
	ExpiresAt   time.Time          `json:"expiresAt"`
	StoppedAt   *time.Time         `json:"stoppedAt,omitempty"`
	Active      bool               `json:"active"`
	Mode        string             `json:"mode"`
	CreatedBy   string             `json:"createdBy"`
	StoppedBy   *string            `json:"stoppedBy,omitempty"`
	CreatedAt   time.Time          `json:"createdAt"`
	UpdatedAt   time.Time          `json:"updatedAt"`
}

type ChaosEvent struct {
	ID         int64          `json:"id"`
	ScenarioID string         `json:"scenarioId"`
	EventType  EventType      `json:"eventType"`
	TargetID   string         `json:"targetId"`
	FaultType  string         `json:"faultType"`
	ActorID    string         `json:"actorId"`
	ActorRole  string         `json:"actorRole"`
	Parameters map[string]any `json:"parameters,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
	OccurredAt time.Time      `json:"occurredAt"`
}

type StartRequest struct {
	ScenarioID string             `json:"scenarioId"`
	Type       ScenarioType       `json:"type"`
	TargetID   string             `json:"targetId"`
	Parameters ScenarioParameters `json:"parameters"`
}

func (r *StartRequest) Validate() error {
	r.ScenarioID = strings.TrimSpace(r.ScenarioID)
	if r.ScenarioID == "" || len(r.ScenarioID) > 120 {
		return ErrInvalidScenarioID
	}
	r.TargetID = strings.TrimSpace(r.TargetID)
	if r.TargetID == "" || len(r.TargetID) > 120 {
		return ErrInvalidTargetID
	}
	switch r.Type {
	case ScenarioTypeBankOutage, ScenarioTypeLatency, ScenarioTypeTransientDrop, ScenarioTypeTransient, ScenarioTypeMessageDrop, ScenarioTypePartition:
	default:
		return ErrInvalidScenario
	}
	return r.Parameters.Validate(r.Type)
}
