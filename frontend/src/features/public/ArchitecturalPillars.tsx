import React, { useState } from "react";
import { Scale, RefreshCw, GitBranch, ShieldCheck, WifiOff } from "lucide-react";

interface InvariantSpec {
  id: string;
  label: string;
  headline: string;
  summary: string;
  guarantees: string[];
  artifactTitle: string;
  artifactBody: string;
}

const INVARIANTS: InvariantSpec[] = [
  {
    id: "saga",
    label: "Two-phase recovery",
    headline: "Isolated holds and fenced lease recovery.",
    summary:
      "TransactX avoids distributed database row locks across participant banks. Phase 1 places an isolated debit hold; if a gateway reply is lost, background workers acquire a monotonic HeartbeatLease and probe the original idempotency key without double-charging.",
    guarantees: ["Zero distributed row locks", "Monotonic LeaseEpoch fencing", "PENDING_RECONCILIATION non-assumption"],
    artifactTitle: "recovery_sequence.log · illustrative",
    artifactBody: `[ClaimDueJobs]  Worker acquired job · LeaseEpoch #4092
[HeartbeatLease] Lease renewed during active probe
[ProbeStatus]   Bank B returned STATUS_CREDITED
[Outbox]        Converged to COMPLETED (0 duplicate debits)`,
  },
  {
    id: "conservation",
    label: "SQL sum conservation",
    headline: "Integer paise arithmetic and double-entry equilibrium.",
    summary:
      "Every monetary amount is represented as an integer in paise (BigInt / int64) — never floating-point rupees. Double-entry ledger postings across participant schemas are continuously audited via SQL conservation checks.",
    guarantees: ["Integer paise precision", "Zero floating-point drift", "Double-entry SQL SUM audit"],
    artifactTitle: "conservation_audit.sql · illustrative",
    artifactBody: `SELECT participant_id, SUM(debit_paise - credit_paise) AS delta
FROM ledger_entries
GROUP BY participant_id
HAVING SUM(debit_paise - credit_paise) <> 0;
-- Expected: 0 rows (invariant preserved)`,
  },
  {
    id: "merkle",
    label: "Merkle audit trees",
    headline: "Cryptographic inclusion proofs using merkle-v1.",
    summary:
      "Settlement windows commit records into a binary Merkle accumulator with SHA-256 and TXNODE|v1| domain separation. Operators inspect bucket roots and verify membership inclusion proofs in O(log N) steps.",
    guarantees: ["SHA-256 merkle-v1 accumulator", "TXNODE|v1| domain separation", "O(log N) inclusion proofs"],
    artifactTitle: "merkle_proof.txt · illustrative",
    artifactBody: `Algorithm : merkle-v1
Node      : SHA256("TXNODE|v1|" + L_Hash + R_Hash)
Root      : 0x9f1a23e8...c890de44
Verify    : Reconstructed root matches committed participant root`,
  },
  {
    id: "circuits",
    label: "Circuit breakers",
    headline: "Rolling-window health and progressive probe restoration.",
    summary:
      "Execution targets maintain independent circuit-breaker state machines (CLOSED, HALF_OPEN, OPEN) driven by rolling availability, success rate, and latency penalties. Unavailable probe responses never fabricate a bank failure.",
    guarantees: ["CLOSED / HALF_OPEN / OPEN states", "Bounded cooldown windows", "Independent target telemetry"],
    artifactTitle: "circuit_snapshot.json · illustrative",
    artifactBody: `{
  "executionTargetId": "RAIL-B",
  "state": "HALF_OPEN",
  "restorationStep": 1,
  "maxRestorationSteps": 3,
  "note": "Limited probe eligibility active"
}`,
  },
  {
    id: "offline",
    label: "Offline & RBAC",
    headline: "Explicit outbox replay and strict read-only inspection.",
    summary:
      "Disconnected clients reserve a UUIDv4 payment intent in IndexedDB and require explicit operator/user action to replay safely. Public operations access is strictly isolated to read-only OPS_VIEWER with zero mutation controls.",
    guarantees: ["Client UUIDv4 idempotency", "Zero silent auto-retry", "Strict OPS_VIEWER read-only RBAC"],
    artifactTitle: "rbac_boundary.policy · illustrative",
    artifactBody: `Role     : OPS_VIEWER (Public Demo)
Allowed  : /console/worklist, /console/payments/:id, /console/analytics
Blocked  : /console/routing, /console/reconciliation, /console/chaos
Replay   : User-triggered only (never automatic on reconnect)`,
  },
];

