import React from "react";
import { Check, ArrowRight, ArrowUpRight, Sparkles } from "lucide-react";
import { BrandMark } from "../../components";

export function PaymentScene() {
  return (
    <div
      className="rounded-2xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] p-5 sm:p-6 shadow-[var(--shadow-raised)] space-y-5"
      aria-label="Illustrative payment receipt, bank path and cited investigation"
    >
      {/* Top bar */}
      <div className="flex items-center justify-between gap-2 text-[11px] font-mono text-[var(--text-tertiary)]">
        <span className="font-semibold uppercase tracking-wider text-[var(--text-secondary)]">
          The payment, opened up
        </span>
        <span className="rounded-full border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)] px-2.5 py-0.5">
          Illustrative example
        </span>
      </div>

      {/* Bank Corridor Path */}
      <div className="grid grid-cols-3 items-center gap-2 rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/70 p-3.5 text-center">
        <div className="flex flex-col items-center gap-1 min-w-0">
          <span className="inline-flex h-7 w-7 items-center justify-center rounded-full border border-[var(--border-hairline)] bg-[var(--bg-surface)] font-mono text-xs font-bold text-[var(--text-primary)]">
            A
          </span>
          <span className="text-xs font-semibold text-[var(--text-primary)] truncate">Bank A</span>
          <span className="text-[10px] font-mono text-[var(--text-tertiary)]">Sender</span>
        </div>

        <div className="flex flex-col items-center gap-1 min-w-0">
          <div className="flex items-center gap-1.5 text-[var(--action-accent-bg)]">
            <ArrowRight className="w-3.5 h-3.5 shrink-0" aria-hidden="true" />
            <BrandMark />
            <ArrowRight className="w-3.5 h-3.5 shrink-0" aria-hidden="true" />
          </div>
          <span className="text-[10px] font-mono text-[var(--text-tertiary)]">Recorded outcome</span>
        </div>

        <div className="flex flex-col items-center gap-1 min-w-0">
          <span className="inline-flex h-7 w-7 items-center justify-center rounded-full border border-[var(--border-hairline)] bg-[var(--bg-surface)] font-mono text-xs font-bold text-[var(--text-primary)]">
            B
          </span>
          <span className="text-xs font-semibold text-[var(--text-primary)] truncate">Bank B</span>
          <span className="text-[10px] font-mono text-[var(--text-tertiary)]">Recipient</span>
        </div>
      </div>

      {/* Receipt + Cited Investigation */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div className="rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] p-4 space-y-3">
          <div className="flex items-center justify-between">
            <span className="inline-flex h-8 w-8 items-center justify-center rounded-full bg-[var(--status-success-bg)] text-[var(--status-success-fg)]">
              <Check className="w-4 h-4" aria-hidden="true" />
            </span>
            <span className="rounded-full border border-[var(--status-success-border)] bg-[var(--status-success-bg)] px-2.5 py-0.5 text-[11px] font-mono font-semibold text-[var(--status-success-fg)]">
              Completed
            </span>
          </div>

          <div>
            <p className="text-[11px] font-mono uppercase tracking-wider text-[var(--text-tertiary)] m-0">
              Payment receipt
            </p>
            <div className="mt-0.5 font-mono text-2xl font-bold tracking-tight text-[var(--text-primary)] tabular-nums">
              ₹1,250<span className="text-base text-[var(--text-tertiary)]">.00</span>
            </div>
          </div>

          <dl className="space-y-1.5 border-t border-[var(--border-hairline)] pt-2.5 text-xs m-0">
            <div className="flex justify-between gap-2">
              <dt className="text-[var(--text-tertiary)]">From</dt>
              <dd className="font-medium text-[var(--text-primary)] m-0">Alice · Bank A</dd>
            </div>
            <div className="flex justify-between gap-2">
              <dt className="text-[var(--text-tertiary)]">To</dt>
              <dd className="font-medium text-[var(--text-primary)] m-0">Demo Store · Bank B</dd>
            </div>
          </dl>

          <div className="flex items-center gap-2 border-t border-[var(--border-hairline)] pt-2.5 text-[11px] text-[var(--text-tertiary)]">
            <BrandMark />
            <span>Original payment. One reference.</span>
          </div>
        </div>

        <div className="rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/60 p-4 flex flex-col justify-between space-y-3">
          <div className="space-y-2">
            <div className="flex items-center gap-1.5 text-[11px] font-mono font-semibold uppercase tracking-wider text-[var(--accent-strong)]">
              <Sparkles className="w-3.5 h-3.5" aria-hidden="true" />
              <span>Cited investigation</span>
            </div>
            <h3 className="text-base font-bold tracking-tight text-[var(--text-primary)] m-0 leading-snug">
              The outcome.
              <br />
              With its sources.
            </h3>
            <p className="text-xs leading-relaxed text-[var(--text-secondary)] m-0">
              Central outcome and bank observations stay distinct.
            </p>
          </div>

          <div className="space-y-2">
            <div className="inline-flex items-center gap-1.5 rounded-lg border border-[var(--border-hairline)] bg-[var(--bg-surface)] px-2.5 py-1.5 text-[11px] font-mono font-medium text-[var(--text-primary)]">
              <ArrowUpRight className="w-3.5 h-3.5 text-[var(--action-accent-bg)]" aria-hidden="true" />
              <span>Collected payment evidence</span>
            </div>
            <p className="text-[10px] font-mono text-[var(--text-tertiary)] m-0">
              Illustrative report · no provider request
            </p>
          </div>
        </div>
      </div>

      {/* Bottom Pipeline Strip */}
      <div className="flex items-center justify-between gap-2 border-t border-[var(--border-hairline)] pt-3 text-[10px] font-mono font-semibold uppercase tracking-widest text-[var(--text-tertiary)]">
        <span>Intent</span>
        <span className="h-px flex-1 bg-[var(--border-hairline)]" aria-hidden="true" />
        <span>Observations</span>
        <span className="h-px flex-1 bg-[var(--border-hairline)]" aria-hidden="true" />
        <span>Evidence</span>
      </div>
    </div>
  );
}
