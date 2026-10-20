package investigator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

var ErrFreeTierUnverified = errors.New("Gemini owner review is missing or does not match configuration")

// GeminiReview records an owner's actual review, not independently verified Google
// billing state. It has no calendar expiry. Configuration changes require review.
type GeminiReview struct {
	Provider          string    `json:"provider"`
	ProjectID         string    `json:"projectId"`
	Model             string    `json:"model"`
	KeySHA256         string    `json:"keySha256"`
	BillingDisabled   bool      `json:"billingDisabled"`
	FreeTierAvailable bool      `json:"freeTierAvailable"`
	ReviewedAt        time.Time `json:"reviewedAt"`
}

func ReadGeminiReview(raw, key, provider, project, model string) (GeminiReview, error) {
	if len(raw) == 0 || len(raw) > 4096 {
		return GeminiReview{}, ErrFreeTierUnverified
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var review GeminiReview
	if decoder.Decode(&review) != nil || decoder.Decode(&struct{}{}) != io.EOF || !review.valid(key, provider, project, model, time.Now()) {
		return GeminiReview{}, ErrFreeTierUnverified
	}
	return review, nil
}

func (r GeminiReview) valid(key, provider, project, model string, now time.Time) bool {
	digest := sha256.Sum256([]byte(key))
	return key != "" && provider == "google" && r.Provider == provider && project != "" && r.ProjectID == project && r.Model == model &&
		(model == "gemini-2.5-flash-lite" || model == "gemini-3.5-flash-lite") && r.KeySHA256 == hex.EncodeToString(digest[:]) &&
		r.BillingDisabled && r.FreeTierAvailable && !r.ReviewedAt.IsZero() && !r.ReviewedAt.After(now.Add(time.Minute))
}
