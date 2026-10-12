// Package anomaly scores advisory operational windows without payment dependencies.
package anomaly

import (
	"errors"
	"math"
)

const FeatureSchemaVersion = "tx.features.v1"
const MinimumSamples = 20

var featureNames = [6]string{"timeoutRate", "failureRate", "p95LatencyMs", "oldestRecoveryAgeMs", "recoveryAttemptsPerPayment", "healthAvailabilityRate"}

func FeatureNames() []string { return append([]string(nil), featureNames[:]...) }

type FeatureVector struct {
	SchemaVersion string      `json:"schemaVersion"`
	SampleCount   int         `json:"sampleCount"`
	Values        [6]*float64 `json:"values"`
}

// LiveFeatureValues is the named tx.features.v1 shape produced by the B10
// aggregate-window boundary. FromLiveFeatures validates and copies it into the
// scorer's immutable ordered representation.
type LiveFeatureValues struct {
	TimeoutRate                *float64 `json:"timeoutRate"`
	FailureRate                *float64 `json:"failureRate"`
	P95LatencyMs               *float64 `json:"p95LatencyMs"`
	OldestRecoveryAgeMs        *float64 `json:"oldestRecoveryAgeMs"`
	RecoveryAttemptsPerPayment *float64 `json:"recoveryAttemptsPerPayment"`
	HealthAvailabilityRate     *float64 `json:"healthAvailabilityRate"`
}

func copyMeasurement(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

// FromLiveFeatures is the package-owned adapter seam for durable B10 windows.
// Missing measurements stay nil; invalid schemas, counts, rates and non-finite
// values fail closed before either baseline or model scoring.
func FromLiveFeatures(schemaVersion string, sampleCount int, values LiveFeatureValues) (FeatureVector, error) {
	vector := FeatureVector{
		SchemaVersion: schemaVersion,
		SampleCount:   sampleCount,
		Values: [6]*float64{
			copyMeasurement(values.TimeoutRate),
			copyMeasurement(values.FailureRate),
			copyMeasurement(values.P95LatencyMs),
			copyMeasurement(values.OldestRecoveryAgeMs),
			copyMeasurement(values.RecoveryAttemptsPerPayment),
			copyMeasurement(values.HealthAvailabilityRate),
		},
	}
	if !vector.valid() {
		return FeatureVector{}, errors.New("invalid live feature vector")
	}
	return vector, nil
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
