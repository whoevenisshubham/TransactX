# Operational analytics and anomaly model

## Useful problem

Detect unusual service and recovery windows so operators can inspect them earlier. This is operational anomaly detection, not fraud detection, credit scoring, customer profiling or automated payment approval. Synthetic scenarios can test engineering behavior but cannot establish production fraud accuracy.

## Durable aggregate windows

M2 implements completed five-minute windows per configured bank/execution target using central payment facts, recovery attempts and health samples. Query half-open UTC scopes. Fields: request/sample counts, terminal/pending counts, timeout rate, permanent failure rate, latency p50/p95 when the sample supports them, recovery due age, attempt rate, queue depth snapshot and health sample availability.

Store `(entity_type,entity_id,window_from,feature_schema_version)` uniquely. Job retries upsert the same window. Do not derive an authoritative throughput history from process-local counters that reset. Latency metrics without a durable source are marked unavailable; add a bounded operational request observation table or use persisted attempt times explicitly, rather than fabricating payment latency from a trace sample.

The scorer uses only features known by window end. Freeze `tx.features.v1` with ordered fields `timeoutRate`, `failureRate`, `p95LatencyMs`, `oldestRecoveryAgeMs`, `recoveryAttemptsPerPayment`, `healthAvailabilityRate`. Missing values are null; rates are in [0,1], and age/latency/frequency are nonnegative. Exclude scenario labels, fault names, future recovery results, end-of-run statistics and payment identifiers from model features. Minimum sample count: 20; otherwise show `insufficient_data` and do not produce a confident score. Training selects only consistently available fields, stores the exact chosen subset/order, and Go inference falls back when a required feature is missing. Do not replace missing measurements with invented zeros.

## Statistical baseline

Implement a transparent EWMA or median/MAD baseline over prior healthy windows, with documented warmup and thresholds. Alert only when deviation and minimum samples pass the threshold. Use explicit reason codes and measured features. No payment mutation follows an alert.

This baseline runs in Go and remains available if Python or the ML artifact is absent. It establishes whether ML adds value rather than treating a trained model as inherently better.

## Dataset

Generate repeatable network scenarios through the existing simulator/experiment seams: healthy, rising latency, controlled outage, transient drops, recovery backlog, projection backlog and traffic bursts. Use at least 30 independent seeded scenario runs, with enough completed windows for the model's minimum sample requirements. Keep scenario generation deterministic but vary seeds and timings.

Record provenance per row: source commit, seed, scenario/run ID, observed window, feature schema, collection method and synthetic flag. Hold out entire scenario runs and later time blocks. Never split neighboring windows from the same scenario randomly across train and test. Ground-truth labels are for evaluation only and must not enter model input.

If insufficient data can be collected within the budget, complete the pipeline and statistical baseline, record the shortage, and defer trained-model claims. Do not create thousands of identical invented rows to pretend evaluation is meaningful.

## Model and training

Use scikit-learn IsolationForest with a small deterministic configuration: 64 trees, max_samples=min(256,training_rows), max_features=1.0, bootstrap=false, fixed seed, one CPU thread. Choose and freeze normalization/feature ordering from training data only. Robust median/IQR scaling is acceptable; guard zero IQR and nonfinite values.

Train on healthy training windows. Select the anomaly threshold on validation data to balance detection and false alerts. Compare against the statistical baseline on an untouched held-out set. Report precision, recall, F1, PR-AUC when meaningful, false alerts per healthy window, and detection delay by scenario. Include uncertainty from scenario-level resampling if sufficient runs exist; do not claim a universal percentage from one run.

Keep `analytics/requirements.txt` pinned, scripts for generation/training/evaluation/export, and a model card with intended use, limitations, dataset hashes, feature schema, source commit, dependency versions, seed and threshold selection. Do not load untrusted pickle/joblib files at runtime.

## Native Go scoring

Export a bounded JSON model containing normalization parameters, feature names/order, tree arrays, training subsample count, threshold and metadata/hash. Include node feature, threshold, child indexes and leaf sample count. Limit the artifact to 5 MiB, 128 trees, 1,024 nodes per tree and depth 32; reject invalid cycles/indexes, NaN/Infinity and mismatched feature schema.

Go scorer computes path length and IsolationForest anomaly score using the leaf-size adjustment `c(n)` and exported tree structure. Export the actual estimator-to-global feature mapping; do not assume a tree's feature index is always the global feature index. Match the Python input float precision where relevant and test against at least 100 exported Python reference vectors with tolerance <=1e-6. Handle n<=1 and n=2 explicitly. Use no financial values or money arithmetic in model inference.

Inference is pure and bounded; no Python service is needed online. Load one verified version at worker startup and atomically replace only after validation. Store model version with every scored window. New models do not reinterpret saved scores silently.

## Drift and fallback

Monitor missing feature ratio, sample counts, feature distribution changes and held-out calibration bounds. Mark a model stale after its configured age, default 30 days, or after feature-schema change. Operational drift alerts request review; they do not automatically retrain and promote.

If model artifact is absent, invalid, stale or out of distribution, use the statistical baseline and display model status. Model rollback selects the previous verified hash. The deterministic routing/circuit/recovery policies remain unchanged in every mode.

## User experience

Analytics shows window boundaries, sample count, rates, collection gaps, baseline reason, model score/threshold/version and synthetic-data qualification. A spike links to the worklist and related evidence; it does not announce a proven root cause. Investigator may explain anomalies as advisory context and must cite the window/model version.
