package anomaly

import (
	"testing"
	"time"
)

var liveBaselineStart = time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)

func liveCalibrationWindow(index int) LiveCalibrationWindow {
	from := liveBaselineStart.Add(time.Duration(index) * LiveWindowDuration)
	return LiveCalibrationWindow{
		ID:            "window-" + time.Unix(int64(index), 0).UTC().Format("150405"),
		EntityType:    "bank",
		EntityID:      "BANK_A",
		From:          from,
		To:            from.Add(LiveWindowDuration),
		SchemaVersion: FeatureSchemaVersion,
		SampleCount:   42,
		Values: LiveFeatureValues{
			TimeoutRate:                measurement(0.01),
			FailureRate:                measurement(0.02),
			P95LatencyMs:               measurement(125),
			OldestRecoveryAgeMs:        measurement(2_000),
			RecoveryAttemptsPerPayment: measurement(0.3),
			HealthAvailabilityRate:     measurement(0.99),
		},
	}
}

func priorLiveCalibrationWindows() []LiveCalibrationWindow {
	windows := make([]LiveCalibrationWindow, 8)
	for index := range windows {
		windows[index] = liveCalibrationWindow(index)
	}
	return windows
}

func TestFitLiveBaselineAcceptsUniquePriorHealthyWindows(t *testing.T) {
	windows := priorLiveCalibrationWindows()
	baseline, err := FitLiveBaseline(windows, "bank", "BANK_A", windows[len(windows)-1].To, 3)
	if err != nil {
		t.Fatal(err)
	}
	vector, err := FromLiveFeatures(FeatureSchemaVersion, 42, windows[0].Values)
	if err != nil {
		t.Fatal(err)
	}
	result := baseline.Score(vector)
	if result.Status != "ready" || len(result.Reasons) != 0 {
		t.Fatalf("unexpected live baseline result: %+v", result)
	}
}

func TestFitLiveBaselineAllowsWarmupWithoutImputation(t *testing.T) {
	baseline, err := FitLiveBaseline(nil, "execution_target", "BANK_A:UPI", liveBaselineStart, 3)
	if err != nil {
		t.Fatal(err)
	}
	vector, err := FromLiveFeatures(FeatureSchemaVersion, 42, liveCalibrationWindow(0).Values)
	if err != nil {
		t.Fatal(err)
	}
	if result := baseline.Score(vector); result.Status != "insufficient_data" {
		t.Fatalf("empty live calibration must warm up: %+v", result)
	}
}

func TestFitLiveBaselineRejectsScopeTimeAndFeatureLeakage(t *testing.T) {
	valid := priorLiveCalibrationWindows()
	scoreFrom := valid[len(valid)-1].To

	tests := map[string]func([]LiveCalibrationWindow) []LiveCalibrationWindow{
		"cross_entity": func(windows []LiveCalibrationWindow) []LiveCalibrationWindow {
			windows[0].EntityID = "BANK_B"
			return windows
		},
		"duplicate_id": func(windows []LiveCalibrationWindow) []LiveCalibrationWindow {
			windows[1].ID = windows[0].ID
			return windows
		},
		"duplicate_window": func(windows []LiveCalibrationWindow) []LiveCalibrationWindow {
			windows[1].From = windows[0].From
			windows[1].To = windows[0].To
			return windows
		},
		"overlap_scored_window": func(windows []LiveCalibrationWindow) []LiveCalibrationWindow {
			windows[0].From = scoreFrom
			windows[0].To = scoreFrom.Add(LiveWindowDuration)
			return windows
		},
		"wrong_duration": func(windows []LiveCalibrationWindow) []LiveCalibrationWindow {
			windows[0].To = windows[0].From.Add(4 * time.Minute)
			return windows
		},
		"unaligned_window": func(windows []LiveCalibrationWindow) []LiveCalibrationWindow {
			windows[0].From = windows[0].From.Add(time.Minute)
			windows[0].To = windows[0].From.Add(LiveWindowDuration)
			return windows
		},
		"invalid_schema": func(windows []LiveCalibrationWindow) []LiveCalibrationWindow {
			windows[0].SchemaVersion = "tx.features.future"
			return windows
		},
		"invalid_measurement": func(windows []LiveCalibrationWindow) []LiveCalibrationWindow {
			windows[0].Values.TimeoutRate = measurement(2)
			return windows
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			windows := mutate(priorLiveCalibrationWindows())
			if _, err := FitLiveBaseline(windows, "bank", "BANK_A", scoreFrom, 3); err == nil {
				t.Fatal("unsafe live calibration was accepted")
			}
		})
	}

	if _, err := FitLiveBaseline(valid, "customer", "BANK_A", scoreFrom, 3); err == nil {
		t.Fatal("invalid target entity type was accepted")
	}
	if _, err := FitLiveBaseline(valid, "bank", "BANK_A", time.Time{}, 3); err == nil {
		t.Fatal("missing score window time was accepted")
	}
	if _, err := FitLiveBaseline(valid, "bank", "BANK_A", scoreFrom.Add(time.Minute), 3); err == nil {
		t.Fatal("unaligned score window time was accepted")
	}
	if _, err := FitLiveBaseline(make([]LiveCalibrationWindow, MaxLiveCalibrationWindows+1), "bank", "BANK_A", scoreFrom, 3); err == nil {
		t.Fatal("unbounded live calibration was accepted")
	}
}
