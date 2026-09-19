# M1 Handoff Contract

This document records the current Member-1 implementation boundary as of the `feat/m1-customer-frontend` branch. It describes only behavior that exists in the repository. Future phases are listed as out of scope, not as incomplete M1 work.

## Architecture boundary

- Central Go API + PostgreSQL own users, JWT/RBAC, central accounts, payments, user-scoped idempotency, central double-entry ledger, route metadata, bank-operation tracking, and recovery state.
- Bank A and Bank B are separate Go processes with independent `bank_a` / `bank_b` schemas and HTTP boundaries.
- Two execution paths:
  - **Local settlement** when no matching adapters exist for both source and destination bank IDs.
  - **Routed saga** when both participant bank IDs have configured adapters.
- Fresh registration creates a central account under `DEFAULT_BANK_CODE` (default `BANK-DEV-001`) with balance 0. It does **not** auto-provision Bank A/B participant rows.
- Customer payment creation selects the server-side primary owned account. Clients cannot supply `sourceAccountId`.

## BankAdapter contract

Frozen in `backend/internal/bank/adapter.go`:

- `GetHealth`
- `ResolveAccount`
- `HoldFunds`
- `ProvisionalCredit`
- `ConfirmHold`
- `ReleaseHold`
- `ReverseProvisionalCredit`
- `GetOperationStatus`
- `GetLedgerSnapshot`

No SQL or HTTP types cross this domain boundary. Typed error codes: `INSUFFICIENT_FUNDS`, `INVALID_ACCOUNT`, `INACTIVE_ACCOUNT`, `BANK_UNAVAILABLE`, `TRANSIENT_FAILURE`, `PERMANENT_FAILURE`. Operation statuses: `SUCCEEDED`, `FAILED`, `PENDING`.

## Payment states and transitions

Authoritative transitions live in `payments.CanTransition` / `Transition`, enforced by `updateState` under `FOR UPDATE`.

Local path: `CREATED -> VALIDATING -> LOCAL_SETTLEMENT -> COMMITTED -> COMPLETED`

Routed path: `CREATED -> VALIDATING -> ROUTING -> PROCESSING` then hold → provisional credit → confirm hold → finalize credit → central settle → `COMMITTED -> COMPLETED`

Unresolved / recovery:

- `PROCESSING | PENDING_RECONCILIATION` for unknown/timeout/malformed bank outcomes
- `BANK_SETTLED_CENTRAL_PENDING` when banks succeeded but central persistence failed (central-only repair; no bank replay)
- Illegal: `PENDING_RECONCILIATION -> COMPLETED` (must go via `COMMITTED`)

## Idempotency and request hash

- Uniqueness: PostgreSQL `UNIQUE (user_id, key)`
- Canonical hash includes: server-selected source account ID, normalized recipient, amount paise, currency, note
- Exact retry replays the same payment; changed payload returns `409 IDEMPOTENCY_CONFLICT`
- Routed operation IDs are deterministic SHA1 namespaced IDs per payment + logical step; retries reuse them

## Pending / unknown semantics

- Timeout / lost response → transient / pending, **not** definite failure
- Unknown bank status (e.g. `WHATEVER`) → pending op + `PENDING_RECONCILIATION`, no downstream monetary step
- Wrong `PaymentID` / `OperationID` correlation → pending, no continuation
- Customer UI never treats network loss as “Payment failed”; uncertain screen reuses the same idempotency key

## Customer API contracts

Authenticated customer routes:

- `POST /api/payments` — `Idempotency-Key` required; body `{recipient, amountPaise, currency, note?}`
- `GET /api/payments`
- `GET /api/payments/{paymentID}`
- `GET /api/accounts`
- `GET /api/accounts/{accountID}`
- `GET /api/recipients/{paymentIdentifier}`

Envelope: `{ requestId, data }` or `{ requestId, error: { code, message } }`

Customer payment DTO fields: `id`, `amountPaise`, `currency`, `note`, `origin`, `state`, `createdAt`, `completedAt`, `failureReason`, sender/receiver names + payment identifiers, `direction`, optional bank name/code, optional `durationMs`.

Account DTO fields: `accountNumber`, `balancePaise`, `status`.

Internal UUIDs (account IDs, bank IDs, bank-operation IDs) are not customer-visible.

HTTP status notes: `201` create, `200` idempotent replay, `202` intermediate (`PROCESSING`, `PENDING_RECONCILIATION`, `BANK_SETTLED_CENTRAL_PENDING`).

## Error safety

- Internal DB/network/stack details → structured logs (`request_id`, `payment_id`, `old_state`, `new_state`, …)
- Customer `failure_reason` / API error messages → short safe strings via `customerSafeFailureReason` / `writePaymentError`

## Database / migrations

Apply in order:

1. `000001_phase1a_payment_core`
2. `000002_account_opening_balance`
3. `000003_local_settlement_state`
4. `000004_m1_6_routed_payment_boundary`
5. `000005_m1_6_account_identity_hardening`
6. `000006_m1_6_bank_operation_identity`
7. `000007_phase4_bank_b`
8. `000008_m1_customer_payment_contract`

## Concurrency / acceptance tests

With PostgreSQL available:

```powershell
$env:DATABASE_URL = "postgres://postgres@localhost:5432/transactx?sslmode=disable"
cd backend
go test -v ./internal/payments/ -run "TestK"
go test -v ./internal/http/ -run "TestCustomerHTTP|TestK15"
go test -race ./...
```

Without `DATABASE_URL`, DB-gated tests skip; pure-logic K tests still run.

## Development seed / reset

```powershell
$env:APP_DEVELOPMENT = "true"
$env:DEV_ADMIN_PASSWORD = "choose-a-strong-password"
$env:DATABASE_URL = "postgres://postgres@localhost:5432/transactx?sslmode=disable"
cd backend
go run ./cmd/devseed
```

## Known intentional M1 limitations

- No automatic Bank A/B participant provisioning on registration
- No Merkle reconciliation (Phase 5)
- No offline queue / IndexedDB (Phase 6)
- No adaptive routing / circuit breaker (Phase 7)
- No chaos controller (Phase 8)
- No integrity engine / Network Console (Phase 9)
- No merchant dashboard / QR (Phase 10)
- No Kafka, Redis, Docker, Kubernetes, real UPI/NPCI, or real bank connectivity
- Observability is structured logs + request IDs only (no event bus)

## Future-phase boundaries (official roadmap)

| Phase | Scope |
|------|--------|
| 5 | Merkle reconciliation |
| 6 | Offline-first client & safe replay |
| 7 | Adaptive routing & circuit breaker |
| 8 | Chaos |
| 9 | Integrity / console |
| 10 | Merchant / product |
| 11 | Benchmarking / evidence |
| 12 | Release |

Do not confuse the internal M1 routed-payment milestone with official Phase 6.
