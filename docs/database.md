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
12. `000012_m2_chaos_scenarios` — durable chaos scenarios and lifecycle audit events.
13. `000013_m3_5_recon_runs` — reconciliation run summaries and discrepancy evidence.
14. `000014_m3_7_integrity_runs` — runtime integrity runs and check results.
15. `000015_m3_7_integrity_violations` — structured integrity violation evidence.
16. `000016_m3_7_payment_state_transitions` — immutable payment state transition history.
17. `000017_m3_7_merkle_commitments` — durable derived Merkle commitment state.
18. `000018_m3_reconciliation_metrics` — durable production reconciliation work metrics.
19. `000019_m3_reconciliation_discrepancy_categories` — record-level reconciliation mismatch categories.

Reconciliation completion writes roots, counts, and work metrics in the same `recon_runs` update. `elapsed_ns` measures production execution after the durable `RUNNING` row is created. `nodes_visited` counts canonical and participant Merkle node hashes inspected, including both roots. `records_inspected` counts ledger records loaded from divergent buckets. `bytes_examined` counts the exact returned hash bytes plus canonical serialized bytes for those loaded records; it is an in-process algorithm measure, not estimated network traffic. `divergent_buckets` counts distinct logical buckets with unequal commitment or presence, while `divergent_records` counts distinct logical operations that are missing, extra, or field-divergent. Equal roots therefore record two root nodes, their exact hash bytes, and zero record inspection.

Health samples retain stable execution-target identity, sampled time, availability, measured latency in milliseconds, outcome, and optional correlation metadata. The monitor reads a bounded 15-minute/500-sample window; old raw history remains queryable and is not mixed into the active score. Migration `000010_m2_route_decisions` stores immutable `PAYMENT_ROUTED` records: payment ID, candidate ID, logical source/destination bank ownership (distinguished from switch-level execution target ID), selected score, serialized health snapshot JSON, selection mode (`STATIC` or `ADAPTIVE`), reason code, selection timestamp, and event type. Routing facts never alter participant account ownership or ledger state. The table is also queried during pending payment recovery to deterministically resolve the exact original execution target and its associated adapters, ensuring recovery never guesses or substitutes execution paths. Migration `000011_m2_circuit_transitions` stores immutable `CIRCUIT_STATE_TRANSITION` facts: execution target ID, previous state, new state, transition reason, transition timestamp, failure count, timeout count, consecutive successes, active probes, successful probes, restoration step, cooldown duration (ms), rolling window (ms), metadata JSON, and created timestamp. Indexed on `(execution_target_id, transitioned_at DESC, id DESC)` for fast operational auditing. Circuit events never mutate monetary balances or accounts.

Migration `000012_m2_chaos_scenarios` defines two tables for controlled chaos engineering:
- `chaos_scenarios`: Stores durable scenario definitions including `scenario_id` (unique), `scenario_type` (`BANK_OUTAGE`, `LATENCY`, `TRANSIENT_DROP`, `TEMPORARY_PARTITION`), `target_id`, `parameters` JSONB, `started_at`, `expires_at`, `stopped_at`, `active` boolean, `mode` (strictly `SIMULATION`), `created_by`, `stopped_by`, and audit timestamps. Indexed on `(target_id, active)` and `(active, expires_at)`.
- `chaos_events`: Stores append-only scenario lifecycle transitions: `scenario_id`, `event_type` (`CHAOS_STARTED`, `CHAOS_STOPPED`, `CHAOS_RESET`, `CHAOS_EXPIRED`), `target_id`, `fault_type`, `actor_id`, `actor_role`, `parameters` JSONB, `details` JSONB, and `occurred_at`. Indexed on `(scenario_id, occurred_at DESC)` and `(target_id, occurred_at DESC)`.
All scenario state changes (start, stop, reset, expiry) and corresponding lifecycle audit events are committed atomically within single PostgreSQL transactions (`CreateScenarioWithEvent`, `UpdateScenarioWithEvent`, `ExpireScenario`, `ResetScenarios`). Specifically, `ExpireScenario` performs a single-statement conditional update: `UPDATE chaos_scenarios SET active = false, stopped_at = $1, stopped_by = 'SYSTEM_AUTO_EXPIRY' WHERE scenario_id = $3 AND active = true AND expires_at <= $2 RETURNING...`. Only the winning transaction (1 row returned) inserts the `CHAOS_EXPIRED` event into `chaos_events`. If 0 rows are returned (already expired or concurrent worker won), no duplicate event is written. `ResetScenarios` atomically updates active rows via conditional expressions (`stopped_at = CASE WHEN expires_at <= $1 THEN expires_at ELSE $1 END`, `stopped_by = CASE WHEN expires_at <= $1 THEN 'SYSTEM_AUTO_EXPIRY' ELSE $2 END`), inserting `CHAOS_EXPIRED` for expired rows and `CHAOS_RESET` for future active rows, guaranteeing exactly one terminal event per scenario. If PostgreSQL transaction execution fails, the persistence error is propagated and the controller refuses to prematurely deactivate in-memory state, preventing memory/database divergence. Active scenario lookups retrieve rows where `active = true`, and any discovered rows where `expires_at <= now` are atomically finalized in PostgreSQL (`active = false, stopped_at = expires_at, stopped_by = 'SYSTEM_AUTO_EXPIRY'`) with a single `CHAOS_EXPIRED` event during hydration or start processing. Active listing queries strictly exclude expired rows (`active = true AND expires_at > $now`). Chaos tables are operational only and strictly forbidden from modifying financial authority, ledgers, accounts, or balances.

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

# Release candidate schema additions

## `000020_m3_commitment_owners`

Adds non-null `owner_id` to `merkle_commitments`. Current-state uniqueness is `(owner_id, partition, bucket_width_ns, scope_from, scope_to)`, which permits canonical and participant commitments for one logical bank partition and closed scope to coexist. Production owners are `canonical:<bank-code>` and `participant:<bank-code>`. These rows are derived evidence and have no authority over balances, payments, or ledger entries.

## `000021_payment_transition_order`

Adds non-null `BIGSERIAL sequence_number` to append-only `payment_state_transitions`. `CURRENT_TIMESTAMP` is transaction-stable, so timestamp ordering cannot distinguish several transitions written in one settlement. History now reads by `sequence_number ASC`; the immutability trigger still rejects update and delete.

## Registration and participant accounts

Simulation provisioning writes the central account and matching `bank_a.accounts` or `bank_b.accounts` row in the same PostgreSQL transaction. Both rows use the same UUID and account number and start at zero balance. The participant insert is idempotent by UUID. This atomic rule relies on the simulation schemas sharing PostgreSQL; independently hosted participant databases would require a durable provisioning workflow before the account becomes routable.

Maintained commitment absence is an operational error and never starts an implicit ledger scan. Integrity audit persistence failure also returns an operational failure; an unpersisted check is never reported as completed.
