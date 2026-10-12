package investigator

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// MaxEvidenceBundleBytes bounds the serialization bridge used by the M2
// operations adapter. The bridge exists to keep M3 independent of M2 package
// types while both sides use the frozen tx.evidence.v1 JSON contract.
const MaxEvidenceBundleBytes = 1 << 20

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
