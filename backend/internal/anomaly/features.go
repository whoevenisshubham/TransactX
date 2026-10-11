// Package anomaly scores advisory operational windows without payment dependencies.
package anomaly

import "math"

const FeatureSchemaVersion = "tx.features.v1"
const MinimumSamples = 20

var featureNames = [6]string{"timeoutRate", "failureRate", "p95LatencyMs", "oldestRecoveryAgeMs", "recoveryAttemptsPerPayment", "healthAvailabilityRate"}

func FeatureNames() []string { return append([]string(nil), featureNames[:]...) }

type FeatureVector struct {
	SchemaVersion string      `json:"schemaVersion"`
	SampleCount   int         `json:"sampleCount"`
	Values        [6]*float64 `json:"values"`
}

func (v FeatureVector) valid() bool {
	if v.SchemaVersion != FeatureSchemaVersion || v.SampleCount < 0 {
		return false
	}
	for i, x := range v.Values {
		if x == nil {
			continue
		}
		if math.IsNaN(*x) || math.IsInf(*x, 0) || *x < 0 {
			return false
		}
		if (i == 0 || i == 1 || i == 5) && *x > 1 {
			return false
		}
	}
	return true
}

type ScoreResult struct {
	Version   *string  `json:"version"`
	Status    string   `json:"status"`
	Score     *float64 `json:"score"`
	Threshold *float64 `json:"threshold"`
}
type AnomalyScorer interface {
	Score(FeatureVector) ScoreResult
}
