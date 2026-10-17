package analytics

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestHealthyEligibilityRequiresServerAttestation(t *testing.T) {
	tests := []struct {
		name     string
		raw      map[string]any
		eligible bool
		invalid  bool
	}{
		{"absent", nil, false, false},
		{"seed alone", map[string]any{"seeded": true}, false, false},
		{"healthy alone", map[string]any{"healthy": true}, false, false},
		{"legacy seed label", map[string]any{"provenance": "seeded_healthy"}, false, false},
		{"legacy review label", map[string]any{"provenance": "reviewed_healthy"}, false, false},
		{"reviewed seed", map[string]any{"seeded": true, "reviewed_healthy": true}, true, false},
		{"negative review", map[string]any{"seeded": true, "healthy": true, "reviewed_healthy": false}, false, false},
		{"negative operational review", map[string]any{"reviewed_healthy": true, "operational_review_passed": false}, false, false},
		{"negative investigation review", map[string]any{"reviewed_healthy": true, "investigation_review_passed": false}, false, false},
		{"anomalous seed", map[string]any{"reviewed_healthy": true, "seeded": true, "anomalous": true}, false, false},
		{"ineligible", map[string]any{"reviewed_healthy": true, "ineligible": true}, false, false},
		{"unreviewed", map[string]any{"reviewed_healthy": true, "unreviewed": true}, false, false},
		{"unreviewed provenance", map[string]any{"reviewed_healthy": true, "provenance": "unreviewed_live"}, false, false},
		{"string attestation", map[string]any{"reviewed_healthy": "true"}, false, true},
		{"null attestation", map[string]any{"reviewed_healthy": nil}, false, true},
		{"malformed veto", map[string]any{"reviewed_healthy": true, "anomalous": "false"}, false, true},
		{"malformed provenance", map[string]any{"reviewed_healthy": true, "provenance": 1}, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eligible, err := evaluateHealthyEligibility(tt.raw)
			if eligible != tt.eligible || (err != nil) != tt.invalid {
				t.Fatalf("eligible=%v error=%v; want eligible=%v invalid=%v", eligible, err, tt.eligible, tt.invalid)
			}
		})
	}
}

func TestAnalytics_UnreviewedSeedsCannotCalibrate(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	repo := NewPostgresRepository(pool)
	entity := "BANK_SEED_" + uuid.NewString()
	from := time.Now().UTC().Truncate(WindowDuration).Add(-2 * WindowDuration)
	for i := 0; i < 8; i++ {
		start := from.Add(time.Duration(i-8) * WindowDuration)
		_, err := pool.Exec(ctx, `INSERT INTO operational_aggregate_windows
			(id, entity_type, entity_id, window_from, window_to, sample_count,
			feature_schema_version, features, raw_aggregates, model_status)
			VALUES ($1, 'bank', $2, $3, $4, 40, $5,
			'{"failureRate":0.01}', '{"seeded":true,"provenance":"seeded_healthy"}', 'unavailable')`,
			uuid.New(), entity, start, start.Add(WindowDuration), FeatureSchemaVersionV1)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, raw, err := repo.AggregateWindow(ctx, "bank", entity, from, from.Add(WindowDuration))
	if err != nil {
		t.Fatal(err)
	}
	if got := raw["calibration_samples"]; got != 0 {
		t.Fatalf("unreviewed seeds calibrated baseline: %v", got)
	}
	// Reading/scoring must retain historical provenance, not silently attest it.
	var unchanged int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM operational_aggregate_windows
		WHERE entity_id=$1 AND raw_aggregates='{"seeded":true,"provenance":"seeded_healthy"}'::jsonb`, entity).Scan(&unchanged); err != nil {
		t.Fatal(err)
	}
	if unchanged != 8 {
		t.Fatalf("historical seed provenance changed: %d rows retained", unchanged)
	}
}
