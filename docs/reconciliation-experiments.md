# Reconciliation scale experiment

The raw artifact at `artifacts/experiments/reconciliation/reconciliation-20260929-raw.json` was generated from reconciliation source commit `57221c1e6706387586a6efa0f756e6883040dfe5` and contains three measured repetitions at 10,000 and 100,000 records for identical, single, clustered (10 adjacent records), and dispersed (10 separated records) mutations. A warmup run for each case is excluded. Seed 42 and a fixed UTC scope create the same canonical records and ordering for both algorithms. Participant commitments are built before timing. The naive timing includes full sorting and record comparison; the Merkle timing includes `Engine.Execute` and its in-memory run store. The experiment does not measure PostgreSQL or network transfer. Uncommitted HTTP fixture and documentation edits existed during the run; the reconciliation benchmark source matched the recorded commit.

Run from `backend` with a new output path:

```powershell
$env:GOCACHE='C:\Users\HP\Downloads\TransactX\.cache\go-build'
$env:M3_GIT_COMMIT=(git rev-parse HEAD)
$env:M3_EVIDENCE_PATH='C:\Users\HP\Downloads\TransactX\artifacts\experiments\reconciliation\reconciliation-new-raw.json'
go test ./internal/reconciliation -run '^TestReconciliationScaleEvidence$' -count=1 -timeout 30m
```

The test fails if naive and Merkle discrepancy evidence differs. It creates the raw JSON exclusively after all cases pass and refuses to overwrite an existing artifact. Choose a new path for each run. The recorded commit is the source revision at experiment time; local working-tree modifications should be disclosed when present.

The results are mixed. Root pruning makes identical scopes cheaper in this environment, while dispersed mutations can be slower than the naive baseline. No general speedup claim follows from this experiment alone.

Raw artifact SHA-256: `c9c667ee401558ed8716b8b92e56dd49b817519886220e14cea28a9b4fbc494c`.
