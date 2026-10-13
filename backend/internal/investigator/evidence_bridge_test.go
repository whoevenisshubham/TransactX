package investigator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

type recordingEvidenceJSONSource struct {
	raw   []byte
	err   error
	calls int
	scope TrustedScope
	id    string
}

func (source *recordingEvidenceJSONSource) ReadPaymentEvidenceJSON(_ context.Context, scope TrustedScope, paymentID string) ([]byte, error) {
	source.calls++
	source.scope = scope
	source.id = paymentID
	return source.raw, source.err
}

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

func TestBridgedEvidenceReaderValidatesBeforeAndAfterSource(t *testing.T) {
	raw, expected := healthyEvidenceJSON(t)
	source := &recordingEvidenceJSONSource{raw: raw}
	reader := BridgedEvidenceReader{Source: source}
	scope := bridgeScope(expected.PaymentId)

	bundle, err := reader.ReadPaymentEvidence(context.Background(), scope, expected.PaymentId)
	if err != nil {
		t.Fatal(err)
	}
	if source.calls != 1 || source.scope != scope || source.id != expected.PaymentId || bundle.SnapshotId != expected.SnapshotId {
		t.Fatal("bridge did not preserve trusted source inputs and evidence identity")
	}

	if _, err := reader.ReadPaymentEvidence(context.Background(), scope, "foreign-payment"); !errors.Is(err, ErrScopeViolation) {
		t.Fatalf("foreign request: got %v, want scope violation", err)
	}
	if source.calls != 1 {
		t.Fatal("source ran before foreign payment scope was rejected")
	}

	badSource := &recordingEvidenceJSONSource{raw: []byte(`{"paymentId":"foreign-payment"}`)}
	if _, err := (BridgedEvidenceReader{Source: badSource}).ReadPaymentEvidence(context.Background(), scope, expected.PaymentId); err == nil {
		t.Fatal("invalid source bundle was accepted")
	}
	if badSource.calls != 1 {
		t.Fatal("valid trusted request did not reach source")
	}
}

func TestBridgedEvidenceReaderPropagatesSourceFailure(t *testing.T) {
	_, expected := healthyEvidenceJSON(t)
	want := errors.New("operations unavailable")
	source := &recordingEvidenceJSONSource{err: want}
	_, err := (BridgedEvidenceReader{Source: source}).ReadPaymentEvidence(context.Background(), bridgeScope(expected.PaymentId), expected.PaymentId)
	if !errors.Is(err, want) {
		t.Fatalf("got %v, want source failure", err)
	}
	if _, err := (BridgedEvidenceReader{}).ReadPaymentEvidence(context.Background(), bridgeScope(expected.PaymentId), expected.PaymentId); err == nil {
		t.Fatal("nil source was accepted")
	}
}
