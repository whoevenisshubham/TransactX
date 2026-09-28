package experiments

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// EnvironmentMetadata captures runtime and environment properties for reproducibility.
type EnvironmentMetadata struct {
	GitCommit         string            `json:"gitCommit"`
	Platform          string            `json:"platform"`
	OS                string            `json:"os"`
	Arch              string            `json:"arch"`
	GoVersion         string            `json:"goVersion"`
	NodeVersion       string            `json:"nodeVersion,omitempty"`
	DeterministicSeed int64             `json:"deterministicSeed"`
	DatasetSize       int               `json:"datasetSize"`
	RunTimestamp      time.Time         `json:"runTimestamp"`
	RunID             string            `json:"runId"`
	ScenarioParams    map[string]string `json:"scenarioParams,omitempty"`
}

// CaptureEnvironmentMetadata captures standard environment metadata.
func CaptureEnvironmentMetadata(seed int64, datasetSize int, scenarioParams map[string]string) EnvironmentMetadata {
	commit := "unknown"
	cmd := exec.Command("git", "rev-parse", "HEAD")
	if out, err := cmd.Output(); err == nil {
		commit = strings.TrimSpace(string(out))
	}

	nodeVer := ""
	if nodeOut, err := exec.Command("node", "--version").Output(); err == nil {
		nodeVer = strings.TrimSpace(string(nodeOut))
	}

	now := time.Now().UTC()
	runID := fmt.Sprintf("run-%d-%s", now.Unix(), strings.ToLower(runtime.GOOS))

	return EnvironmentMetadata{
		GitCommit:         commit,
		Platform:          runtime.GOOS + "/" + runtime.GOARCH,
		OS:                runtime.GOOS,
		Arch:              runtime.GOARCH,
		GoVersion:         runtime.Version(),
		NodeVersion:       nodeVer,
		DeterministicSeed: seed,
		DatasetSize:       datasetSize,
		RunTimestamp:      now,
		RunID:             runID,
		ScenarioParams:    scenarioParams,
	}
}

// LatencyPercentiles holds calculated latency percentiles.
type LatencyPercentiles struct {
	MeanMs float64 `json:"meanMs"`
	P50Ms  float64 `json:"p50Ms"`
	P95Ms  float64 `json:"p95Ms"`
	P99Ms  float64 `json:"p99Ms"`
	MaxMs  float64 `json:"maxMs"`
}

// TrafficShare records the traffic distribution across execution targets.
type TrafficShare struct {
	TargetID   string  `json:"targetId"`
	Count      int     `json:"count"`
	Percentage float64 `json:"percentage"`
}

// InvariantResult captures verification of strong system invariants.
type InvariantResult struct {
	InvariantName string `json:"invariantName"`
	Passed        bool   `json:"passed"`
	Details       string `json:"details"`
}

// ExperimentSample represents a single request or timeline tick.
type ExperimentSample struct {
	Index          int           `json:"index"`
	Timestamp      time.Time     `json:"timestamp"`
	Mode           string        `json:"mode"`
	TargetID       string        `json:"targetId"`
	Success        bool          `json:"success"`
	Latency        time.Duration `json:"latencyNs"`
	LatencyMs      float64       `json:"latencyMs"`
	DecisionReason string        `json:"decisionReason"`
	CircuitState   string        `json:"circuitState,omitempty"`
	ErrorMessage   string        `json:"errorMessage,omitempty"`
}

// ExperimentSummary contains aggregate metrics for a scenario run.
type ExperimentSummary struct {
	TotalRequests          int                  `json:"totalRequests"`
	SuccessCount           int                  `json:"successCount"`
	FailureCount           int                  `json:"failureCount"`
	PendingCount           int                  `json:"pendingCount"`
	SuccessRate            float64              `json:"successRate"`
	Latency                LatencyPercentiles   `json:"latency"`
	RecoveryTimeMs         float64              `json:"recoveryTimeMs"`
	IsolationTimeMs        float64              `json:"isolationTimeMs,omitempty"`
	TrafficShare           []TrafficShare       `json:"trafficShare"`
	DecisionReasons        map[string]int       `json:"decisionReasons"`
	Invariants             []InvariantResult    `json:"invariants"`
	AllInvariantsSatisfied bool                 `json:"allInvariantsSatisfied"`
	AdditionalMetrics      map[string]any       `json:"additionalMetrics,omitempty"`
}

// ExperimentResult represents the full benchmark result containing both baseline and proposed (or individual scenarios).
type ExperimentResult struct {
	ExperimentID string              `json:"experimentId"`
	Title        string              `json:"title"`
	Environment  EnvironmentMetadata `json:"environment"`
	Baseline     *ExperimentSummary  `json:"baseline,omitempty"`
	Proposed     *ExperimentSummary  `json:"proposed,omitempty"`
	Summary      *ExperimentSummary  `json:"summary,omitempty"`
	Samples      []ExperimentSample  `json:"samples,omitempty"`
}

// SaveArtifacts saves machine-readable JSON and CSV files to the specified directory.
func (r *ExperimentResult) SaveArtifacts(rootDir string) (jsonPath string, csvPath string, err error) {
	expDir := filepath.Join(rootDir, "experiments", r.ExperimentID)
	if err := os.MkdirAll(expDir, 0755); err != nil {
		return "", "", fmt.Errorf("create artifact dir: %w", err)
	}

	timestamp := r.Environment.RunTimestamp.Format("20060102-150405")
	baseFileName := fmt.Sprintf("%s-%s", r.ExperimentID, timestamp)

	// 1. JSON output
	jsonPath = filepath.Join(expDir, baseFileName+".json")
	jsonData, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", "", fmt.Errorf("marshal json: %w", err)
	}
	if err := os.WriteFile(jsonPath, jsonData, 0644); err != nil {
		return "", "", fmt.Errorf("write json file: %w", err)
	}

	// 2. CSV output (sample trace)
	csvPath = filepath.Join(expDir, baseFileName+".csv")
	csvFile, err := os.Create(csvPath)
	if err != nil {
		return jsonPath, "", fmt.Errorf("create csv file: %w", err)
	}
	defer csvFile.Close()

	w := csv.NewWriter(csvFile)
	defer w.Flush()

	// CSV Header
	if err := w.Write([]string{
		"index", "timestamp", "mode", "target_id", "success", "latency_ms", "decision_reason", "circuit_state", "error_message",
	}); err != nil {
		return jsonPath, "", fmt.Errorf("write csv header: %w", err)
	}

	for _, s := range r.Samples {
		record := []string{
			strconv.Itoa(s.Index),
			s.Timestamp.Format(time.RFC3339Nano),
			s.Mode,
			s.TargetID,
			strconv.FormatBool(s.Success),
			fmt.Sprintf("%.3f", s.LatencyMs),
			s.DecisionReason,
			s.CircuitState,
			s.ErrorMessage,
		}
		if err := w.Write(record); err != nil {
			return jsonPath, "", fmt.Errorf("write csv row: %w", err)
		}
	}

	return jsonPath, csvPath, nil
}
