# Database

## Ownership

The central database and Bank A schema are separate authority domains even when they run on the same local PostgreSQL server.

Central PostgreSQL owns `users`, `banks`, central `accounts`, `payments`, `idempotency_records`, `ledger_transactions`, `ledger_entries`, and `payment_bank_operations`. Central `accounts.bank_account_id` maps a logical central account to its participant account; existing rows are backfilled to the same UUID for compatibility.

Bank A owns `bank_a.accounts`, `bank_a.operations`, and `bank_a.ledger_entries`. Participant balances are never updated by the central ledger transaction. Bank-side operation records contain bank ID, payment ID, stable operation ID, idempotency key, operation type, participant account ID, amount, currency, related operation IDs, status, and bank reference.

## Migrations

Apply the explicit SQL migrations in order:

1. `000001_phase1a_payment_core`
2. `000002_account_opening_balance`
3. `000003_local_settlement_state`
4. `000004_m1_6_routed_payment_boundary` — route identity, central operation tracking, and Bank A schema.
5. `000005_m1_6_account_identity_hardening` — participant account mapping and operation tracking payload fields.
6. `000006_m1_6_bank_operation_identity` — explicit Bank A identity on every durable operation row.

The API and Bank A process do not run migrations automatically at startup.

## Monetary and transaction invariants

- All amounts are positive integer paise in `BIGINT` columns; currency is `INR`.
- Account balances are non-negative and mutate under PostgreSQL row locks/conditional updates.
- A completed central transfer has one debit and one credit for the same amount.
- A provisional Bank A credit is durable and ledger-visible but does not change spendable balance. Finalization adds the spendable balance; reversal is a separate durable compensation operation.
- Idempotency is scoped to `(user_id, key)` centrally and to `(operation_id, idempotency_key)` at Bank A. Equivalent retries replay the original result; payload conflicts are rejected.
- Central settlement is atomic within central PostgreSQL. Bank calls and central settlement are separate commits coordinated by the durable saga.

## Deterministic participant ledger

Bank A ledger entries include operation ID, payment ID, account ID, entry type, amount, currency, and occurrence time. `GetLedgerSnapshot` orders records by timestamp and row ID, providing stable correlation data for a later reconciliation/Merkle phase without implementing that algorithm in Phase 6.
