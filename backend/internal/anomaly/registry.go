package anomaly

import (
	"io"
	"sync/atomic"
	"time"
)

// Registry retains the last verified model if replacement validation fails.
type Registry struct{ current atomic.Pointer[Model] }

func (r *Registry) Replace(src io.Reader, hash string, now time.Time, maxAge time.Duration) error {
	m, e := Load(src, hash, now, maxAge)
	if e != nil {
		return e
	}
	r.current.Store(m)
	return nil
}
func (r *Registry) Current() *Model { return r.current.Load() }

type Evaluation struct {
	Model               ScoreResult
	Baseline            BaselineResult
	MissingFeatureRatio float64
	Advisory            bool
}

func Evaluate(model *Model, baseline *Baseline, v FeatureVector, now time.Time) Evaluation {
	missing := 0
	for _, x := range v.Values {
		if x == nil {
			missing++
		}
	}
	return Evaluation{Model: model.ScoreAt(v, now), Baseline: baseline.Score(v), MissingFeatureRatio: float64(missing) / 6, Advisory: true}
}
