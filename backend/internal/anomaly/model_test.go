package anomaly

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

var testNow = time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)

func artifactBytes(t *testing.T) []byte {
	t.Helper()
	b, e := os.ReadFile("../../../analytics/artifacts/model.json")
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func loadBytes(t *testing.T, b []byte, now time.Time) *Model {
	t.Helper()
	m, e := Load(bytes.NewReader(b), fmt.Sprintf("%x", sha256.Sum256(b)), now, 0)
	if e != nil {
		t.Fatal(e)
	}
	return m
}
func vector(xs ...float64) FeatureVector {
	v := FeatureVector{SchemaVersion: FeatureSchemaVersion, SampleCount: 50}
	for i, x := range xs {
		z := x
		v.Values[i] = &z
	}
	return v
}
func TestPythonParityAndBaseline(t *testing.T) {
	m := loadBytes(t, artifactBytes(t), testNow)
	var refs []struct {
		Vector                   FeatureVector
		Score, BaselineDeviation float64
		Status                   string
		BaselineAlert            bool
	}
	raw, e := os.ReadFile("../../../analytics/artifacts/reference.json")
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(raw, &refs); e != nil {
		t.Fatal(e)
	}
	if len(refs) < 100 {
		t.Fatal("requires >=100 reference vectors")
	}
	var rows []struct {
		Split       string
		Anomaly     bool
		SampleCount int
		Features    map[string]float64
	}
	raw, e = os.ReadFile("../../../analytics/artifacts/dataset.json")
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(raw, &rows); e != nil {
		t.Fatal(e)
	}
	prior := []FeatureVector{}
	for _, r := range rows {
		if r.Split != "train" || r.Anomaly {
			continue
		}
		v := FeatureVector{SchemaVersion: FeatureSchemaVersion, SampleCount: r.SampleCount}
		for i, name := range featureNames {
			x := r.Features[name]
			v.Values[i] = &x
		}
		prior = append(prior, v)
	}
	var calibration struct{ Threshold float64 }
	raw, e = os.ReadFile("../../../analytics/artifacts/baseline.json")
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(raw, &calibration); e != nil {
		t.Fatal(e)
	}
	baseline, e := FitBaseline(prior, calibration.Threshold)
	if e != nil {
		t.Fatal(e)
	}
	maxError := 0.0
	readyCount := 0
	for _, ref := range refs {
		r := m.Score(ref.Vector)
		if r.Status != ref.Status {
			t.Fatalf("reference status expected %s got %s", ref.Status, r.Status)
		}
		if ref.Status == "ready" {
			readyCount++
			if r.Score == nil {
				t.Fatal("missing ready score")
			}
			err := math.Abs(*r.Score - ref.Score)
			if err > maxError {
				maxError = err
			}
			if err > 1e-6 {
				t.Fatalf("parity error %.12g", err)
			}
		} else if r.Score != nil {
			t.Fatal("OOD must not publish score")
		}
		b := baseline.Score(ref.Vector)
		if b.Deviation == nil || math.Abs(*b.Deviation-ref.BaselineDeviation) > 1e-9 || (len(b.Reasons) > 0) != ref.BaselineAlert {
			t.Fatal("baseline parity")
		}
	}
	if readyCount < 100 {
		t.Fatal("requires >=100 ready reference vectors")
	}
	t.Logf("%d Python vectors, max absolute model error %.12g; baseline parity passed", readyCount, maxError)
}
func TestLoaderRejectsUnsafeArtifacts(t *testing.T) {
	raw := artifactBytes(t)
	mutations := map[string]func(*Artifact){
		"cycle":             func(a *Artifact) { a.Trees[0].Nodes[0].Left = 0 },
		"bad_child":         func(a *Artifact) { a.Trees[0].Nodes[0].Right = 99999 },
		"bad_feature":       func(a *Artifact) { a.Trees[0].Nodes[0].Feature = 6 },
		"schema":            func(a *Artifact) { a.FeatureSchemaVersion = "tx.features.future" },
		"duplicate_feature": func(a *Artifact) { a.Features[1] = a.Features[0] },
		"zero_scale":        func(a *Artifact) { a.Scale[0] = 0 },
		"node_count":        func(a *Artifact) { a.Trees[0].Nodes = make([]Node, 1025) },
		"tree_count":        func(a *Artifact) { a.Trees = make([]Tree, 129) },
		"empty_trees":       func(a *Artifact) { a.Trees = nil },
		"invalid_samples":   func(a *Artifact) { a.Samples = 1 },
		"unreachable": func(a *Artifact) {
			a.Trees[0].Nodes = append(a.Trees[0].Nodes, Node{Feature: -2, Left: -1, Right: -1, Samples: 1})
		},
		"future": func(a *Artifact) { a.TrainedAt = testNow.Add(time.Hour) },
		"bad_leaf": func(a *Artifact) {
			for i, n := range a.Trees[0].Nodes {
				if n.Left == -1 {
					a.Trees[0].Nodes[i].Feature = 0
					break
				}
			}
		},
		"bad_count": func(a *Artifact) { a.Trees[0].Nodes[0].Samples = 1 },
		"depth": func(a *Artifact) {
			nodes := []Node{}
			for i := 0; i < 34; i++ {
				nodes = append(nodes, Node{Feature: 0, Left: i + 1, Right: 34 + i, Samples: 128 - i})
			}
			nodes = append(nodes, Node{Feature: -2, Left: -1, Right: -1, Samples: 94})
			for i := 0; i < 34; i++ {
				nodes = append(nodes, Node{Feature: -2, Left: -1, Right: -1, Samples: 1})
			}
			a.Trees[0].Nodes = nodes
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			var a Artifact
			if e := json.Unmarshal(raw, &a); e != nil {
				t.Fatal(e)
			}
			mutate(&a)
			b, e := json.Marshal(a)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = Load(bytes.NewReader(b), fmt.Sprintf("%x", sha256.Sum256(b)), testNow, 0); e == nil {
				t.Fatal("accepted invalid artifact")
			}
		})
	}
	for name, b := range map[string][]byte{"oversize": bytes.Repeat([]byte(" "), MaxArtifactBytes+1), "nan": []byte(`{"threshold":NaN}`), "infinity": []byte(`{"threshold":1e999}`), "trailing": append(append([]byte(nil), raw...), []byte("{}")...), "unknown": []byte(`{"unexpected":1}`)} {
		t.Run(name, func(t *testing.T) {
			if _, e := Load(bytes.NewReader(b), fmt.Sprintf("%x", sha256.Sum256(b)), testNow, 0); e == nil {
				t.Fatal("accepted malformed JSON")
			}
		})
	}
	if _, e := Load(bytes.NewReader(raw), strings.Repeat("0", 64), testNow, 0); e == nil {
		t.Fatal("hash mismatch accepted")
	}
	if _, e := Load(nil, "", testNow, 0); e == nil {
		t.Fatal("nil model accepted")
	}
}
func TestModelFallbacks(t *testing.T) {
	raw := artifactBytes(t)
	m := loadBytes(t, raw, testNow)
	v := vector(.01, .01, 90, 1200, .15, .98)
	cases := map[string]FeatureVector{"insufficient_data": v, "unavailable": v, "out_of_distribution": v}
	a := cases["insufficient_data"]
	a.SampleCount = 19
	cases["insufficient_data"] = a
	a = cases["unavailable"]
	x := math.NaN()
	a.Values[0] = &x
	cases["unavailable"] = a
	a = cases["out_of_distribution"]
	x = 1e12
	a.Values[2] = &x
	cases["out_of_distribution"] = a
	for expected, input := range cases {
		if r := m.Score(input); r.Status != expected || r.Score != nil {
			t.Fatalf("expected %s: %+v", expected, r)
		}
	}
	a = v
	a.Values[0] = nil
	if r := m.Score(a); r.Status != "insufficient_data" || r.Score != nil {
		t.Fatal("missing required feature")
	}
	a = v
	a.SchemaVersion = "future"
	if r := m.Score(a); r.Status != "unavailable" {
		t.Fatal("schema mismatch")
	}
	stale := loadBytes(t, raw, testNow.Add(31*24*time.Hour))
	if r := stale.Score(v); r.Status != "stale" || r.Score != nil {
		t.Fatal("stale")
	}
	var missing *Model
	if missing.Score(v).Status != "unavailable" {
		t.Fatal("missing")
	}
	b, e := FitBaseline([]FeatureVector{v, v, v, v, v, v, v, v}, 3)
	if e != nil {
		t.Fatal(e)
	}
	if b.Score(v).Status != "ready" {
		t.Fatal("baseline unavailable without model")
	}
}
func TestBaselineWarmupAndMissing(t *testing.T) {
	v := vector(.01, .01, 90, 1200, .15, .98)
	b, e := FitBaseline([]FeatureVector{v}, 3)
	if e != nil {
		t.Fatal(e)
	}
	if b.Score(v).Status != "insufficient_data" {
		t.Fatal("warmup")
	}
	b, e = FitBaseline([]FeatureVector{v, v, v, v, v, v, v, v}, 3)
	if e != nil {
		t.Fatal(e)
	}
	a := v
	a.Values[0] = nil
	if b.Score(a).Status != "ready" {
		t.Fatal("available fields should work")
	}
	a = FeatureVector{SchemaVersion: FeatureSchemaVersion, SampleCount: 50}
	if b.Score(a).Status != "insufficient_data" {
		t.Fatal("no observations")
	}
	a = v
	a.SampleCount = 19
	if b.Score(a).Status != "insufficient_data" {
		t.Fatal("minimum samples")
	}
	x := .9
	a = v
	a.Values[0] = &x
	if len(b.Score(a).Reasons) != 1 {
		t.Fatal("explicit timeout reason")
	}
	if _, e = FitBaseline([]FeatureVector{v}, math.NaN()); e == nil {
		t.Fatal("nan threshold")
	}
}
func TestPathAdjustment(t *testing.T) {
	if pathAdjustment(0) != 0 || pathAdjustment(1) != 0 || pathAdjustment(2) != 1 {
		t.Fatal("leaf adjustment")
	}
}
func BenchmarkScore(b *testing.B) {
	raw, _ := os.ReadFile("../../../analytics/artifacts/model.json")
	m, e := Load(bytes.NewReader(raw), fmt.Sprintf("%x", sha256.Sum256(raw)), testNow, 0)
	if e != nil {
		b.Fatal(e)
	}
	v := vector(.01, .01, 90, 1200, .15, .98)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Score(v)
	}
}

