package investigator

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"
)

var ErrFreeTierUnverified = errors.New("Gemini free-tier project review is missing or expired")

type GeminiApproval struct {
	ProjectID         string    `json:"projectId"`
	Model             string    `json:"model"`
	KeySHA256         string    `json:"keySha256"`
	BillingDisabled   bool      `json:"billingDisabled"`
	FreeTierAvailable bool      `json:"freeTierAvailable"`
	ReviewedAt        time.Time `json:"reviewedAt"`
	ValidUntil        time.Time `json:"validUntil"`
}

func ReadGeminiApproval(path, key, model string) (GeminiApproval, error) {
	if !filepath.IsAbs(path) {
		return GeminiApproval{}, ErrFreeTierUnverified
	}
	file, err := os.Open(path)
	if err != nil {
		return GeminiApproval{}, ErrFreeTierUnverified
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(raw) > 4096 {
		return GeminiApproval{}, ErrFreeTierUnverified
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var approval GeminiApproval
	if decoder.Decode(&approval) != nil || decoder.Decode(&struct{}{}) != io.EOF || !approval.valid(key, model, time.Now()) {
		return GeminiApproval{}, ErrFreeTierUnverified
	}
	return approval, nil
}
func (a GeminiApproval) valid(key, model string, now time.Time) bool {
	digest := sha256.Sum256([]byte(key))
	return key != "" && a.ProjectID != "" && a.Model == model && (model == "gemini-2.5-flash-lite" || model == "gemini-3.5-flash-lite") && a.KeySHA256 == hex.EncodeToString(digest[:]) && a.BillingDisabled && a.FreeTierAvailable && !a.ReviewedAt.IsZero() && !a.ReviewedAt.After(now.Add(time.Minute)) && a.ValidUntil.After(now) && a.ValidUntil.After(a.ReviewedAt) && !a.ValidUntil.After(a.ReviewedAt.Add(7*24*time.Hour))
}
