package main

import (
	"reflect"
	"testing"
)

func TestDevelopmentBankCodesIncludesBothConfiguredBanks(t *testing.T) {
	codes, err := developmentBankCodes("BANK-A", "BANK-A", "BANK-B")
	if err != nil {
		t.Fatalf("developmentBankCodes() error = %v", err)
	}
	want := []string{"BANK-A", "BANK-B"}
	if !reflect.DeepEqual(codes, want) {
		t.Fatalf("developmentBankCodes() = %v, want %v", codes, want)
	}
}

func TestDevelopmentBankCodesRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name        string
		defaultCode string
		bankACode   string
		bankBCode   string
	}{
		{name: "missing bank", defaultCode: "BANK-A", bankACode: "BANK-A"},
		{name: "duplicate banks", defaultCode: "BANK-A", bankACode: "BANK-A", bankBCode: "BANK-A"},
		{name: "unknown default", defaultCode: "BANK-C", bankACode: "BANK-A", bankBCode: "BANK-B"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := developmentBankCodes(test.defaultCode, test.bankACode, test.bankBCode); err == nil {
				t.Fatal("developmentBankCodes() error = nil, want error")
			}
		})
	}
}
