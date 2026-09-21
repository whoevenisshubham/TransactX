# Database

## Ownership

The central database and Bank A/Bank B schemas are separate authority domains even when they run on the same local PostgreSQL server.

Central PostgreSQL owns `users`, `banks`, central `accounts`, `payments`, `idempotency_records`, `ledger_transactions`, `ledger_entries`, and `payment_bank_operations`. Central `accounts.bank_account_id` maps a logical central account to its participant account; existing rows are backfilled to the same UUID for compatibility.

Each participant owns its own `accounts`, `operations`, and `ledger_entries` tables in its schema. Participant balances are never updated by the central ledger transaction. Bank-side operation records contain bank ID, payment ID, stable operation ID, idempotency key, operation type, participant account ID, amount, currency, related operation IDs, status, and bank reference.

## Migrations

Apply the explicit SQL migrations in order:

1. `000001_phase1a_payment_core`
2. `000002_account_opening_balance`
3. `000003_local_settlement_state`
4. `000004_m1_6_routed_payment_boundary` — route identity, central operation tracking, and Bank A schema.
5. `000005_m1_6_account_identity_hardening` — participant account mapping and operation tracking payload fields.
6. `000006_m1_6_bank_operation_identity` — explicit Bank A identity on every durable operation row.
7. `000007_phase4_bank_b` — independent Bank B participant schema with the same durable boundary.

8. `000008_m1_customer_payment_contract` — payment note/origin fields, history lookup index, and customer contract support.
9. `000009_m2_health_samples` — raw observational health samples and recent-window indexes.
10. `000010_m2_route_decisions` — immutable route-decision history.
11. `000011_m2_circuit_transitions` — immutable circuit breaker state transition facts.

Health samples retain stable execution-target identity, sampled time, availability, measured latency in milliseconds, outcome, and optional correlation metadata. The monitor reads a bounded 15-minute/500-sample window; old raw history remains queryable and is not mixed into the active score. Migration `000010_m2_route_decisions` stores immutable `PAYMENT_ROUTED` records: payment ID, candidate ID, logical source/destination bank ownership (distinguished from switch-level execution target ID), selected score, serialized health snapshot JSON, selection mode (`STATIC` or `ADAPTIVE`), reason code, selection timestamp, and event type. Routing facts never alter participant account ownership or ledger state. The table is also queried during pending payment recovery to deterministically resolve the exact original execution target and its associated adapters, ensuring recovery never guesses or substitutes execution paths. Migration `000011_m2_circuit_transitions` stores immutable `CIRCUIT_STATE_TRANSITION` facts: execution target ID, previous state, new state, transition reason, transition timestamp, failure count, timeout count, consecutive successes, active probes, successful probes, restoration step, cooldown duration (ms), rolling window (ms), metadata JSON, and created timestamp. Indexed on `(execution_target_id, transitioned_at DESC, id DESC)` for fast operational auditing. Circuit events never mutate monetary balances or accounts.

The API and Bank A process do not run migrations automatically at startup.

## Monetary and transaction invariants

- All amounts are positive integer paise in `BIGINT` columns; currency is `INR`.
- Account balances are non-negative and mutate under PostgreSQL row locks/conditional updates.
- A completed central transfer has one debit and one credit for the same amount.
- A provisional Bank A credit is durable and ledger-visible but does not change spendable balance. Finalization adds the spendable balance; reversal is a separate durable compensation operation.
- Idempotency is scoped to `(user_id, key)` centrally and to `(operation_id, idempotency_key)` at Bank A. Equivalent retries replay the original result; payload conflicts are rejected.
- Customer payment history is scoped to payments where the authenticated customer owns either side of the transfer; the API derives explicit `SENT`/`RECEIVED` direction and does not expose internal account, bank, or operation identifiers. Notes are limited to 280 characters and are included in the idempotency request hash.
- Central settlement is atomic within central PostgreSQL. Bank calls and central settlement are separate commits coordinated by the durable saga.

## Deterministic participant ledger

Bank A ledger entries include operation ID, payment ID, account ID, entry type, amount, currency, and occurrence time. `GetLedgerSnapshot` orders records by timestamp and row ID, providing stable correlation data for a later reconciliation/Merkle phase (official Phase 5) without implementing that algorithm in M1.
