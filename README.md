# TransactX

TransactX is a simulated payment infrastructure research prototype. It does not process real money, implement UPI/NPCI, or connect to real banks.

## Current status

Implemented through the routed Phase-6 boundary:

- Go HTTP API, React + TypeScript frontend shell, JWT authentication, accounts, recipients, and user-scoped idempotency.
- Atomic local PostgreSQL settlement with integer paise, double-entry ledger entries, and concurrency protection.
- A typed `BankAdapter` contract and durable Bank A process with its own PostgreSQL schema, HTTP boundary, holds, provisional/final credits, operation status, ledger, and restart-safe operation identity.
- Routed saga persistence for source/destination bank identity and bank operations, deterministic retries, compensation, pending-operation recovery, and central-only recovery after bank settlement.
- Unit, PostgreSQL-backed integration, and race-detector coverage for the critical payment and bank paths.

Phase-7 routing intelligence, circuit breakers, Bank B, Merkle reconciliation, chaos orchestration, and offline queue UX remain future work.

## Local development

Prerequisites:

- Go 1.27+
- Node.js and npm
- PostgreSQL 18 running locally

Create a local database named `transactx`, apply migrations in order with `psql`, and configure the processes. The repository does not load a `.env` file automatically.

```powershell
$env:DATABASE_URL = "postgres://postgres@localhost:5432/transactx?sslmode=disable"
psql $env:DATABASE_URL -f backend/migrations/000001_phase1a_payment_core.up.sql
psql $env:DATABASE_URL -f backend/migrations/000002_account_opening_balance.up.sql
psql $env:DATABASE_URL -f backend/migrations/000003_local_settlement_state.up.sql
psql $env:DATABASE_URL -f backend/migrations/000004_m1_6_routed_payment_boundary.up.sql
psql $env:DATABASE_URL -f backend/migrations/000005_m1_6_account_identity_hardening.up.sql
psql $env:DATABASE_URL -f backend/migrations/000006_m1_6_bank_operation_identity.up.sql
```

Start Bank A and the API in separate terminals. `BANK_A_DATABASE_URL` may point to the same PostgreSQL server because the participant uses the separate `bank_a` schema.

```powershell
cd backend
$env:DATABASE_URL = "postgres://postgres@localhost:5432/transactx?sslmode=disable"
$env:BANK_A_DATABASE_URL = $env:DATABASE_URL
$env:BANK_A_ADDR = ":8081"
go run ./cmd/bank-a
```

```powershell
cd backend
$env:DATABASE_URL = "postgres://postgres@localhost:5432/transactx?sslmode=disable"
$env:JWT_SECRET = "replace-with-at-least-32-random-bytes"
$env:DEFAULT_BANK_CODE = "BANK-DEV"
$env:BANK_A_URL = "http://localhost:8081"
go run ./cmd/api
```

Start the frontend separately:

```powershell
cd frontend
npm install
npm run dev
```

For development seed data, set `APP_DEVELOPMENT=true` and `DEV_ADMIN_PASSWORD`, then run `go run ./cmd/devseed` from `backend`.

## Design boundaries

- Central PostgreSQL owns payment state, idempotency, central accounts, central ledger, and recovery state.
- Bank A owns participant accounts, balances, holds, operations, participant ledger records, and operation status.
- Routed settlement is a durable saga, not a distributed ACID transaction. Unknown outcomes are resolved with the original operation ID; unresolved effects stay pending reconciliation.
- Monetary values are integer paise (`BIGINT`). Docker, Redis, Kafka, Kubernetes, real banking integrations, AI/ML, blockchain, and real-money movement are intentionally excluded.