func TestFeatureMappingAndFloatPrecision(t *testing.T) {
	raw := artifactBytes(t)
	original := loadBytes(t, raw, testNow)
	var a Artifact
	if e := json.Unmarshal(raw, &a); e != nil {
		t.Fatal(e)
	}
	// Reverse feature ordering; convert global node indexes rather than assuming identity.
	for i, j := 0, len(a.Features)-1; i < j; i, j = i+1, j-1 {
		a.Features[i], a.Features[j] = a.Features[j], a.Features[i]
		a.Center[i], a.Center[j] = a.Center[j], a.Center[i]
		a.Scale[i], a.Scale[j] = a.Scale[j], a.Scale[i]
		a.Lower[i], a.Lower[j] = a.Lower[j], a.Lower[i]
		a.Upper[i], a.Upper[j] = a.Upper[j], a.Upper[i]
	}
	for k := range a.Trees {
		for i := range a.Trees[k].Nodes {
			n := &a.Trees[k].Nodes[i]
			if n.Feature >= 0 {
				n.Feature = 5 - n.Feature
			}
		}
	}
	b, e := json.Marshal(a)
	if e != nil {
		t.Fatal(e)
	}
	mapped := loadBytes(t, b, testNow)
	v := vector(.01, .01, 90, 1200, .15, .98)
	if math.Abs(*mapped.Score(v).Score-*original.Score(v).Score) > 1e-12 {
		t.Fatal("feature mapping")
	}
	a.Features = []string{"p95LatencyMs"}
	a.Center = []float64{0}
	a.Scale = []float64{1}
	a.Lower = []float64{0}
	a.Upper = []float64{1000}
	a.Samples = 3
	a.Trees = []Tree{{Nodes: []Node{{Feature: 0, Threshold: 100, Left: 1, Right: 2, Samples: 3}, {Feature: -2, Left: -1, Right: -1, Samples: 1}, {Feature: -2, Left: -1, Right: -1, Samples: 2}}}}
	b, e = json.Marshal(a)
	if e != nil {
		t.Fatal(e)
	}
	precision := loadBytes(t, b, testNow)
	x := 100. + 1e-8
	v.Values[2] = &x
	want := math.Pow(2, -1/pathAdjustment(3))
	if math.Abs(*precision.Score(v).Score-want) > 1e-12 {
		t.Fatal("float32 threshold branch")
	}
}
func TestAtomicReplacementAndAge(t *testing.T) {
	raw := artifactBytes(t)
	hash := fmt.Sprintf("%x", sha256.Sum256(raw))
	var registry Registry
	if e := registry.Replace(bytes.NewReader(raw), hash, testNow, 0); e != nil {
		t.Fatal(e)
	}
	before := registry.Current()
	if e := registry.Replace(strings.NewReader("invalid"), hash, testNow, 0); e == nil || registry.Current() != before {
		t.Fatal("invalid replacement changed model")
	}
	v := vector(.01, .01, 90, 1200, .15, .98)
	if registry.Current().ScoreAt(v, testNow.Add(31*24*time.Hour)).Status != "stale" {
		t.Fatal("long-lived worker expiry")
	}
	result := before.Score(v)
	*result.Version = "changed"
	wantThreshold := *result.Threshold
	*result.Threshold = 0
	if *before.Score(v).Threshold != wantThreshold {
		t.Fatal("result must not expose model threshold")
	}
	if *before.Score(v).Version == "changed" {
		t.Fatal("result must not expose model fields")
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if registry.Current().Score(v).Status != "ready" {
					t.Error("concurrent score")
				}
			}
		}()
	}
	for i := 0; i < 3; i++ {
		if e := registry.Replace(bytes.NewReader(raw), hash, testNow, 0); e != nil {
			t.Fatal(e)
		}
	}
	wg.Wait()
	e := Evaluate(nil, nil, v, testNow)
	if e.Model.Status != "unavailable" || !e.Advisory || e.MissingFeatureRatio != 0 {
		t.Fatal("fallback metadata")
	}
}