const ICONS: Record<string, React.ReactNode> = {
  saga: <RefreshCw className="w-4 h-4" aria-hidden="true" />,
  conservation: <Scale className="w-4 h-4" aria-hidden="true" />,
  merkle: <GitBranch className="w-4 h-4" aria-hidden="true" />,
  circuits: <ShieldCheck className="w-4 h-4" aria-hidden="true" />,
  offline: <WifiOff className="w-4 h-4" aria-hidden="true" />,
};

export function ArchitecturalPillars() {
  const [selectedId, setSelectedId] = useState(INVARIANTS[0].id);
  const current = INVARIANTS.find((item) => item.id === selectedId) ?? INVARIANTS[0];

  return (
    <section
      id="architectural-pillars"
      className="border-t border-[var(--border-hairline)] bg-[var(--bg-surface)]/50"
    >
      <div className="mx-auto max-w-7xl px-4 py-14 sm:px-6 lg:px-8 lg:py-20 space-y-8">
        <div className="max-w-2xl space-y-2">
          <p className="text-xs font-mono font-semibold text-[var(--accent-strong)] m-0">
            System guarantees
          </p>
          <h2 className="text-3xl font-bold tracking-tight text-[var(--text-primary)] sm:text-4xl m-0">
            Built for unreliable distributed networks.
          </h2>
          <p className="text-sm leading-relaxed text-[var(--text-secondary)] m-0">
            TransactX coordinates settlement through isolated holds, monotonic lease fencing, SQL
            conservation audits, and verifiable Merkle accumulators.
          </p>
        </div>

        <div className="grid grid-cols-1 gap-6 lg:grid-cols-12 lg:items-start">
          {/* Left Rail Selector */}
          <div
            role="tablist"
            aria-label="System invariants"
            className="lg:col-span-4 flex flex-col gap-1.5"
          >
            {INVARIANTS.map((item) => {
              const active = item.id === current.id;
              return (
                <button
                  key={item.id}
                  type="button"
                  role="tab"
                  aria-selected={active}
                  onClick={() => setSelectedId(item.id)}
                  className={`flex items-center gap-3 rounded-xl border px-4 py-3 text-left text-sm font-semibold transition-colors cursor-pointer focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--border-focus)] ${
                    active
                      ? "border-[var(--accent-strong)] bg-[var(--bg-surface)] text-[var(--text-primary)] shadow-[var(--shadow-subtle)]"
                      : "border-transparent bg-transparent text-[var(--text-secondary)] hover:bg-[var(--bg-surface)] hover:text-[var(--text-primary)]"
                  }`}
                >
                  <span className={active ? "text-[var(--accent-strong)]" : "text-[var(--text-tertiary)]"}>
                    {ICONS[item.id]}
                  </span>
                  <span>{item.label}</span>
                </button>
              );
            })}
          </div>

          {/* Right Specification & Illustrative Proof Pane */}
          <div
            role="tabpanel"
            className="lg:col-span-8 rounded-2xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] p-6 sm:p-7 shadow-[var(--shadow-subtle)] space-y-5"
          >
            <div className="space-y-2">
              <h3 className="text-xl font-bold tracking-tight text-[var(--text-primary)] m-0">
                {current.headline}
              </h3>
              <p className="text-sm leading-relaxed text-[var(--text-secondary)] m-0">
                {current.summary}
              </p>
            </div>

            <div className="flex flex-wrap gap-2">
              {current.guarantees.map((tag) => (
                <span
                  key={tag}
                  className="rounded-md border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)] px-2.5 py-1 text-xs font-mono text-[var(--text-primary)]"
                >
                  {tag}
                </span>
              ))}
            </div>

            <div className="rounded-xl border border-[var(--border-hairline)] bg-[#0f172a] text-[#f8fafc] overflow-hidden">
              <div className="border-b border-white/10 px-4 py-2 text-[11px] font-mono text-[#94a3b8]">
                {current.artifactTitle}
              </div>
              <pre
                tabIndex={0}
                aria-label={current.artifactTitle}
                className="m-0 overflow-x-auto p-4 font-mono text-xs leading-relaxed text-[#e2e8f0] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--border-focus)]"
              >
                <code>{current.artifactBody}</code>
              </pre>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}
