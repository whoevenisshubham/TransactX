# Product workflows and interface specification

## Personas

Customer: send one payment safely and understand pending state. Merchant: identify incoming payments and share receive information. Operator: find interrupted payments and explain what the system knows. Administrator: run controlled recovery/reconciliation/fault workflows. Interview viewer: understand the architecture and inspect a safe live demonstration without administrator access.

## Screen requirements

| Screen | Primary job and displayed data | Actions | Degraded behavior |
| --- | --- | --- | --- |
| Worklist | Find payments needing attention; state, exact amount, original banks, age, attempt count, due time, projection freshness | Filter, exact payment search, open evidence, refresh | Keep prior rows marked stale; show lag/unavailable banner and last update |
| Payment evidence | Follow route, transitions, original operations, participant observations, attempts and related checks | Copy identity, inspect trace, start investigation, export evidence; admin recheck if enabled | Show source-by-source unavailable state; never replace missing operation outcome with failure |
| Investigation | Ask a scoped question; view facts, inferences, recommendations, documents and tool timeline | Start bounded job, open citations, refresh current payment evidence | Evidence-only mode remains useful; provider failure shows collected evidence and precise limitation |
| Analytics | Inspect unusual windows; sample sizes, rates, baseline, model version/score | Choose bounded window/entity, drill into worklist | Missing/low sample data stays unavailable; stale model shows fallback |
| Trace panel | Follow observed service spans associated with the selected payment | Expand safe attributes, copy trace ID | Missing/expired spans are telemetry gaps; financial timeline remains available |
| Reconciliation/proof explorer | Understand a discrepancy and independently bound a proof | Existing run/proof workflows, inspect scope/generation | Missing maintenance state is actionable setup information, not automatic rebuild |
| Customer status | Understand success, rejection or unresolved progress | Existing create/history/status/read/receipt behavior | Pending text says outcome is still being checked; no request for a new idempotency key |
| Merchant receive/history | Receive simulated payments and understand incoming status | Existing QR/copy/history controls | Amount and identity validation persist when offline; avoid false paid/settled language |
| Deployment status/walkthrough | Understand whether the backend is available and what is live | Retry, open architecture/demo walkthrough | Clearly label recorded artifacts; never switch to apparently live fake data |

## Worklist interaction

Default view focuses nonterminal or attention payments, with a clear option to inspect terminal history. Filters are URL-backed and validated; refresh preserves them. Count badges explicitly state whether they cover the loaded page, filtered result or global system. Empty results distinguish no matching payments from an unavailable projection.

Selecting a row fetches authoritative evidence independently. A lagging worklist row cannot override the detail state. Recovery button is absent for viewers. For administrators it says schedule a recheck, explains the scheduling action and returns an audited result without claiming immediate settlement.

## Payment journey

Display a summary card with central state and exact amount; a route card with source, destination and execution target; an ordered transition timeline; an operation table; recovery attempts; related reconciliation/integrity context; telemetry links. Include collection time and a missing-evidence section before an AI explanation.

Distinguish customer-visible state from internal stage. Human copy: pending means the system has not established the safe final result; bank-settled/central-pending means bank evidence is complete but central completion still needs work; attention means automation paused and needs investigation, not that money vanished.

## Investigation layout

Three visual groups: facts with evidence links, interpretation with explicit inference labels, and suggested next checks with runbook links. A side/below panel shows tool execution and retrieved documents. The selected payment is fixed; an unscoped question cannot cause hidden access to other payments.

Use a visible mode badge: evidence summary or LLM investigation. Display model/prompt version in report details, not as excessive technical clutter in the primary experience. Never display a fake confidence percentage. Citation clicks open the relevant evidence row/section and its source time.

## Empty loading error and success conventions

- First load uses bounded skeletons with accessible loading text.
- Empty states explain the next meaningful action without inventing sample records.
- A recoverable fetch error shows concise explanation, preserved filters and retry.
- A permission error explains access level and does not automatically retry forever.
- Success notification names the actual action: export ready, investigation complete, recheck scheduled. It does not claim a payment completed unless server truth says so.
- Partial evidence shows available facts and missing sources simultaneously.

## Accessibility and responsive gates

Keyboard-only completion of filter/open/citation/export flows. Visible focus and logical tab order. Every control has an accessible name. New views pass automated serious/critical accessibility checks plus a manual keyboard check. Test 360px mobile, 768px tablet and 1440px desktop, with 200 percent zoom and long IDs/text. Use reduced-motion preference; no animation is necessary to understand payment state.

## Product acceptance demonstration

1. Viewer opens worklist and sees a payment with a real interruption.
2. Detail explains original route and unresolved operation with source times.
3. Worker safely resumes after restoration; UI refetch shows current state and a real attempt.
4. Investigator explains the before/after evidence with resolvable citations.
5. Viewer exports redacted evidence and sees model/telemetry limitations where applicable.
6. Admin separately rebuilds projection or restores an isolated backup, with proof of conserved ledger and stable operation identities.

Complete flows have higher priority than adding decorative topology, an unrelated merchant portal redesign or extra navigation pages.
