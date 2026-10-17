package anomaly

import (
	"fmt"
	"time"
)

const (
	LiveWindowDuration        = 5 * time.Minute
	MaxLiveCalibrationWindows = 10_000
)

// LiveCalibrationWindow is one completed, independently designated healthy
// B10 window. The caller owns the healthy designation; this boundary enforces
// entity, time, uniqueness and feature invariants before calibration.
type LiveCalibrationWindow struct {
	ID            string
	EntityType    string
	EntityID      string
	From          time.Time
	To            time.Time
	SchemaVersion string
	SampleCount   int
	Values        LiveFeatureValues
}

// FitLiveBaseline validates prior healthy B10 windows for one scoring target
// and converts them to the package's median/MAD baseline. Windows must be
// completed, non-overlapping with the scored window and aligned to five-minute
// UTC boundaries. Empty or low-sample input remains a safe warmup baseline.
func FitLiveBaseline(priorHealthy []LiveCalibrationWindow, entityType, entityID string, scoreWindowFrom time.Time, threshold float64) (*Baseline, error) {
	scoreFrom := scoreWindowFrom.UTC()
	if (entityType != "bank" && entityType != "execution_target") || entityID == "" || scoreWindowFrom.IsZero() || !scoreFrom.Equal(scoreFrom.Truncate(LiveWindowDuration)) {
		return nil, fmt.Errorf("invalid live baseline target")
	}
	if len(priorHealthy) > MaxLiveCalibrationWindows {
		return nil, fmt.Errorf("live baseline calibration exceeds bound")
	}

	vectors := make([]FeatureVector, 0, len(priorHealthy))
	seenIDs := make(map[string]struct{}, len(priorHealthy))
	seenWindows := make(map[string]struct{}, len(priorHealthy))
	for _, window := range priorHealthy {
		if window.ID == "" || window.EntityType != entityType || window.EntityID != entityID {
			return nil, fmt.Errorf("invalid live baseline window identity")
		}
		if _, duplicate := seenIDs[window.ID]; duplicate {
			return nil, fmt.Errorf("duplicate live baseline window ID")
		}
		seenIDs[window.ID] = struct{}{}

		from := window.From.UTC()
		to := window.To.UTC()
		if window.From.IsZero() || window.To.IsZero() || !from.Before(to) || to.Sub(from) != LiveWindowDuration || !from.Equal(from.Truncate(LiveWindowDuration)) || to.After(scoreFrom) {
			return nil, fmt.Errorf("invalid live baseline window time")
		}
		windowKey := entityType + "\x00" + entityID + "\x00" + from.Format(time.RFC3339Nano) + "\x00" + window.SchemaVersion
		if _, duplicate := seenWindows[windowKey]; duplicate {
			return nil, fmt.Errorf("duplicate live baseline window")
		}
		seenWindows[windowKey] = struct{}{}

		vector, err := FromLiveFeatures(window.SchemaVersion, window.SampleCount, window.Values)
		if err != nil {
			return nil, fmt.Errorf("invalid live baseline features: %w", err)
		}
		vectors = append(vectors, vector)
	}
	return FitBaseline(vectors, threshold)
}
