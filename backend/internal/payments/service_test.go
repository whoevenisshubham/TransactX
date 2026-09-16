package payments

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestCreateRejectsInvalidRequestBeforeDatabaseAccess(t *testing.T) {
	service := &Service{}
	validUserID := uuid.New()
	validAccountID := uuid.New()
	tests := []struct {
		name  string
		input CreateInput
	}{
		{"missing user", CreateInput{SourceAccountID: validAccountID, Recipient: "bob@transactx", AmountPaise: 100, Currency: "INR"}},
		{"zero amount", CreateInput{UserID: validUserID, SourceAccountID: validAccountID, Recipient: "bob@transactx", AmountPaise: 0, Currency: "INR"}},
		{"negative amount", CreateInput{UserID: validUserID, SourceAccountID: validAccountID, Recipient: "bob@transactx", AmountPaise: -1, Currency: "INR"}},
		{"unsupported currency", CreateInput{UserID: validUserID, SourceAccountID: validAccountID, Recipient: "bob@transactx", AmountPaise: 100, Currency: "USD"}},
		{"empty recipient", CreateInput{UserID: validUserID, SourceAccountID: validAccountID, AmountPaise: 100, Currency: "INR"}},
		{"oversized idempotency key", CreateInput{UserID: validUserID, SourceAccountID: validAccountID, Recipient: "bob@transactx", AmountPaise: 100, Currency: "INR", IdempotencyKey: string(make([]byte, 256))}},
		{"missing idempotency key", CreateInput{UserID: validUserID, SourceAccountID: validAccountID, Recipient: "bob@transactx", AmountPaise: 100, Currency: "INR"}},
		{"control character in idempotency key", CreateInput{UserID: validUserID, SourceAccountID: validAccountID, Recipient: "bob@transactx", AmountPaise: 100, Currency: "INR", IdempotencyKey: "key\nvalue"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := service.Create(context.Background(), test.input); err != ErrInvalidRequest {
				t.Fatalf("Create returned %v, want ErrInvalidRequest", err)
			}
		})
	}
}

func TestPaymentRequestHashIsCanonicalForLogicalRequest(t *testing.T) {
	source := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	first := paymentRequestHash(source, "bob@transactx", 100, "INR")
	second := paymentRequestHash(source, "bob@transactx", 100, "INR")
	if first != second {
		t.Fatalf("same logical request hashes differ: %q != %q", first, second)
	}
	if first == paymentRequestHash(source, "bob@transactx", 101, "INR") {
		t.Fatal("different amount produced the same request hash")
	}
	if first == paymentRequestHash(uuid.New(), "bob@transactx", 100, "INR") {
		t.Fatal("different source account produced the same request hash")
	}
}
