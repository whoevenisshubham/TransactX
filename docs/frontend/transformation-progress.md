# TransactX Frontend Transformation Progress

**Branch:** `feat/uiux-redesign` (baseline commit `c8bf05b`)
**Stack Additions:** `motion`, `sonner`, `@fontsource/space-grotesk`, plus zero-dependency Canvas 2D physics/packet engine, interactive SVG systems, and Web Audio procedural synthesizer.

---

## Pass Tracker

| Pass / Phase | Scope | Status | Verification | Notes |
|---|---|---|---|---|
| **Pass 1** | Repository Reconnaissance & Route Inventory | **Completed** | Direct file reads + `npm run test` (134/134 green) | Mapped all routes, Playwright fixture hooks, legacy CSS overrides (`visual-reset.css`), and `/demo` bug root cause. |
| **Pass 2** | Live Inspection of All 12 Reference Sites | **Completed** | Real Chromium browser subagents (`1440x900`) | Inspected Stripe, Linear, Apple, Vercel, Datadog Service Map, Temporal + Docs, Grafana Docs, WhatTheFrontend, Recent.design, Lusion v2, plus `shubhamsoni.me/projects/transactx`. Documented in `docs/frontend/reference-study.md`. |
| **Pass 3** | Shared Motion, Audio, Canvas & Command Palette Infrastructure | **Completed** | `tsc -b && vite build` + Playwright Axe | `tokens.css` dark-first blueprint enhancements + `@fontsource/space-grotesk`; `AudioEngine`, `SpotlightCard`, `MagneticButton`, `ScrambleText`, `AnimatedCounter`, `DotSentinel`, `NetworkCanvas`, `CursorFollower`, `CommandPalette`, high-contrast `ThemedToaster`. |
| **Pass 4 (Phase 1)** | `/` Landing, `WireDropSimulator`, `BlueprintExplorerModal`, `/demo` Fix | **Completed** | `public.fixture.spec.ts` + `creative-ui.fixture.spec.ts` | Fixed `api.ts` (405 HTML `API_ROUTE_MISCONFIGURED` + 502/504 `DEMO_GATEWAY_WAKEUP`), forwarded `AbortSignal` in `authClient.ts`, upgraded `DemoPage` wakeup/retry UX, aurora hero, packet scene, 4-strategy Wire-Drop lab, and 3-tab Blueprint Explorer Modal. |
| **Pass 5 (Phase 2)** | `/login`, `/showcase`, Customer & Merchant Workspaces | **Completed** | `auth` + `showcase` + `customer` + `merchant` + `offline-shell` Playwright specs | Split-screen blueprint `AuthPage`, interactive `/showcase` playground (particle physics Canvas, Web Audio synth lab, `DotSentinel` probe, draggable 3D vault cube), animated `CustomerDashboard` + `SendMoneyPage` celebration burst, `TransactionActivity` shared `layoutId` pills, `ReceiptDrawer`, `OfflineQueuePanel`, `MerchantDashboard` + QR corner brackets + `SettlementOverview` + `IncomingPayments`. |
| **Pass 6 (Phase 3)** | `/console/*` Distributed Systems Observability Consoles | **Completed** | `ops.fixture.spec.ts` + `admin.fixture.spec.ts` | Animated Bank A -> Switch -> Bank B live packet visualizer (`PaymentEvidencePage`), `DotSentinel` telemetry pills (`OperationsShell`, `InvestigationPanel`, `ChaosView`, `ReconciliationView`), interactive `CLOSED`/`HALF_OPEN`/`OPEN` circuit state machine (`RoutingView`), 10-segment Grafana LCD bars (`HealthObservationCard`), clickable hash paths (`MerkleView`), animated SVG `AnalyticsCharts` with hover callouts, verified `IntegrityView` banner. |
| **Pass 7–11** | Visual, Functional, Responsive (`320`–`1440`), Axe, Perf & Final Design Audit | **Completed** | `134/134` unit + `test:money` + `160/160` Playwright Axe + `3/3` offline-shell + CodeRabbit (`0` findings) + Live Preview E2E | Completed pre-preview cleanup (`gsap` removal, `BlueprintExplorerModal` keyboard/Escape/focus-restore, route lazy loading `383.61 kB`), passed CodeRabbit CLI review (`0` findings), pushed `c8dfe52` to `feat/uiux-redesign`, and verified live Vercel Preview + Render API end-to-end. |
| **Pass 12** | Complete Visual Transformation (Obsidian Dark + Warm Editorial Alabaster + View Transitions + Upgraded Motion/3D/Canvas Primitives) | **Completed** | `134/134` unit + `test:money` + `lint` + `vite build` (`383.95 kB` / `113.20 kB` gzip) + `160/160` Playwright Axe + `3/3` offline-shell + multi-route Playwright screenshots (`dark`/`light`, `320`–`1440` px) | Eliminated legacy CSS `:root` cascade overrides (`experience.css`, `visual-reset.css`, `styles.css`), established Obsidian Carbon (`#090a0c`/`#121316`/`#9bb9ff`) & Warm Editorial Alabaster (`#f5f3ed`/`#fcfbf8`/`#1d4ed8`), added View Transitions triangle clip-path theme switch, `ClipReveal`, `BlurReveal`, `ScrollScrubBand`, `NetworkCanvas` pointer vector trail, multi-layer `preserve-3d` vault prism, portaled `BlueprintExplorerModal`, and `CelebrationOverlay` `activeRafRef` cleanup (`100/100` effect coverage). |

