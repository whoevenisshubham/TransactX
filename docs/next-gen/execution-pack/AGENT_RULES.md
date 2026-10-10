# TransactX implementation rules

This is the shared operating contract. Install its concise repository equivalent as `AGENTS.md` only after checking for existing applicable instructions; preserve compatible existing rules. Current human instructions and the actual current implementation take precedence over an outdated plan statement.

1. Continue from the canonical branch. Read `SOURCE_BASELINE.md`; inspect only the next task's source files and tests. Never resume stale integration branches.
2. Protect integer money, central authority, participant ownership, user-scoped idempotency, stable operation identities, and read-only reconciliation. Unknown bank outcome is never proof of failure.
3. Implement your assigned task IDs. Read their required documents. Do not independently redesign the stack or add unrelated features.
4. M2 owns payment code, central API composition, shared backend configuration, Go dependencies, migrations, and shared API contracts. M1 owns frontend, deployment, integration, and final documentation. M3 owns investigator and anomaly packages, corpus, and their tests. Shared-file changes go through the named owner.
5. Use isolated checkouts and owner branches. No force-push, history backdating, false authors, empty activity commits, or invented benchmark results. Commits describe actual changes in plain English; preserve any required agent attribution.
6. Financial effects must serialize through one shared per-payment recovery entry point used by request retries and workers. Leases schedule work; they do not replace monetary serialization.
7. Outbox event insertion is atomic with its source fact. Consumption is at least once and projections are idempotent. Do not claim exactly-once transport.
8. GET evidence tools are read-only. AI tools receive trusted authorization context outside model arguments. No SQL, shell, arbitrary URL, arbitrary file, or mutation tool is available to the investigator.
9. Use native Go, Node, PostgreSQL, and Linux services. Preserve existing GitHub Actions PostgreSQL service containers; they do not introduce a local Docker requirement. Defer Docker and Kubernetes deployment until separately requested.
10. Runtime LLM calls and paid cloud resources remain disabled unless the owners configure and authorize a budget. Never use a trial as a permanent free dependency.
11. Use real tests at risky boundaries. Verify PostgreSQL integration actually ran; unset `DATABASE_URL` is not a database pass. Inspect skipped tests and exit codes.
12. Do not weaken assertions or delete tests to get green CI. Reproduce a failure; fix the cause; retest the affected boundary. Keep expected failures separate from unexpected failures.
13. Record concise task checkpoints in your lane's state file. Report observed results, source SHA, commands, unresolved issues, and the next task. Never mark blocked or incomplete work done.
14. Continue all dependency-ready work without routine approval questions. Missing external credentials, paid actions, destructive financial changes, or incompatible upstream changes require a narrow blocker report.
15. Before release, run the release gates in `TESTING_AND_BENCHMARK_PLAN.md`. Feature flags are rollback tools, not an excuse to ship an untested financial path.
