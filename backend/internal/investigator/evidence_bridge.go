package investigator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// MaxEvidenceBundleBytes bounds the serialization bridge used by the M2
// operations adapter. The bridge exists to keep M3 independent of M2 package
// types while both sides use the frozen tx.evidence.v1 JSON contract.
const MaxEvidenceBundleBytes = 1 << 20

// EvidenceJSONSource is the narrow composition seam implemented around M2's
// operations reader. It receives only authenticated server scope and returns a
// serialized tx.evidence.v1 DTO; it cannot bypass M3's strict decoder.
type EvidenceJSONSource interface {
	ReadPaymentEvidenceJSON(context.Context, TrustedScope, string) ([]byte, error)
}

// BridgedEvidenceReader turns an M2 composition wrapper into the EvidenceReader
// required by the M3 service. Scope and payment identity are checked before the
// source runs, then checked again against the decoded bundle.
type BridgedEvidenceReader struct {
	Source EvidenceJSONSource
}

func (reader BridgedEvidenceReader) ReadPaymentEvidence(ctx context.Context, scope TrustedScope, paymentID string) (EvidenceBundle, error) {
	if err := validateTrustedScope(scope); err != nil || paymentID != scope.SelectedPaymentID {
		return EvidenceBundle{}, ErrScopeViolation
	}
	if reader.Source == nil {
		return EvidenceBundle{}, errors.New("evidence JSON source is required")
	}
	raw, err := reader.Source.ReadPaymentEvidenceJSON(ctx, scope, paymentID)
	if err != nil {
		return EvidenceBundle{}, err
	}
	return DecodeTrustedEvidenceBundle(scope, raw)
}

var _ EvidenceReader = BridgedEvidenceReader{}

// DecodeTrustedEvidenceBundle strictly decodes a shared evidence DTO and binds
// it to authenticated server scope. Callers must construct scope from trusted
// middleware and must not accept its fields from an HTTP or model payload.
func DecodeTrustedEvidenceBundle(scope TrustedScope, raw []byte) (EvidenceBundle, error) {
	if err := validateTrustedScope(scope); err != nil {
		return EvidenceBundle{}, err
	}
	if len(raw) == 0 {
		return EvidenceBundle{}, errors.New("empty evidence bundle")
	}
	if len(raw) > MaxEvidenceBundleBytes {
		return EvidenceBundle{}, errors.New("evidence bundle exceeds byte bound")
	}

	var bundle EvidenceBundle
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&bundle); err != nil {
		return EvidenceBundle{}, fmt.Errorf("decode evidence bundle: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return EvidenceBundle{}, errors.New("decode evidence bundle: trailing JSON")
	}
	if err := validateEvidenceBundle(scope, scope.SelectedPaymentID, bundle); err != nil {
		return EvidenceBundle{}, err
	}
	return bundle, nil
}
