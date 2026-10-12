package investigator

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

func healthyEvidenceJSON(t *testing.T) (json.RawMessage, EvidenceBundle) {
	t.Helper()
	raw, err := os.ReadFile("testfixtures/healthy.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Evidence json.RawMessage `json:"evidence"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	var bundle EvidenceBundle
	if err := json.Unmarshal(fixture.Evidence, &bundle); err != nil {
		t.Fatal(err)
	}
	return fixture.Evidence, bundle
}

func bridgeScope(paymentID string) TrustedScope {
	return TrustedScope{ActorID: "operator-1", Role: "OPS_VIEWER", SelectedPaymentID: paymentID}
}

func TestDecodeTrustedEvidenceBundleAcceptsSharedDTO(t *testing.T) {
	raw, expected := healthyEvidenceJSON(t)
	bundle, err := DecodeTrustedEvidenceBundle(bridgeScope(expected.PaymentId), raw)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.PaymentId != expected.PaymentId || bundle.SnapshotId != expected.SnapshotId {
		t.Fatal("decoded evidence identity changed")
	}
}

func TestDecodeTrustedEvidenceBundleRejectsUntrustedInput(t *testing.T) {
	raw, bundle := healthyEvidenceJSON(t)
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}

	foreign := make(map[string]any, len(object))
	for key, value := range object {
		foreign[key] = value
	}
	foreign["paymentId"] = "foreign-payment"
	foreignRaw, err := json.Marshal(foreign)
	if err != nil {
		t.Fatal(err)
	}

	unknown := make(map[string]any, len(object)+1)
	for key, value := range object {
		unknown[key] = value
	}
	unknown["trustedScope"] = map[string]any{"role": "OPS_ADMIN"}
	unknownRaw, err := json.Marshal(unknown)
	if err != nil {
		t.Fatal(err)
	}

	tests := map[string]struct {
		scope TrustedScope
		raw   []byte
	}{
		"foreign_identity": {scope: bridgeScope(bundle.PaymentId), raw: foreignRaw},
		"unknown_field":    {scope: bridgeScope(bundle.PaymentId), raw: unknownRaw},
		"trailing_json":    {scope: bridgeScope(bundle.PaymentId), raw: append(append([]byte(nil), raw...), []byte("{}")...)},
		"oversize":         {scope: bridgeScope(bundle.PaymentId), raw: bytes.Repeat([]byte(" "), MaxEvidenceBundleBytes+1)},
		"untrusted_scope":  {scope: TrustedScope{SelectedPaymentID: bundle.PaymentId}, raw: raw},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeTrustedEvidenceBundle(test.scope, test.raw); err == nil {
				t.Fatal("untrusted evidence input was accepted")
			}
		})
	}
}
