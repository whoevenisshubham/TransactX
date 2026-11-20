import React from "react";
import { ArrowRight, CheckCircle2, Lock, ShieldCheck, Cpu, ArrowDown } from "lucide-react";

export function ArchitectureTopologyDiagram() {
  return (
    <div className="w-full rounded-2xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] p-6 sm:p-8 shadow-sm space-y-6">
      {/* Topology Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 pb-4 border-b border-[var(--border-hairline)]">
        <div>
          <span className="text-[10px] font-mono uppercase tracking-widest text-[var(--action-accent-bg)] font-semibold">
            TOPOLOGY & STATE COORDINATION
          </span>
          <h3 className="text-xl sm:text-2xl font-bold tracking-tight text-[var(--text-primary)] mt-1">
            Multi-Bank Two-Phase Settlement Pipeline
          </h3>
          <p className="text-xs text-[var(--text-secondary)] mt-1 max-w-2xl leading-relaxed">
            Real-time inspection of distributed state transitions. Each bank participant maintains sovereign database isolation without cross-network row locking.
          </p>
        </div>
        <div className="flex items-center gap-2 self-start sm:self-auto shrink-0">
          <div className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-mono font-medium bg-[var(--status-info-bg)] text-[var(--status-info-fg)] border border-[var(--status-info-border)]">
            <span className="w-2 h-2 rounded-full bg-[var(--action-accent-bg)] animate-pulse" />
            <span>RAFT LEADER: Node-01</span>
          </div>
        </div>
      </div>

      {/* 3 Distributed Nodes Grid */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6 relative">
        {/* Node 1: Bank A (Originator) */}
        <div className="p-5 rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/60 flex flex-col justify-between space-y-4">
          <div>
            <div className="flex items-center justify-between mb-3">
              <div className="flex items-center gap-2.5">
                <div className="w-8 h-8 rounded-lg bg-[var(--bg-surface)] border border-[var(--border-hairline)] flex items-center justify-center font-bold text-xs text-[var(--text-primary)] shadow-sm">
                  A
                </div>
                <div>
                  <h4 className="font-semibold text-sm text-[var(--text-primary)]">Apex National</h4>
                  <span className="text-[11px] font-mono text-[var(--text-tertiary)]">Bank A (Originator)</span>
                </div>
              </div>
              <span className="px-2 py-0.5 rounded text-[10px] font-mono font-semibold bg-[var(--bg-surface)] text-[var(--text-secondary)] border border-[var(--border-hairline)]">
                RTGS-01
              </span>
            </div>

            <div className="space-y-2 text-xs">
              <div className="flex justify-between items-center">
                <span className="text-[var(--text-secondary)]">Sovereign State:</span>
                <span className="font-mono text-[11px] font-semibold text-[var(--status-success-fg)] bg-[var(--status-success-bg)] px-1.5 py-0.5 rounded border border-[var(--status-success-border)]">
                  HOLD_ACQUIRED
                </span>
              </div>
              <div className="flex justify-between items-center">
                <span className="text-[var(--text-secondary)]">Escrow Hold:</span>
                <span className="font-mono font-semibold text-[var(--text-primary)]">₹150.00 isolated</span>
              </div>
              <div className="flex justify-between items-center">
                <span className="text-[var(--text-secondary)]">Nonce Invariant:</span>
                <span className="font-mono text-[var(--text-tertiary)]">#849201 verified</span>
              </div>
              <div className="flex justify-between items-center">
                <span className="text-[var(--text-secondary)]">Lease Timeout:</span>
                <span className="font-mono text-[var(--status-warning-fg)]">T+1800ms auto-rollback</span>
              </div>
            </div>
          </div>

          <div className="p-2.5 rounded bg-[var(--bg-surface)] border border-[var(--border-hairline)] font-mono text-[11px] text-[var(--text-secondary)] truncate">
            <span className="text-[var(--text-tertiary)] mr-1">$</span>
            INSERT INTO debits_escrow (uid, amt) VALUES ('usr_alice', 15000);
          </div>
        </div>

        {/* Node 2: Central Coordinator (TransactX Core) */}
        <div className="p-5 rounded-xl border-2 border-[var(--action-accent-bg)] bg-[var(--bg-surface)] text-[var(--text-primary)] flex flex-col justify-between space-y-4 shadow-md relative ring-1 ring-[var(--action-accent-bg)]/20">
          <div>
            <div className="flex items-center justify-between mb-3">
              <div className="flex items-center gap-2.5">
                <div className="w-8 h-8 rounded-lg bg-[var(--action-accent-bg)] text-[var(--action-accent-fg)] flex items-center justify-center font-bold text-xs shadow-sm">
                  TX
                </div>
                <div>
                  <h4 className="font-semibold text-sm text-[var(--text-primary)]">TransactX Coordinator</h4>
                  <span className="text-[11px] font-mono text-[var(--text-secondary)]">Two-Phase Saga Engine</span>
                </div>
              </div>
              <span className="px-2 py-0.5 rounded text-[10px] font-mono font-semibold bg-[var(--action-accent-bg)]/10 text-[var(--action-accent-bg)] border border-[var(--action-accent-bg)]/20">
                Raft 3/3
              </span>
            </div>

            <div className="space-y-2 text-xs">
              <div className="flex justify-between items-center">
                <span className="text-[var(--text-secondary)]">Coordinator Phase:</span>
                <span className="font-mono text-[11px] font-semibold text-[var(--status-success-fg)] bg-[var(--status-success-bg)] px-1.5 py-0.5 rounded border border-[var(--status-success-border)]">
                  PREPARED (SYN_ACK)
                </span>
              </div>
              <div className="flex justify-between items-center">
                <span className="text-[var(--text-secondary)]">Idempotency Key:</span>
                <span className="font-mono text-[var(--text-primary)]">idem_pay_89f02c</span>
              </div>
              <div className="flex justify-between items-center">
                <span className="text-[var(--text-secondary)]">Outbox Worker:</span>
                <span className="font-mono text-[var(--text-primary)]">wrk_03_fenced</span>
              </div>
              <div className="flex justify-between items-center">
                <span className="text-[var(--text-secondary)]">Consensus Term:</span>
                <span className="font-mono text-[var(--text-tertiary)]">Term #108,492 (Zero Skew)</span>
              </div>
            </div>
          </div>

          <div className="p-2.5 rounded bg-[var(--bg-surface-raised)] border border-[var(--border-hairline)] font-mono text-[11px] text-[var(--text-secondary)] truncate">
            <span className="text-[var(--action-accent-bg)] mr-1">$</span>
            SELECT state FROM sagas WHERE xid='x_8819a' FOR UPDATE NOWAIT;
          </div>
        </div>

        {/* Node 3: Bank B (Beneficiary) */}
        <div className="p-5 rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/60 flex flex-col justify-between space-y-4">
          <div>
            <div className="flex items-center justify-between mb-3">
              <div className="flex items-center gap-2.5">
                <div className="w-8 h-8 rounded-lg bg-[var(--bg-surface)] border border-[var(--border-hairline)] flex items-center justify-center font-bold text-xs text-[var(--text-primary)] shadow-sm">
                  B
                </div>
                <div>
                  <h4 className="font-semibold text-sm text-[var(--text-primary)]">Pacific Reserve</h4>
                  <span className="text-[11px] font-mono text-[var(--text-tertiary)]">Bank B (Beneficiary)</span>
                </div>
              </div>
              <span className="px-2 py-0.5 rounded text-[10px] font-mono font-semibold bg-[var(--bg-surface)] text-[var(--text-secondary)] border border-[var(--border-hairline)]">
                CORE-09
              </span>
            </div>

            <div className="space-y-2 text-xs">
              <div className="flex justify-between items-center">
                <span className="text-[var(--text-secondary)]">Sovereign State:</span>
                <span className="font-mono text-[11px] font-semibold text-[var(--status-info-fg)] bg-[var(--status-info-bg)] px-1.5 py-0.5 rounded border border-[var(--status-info-border)]">
                  PROVISIONAL_CREDIT
                </span>
              </div>
              <div className="flex justify-between items-center">
                <span className="text-[var(--text-secondary)]">Ledger Journal:</span>
                <span className="font-mono font-semibold text-[var(--text-primary)]">+₹150.00 atomic credit</span>
              </div>
              <div className="flex justify-between items-center">
                <span className="text-[var(--text-secondary)]">Ack Ingress:</span>
                <span className="font-mono text-[var(--text-tertiary)]">14:02:18.421 (200 OK)</span>
              </div>
              <div className="flex justify-between items-center">
                <span className="text-[var(--text-secondary)]">Final Seal:</span>
                <span className="font-mono text-[var(--status-success-fg)] font-semibold">Invariant Validated</span>
              </div>
            </div>
          </div>

          <div className="p-2.5 rounded bg-[var(--bg-surface)] border border-[var(--border-hairline)] font-mono text-[11px] text-[var(--text-secondary)] truncate">
            <span className="text-[var(--text-tertiary)] mr-1">$</span>
            INSERT INTO customer_journal (dest, amt, idem) VALUES ('usr_bob', 15000, ...);
          </div>
        </div>
      </div>

      {/* 3-Step Progression Track */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4 pt-2">
        <div className="p-3.5 rounded-lg border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/30 space-y-1">
          <div className="flex items-center gap-1.5 text-xs font-mono font-semibold text-[var(--action-accent-bg)]">
            <Lock className="w-3.5 h-3.5 shrink-0" />
            <span>STEP 01: HOLD RESERVED (14ms)</span>
          </div>
          <p className="text-[11px] text-[var(--text-secondary)] leading-relaxed">
            Bank A acquires sovereign ledger hold. Zero locks exposed to external coordinator.
          </p>
        </div>

        <div className="p-3.5 rounded-lg border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/30 space-y-1">
          <div className="flex items-center gap-1.5 text-xs font-mono font-semibold text-[var(--action-accent-bg)]">
            <Cpu className="w-3.5 h-3.5 shrink-0" />
            <span>STEP 02: PROVISIONAL ACK (18ms)</span>
          </div>
          <p className="text-[11px] text-[var(--text-secondary)] leading-relaxed">
            Bank B signs tentative credit payload with counterparty nonce and validates signature.
          </p>
        </div>

        <div className="p-3.5 rounded-lg border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/30 space-y-1">
          <div className="flex items-center gap-1.5 text-xs font-mono font-semibold text-[var(--status-success-fg)]">
            <CheckCircle2 className="w-3.5 h-3.5 shrink-0" />
            <span>STEP 03: DUAL COMMIT SEAL (326ms)</span>
          </div>
          <p className="text-[11px] text-[var(--text-secondary)] leading-relaxed">
            Coordinator flushes outbox lease. Simultaneous release of hold and finalization of credit.
          </p>
        </div>
      </div>

      {/* Micro-footer Telemetry */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 pt-3 border-t border-[var(--border-hairline)] text-xs font-mono text-[var(--text-secondary)]">
        <div className="flex items-center gap-2">
          <span className="w-2 h-2 rounded-full bg-[var(--status-success-fg)]" />
          <span>Active Corridor: Apex RTGS-01 → Pacific CORE-09</span>
          <span className="text-[var(--text-tertiary)]">·</span>
          <span className="text-[var(--status-success-fg)] font-semibold">100% Zero-Loss Parity</span>
        </div>
        <div className="text-[11px] text-[var(--text-tertiary)]">
          SHA-256 Digest: <span className="text-[var(--text-secondary)] font-semibold">8f44...ce19</span>
        </div>
      </div>
    </div>
  );
}
