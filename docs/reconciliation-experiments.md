# Reconciliation scale experiment

## RC1 release evidence

The current immutable release artifact is
`artifacts/experiments/reconciliation/reconciliation-rc1-8e1efb0-raw.json`.
It was generated from clean source checkpoint
`8e1efb0c463aab539b417de87b6aa38a84fc8fe8` in release mode on Windows with
Go 1.27.1, 20 logical CPUs, seed 42, three measured repetitions, and 32
algorithm invocations per measurement. Its SHA-256 digest is
`fec99dbf5be08f73f52517d55cdc8fb1312c4a875b5aedb60aade6aee5766bd4`.

The artifact contains 24 measured datapoints and no zero or negative duration.
The smallest naive batch was 179,542,600 ns and the smallest Merkle batch was
20,578,600 ns. Average batch durations were:

| Records | Scenario | Naive | Merkle | Discrepancies |
| ---: | --- | ---: | ---: | ---: |
| 10,000 | clustered | 430.35 ms | 519.53 ms | 10 |
| 10,000 | dispersed | 424.02 ms | 3,319.58 ms | 10 |
| 10,000 | identical | 181.95 ms | 21.27 ms | 0 |
| 10,000 | single | 196.58 ms | 269.75 ms | 1 |
| 100,000 | clustered | 5,181.89 ms | 3,617.19 ms | 10 |
| 100,000 | dispersed | 5,553.65 ms | 24,253.80 ms | 10 |
| 100,000 | identical | 5,505.70 ms | 509.95 ms | 0 |
| 100,000 | single | 5,467.07 ms | 3,957.99 ms | 1 |

The identical cases demonstrate root pruning. Dispersed mutations remain an
unfavorable workload for this implementation, so the evidence does not support
a claim that Merkle comparison is always faster.

## Earlier M3 evidence

The raw artifact at `artifacts/experiments/reconciliation/reconciliation-20260929-raw.json` was generated from reconciliation source commit `57221c1e6706387586a6efa0f756e6883040dfe5` and contains three measured repetitions at 10,000 and 100,000 records for identical, single, clustered (10 adjacent records), and dispersed (10 separated records) mutations. A warmup run for each case is excluded. Seed 42 and a fixed UTC scope create the same canonical records and ordering for both algorithms. Participant commitments are built before timing. The naive timing includes full sorting and record comparison; the Merkle timing includes `Engine.Execute` and its in-memory run store. The experiment does not measure PostgreSQL or network transfer. Uncommitted HTTP fixture and documentation edits existed during the run; the reconciliation benchmark source matched the recorded commit.

Run from `backend` with a new output path:

```powershell
$env:GOCACHE='C:\Users\HP\Downloads\TransactX\.cache\go-build'
$env:M3_EVIDENCE_PATH='C:\Users\HP\Downloads\TransactX\artifacts\experiments\reconciliation\reconciliation-new-raw.json'
$env:M3_EVIDENCE_MODE='release'
go test ./internal/reconciliation -run '^TestReconciliationScaleEvidence$' -count=1 -timeout 30m
```

The test resolves the commit with Git, fails release mode when the source tree is
dirty, fails any non-positive duration, creates the raw JSON only after all
cases pass, and refuses to overwrite an existing artifact. Choose a new path
for each run.

The results are mixed. Root pruning makes identical scopes cheaper in this environment, while dispersed mutations can be slower than the naive baseline. No general speedup claim follows from this experiment alone.

Raw artifact SHA-256: `c9c667ee401558ed8716b8b92e56dd49b817519886220e14cea28a9b4fbc494c`.
