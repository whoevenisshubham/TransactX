package anomaly

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"time"
)

const MaxArtifactBytes = 5 * 1024 * 1024
const DefaultMaxAge = 30 * 24 * time.Hour

type Node struct {
	Feature   int     `json:"feature"`
	Threshold float64 `json:"threshold"`
	Left      int     `json:"left"`
	Right     int     `json:"right"`
	Samples   int     `json:"samples"`
}
type Tree struct {
	Nodes []Node `json:"nodes"`
}
type Artifact struct {
	FormatVersion        string    `json:"formatVersion"`
	FeatureSchemaVersion string    `json:"featureSchemaVersion"`
	Version              string    `json:"version"`
	TrainedAt            time.Time `json:"trainedAt"`
	SourceCommit         string    `json:"sourceCommit"`
	DatasetSHA256        string    `json:"datasetSha256"`
	SplitSHA256          string    `json:"splitSha256"`
	Synthetic            bool      `json:"synthetic"`
	Features             []string  `json:"features"`
	Center               []float64 `json:"center"`
	Scale                []float64 `json:"scale"`
	Lower                []float64 `json:"lower"`
	Upper                []float64 `json:"upper"`
	Samples              int       `json:"samples"`
	Threshold            float64   `json:"threshold"`
	Trees                []Tree    `json:"trees"`
}

// Model privately owns validated arrays; concurrent scoring performs no writes.
type Model struct {
	artifact Artifact
	global   []int
	stale    bool
	hash     string
	maxAge   time.Duration
}

