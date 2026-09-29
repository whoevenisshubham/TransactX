package auth

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/transactx/backend/internal/accounts"
	"github.com/transactx/backend/internal/bank"
	"github.com/transactx/backend/internal/bankservice"
	"github.com/transactx/backend/internal/payments"
	"github.com/transactx/backend/internal/recipients"
)

func TestPublicRegistrationRejectsPrivilegedRolesBeforeDatabaseAccess(t *testing.T) {
	service := &Service{}
	for _, role := range []string{"OPS_ADMIN", "UNKNOWN"} {
		_, err := service.Register(context.Background(), RegisterInput{
			Name:              "Test User",
			Phone:             "919876543210",
			PaymentIdentifier: "test@transactx",
			Password:          "password123",
			Role:              role,
		})
		if err == nil || err.Error() != "INVALID_REQUEST" {
			t.Fatalf("role %q was not rejected before database access: %v", role, err)
		}
	}
}

func TestRegistrationProvisioningSupportsRoutedPayment(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var bankID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO banks (id, code, name, status)
		VALUES ($1, 'BANK-A', 'Simulation Bank A', 'ACTIVE')
		ON CONFLICT (code) DO UPDATE SET status = 'ACTIVE'
		RETURNING id`, uuid.New()).Scan(&bankID); err != nil {
		t.Fatal(err)
	}
	manager, err := NewJWTManager("registration-routing-secret-32bytes!", "test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	authService := NewService(pool, manager, "BANK-A").WithParticipantProvisioner(NewSimulationParticipantAccountProvisioner())
	suffix := uuid.NewString()
	payer, err := authService.Register(ctx, RegisterInput{
		Name: "Routed Payer", Phone: "payer-" + suffix[:20], PaymentIdentifier: "payer-" + suffix + "@transactx", Password: "password123", Role: "CUSTOMER",
	})
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := authService.Register(ctx, RegisterInput{
		Name: "Routed Receiver", Phone: "receiver-" + suffix[:20], PaymentIdentifier: "receiver-" + suffix + "@transactx", Password: "password123", Role: "CUSTOMER",
	})
	if err != nil {
		t.Fatal(err)
	}
	var payerAccountID, receiverAccountID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM accounts WHERE user_id = $1`, payer.ID).Scan(&payerAccountID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM accounts WHERE user_id = $1`, receiver.ID).Scan(&receiverAccountID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE accounts SET balance_paise = 5000 WHERE id = $1`, payerAccountID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE bank_a.accounts SET balance_paise = 5000 WHERE id = $1`, payerAccountID); err != nil {
		t.Fatal(err)
	}
	participant := bankservice.NewParticipantService(pool, "BANK-A", "bank_a")
	paymentService := payments.NewServiceWithAdapters(
		accounts.NewRepository(pool), recipients.NewRepository(pool), payments.NewRepository(pool),
		map[uuid.UUID]bank.BankAdapter{bankID: participant},
	)
	payment, duplicate, err := paymentService.CreateWithResult(ctx, payments.CreateInput{
		UserID: payer.ID, SourceAccountID: payerAccountID, Recipient: receiver.PaymentID,
		AmountPaise: 750, Currency: "INR", Note: "registration route", IdempotencyKey: "registration-route-" + suffix,
	})
	if err != nil {
		t.Fatal(err)
	}
	if duplicate || payment.State != payments.StateCompleted {
		t.Fatalf("routed payment result: duplicate=%t state=%s", duplicate, payment.State)
	}
	if payment.SourceBankID == nil || *payment.SourceBankID != bankID || payment.DestinationBankID == nil || *payment.DestinationBankID != bankID {
		t.Fatalf("routed bank ownership missing: source=%v destination=%v", payment.SourceBankID, payment.DestinationBankID)
	}
	var receiverParticipantBalance int64
	if err := pool.QueryRow(ctx, `SELECT balance_paise FROM bank_a.accounts WHERE id = $1`, receiverAccountID).Scan(&receiverParticipantBalance); err != nil {
		t.Fatal(err)
	}
	if receiverParticipantBalance != 750 {
		t.Fatalf("provisioned receiver participant balance = %d, want 750", receiverParticipantBalance)
	}
}

func TestRegistrationProvisioningCreatesParticipantAccountAtomically(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	bankID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO banks (id, code, name, status) VALUES ($1, 'BANK-A', 'Simulation Bank A', 'ACTIVE') ON CONFLICT (code) DO UPDATE SET status = 'ACTIVE'`, bankID); err != nil {
		t.Fatal(err)
	}
	manager, err := NewJWTManager("registration-provisioning-secret-32bytes", "test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(pool, manager, "BANK-A").WithParticipantProvisioner(NewSimulationParticipantAccountProvisioner())
	suffix := uuid.NewString()
	registered, err := service.Register(ctx, RegisterInput{
		Name: "Provisioned User", Phone: suffix[:20], PaymentIdentifier: suffix + "@transactx", Password: "password123", Role: "CUSTOMER",
	})
	if err != nil {
		t.Fatal(err)
	}
	var accountID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT bank_account_id FROM accounts WHERE user_id = $1`, registered.ID).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM bank_a.accounts WHERE id = $1`, accountID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM accounts WHERE user_id = $1`, registered.ID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, registered.ID)
	})
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM bank_a.accounts WHERE id = $1`, accountID).Scan(&status); err != nil {
		t.Fatalf("participant account missing: %v", err)
	}
	if status != "ACTIVE" {
		t.Fatalf("participant status = %s", status)
	}
}
