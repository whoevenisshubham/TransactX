# TransactX v2 agent instructions

The execution plan is in `docs/next-gen/execution-pack/`. Start with its
`START_HERE.md`, `AGENT_RULES.md`, and the prompt and manual for your lane.
Read `TASK_REGISTRY.json` for dependencies and integration gates. Current human
instructions and the current source take precedence over historical planning
assumptions.

- Start from the verified GitHub default branch, preserve completed and local
  work, and use an owner branch and isolated checkout for implementation.
- M1 owns frontend, deployment, browser tests, release integration, and public
  documentation. M2 owns payment/backend composition, configuration,
  migrations, and shared API contracts. M3 owns investigator and anomaly
  packages, corpus, evaluation, and its state file. Coordinate shared changes
  through the named owner.
- Preserve integer paise, central payment authority, participant ownership,
  stable operation identities, user-scoped idempotency, and read-only evidence.
  An unknown bank outcome is not proof of failure.
- Run focused checks and record actual results in the lane's state file.
  Fixtures do not prove live integration. Do not weaken tests to make CI green.
- Commit reviewable changes with the configured identity. Do not force-push,
  invent evidence, expose credentials, spend money, or bypass account checks.
  Keep bank and database ports private in any deployment.
