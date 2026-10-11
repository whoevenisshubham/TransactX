package anomaly

import (
	"fmt"
	"math"
	"sort"
)

// Baseline calibrates only on prior independently designated healthy windows.
// Warmup is 8 eligible observations per feature; missing data is never imputed.
type Baseline struct {
	center, scale [6]float64
	counts        [6]int
	threshold     float64
}
type BaselineResult struct {
	Status    string
	Reasons   []string
	Deviation *float64
}

var floors = [6]float64{0.03, 0.03, 30, 1000, 0.2, 0.03}

func median(x []float64) float64 {
	sort.Float64s(x)
	n := len(x)
	if n%2 == 1 {
		return x[n/2]
	}
	return (x[n/2-1] + x[n/2]) / 2
}
func FitBaseline(priorHealthy []FeatureVector, threshold float64) (*Baseline, error) {
	if math.IsNaN(threshold) || math.IsInf(threshold, 0) || threshold <= 0 {
		return nil, fmt.Errorf("invalid threshold")
	}
	b := &Baseline{threshold: threshold}
	for i := range featureNames {
		xs := []float64{}
		for _, v := range priorHealthy {
			if !v.valid() {
				return nil, fmt.Errorf("invalid calibration vector")
			}
			if v.SampleCount >= MinimumSamples && v.Values[i] != nil {
				xs = append(xs, *v.Values[i])
			}
		}
		b.counts[i] = len(xs)
		if len(xs) < 8 {
			continue
		}
		b.center[i] = median(xs)
		dev := make([]float64, len(xs))
		for j, x := range xs {
			dev[j] = math.Abs(x - b.center[i])
		}
		b.scale[i] = math.Max(1.4826*median(dev), floors[i])
	}
	return b, nil
}
func (b *Baseline) Score(v FeatureVector) BaselineResult {
	r := BaselineResult{Status: "insufficient_data", Reasons: []string{}}
	if !v.valid() {
		r.Status = "unavailable"
		return r
	}
	if b == nil || v.SampleCount < MinimumSamples {
		return r
	}
	maxDev := 0.0
	used := 0
	for i, x := range v.Values {
		if x == nil || b.counts[i] < 8 {
			continue
		}
		used++
		d := (*x - b.center[i]) / b.scale[i]
		if i == 5 {
			d = -d
		}
		if d > maxDev {
			maxDev = d
		}
		if d > b.threshold {
			r.Reasons = append(r.Reasons, featureNames[i]+"_deviation")
		}
	}
	if used == 0 {
		return r
	}
	r.Status = "ready"
	r.Deviation = &maxDev
	return r
}
