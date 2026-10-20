# Contributing to TransactX

Start with the [local development guide](docs/runbooks/local-development.md),
[architecture](docs/architecture.md) and [documentation index](docs/README.md).
Use a branch based on the current stable release and keep changes reviewable.

## Correctness and security

- Represent money as integer paise. Preserve central payment authority,
  independently owned bank records and user-scoped idempotency.
- Keep operation identities stable across retries. A missing response means an
  unknown outcome until authoritative evidence establishes what happened.
- Keep evidence reads and investigation tools read-only. Preserve authorization,
  source scope, missing observations and strict report/citation validation.
- Use isolated disposable databases for tests. Never initialize, reset or clean
  an existing financial dataset. Keep credentials and backups outside Git.
- Preserve migration checksums, approved corpus hashes, licenses, benchmark
  provenance and contributor history. Do not enable paid resources for tests.

## Validation

Run checks relevant to the changed boundary. Frontend commands are documented in
`frontend/package.json`; Go packages live in `backend/`. Contract definitions and
explicit test fixtures are maintained in [docs/api/contracts](docs/api/contracts).
Explain which integration tests ran, skipped or require external configuration.

Rendered fixtures, local tests, hosted integration and native recovery each prove
different things. Record the source and limitations of evidence; do not infer
live acceptance from a screenshot or a green build alone. Follow required CI and
review checks before merging, and preserve a compatible deployment rollback.