func validHash(s string, n int) bool {
	if len(s) != n {
		return false
	}
	_, e := hex.DecodeString(s)
	return e == nil
}
func finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }
func Load(r io.Reader, expectedSHA256 string, now time.Time, maxAge time.Duration) (*Model, error) {
	if r == nil {
		return nil, fmt.Errorf("missing model")
	}
	raw, e := io.ReadAll(io.LimitReader(r, MaxArtifactBytes+1))
	if e != nil {
		return nil, e
	}
	if len(raw) > MaxArtifactBytes {
		return nil, fmt.Errorf("artifact exceeds bound")
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	if len(expectedSHA256) != 64 || hash != expectedSHA256 {
		return nil, fmt.Errorf("artifact hash mismatch")
	}
	var a Artifact
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if e = dec.Decode(&a); e != nil {
		return nil, e
	}
	if dec.Decode(new(any)) != io.EOF {
		return nil, fmt.Errorf("trailing JSON")
	}
	if !validHash(a.SourceCommit, 40) || !validHash(a.DatasetSHA256, 64) || !validHash(a.SplitSHA256, 64) || a.FormatVersion != "tx.iforest.v1" || a.FeatureSchemaVersion != FeatureSchemaVersion || a.Version == "" || len(a.Version) > 128 || a.TrainedAt.IsZero() || a.TrainedAt.After(now) || a.Samples < 2 || a.Samples > 256 || !finite(a.Threshold) || a.Threshold < 0 || a.Threshold > 1 {
		return nil, fmt.Errorf("invalid model metadata")
	}
	n := len(a.Features)
	if n < 1 || n > 6 || len(a.Center) != n || len(a.Scale) != n || len(a.Lower) != n || len(a.Upper) != n || len(a.Trees) < 1 || len(a.Trees) > 128 {
		return nil, fmt.Errorf("invalid dimensions")
	}
	m := &Model{artifact: a, hash: hash}
	used := map[string]bool{}
	for i, name := range a.Features {
		idx := -1
		for j, s := range featureNames {
			if s == name {
				idx = j
			}
		}
		if idx < 0 || used[name] || !finite(a.Center[i]) || !finite(a.Scale[i]) || a.Scale[i] <= 0 || !finite(a.Lower[i]) || !finite(a.Upper[i]) || a.Lower[i] > a.Upper[i] {
			return nil, fmt.Errorf("invalid normalization")
		}
		used[name] = true
		m.global = append(m.global, idx)
	}
	for _, t := range a.Trees {
		if len(t.Nodes) < 1 || len(t.Nodes) > 1024 {
			return nil, fmt.Errorf("node bound")
		}
		seen := make([]bool, len(t.Nodes))
		var walk func(int, int) error
		walk = func(i, depth int) error {
			if i < 0 || i >= len(t.Nodes) || depth > 32 || seen[i] {
				return fmt.Errorf("invalid tree graph")
			}
			seen[i] = true
			x := t.Nodes[i]
			if !finite(x.Threshold) || x.Samples < 1 || x.Samples > a.Samples {
				return fmt.Errorf("invalid node")
			}
			if x.Left == -1 && x.Right == -1 {
				if x.Feature != -2 {
					return fmt.Errorf("invalid leaf")
				}
				return nil
			}
			if x.Feature < 0 || x.Feature >= n || x.Left == x.Right {
				return fmt.Errorf("invalid split")
			}
			if e := walk(x.Left, depth+1); e != nil {
				return e
			}
			if e := walk(x.Right, depth+1); e != nil {
				return e
			}
			if t.Nodes[x.Left].Samples+t.Nodes[x.Right].Samples != x.Samples {
				return fmt.Errorf("sample count mismatch")
			}
			return nil
		}
		if e = walk(0, 0); e != nil {
			return nil, e
		}
		if t.Nodes[0].Samples != a.Samples {
			return nil, fmt.Errorf("root sample mismatch")
		}
		for _, s := range seen {
			if !s {
				return nil, fmt.Errorf("unreachable node")
			}
		}
	}
	if maxAge <= 0 {
		maxAge = DefaultMaxAge
	}
	m.maxAge = maxAge
	m.stale = now.Sub(a.TrainedAt) > maxAge
	return m, nil
}
func pathAdjustment(n int) float64 {
	if n <= 1 {
		return 0
	}
	if n == 2 {
		return 1
	}
	return 2*(math.Log(float64(n-1))+0.5772156649015329) - 2*float64(n-1)/float64(n)
}
func (m *Model) Score(v FeatureVector) ScoreResult {
	r := ScoreResult{Status: "unavailable"}
	if m == nil {
		return r
	}
	version := m.artifact.Version
	r.Version = &version
	if !v.valid() {
		return r
	}
	if v.SampleCount < MinimumSamples {
		r.Status = "insufficient_data"
		return r
	}
	if m.stale {
		r.Status = "stale"
		return r
	}
	xs := make([]float64, len(m.global))
	for i, j := range m.global {
		if v.Values[j] == nil {
			r.Status = "insufficient_data"
			return r
		}
		x := (*v.Values[j] - m.artifact.Center[i]) / m.artifact.Scale[i]
		if !finite(x) || x < m.artifact.Lower[i] || x > m.artifact.Upper[i] {
			r.Status = "out_of_distribution"
			return r
		}
		xs[i] = float64(float32(x))
	}
	total := 0.0
	for _, t := range m.artifact.Trees {
		idx, depth := 0, 0
		for {
			n := t.Nodes[idx]
			if n.Left == -1 {
				total += float64(depth) + pathAdjustment(n.Samples)
				break
			}
			if xs[n.Feature] <= n.Threshold {
				idx = n.Left
			} else {
				idx = n.Right
			}
			depth++
		}
	}
	score := math.Pow(2, -total/float64(len(m.artifact.Trees))/pathAdjustment(m.artifact.Samples))
	r.Status = "ready"
	r.Score = &score
	r.Threshold = &m.artifact.Threshold
	return r
}
func (m *Model) Hash() string {
	if m == nil {
		return ""
	}
	return m.hash
}

// ScoreAt rechecks expiry for long-lived workers against server-owned observation time.
// Score uses the validation time provided to Load; mounted workers should call ScoreAt.
func (m *Model) ScoreAt(v FeatureVector, now time.Time) ScoreResult {
	if m != nil && (now.Before(m.artifact.TrainedAt) || now.Sub(m.artifact.TrainedAt) > m.maxAge) {
		version := m.artifact.Version
		return ScoreResult{Version: &version, Status: "stale"}
	}
	return m.Score(v)
}
