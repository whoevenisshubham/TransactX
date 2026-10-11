package investigator

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

type fakeReader struct{ bundle EvidenceBundle }

func (f fakeReader) ReadPaymentEvidence(_ context.Context, scope TrustedScope, id string) (EvidenceBundle, error) {
	if scope.SelectedPaymentID != id || f.bundle.PaymentId != id {
		return EvidenceBundle{}, fmt.Errorf("identity mismatch")
	}
	return f.bundle, nil
}

var _ EvidenceReader = fakeReader{}

func TestImmutableFixturesAndCitations(t *testing.T) {
	var manifest struct {
		Cases []struct{ Path, Sha256 string }
	}
	raw, e := os.ReadFile("testfixtures/manifest.json")
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(raw, &manifest); e != nil {
		t.Fatal(e)
	}
	for _, entry := range manifest.Cases {
		t.Run(entry.Path, func(t *testing.T) {
			raw, e := os.ReadFile(filepath.Join("testfixtures", entry.Path))
			if e != nil {
				t.Fatal(e)
			}
			if fmt.Sprintf("%x", sha256.Sum256(raw)) != entry.Sha256 {
				t.Fatal("fixture hash changed")
			}
			var c struct {
				TrustedPaymentId, ExpectedValidation string
				Evidence                             EvidenceBundle
				Report                               InvestigationReport
			}
			if e = json.Unmarshal(raw, &c); e != nil {
				t.Fatal(e)
			}
			_, e = fakeReader{c.Evidence}.ReadPaymentEvidence(context.Background(), TrustedScope{SelectedPaymentID: c.TrustedPaymentId}, c.TrustedPaymentId)
			if (e != nil) != (c.ExpectedValidation == "reject_identity") {
				t.Fatal("identity expectation")
			}
			ids := map[string]bool{c.Evidence.Payment.EvidenceId: true}
			for _, o := range c.Evidence.Operations {
				if ids[o.EvidenceId] {
					t.Fatal("duplicate evidence ID")
				}
				ids[o.EvidenceId] = true
			}
			if c.Report.SnapshotId != c.Evidence.SnapshotId {
				t.Fatal("snapshot citation")
			}
			for _, f := range c.Report.Facts {
				for _, id := range f.EvidenceIds {
					if !ids[id] {
						t.Fatal("unknown citation")
					}
				}
				for _, a := range f.Assertions {
					if !ids[a.EvidenceId] {
						t.Fatal("unknown assertion citation")
					}
					if a.Field == "central_state" && a.Value != string(c.Evidence.Payment.State) {
						t.Fatal("wrong state citation")
					}
					if a.EntityId == nil || *a.EntityId != c.Evidence.PaymentId {
						t.Fatal("wrong assertion identity")
					}
				}
			}
		})
	}
}
