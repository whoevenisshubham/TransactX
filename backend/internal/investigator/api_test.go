package investigator

import (
	"errors"
	"strings"
	"testing"
)

func TestDecodeCreateInvestigationRequestStrictlyBoundsPublicFields(t *testing.T) {
	valid := `{"paymentId":"00000000-0000-4000-8000-000000000001","question":"What happened?"}`
	request, err := DecodeCreateInvestigationRequest(strings.NewReader(valid))
	if err != nil || request.PaymentId == "" || request.Question != "What happened?" {
		t.Fatalf("valid request = %+v, err=%v", request, err)
	}

	for name, body := range map[string]string{
		"actor injection": valid[:len(valid)-1] + `,"actorId":"foreign"}`,
		"role injection":  valid[:len(valid)-1] + `,"role":"OPS_ADMIN"}`,
		"mode injection":  valid[:len(valid)-1] + `,"mode":"llm"}`,
		"unknown field":   valid[:len(valid)-1] + `,"unexpected":true}`,
		"trailing value":  valid + ` {}`,
		"malformed":       `{"paymentId":`,
		"empty":           ``,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeCreateInvestigationRequest(strings.NewReader(body)); !errors.Is(err, ErrInvalidCreateInvestigationRequest) {
				t.Fatalf("decode error = %v", err)
			}
		})
	}

	oversized := strings.Repeat(" ", int(MaxCreateInvestigationRequestBytes)+1)
	if _, err := DecodeCreateInvestigationRequest(strings.NewReader(oversized)); !errors.Is(err, ErrInvalidCreateInvestigationRequest) {
		t.Fatalf("oversized decode error = %v", err)
	}
}
