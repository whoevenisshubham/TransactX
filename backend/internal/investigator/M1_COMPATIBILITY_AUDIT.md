# M1 fixture and browser compatibility audit

Audit target: `origin/v2/m1-product-delivery` at `8afec7afdb2f78f43283e6c7d78c65aee28331a8` against M3 head `728f8e26969c00fb12711688df06d3401e8602c4` on 13 October 2026. Testing used a disposable detached M1 worktree. No M1 commit, branch update or merge was made.

## Contract result

M1's real TypeScript decoders accepted all committed M3 artifacts:

- Four `evaluation/results.json` investigation UI fixtures: completed evidence-only, partial evidence-only, provider-failed evidence fallback, and fixture-only validated LLM mode.
- The matching healthy and partial `tx.evidence.v1` snapshots, preserving payment/snapshot identity for citation resolution.
- All four `analytics/artifacts/windows.json` anomaly windows when wrapped in the shared cursor page envelope.

M1 `npm run lint` passed after the probe. This proves cross-branch DTO compatibility at the audited heads; it is not a live HTTP, worker or PostgreSQL result.

## Browser result and exact M1 patch

A disposable Playwright probe routed M1's real pages to the exact M3 artifacts. The anomaly page passed immediately. All four investigation modes rendered their status/mode, summary, facts, citations, missing-evidence/provider-error state and matching evidence snapshot, but each failed the 360px no-horizontal-overflow gate. The measured document width was 413px for a 360px viewport.

DOM diagnostics identified the unbroken immutable `runbook:<hash>` references rendered by `InvestigationPanel` inside `span.ops-muted`. The span extended to approximately 413px because `.ops-muted` had no wrapping rule. This is an M1-owned presentation defect; shortening M3's stable citation IDs would break provenance and is not an acceptable workaround.

The following one-line M1 patch was applied only in the disposable checkout:

```diff
diff --git a/frontend/src/design/ops.css b/frontend/src/design/ops.css
@@
 .ops-investigation { border: 1px solid var(--line); border-radius: var(--radius-md); padding: var(--space-md); margin-top: var(--space-md); }
+.ops-investigation .ops-muted { overflow-wrap: anywhere; }
```

After that exact patch, all five cross-branch Playwright tests passed in 11.8 seconds and `npm run lint` passed. The tests covered all four M3 investigation fixtures at a 360x800 viewport with no horizontal overflow plus the four-window advisory analytics page. M1 should apply this patch and retain a browser regression using M3's immutable runbook IDs.

## Remaining live gate

This audit reduces the M1 handoff to a verified CSS fix and live composition. It does not satisfy C6's real authenticated API/worker/database/browser gate: M2 must first mount the compatible M3 service/store/evidence/scoring boundaries, then M1 must run the same views against those real responses rather than routed artifacts.
