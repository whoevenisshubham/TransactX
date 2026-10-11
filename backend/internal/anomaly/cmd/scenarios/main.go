// Scenarios exercises the existing mock adapter seam using virtual five-minute windows.
// Latency is a seeded logical simulation input, not observed wall-clock or durable backend latency.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/transactx/backend/internal/experiments"
	"math/rand"
	"os"
	"time"
)

type Row struct {
	SourceCommit         string             `json:"sourceCommit"`
	Seed                 int64              `json:"seed"`
	Scenario             string             `json:"scenario"`
	RunID                string             `json:"runId"`
	From                 string             `json:"from"`
	To                   string             `json:"to"`
	FeatureSchemaVersion string             `json:"featureSchemaVersion"`
	CollectionMethod     string             `json:"collectionMethod"`
	Synthetic            bool               `json:"synthetic"`
	Split                string             `json:"split"`
	Anomaly              bool               `json:"anomaly"`
	SampleCount          int                `json:"sampleCount"`
	Features             map[string]float64 `json:"features"`
}

func main() {
	source := flag.String("source", "", "source SHA")
	out := flag.String("out", "", "JSON output")
	flag.Parse()
	if *source == "" || *out == "" {
		panic("source and out required")
	}
	rows := []Row{}
	scenarios := []string{"healthy", "rising_latency", "controlled_outage", "transient_drops", "recovery_backlog", "projection_backlog", "traffic_burst"}
	for group, split := range []string{"train", "validation", "test"} {
		for repeat := 0; repeat < 2; repeat++ {
			for si, scenario := range scenarios {
				seed := int64(1000 + group*100 + repeat*10 + si)
				rng := rand.New(rand.NewSource(seed))
				adapter := experiments.NewMockBankAdapter("BANK-A-RAIL-A")
				base := time.Date(2026, 10, 1+group*3+repeat, si*2, 0, 0, 0, time.UTC)
				for w := 0; w < 16; w++ {
					active := w >= 8 && scenario != "healthy"
					count := 40 + rng.Intn(30)
					if active && scenario == "traffic_burst" {
						count *= 4
					}
					timeouts, failures, healthy := 0, 0, 0
					latencies := []float64{}
					for q := 0; q < count; q++ {
						timeoutP, failP := 0.015, 0.01
						delay := 80 + 20*rng.Float64()
						if active {
							switch scenario {
							case "rising_latency":
								delay += float64(w-7) * 100
							case "controlled_outage":
								timeoutP = 0.8
							case "transient_drops":
								timeoutP = 0.25
							case "recovery_backlog":
								failP = 0.03
							}
						}
						timeout := rng.Float64() < timeoutP
						fail := !timeout && rng.Float64() < failP
						adapter.SetAvailable(!timeout)
						adapter.SetErrorHandler(func(context.Context, string) error {
							if fail {
								return fmt.Errorf("synthetic permanent failure")
							}
							return nil
						})
						_, err := adapter.GetHealth(context.Background())
						if err == nil {
							healthy++
						}
						if timeout {
							timeouts++
							delay += 1000
						}
						if fail {
							failures++
						}
						latencies = append(latencies, delay)
					}
					// Logical p95 from seeded per-request delay observations (no wall-clock measurement).
					for i := 1; i < len(latencies); i++ {
						for j := i; j > 0 && latencies[j] < latencies[j-1]; j-- {
							latencies[j], latencies[j-1] = latencies[j-1], latencies[j]
						}
					}
					age := 1000 + 500*rng.Float64()
					attempts := 0.1 + 0.1*rng.Float64()
					if active && scenario == "recovery_backlog" {
						age += float64(w-7) * 60000
						attempts += float64(w - 7)
					}
					from := base.Add(time.Duration(w) * 5 * time.Minute)
					rows = append(rows, Row{*source, seed, scenario, fmt.Sprintf("%s-%d", scenario, seed), from.Format(time.RFC3339), from.Add(5 * time.Minute).Format(time.RFC3339), "tx.features.v1", "existing MockBankAdapter.GetHealth + seeded virtual latency/backlog simulation", true, split, active, count, map[string]float64{"timeoutRate": float64(timeouts) / float64(count), "failureRate": float64(failures) / float64(count), "p95LatencyMs": latencies[(len(latencies)*95-1)/100], "oldestRecoveryAgeMs": age, "recoveryAttemptsPerPayment": attempts, "healthAvailabilityRate": float64(healthy) / float64(count)}})
				}
			}
		}
	}
	raw, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		panic(err)
	}
	if err = os.WriteFile(*out, append(raw, '\n'), 0644); err != nil {
		panic(err)
	}
	fmt.Printf("%d windows, 42 independent seeded runs\n", len(rows))
}
