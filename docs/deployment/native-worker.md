# Native Worker Operations and Runbook

## 1. Overview and Architecture

The TransactX native worker (`cmd/worker`) is the background processing daemon responsible for asynchronous platform reliability and durability tasks, starting with durable recovery scheduling (Task B3/B4) and later outbox delivery and analytics.

### Architectural Invariants
- **No Public HTTP Listener:** The worker does NOT expose an HTTP port. Its health and operational presence are tracked via durable heartbeats in `worker_heartbeats`.
- **Shared Composition (`internal/app`):** The worker and the API share identical participant, adapter, chaos, circuit, and payments wiring through `internal/app.BuildEnvironment`.
- **Single Circuit Authority:** The worker never duplicates circuit breaker authority or re-runs route selection. Routed payments in progress resume their already-persisted target.
- **Lease-Backed Execution:** The worker claims jobs using PostgreSQL `FOR UPDATE SKIP LOCKED` with explicit lease epochs. If a lease expires, another worker can safely pick up the task without risking double monetary mutation.

---

## 2. Configuration Contract

The worker respects the platform environment contract defined in `docs/next-gen/DEPLOYMENT_AND_BACKUP.md`:

| Variable | Default | Description & Invariant |
| --- | --- | --- |
| `DATABASE_URL` | *Required* | PostgreSQL connection string |
| `TX_DB_MAX_CONNS` | `8` | Maximum database pool connections |
| `TX_RECOVERY_ENABLED` | `true` | Enables or disables recovery processing |
| `TX_RECOVERY_CONCURRENCY` | `2` | Max concurrent recovery continuations (must be $\le \text{DBMaxConns} - 2$) |
| `TX_RECOVERY_STALE_AFTER` | `30s` | Stale payment discovery threshold |
| `TX_RECOVERY_SCAN_INTERVAL` | `10s` | Discovery scan interval |
| `TX_RECOVERY_LEASE` | `60s` | Monotonic lease duration |
| `TX_RECOVERY_HEARTBEAT` | `15s` | Worker heartbeat interval ($< \text{LeaseDuration}$) |
| `TX_RECOVERY_ATTEMPT_TIMEOUT`| `20s` | Timeout per continuation attempt ($< \text{LeaseDuration}$) |
| `TX_RECOVERY_MAX_ATTEMPTS` | `20` | Max attempts before safe parking in `needs_attention` |
| `TX_JOB_FAMILIES` | `recovery` | Comma-separated list of enabled job families |
| `TX_WORKER_LABEL` | `transactx-worker` | Sanitized human-readable worker identity |
| `TX_BUILD_SHA` | `dev` | Build commit SHA recorded in telemetry |

---

## 3. Operations Runbook

### Starting the Worker
```bash
# Build
go build -o transactx-worker ./cmd/worker

# Run with environment file or systemd
./transactx-worker
```

### Graceful Shutdown
The worker traps `SIGINT` (Ctrl+C) and `SIGTERM`:
1. Immediately closes claim loop to prevent acquiring new jobs.
2. Waits for currently in-flight continuations to complete.
3. Flushes final heartbeat to `worker_heartbeats`.
4. Closes database pool cleanly within 5 seconds.

### Health and Monitoring
Because the worker has no HTTP port, operational health is verified via:
1. **Heartbeat Table:**
   ```sql
   SELECT worker_id, worker_label, last_seen_at, enabled_job_families, build_sha
   FROM worker_heartbeats
   WHERE last_seen_at > NOW() - INTERVAL '45 seconds';
   ```
2. **Queue Depth & Attention Queue:**
   ```sql
   SELECT status, COUNT(*), MIN(due_at) AS oldest_due
   FROM recovery_jobs
   GROUP BY status;
   ```
3. **API Independence:**
   The API `/health` and `/health/db` endpoints remain fully functional regardless of worker state.

---

## 4. Rollback and Disaster Recovery

If the worker must be taken down immediately:
1. **Stop Worker Service:** Stop the process (`systemctl stop transactx-worker` or terminate process).
2. **Disable via Configuration:** Set `TX_RECOVERY_ENABLED=false` or `TX_JOB_FAMILIES=""`.
3. **Safe Fallback:** The platform financial kernel is fail-safe. User duplicate requests continue to trigger safe request-driven recovery via `payments.Service.Resume()`.
4. **Parked Records:** All recovery jobs and attempts in PostgreSQL are preserved intact as durable audit evidence.
