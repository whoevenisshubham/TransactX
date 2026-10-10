# Frontend implementation architecture

## Preserve and extract

The existing app has role-aware customer/merchant shells, a large `main.tsx`, a large `console.tsx`, helper tests, exact money handling, QR and IndexedDB replay. Do not rewrite the entire frontend to add new screens. Extract only the parts touched by the new workflow into typed modules, keeping existing payment behavior covered.

Recommended structure:

```text
frontend/src/
  api.ts                         existing client facade
  config.ts                      one validated API-base resolver
  components.tsx                 existing reusable components
  ops/
    contracts.ts                 v2 DTOs and decoders
    client.ts                    typed endpoint methods
    fixtures.ts                  explicit development/test adapter
    WorklistPage.tsx
    PaymentEvidencePage.tsx
    InvestigationPanel.tsx
    AnalyticsPage.tsx
    TracePanel.tsx
    EvidenceExport.tsx
    useResource.ts               bounded fetch/cancel/stale handling
  design/
    tokens.css                   extracted shared variables
    StatusBadge.tsx
    DataTable.tsx
    Timeline.tsx
  tests/                         new component/browser helpers
```

Avoid adding a large state-management or UI framework. Use existing React state and small shared hooks. If new routing needs require a router, decide once in M1's task with compatibility tests; otherwise extend the current pathname/history handling for console deep links. Existing `/console/...` routes already use browser history.

## Navigation

Keep customer payment and merchant receive workflows. Operator routes:

- `/console` overview.
- `/console/worklist` filterable worklist.
- `/console/payments/:paymentId` evidence journey.
- `/console/investigations/:investigationId` saved investigation.
- `/console/analytics` operational windows/anomalies.
- Existing health, routing, reconciliation, Merkle, integrity, chaos and activity routes remain accessible according to permissions.

Trace detail is a panel linked from payment evidence, not a duplicate dashboard. Chaos appears only with administrator role and server capability enabled. A server denial remains authoritative even if a button is visible.

## Data loading and consistency

Use typed server DTOs. Decode decimal-string money with existing exact money helpers and explicit BigInt/safe formatting. Never pass BigInt directly to JSON.stringify. Preserve existing numeric payment request compatibility and safe-range checks.

Cancel fetches on navigation/logout/filter changes. A slower response for payment A cannot overwrite payment B. Store resource key, fetch time and version with each response. Distinguish first-load skeleton, refreshed data with a stale badge, partial evidence and blocking central errors.

Extend SSE topics for worklist/payment/recovery invalidation, but keep the stream as hints. Reconnect or gap triggers authoritative reads. Do not cache a financial status forever because no stream message arrived. Deduplicate/refetch with 200-500 ms debounce; pause background reads when hidden. Fallback polling is bounded, default 15 seconds for active operator pages, exponential backoff during outage, and no permanent one-second loop.

Do not optimistically set a payment to completed. Recheck control may optimistically show a scheduling spinner only; the actual status comes from the response/refetch. Investigator creation displays queued/running state and never substitutes a canned completed answer.

## Design rules

Use the existing visual language with deliberate refinements: readable sans-serif body, monospace UUID/trace snippets, 4/8/12/16/24/32 spacing, consistent table density and two neutral surfaces. Status carries a label and icon; color alone is insufficient. Severity is informational/warning/error, distinct from financial state.

Money is aligned consistently and formatted exactly. Dates show local time for reading and UTC/timezone in detail/export. IDs can be copied via a labelled button with confirmation; show a shortened display but preserve the full accessible value. Long text wraps without enlarging a whole table column.

Reuse accessible native form controls where possible. Dialogs need focus trap/restore, Escape and title. Keyboard focus is visible. Tables use proper headings and a caption/accessible label. Small-screen cards preserve the same data labels. Any chart has a text/table alternative and sample/window context.

## Test boundaries

Existing Node/tsx helper tests remain. Add React component behavior testing with one lightweight supported setup when necessary; do not test copied implementation details. Add Playwright for real browser flows and axe accessibility checks on new critical screens. M1 owns frontend dependencies and lockfile changes.

Test worklist filters/deep links, decimal money, partial evidence, response cancellation, failed auth, SSE reconnect, no duplicate investigator creation, and viewer inability to schedule recovery. Verify IndexedDB intent survives real reload and replays once against a real local API under the correct user.

## Static deployment requirements

The compiled app is static. `VITE_API_BASE_URL` is validated HTTPS for production and resolved in one place. Add SPA fallback so refreshing `/console/payments/...` serves index.html. Asset caching may be immutable for hashed build files; index.html and authenticated API data are not immutable.

Do not add Next.js or move Go into Vercel Functions to make hosting seem easier. Backend worker and SSE remain on the native service host. Display an API-unavailable screen with retry and a link to an explicitly labelled read-only recorded walkthrough if one is provided; do not silently render fixtures as live data.

## Performance targets

Measure rather than assume. Target new operator page usable in under two seconds on the documented local baseline; keep normal initial frontend JavaScript below 500 KiB gzip unless an evidence-backed exception is documented. Lazy-load trace/analytics pages and heavy chart code. Paginate large tables; do not load all 100K records into the browser. Avoid chart libraries when simple SVG/CSS plus a data table suffices.
