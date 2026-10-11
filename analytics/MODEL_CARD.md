# Synthetic operational anomaly model v1

Intended use: read-only advisory inspection of completed five-minute bank/target windows. No financial values, payment identities or policy changes enter inference. This is operational anomaly scoring, not fraud detection. Recommendation: use the statistical baseline; this model does not improve the held-out results.

Dataset: 42 independent seeded virtual-time scenario runs, 16 windows each (672 rows), via existing `experiments.MockBankAdapter.GetHealth` with seeded availability/error injections. Latency and recovery backlog are explicit logical simulation observations, not measured HTTP latency or durable database facts. Projection backlog is a stress label with no queue feature in tx.features.v1; traffic burst changes sample volume without a throughput feature. Those limitations are intentionally represented in evaluation. No production data, live provider, runtime generator, paid API, cloud GPU or extra online Python service was used.

The source adapter baseline is `6ad42bff0ff01da6b4c88e5b702aa2b897fd452d`. The generator/training implementation is versioned in this branch's commits; the sourceCommit field records the source seam, not a claim that the new generator existed at that baseline. Seeds and observed virtual UTC windows are in every dataset row: 1000-1006/1010-1016 train; 1100-1106/1110-1116 validation; 1200-1206/1210-1216 test. Entire runs are disjoint; validation/test occur strictly after the preceding partition. Eight initial healthy windows precede stress in each nonhealthy run. Labels, seeds, run/scenario IDs and future outcomes never enter the six ordered features.

Training uses 128 healthy training windows only, train-only median/IQR scaling (zero IQR -> 1), all six consistently observed fields in tx.features.v1. scikit-learn 1.7.2 IsolationForest: 64 trees, max_samples=128, max_features=1, bootstrap=false, random_state=371, n_jobs=1. Native inference converts normalized input to float32, uses actual estimator feature mapping, and applies the sklearn c(n) leaf adjustment with explicit n<=1/n=2 handling. Full development dependencies are pinned locally in requirements.txt.

The statistical baseline is train-only median/MAD with 8 eligible observations/feature warmup, minimum 20 samples/window, scaled MAD=1.4826*MAD and fixed scale floors [.03,.03,30,1000,.2,.03]. Only upward timeout/failure/latency/age/attempt deviations and downward health availability alert. Missing features do not become zero. Calibration windows must be independently designated healthy and precede scored windows; M2 owns that durable selection. Reason codes identify each observed deviation.

Threshold policy was fixed before held-out evaluation: maximize validation F1 subject to <=5% healthy false alerts; ties prefer stricter thresholds. Model candidates .45-.8 in .005 increments; baseline candidates 2,3,4,5,6,8,10. Selected model threshold .6, baseline 3.0. Test data does not calibrate normalization, thresholds or bounds.

| Held-out result (224 windows / 14 runs) | Raw IsolationForest | Baseline |
| --- | ---: | ---: |
| Precision | 0.814815 | 0.942029 |
| Recall | 0.458333 | 0.677083 |
| F1 | 0.586667 | 0.787879 |
| PR-AUC (average precision) | 0.765660 | 0.849214 |
| False alerts / healthy window | 0.078125 | 0.031250 |

Detection delays by scenario and 500 scenario-run bootstrap resamples (seed 371) are in evaluation.json. Only two held-out runs/scenario exist; uncertainty is coarse and these results do not establish production accuracy. The model exceeds its validation false-alert target on test; do not tune against that held-out failure. Neither scorer reliably sees projection backlog/traffic burst through the frozen features.

Native bounds use a predeclared broad +/-128 train-normalized IQR engineering envelope, not an empirically validated drift detector. 156 held-out vectors are scoreable, 68 hit out_of_distribution; their native score/threshold remain null and baseline output remains available. Raw model metrics above describe sklearn before that guard. Guarded hybrid F1 is 0.752941 with 0.078125 healthy false alerts, still worse than baseline. No model promotion is justified. All 224 vectors include expected status; 156 scoreable vectors meet <=1e-6 parity (observed max error 1.11022302463e-16), with baseline deviation/alert parity too.

Artifact format tx.iforest.v1, model version synthetic-iforest-v1; feature version tx.features.v1. JSON export caps 5 MiB, 128 trees, 1024 nodes/tree, depth 32. Loader checks the configured external SHA-256, metadata/schema, dimensions, finite values, unique features, children, cycles/shared nodes/unreachable nodes, sample conservation and timestamps. No pickle/joblib runtime loading. A hash verifies configured bytes, not their authenticity; M2 must pin only reviewed artifacts. Samples <=1 are rejected as unscorable artifacts; c(0/1)=0 and c(2)=1 are independently tested.

Default stale age 30 days. Long-lived workers call ScoreAt/Evaluate with server-owned current observation time to recheck age; Score uses Load's validation time. Registry validates before atomic publication, retains its old verified model on invalid replacement, and supports reviewed prior-hash rollback. Missing/invalid/stale/schema-incompatible/OOD models never authorize policy changes; baseline stays independently usable. Evaluate exposes missing-feature ratio for M2 drift/status metrics. Real drift monitoring and durable score persistence await B10/B11.

Hashes (LF byte encoding; manifests avoid recursive self-hashes):

- Dataset: `95fc33c90f735c36abaa3c3f6064cae53ed1df4239dd9b9dd706a6812e00176a`
- Split: `00495d89cea058d57a268aa5a65a391e628010a1d17cdbbb1a52335955b3536a`
- Model: `854e088946a1024b54dd1908a9a98a2958ec98fe2b04abc42fd01fe5219ebe51`

Reproduction from repository root (PowerShell):

```powershell
python -m venv analytics/.venv
analytics/.venv/Scripts/python -m pip install -r analytics/requirements.txt
go -C backend run ./internal/anomaly/cmd/scenarios -source 6ad42bff0ff01da6b4c88e5b702aa2b897fd452d -out ../analytics/artifacts/dataset.json
analytics/.venv/Scripts/python analytics/pipeline.py --trained-at 2026-10-11T00:00:00Z
analytics/.venv/Scripts/python -m unittest discover -s analytics -p test_pipeline.py -v
go -C backend test -race ./internal/investigator ./internal/anomaly
go -C backend vet ./internal/investigator ./internal/anomaly ./internal/anomaly/cmd/scenarios
go -C backend test ./internal/anomaly -run '^$' -bench BenchmarkScore -benchmem -count 1
```

Use .venv/bin/python on Linux. Model timestamp is an explicit reproducibility parameter, recording this offline evaluation date, not production collection time. Later training must pass its actual date and preserve saved historical scores. Dataset regeneration is deterministic for the same pinned source/Go generator; Python tests compare exact exported bytes from saved input. Git attributes force LF for hashed owned artifacts; the unchanged shared schema and candidate source manifest hash use normalized LF.

Reference: [scikit-learn 1.7.2 IsolationForest documentation](https://scikit-learn.org/1.7/modules/generated/sklearn.ensemble.IsolationForest.html) and installed 1.7.2 scorer source were checked for float32/path adjustment behavior.
