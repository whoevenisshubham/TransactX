import React from "react";
import { Scale, RefreshCw, GitBranch, ArrowRight, Check } from "lucide-react";

export function ArchitecturalPillars() {
  const pillars = [
    {
      number: "01",
      icon: <Scale className="w-5 h-5 text-[var(--action-accent-bg)]" />,
      title: "Two-Phase Commit Sagas",
      subtitle: "Non-blocking sovereign escrow reservations.",
      description:
        "Rather than holding distributed database row locks across networks, TransactX issues isolated debit holds in participant bank schemas. If network partitions occur before Phase 2 commits, holds expire cleanly via idempotent compensating transactions.",
      tags: ["Zero Distributed Locks", "Bounded Lease Timeouts", "BigInt Paise Precision"],
      snippetTitle: "saga_definition.go",
      snippetStatus: "ESCROW_CONFIRMED",
      snippetCode: `type SettlementSaga struct {
  HoldRef:    "hld_apex_8910",
  Compensate: RollbackParticipantA,
  Timeout:    1800 * time.Millisecond,
}`,
    },
    {
      number: "02",
      icon: <RefreshCw className="w-5 h-5 text-[var(--action-accent-bg)]" />,
      title: "Automated Outbox Recovery",
      subtitle: "Self-healing against gateway socket timeouts.",
      description:
        "When Bank B drops connection or returns HTTP 504 during settlement, background recovery workers acquire outbox leases via monotonic fences. Workers probe counterparties with idempotent hashes to confirm status without risk of double-charging.",
      tags: ["Fenced Monotonic Leases", "Exponential Jitter Backoff", "Probe Ack Verification"],
      snippetTitle: "recovery_telemetry.log",
      snippetStatus: "RECONCILED",
      snippetCode: `[14:02:19] [ERR] Socket drop on Bank_B POST /v1/credit
[14:02:20] [WRK] Worker_03 claims lease fence #4092
[14:02:20] [ACK] Probe query returns STATUS_CREDITED
[14:02:21] [OK] Outbox flagged SETTLED. 0 phantom debits.`,
    },
    {
      number: "03",
      icon: <GitBranch className="w-5 h-5 text-[var(--action-accent-bg)]" />,
      title: "Merkle Tree Reconciliation",
      subtitle: "Continuous cryptographic state verification.",
      description:
        "Every balance update appends to an immutable write ledger and hashes into a binary Merkle accumulator. Counterparty institutions independently audit cryptographic balance inclusion proofs in O(log N) operations without sharing raw customer PII.",
      tags: ["ED25519 Hardware Keys", "SHA-256 Merkle Roots", "Inclusion Proofs"],
      snippetTitle: "tree_accumulator.log",
      snippetStatus: "VERIFIED",
      snippetCode: `Root: 0x9f1a23...c890de44
Leaf_L3: SHA256(Hold#8910 + Credit#7712)
Path: [L_Hash: 0x11ab..., R_Hash: 0xdd82...]
Proof Invariant: Validated (0.12ms)`,
    },
  ];

  return (
    <section id="architectural-pillars" className="space-y-8">
      <div>
        <span className="text-[10px] font-mono uppercase tracking-widest text-[var(--action-accent-bg)] font-semibold">
          FOUNDATIONAL GUARANTEES
        </span>
        <h3 className="text-2xl sm:text-3xl font-bold tracking-tight text-[var(--text-primary)] mt-1">
          Built for the reality of unreliable distributed networks.
        </h3>
        <p className="text-sm text-[var(--text-secondary)] mt-2 max-w-2xl leading-relaxed">
          Financial protocols cannot rely on perfect connections. TransactX coordinates state through isolation, monotonic fencing, and continuous proof accumulators.
        </p>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {pillars.map((pillar) => (
          <div
            key={pillar.number}
            className="flex flex-col justify-between p-6 rounded-2xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] shadow-sm hover:shadow-md transition-all space-y-6"
          >
            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <div className="w-10 h-10 rounded-xl bg-[var(--bg-surface-raised)] border border-[var(--border-hairline)] flex items-center justify-center shadow-sm">
                  {pillar.icon}
                </div>
                <span className="font-mono text-xs font-semibold text-[var(--text-tertiary)]">
                  PILLAR {pillar.number}
                </span>
              </div>

              <div>
                <h4 className="text-lg font-bold text-[var(--text-primary)] tracking-tight">
                  {pillar.title}
                </h4>
                <p className="text-xs font-medium text-[var(--text-secondary)] mt-1">
                  {pillar.subtitle}
                </p>
              </div>

              <p className="text-xs text-[var(--text-secondary)] leading-relaxed">
                {pillar.description}
              </p>

              <div className="flex flex-wrap gap-1.5 pt-1">
                {pillar.tags.map((tag) => (
                  <span
                    key={tag}
                    className="px-2 py-0.5 rounded text-[10px] font-mono text-[var(--text-secondary)] bg-[var(--bg-surface-raised)] border border-[var(--border-hairline)]"
                  >
                    {tag}
                  </span>
                ))}
              </div>
            </div>

            <div className="rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/70 p-3 font-mono text-[11px] space-y-1.5">
              <div className="flex justify-between items-center text-[10px] text-[var(--text-tertiary)] border-b border-[var(--border-hairline)] pb-1">
                <span>{pillar.snippetTitle}</span>
                <span className="text-[var(--status-success-fg)] font-semibold">
                  {pillar.snippetStatus}
                </span>
              </div>
              <pre className="overflow-x-auto text-[11px] leading-relaxed text-[var(--text-primary)]">
                <code>{pillar.snippetCode}</code>
              </pre>
            </div>
          </div>
        ))}
      </div>
    </section>
  );
}
