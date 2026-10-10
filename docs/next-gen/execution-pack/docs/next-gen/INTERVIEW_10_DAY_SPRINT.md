# Dependency aware ten day sprint

Days are working-day targets, not guaranteed elapsed completion. Owners can begin together and implement with fixtures before live dependencies. The dependency registry and acceptance gates override calendar pressure.

| Day | M1 | M2 | M3 | Integration and evidence |
| --- | --- | --- | --- | --- |
| 1 | A0 bootstrap, A1 contracts/design | B0 baseline/migration tools; inspect B1 callers | C0 read interfaces, fixtures, feature schema | Verify baseline SHA, freeze contracts/migration ownership |
| 2 | A2 worklist with fixtures | B1 serialization, B2 participant absence protocol | C1 approved runbooks/retrieval | Merge correctness foundation after focused DB tests |
| 3 | A3 evidence journey with fixtures | B3 scheduler, B4 worker; B5 roles/tokens in parallel independent slices | C2 bounded evidence-only orchestration | Genuine pre-dispatch/lost-reply recovery test |
| 4 | A3 export/attempts; native deployment scripts | B6 atomic producer, B7 relay/inbox/projector | C3 provider adapter/grounding | Duplicate/ack-loss/event-rollback tests |
| 5 | A2/A3 real API integration; A4 investigator UI | B8 evidence/worklist API; B9 telemetry | C4 fixed evaluation/security cases | First integrated worklist->evidence->summary flow |
| 6 | A4 analytics/trace UI; A6 release packaging | B9 trace exporter/metrics; B10 durable aggregates | C5 dataset/baseline/model pipeline | Correlated multi-service trace and exporter-down test |
| 7 | A5 real browser tests | B11 package mounting; B12 process crash matrix | C5 native scoring/parity and held-out comparison | Combined private local system with model fallback |
| 8 | A6 configured free deployment; A7 restore rehearsal | B12 concurrency/pool/lease/event proofs | C6 integrated tool/auth/model verification | HTTPS/SSE/private-port smoke; restore artifact |
| 9 | A5 accessibility/offline reconnect; A8 release evidence | B13 full backend gates/fixes | C6 evaluation reports/fixes | Final combined PostgreSQL/race/browser gate |
| 10 | A8 final release, walkthrough and accurate resume | Close integration defects only | Close evaluation/corpus/model defects only | Clean SHA, evidence index, tag/status, run/rollback docs |

## Critical path

Baseline -> shared serialization -> absence/replay proof -> durable scheduler/worker -> event/projection plus evidence API -> combined crash tests -> browser and release integration. Investigator packages and frontend fixtures proceed alongside this path; live integration waits for the corresponding APIs.

M1's deployment scripts, M2's security role work and M3's corpus/training tooling are independent early work. If a lane is waiting, do its next independent task rather than create another lane's implementation.

## Scope cuts

First defer P2 scheduled reconciliation, elaborate topology and rich incident clustering. Then defer P1 chart polish/advanced model experiments/external telemetry backend. Keep the statistical baseline and clearly labelled model status if a trained model lacks evidence. Never replace true recovery/event guarantees with mocked demonstrations or omit their failure tests.

If P0 does not pass by day ten, publish a release candidate with exact remaining blockers and continue from checkpoints. Do not describe unfinished work in past tense on the resume. A shorter first demo can show safe existing payments plus evidence UI while automation remains gated, but it is not completion of this plan.