---

## Route Inventory

### Public & Authentication Routes (`main.tsx`)
- `/` (`!token`): `LandingPage` (Hero + Canvas network, `WorkspaceExplorer`, `TwoPhaseNarrative` / `WireDropSimulator`, `ArchitecturalPillars` + `BlueprintExplorerModal`, `PublicHeader`, `PublicFooter`)
- `/demo` (`!token`): `DemoPage` (`customer`, `merchant`, `operator` (`OPS_VIEWER`), secondary `recipient` Bob · Bank B)
- `/login` & `/register` (`!token`): `AuthPage` + `/auth/callback` (`GoogleCallbackPage`)
- `/privacy`: `PrivacyPage`
- `/showcase`: `ShowcasePage` (192 deterministic scenario library records + Interactive Systems & Effects Playground)
- `/design-system`: `ComponentShowcase` inside `RootLayout`

### Customer Routes (`CUSTOMER`)
- `/` (`CustomerDashboard`): Account balance card, shareable payment ID, recent activity, zero-balance notice
- `/pay` (`SendMoneyPage`): 3-step progressive send flow, recipient resolution, review sheet, settlement outcome, `UnresolvedIntent` lock
- `/transactions` (`TransactionActivity`): Filterable search/date/status ledger
- `/transactions/:id` (`ReceiptDrawer`): Detailed payment receipt & bank route timeline
- Offline overlay / queued intents: `OfflineQueuePanel` + `OfflineRecoveryShell`

### Merchant Routes (`MERCHANT`)
- `/merchant` (`MerchantDashboard`): Account position, receipt totals, sticky QR Receive Station, recent incoming feed
- `/merchant/receive` (`ReceiveStation`): QR code (SVG/PNG export) + payment identifier copy
- `/merchant/incoming` (`IncomingPayments`): Filterable table of received payments with inline receipt accordion
- `/merchant/settlement` (`SettlementOverview`): Grouped Completed / Pending confirmation / Failed settlement status
- `/merchant/search` (`MerchantSearch`): Transaction search by sender, ID, or note

### Operations & Distributed Systems Consoles (`OPS_VIEWER` & `OPS_ADMIN`)
- `/console` (`AdminOverviewPage` — admin only): Execution targets, open circuits, active faults, service reachability, `HealthObservationCard`s
- `/console/health` (`AdminHealthPage` — admin only): Target health probe & rolling telemetry
- `/console/routing` (`RoutingView` — admin only): Route distribution & `CLOSED`/`HALF_OPEN`/`OPEN` circuit breaker state machine
- `/console/worklist` (`WorklistPage` — viewer + admin): Cursor-paginated recovery worklist with filters
- `/console/payments/:id` (`PaymentEvidencePage` — viewer + admin): Payment journey, independent observations, transitions, bank ops, recovery, trace spans, server export, and `InvestigationPanel` + citations
- `/console/investigations/:id` (`InvestigationPage` — viewer + admin): Saved investigation replay
- `/console/analytics` (`AnalyticsPage` + `AnalyticsCharts` — viewer + admin): Measured latency/timeout/failure SVG charts & data tables
- `/console/reconciliation` (`ReconciliationView` — admin only): Ledger vs participant reconciliation runs & discrepancy table
- `/console/merkle` (`MerkleView` — admin only): Interactive SHA-256 `merkle-v1` bucket tree explorer
- `/console/integrity` (`IntegrityView` — admin only): Global conservation audit & cryptographic inclusion proof verification
- `/console/chaos` (`ChaosView` — admin only): Controlled simulation fault injector & ledger corruption fixture
- `/console/activity` (`ActivityView` — admin only): Deterministic operational event feed

---

## Initial `/demo` Error Diagnosis (Pre-Fix)

- **Symptom:** Clicking `Explore customer` displays `"The request could not be completed. You can retry the connection or sign in with private credentials."`
- **Root Cause Trace (`api.ts`, `config.ts`, `vercel.json`, `authClient.ts`, `DemoPage.tsx`):**
  1. In `frontend/src/config.ts`, when `VITE_API_BASE_URL` is unset in a Vercel build (such as Preview Deployments on branch `feat/uiux-redesign`), `apiBaseUrl` resolves to `window.location.origin`. `POST https://<vercel-preview>/api/auth/demo` hits `vercel.json` catch-all rewrite (`/index.html`) or HTTP `405` -> non-JSON body `{}` -> `safeErrorMessage("REQUEST_FAILED", "The request could not be completed.")`.
  2. When `VITE_API_BASE_URL` *is* set to Render free-tier API (`render.yaml`), Render cold-starts/proxy timeouts returning HTTP `500`/`502`/`504` (only `503` mapped to `DEMO_STARTING`) also fell through to `"The request could not be completed."`.
  3. Furthermore, `DemoPage.tsx` passed zero `AbortSignal` into `authClient.demo(persona)`, leaving manual "Cancel wait" disconnected from aborting the fetch.
- **Planned Fix (Phase 1):** Forward `AbortSignal` into `authClient.demo`, distinguish HTML-rewrite / `405` configuration mismatches vs Render `502`/`504` cold-start wakeups vs real JSON `503`/`429`/`403` error codes (while preserving exact `api.ts` behavior required by `ops.fixture.spec.ts` line 95!).
