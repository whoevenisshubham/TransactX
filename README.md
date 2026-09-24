# TransactX

TransactX is a simulated payment infrastructure research prototype. It does not process real money, implement UPI/NPCI, or connect to real banks.

## Current status

Implemented through the current routed-payment milestone:

- Go HTTP API, React + TypeScript frontend shell, JWT authentication, accounts, recipients, and user-scoped idempotency.
- Atomic local PostgreSQL settlement with integer paise, double-entry ledger entries, and concurrency protection.
- A typed `BankAdapter` contract and durable Bank A/Bank B participant processes with independent PostgreSQL schemas, HTTP boundaries, holds, provisional/final credits, operation status, ledgers, and restart-safe operation identity.
- Routed saga persistence for source/destination bank identity and bank operations, deterministic retries, compensation, pending-operation recovery, and central-only recovery after bank settlement.
- Customer payment frontend and API contract: exact decimal-to-paise input, stable idempotency attempts, safe account/payment DTOs, notes, incoming/outgoing history, explicit pending status checks, and transaction details.
- Unit, PostgreSQL-backed integration, and race-detector coverage for the critical payment and bank paths.

Adaptive routing, circuit breakers, chaos orchestration, and offline queue UX are implemented. Reconciliation orchestration, reconciliation APIs, and later M3 work remain future work. Canonical commitment, Merkle bucket, incremental-maintenance, and participant read-boundary foundations are implemented for research use and are not a production reconciliation service.

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
psql $env:DATABASE_URL -f backend/migrations/000007_phase4_bank_b.up.sql
psql $env:DATABASE_URL -f backend/migrations/000008_m1_customer_payment_contract.up.sql
```

Start Bank A, Bank B, and the API in separate terminals. Both participants may use the same PostgreSQL server because they use separate `bank_a` and `bank_b` schemas.

```powershell
cd backend
$env:DATABASE_URL = "postgres://postgres@localhost:5432/transactx?sslmode=disable"
$env:BANK_A_DATABASE_URL = $env:DATABASE_URL
$env:BANK_A_ADDR = ":8081"
go run ./cmd/bank-a
```

```powershell
cd backend
$env:BANK_B_DATABASE_URL = $env:DATABASE_URL
$env:BANK_B_ADDR = ":8082"
go run ./cmd/bank-b
```

```powershell
cd backend
$env:DATABASE_URL = "postgres://postgres@localhost:5432/transactx?sslmode=disable"
$env:JWT_SECRET = "replace-with-at-least-32-random-bytes"
$env:DEFAULT_BANK_CODE = "BANK-DEV-001"
$env:BANK_A_URL = "http://localhost:8081"
$env:BANK_B_URL = "http://localhost:8082"
go run ./cmd/api
```

Start the frontend separately:

```powershell
cd frontend
npm install
npm run dev
```

For development seed data, set `APP_DEVELOPMENT=true` and `DEV_ADMIN_PASSWORD`, then run `go run ./cmd/devseed` from `backend`.
Normal API runs default to `APP_DEVELOPMENT=false`; enable development provisioning explicitly only when running the seed command.

## Design boundaries

- Central PostgreSQL owns payment state, idempotency, central accounts, central ledger, and recovery state.
- Bank A and Bank B independently own participant accounts, balances, holds, operations, participant ledger records, and operation status.
- Routed settlement is a durable saga, not a distributed ACID transaction. Unknown outcomes are resolved with the original operation ID; unresolved effects stay pending reconciliation.
- Monetary values are integer paise (`BIGINT`). Docker, Redis, Kafka, Kubernetes, real banking integrations, AI/ML, blockchain, and real-money movement are intentionally excluded.

## Reconciliation participant boundary

The reconciliation package exposes a separate `ReconciliationParticipant` read boundary. `BankAdapter` remains frozen as the payment-switch contract and is not extended with reconciliation methods.

- A `Scope` is a normalized UTC half-open interval `[From, To)`. Node references carry a participant identifier, deterministic scope identity, and a path (`empty` or `L<level>/<index>`); bucket references carry the versioned `BucketID.String()` key.
- `MemoryParticipant` is a deterministic fixture over logical `CanonicalRecord` values. `RepositoryParticipant` reads the existing participant ledger through `bankservice.Service.GetLedgerSnapshot`; the participant ledger remains authoritative and commitment state is derived, read-only data.
- A participant captures one derived commitment per scope and reuses it across root, child, record, and metadata reads so one boundary read is coherent and does not rebuild the tree repeatedly. Repository-backed commitments must be explicitly initialized (or recovered with `Refresh`); only that path bootstraps from the authoritative ledger. Normal reads load maintained derived state through `IncrementalCommitmentStore` and never call `Bootstrap`.
- Root, node, and bucket references include a process-local commitment generation. `Refresh` creates a new generation and invalidates old references, preventing a child or bucket request from mixing commitment snapshots. Generation values never enter canonical records or Merkle hash inputs. This is a process-local snapshot rule, not distributed transactionality.
- Roots, child nodes, and bucket records use the existing canonical v1 and Merkle v1 implementations. Results are ordered deterministically and exclude snapshot IDs, capture times, and physical database row IDs from canonical content.
- Missing or malformed references return typed reconciliation errors (`ErrNodeNotFound`, `ErrBucketNotFound`, and their invalid-reference counterparts). Participant mismatches are explicit. Every participant method checks and propagates context cancellation and source errors.
