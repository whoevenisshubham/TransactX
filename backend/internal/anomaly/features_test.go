package anomaly

import (
	"math"
	"testing"
)

func measurement(value float64) *float64 { return &value }

func TestFromLiveFeaturesMapsFrozenOrderAndCopiesValues(t *testing.T) {
	values := LiveFeatureValues{
		TimeoutRate:                measurement(0.01),
		FailureRate:                measurement(0.02),
		P95LatencyMs:               measurement(125),
		OldestRecoveryAgeMs:        measurement(2_000),
		RecoveryAttemptsPerPayment: measurement(0.3),
		HealthAvailabilityRate:     measurement(0.99),
	}
	vector, err := FromLiveFeatures(FeatureSchemaVersion, 42, values)
	if err != nil {
		t.Fatal(err)
	}
	want := []float64{0.01, 0.02, 125, 2_000, 0.3, 0.99}
	for index, expected := range want {
		if vector.Values[index] == nil || *vector.Values[index] != expected {
			t.Fatalf("feature %d: got %v, want %v", index, vector.Values[index], expected)
		}
	}
	if vector.Values[0] == values.TimeoutRate {
		t.Fatal("adapter retained caller-owned measurement pointer")
	}
	*values.TimeoutRate = 0.9
	if *vector.Values[0] != 0.01 {
		t.Fatal("caller mutation changed the scoring vector")
	}
}

func TestFromLiveFeaturesPreservesMissingMeasurements(t *testing.T) {
	vector, err := FromLiveFeatures(FeatureSchemaVersion, 0, LiveFeatureValues{})
	if err != nil {
		t.Fatal(err)
	}
	for index, value := range vector.Values {
		if value != nil {
			t.Fatalf("feature %d: missing measurement was imputed", index)
		}
	}
}

func TestFromLiveFeaturesRejectsInvalidLiveInput(t *testing.T) {
	tests := map[string]struct {
		schema string
		count  int
		values LiveFeatureValues
	}{
		"schema":         {schema: "tx.features.future", count: 20},
		"negative_count": {schema: FeatureSchemaVersion, count: -1},
		"rate_over_one":  {schema: FeatureSchemaVersion, count: 20, values: LiveFeatureValues{FailureRate: measurement(1.01)}},
		"negative_value": {schema: FeatureSchemaVersion, count: 20, values: LiveFeatureValues{P95LatencyMs: measurement(-1)}},
		"nan":            {schema: FeatureSchemaVersion, count: 20, values: LiveFeatureValues{TimeoutRate: measurement(math.NaN())}},
		"infinity":       {schema: FeatureSchemaVersion, count: 20, values: LiveFeatureValues{OldestRecoveryAgeMs: measurement(math.Inf(1))}},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := FromLiveFeatures(test.schema, test.count, test.values); err == nil {
				t.Fatal("invalid live input was accepted")
			}
		})
	}
}
