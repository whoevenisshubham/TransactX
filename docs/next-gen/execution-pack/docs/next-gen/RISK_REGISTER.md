# Risks and escalation decisions

| Risk | Trigger or evidence | Required response | Owner |
| --- | --- | --- | --- |
| Financial race | Worker and request use different locks or helpers | Stop worker enablement; unify resumer; add DB race test | M2 |
| Pre-dispatch permanent pending | Journal exists, participant row absent | Presence/replay proof; otherwise park visibly | M2 |
| Unknown confused with failure | Attempt exhaustion or timeout marks FAILED | Revert unsafe path; retain pending financial state | M2 |
| Pool deadlock | All pool slots held by session locks | Semaphore before lock, query reserve, timeout/load test | M2 |
| Lost outbox event | Source commit has no pending delivery | Atomic producer test; block release | M2 |
| Sequence watermark skips commit | Late lower ID not consumed | Status-based claims/inbox; no high-watermark-only relay | M2 |
| AI false financial fact | Report conflicts with typed evidence | Reject report/disable generator; repair grounding tests | M3 |
| Prompt injection/scope escape | Model asks unrelated tool/entity | Reject server-side; adversarial test; no expanded permissions | M3/M2 |
| Paid provider overspend | Parallel reservations exceed cap | Atomic reservation, zero default, no hidden retries | M3 |
| ML leakage | Run labels or future values in features | Rebuild dataset/split; do not publish metric | M3 |
| Free VM capacity/reclaim | Provisioning unavailable or host disappears | Runnable bundle, backups, explicit unavailable state | M1 |
| Public admin abuse | Shared admin credential or control exposed | Viewer role, disable controls, rotate leaked secret | M2/M1 |
| Shared-file conflict | Two lanes edit config/migration/lockfile | Named owner applies patch and compatibility check | M1/M2 |
| Browser verification skipped | Only helper tests run | Real browser gate remains pending | M1 |
| Backup only created | No isolated restore artifact | Run restore/integrity/continuation test | M1 |
| Exhausted agent allowance | Task stops without completion | Checkpoint, resume later; no fake done status | Each |
| Upstream changed | Canonical branch advances | Inspect diff; preserve new work and update contracts once | M1/M2 |
| Ten-day scope overruns | P0 still incomplete near release | Cut P2, then P1 polish; keep correctness gates | M1 |

Escalation is narrow: identify the exact assumption, affected task, evidence and next safe action. Routine implementation problems are handled within the agent's lane. External credentials, rights or a proposed paid/destructive action cannot be inferred from a timeout.
