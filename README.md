# TransactX

TransactX is a simulated payment infrastructure research prototype. It does not process real money or connect to real banking infrastructure.

## Current Status

### IMPLEMENTED

- Docker-free local project foundation.
- Go API using standard `net/http`.
- PostgreSQL connection and readiness check using `pgx`.
- `GET /health` and `GET /health/db` endpoints.
- React + TypeScript + Vite frontend shell.
- Frontend API connectivity check.
- Approved architecture decisions documented in `docs/decisions.md`.

### PLANNED

- Authentication and customer accounts.
- Payment validation, idempotency, state transitions, and ledger.
- BankAdapter and Bank A.
- Customer payment experience.

### FUTURE WORK

- Bank B, routing, circuit breaker, and chaos engineering (M2).
- Reconciliation, integrity engine, and research console (M3).

## Local Development

Prerequisites:

- Go 1.27+
- Node.js and npm
- PostgreSQL 18 running locally

Create a local database named `transactx`, then configure the connection if your local credentials differ from `.env.example`. Environment variables are read by the processes directly; this repository does not load a `.env` file automatically.

Start the backend:

```powershell
cd backend
go mod download
go run ./cmd/api
```

Start the frontend in another terminal:

```powershell
cd frontend
npm install
npm run dev
```

The API runs at `http://localhost:8080` and the Vite frontend runs at the URL printed by Vite, normally `http://localhost:5173`.

## Repository Rules

- PostgreSQL is authoritative for monetary state.
- Monetary values will use integer paise (`BIGINT`).
- Docker and Redis are not part of the initial architecture.
- Payment, idempotency, ledger, and concurrency code requires human review.