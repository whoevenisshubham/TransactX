import React, { useState } from "react";
import { ArrowRight, ArrowUpRight, Sparkles, Activity, RotateCcw } from "lucide-react";
import { BrandMark } from "../../components";
import {
  SpotlightCard,
  AnimatedCheckmark,
  DotSentinel,
  ScrambleText,
  useAudioEngine,
} from "../../components/effects";

export function PaymentScene() {
  const [pulseCount, setPulseCount] = useState(0);
  const { play } = useAudioEngine();

  return (
    <SpotlightCard
      data-cursor="Inspect Receipt"
      accent="rgba(16, 185, 129, 0.16)"
      className="rounded-2xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] p-5 sm:p-6 shadow-[var(--shadow-raised)] space-y-5"
    >
      <div
        className="relative z-10 space-y-5"
        aria-label="Illustrative payment receipt, bank path and cited investigation"
      >
        {/* Top bar */}
        <div className="flex items-center justify-between gap-2 text-[11px] font-mono text-[var(--text-tertiary)]">
          <span className="inline-flex items-center gap-2 font-semibold uppercase tracking-wider text-[var(--text-secondary)]">
            <DotSentinel state="success" />
            <span>The payment, opened up</span>
          </span>
          <span className="inline-flex items-center gap-1.5">
            <button
              type="button"
              onClick={() => {
                play("packet");
                setPulseCount((c) => c + 1);
              }}
              aria-label="Replay illustrative packet pulse"
              className="inline-flex items-center gap-1 rounded-full border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)] px-2 py-0.5 text-[10px] text-[var(--text-secondary)] hover:text-[var(--text-primary)] cursor-pointer transition-colors"
            >
              <RotateCcw className="w-2.5 h-2.5" aria-hidden="true" />
              <span>Pulse</span>
            </button>
            <span className="rounded-full border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)] px-2.5 py-0.5">
              Illustrative example
            </span>
          </span>
        </div>

        {/* Animated Bank Corridor Path */}
        <div className="relative overflow-hidden rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/70 p-3.5">
          <svg
            key={pulseCount}
            viewBox="0 0 420 56"
            className="w-full h-14"
            aria-hidden="true"
          >
            <line
              x1="54"
              y1="26"
              x2="195"
              y2="26"
              stroke="var(--blueprint-cobalt,#2563eb)"
              strokeWidth="1.5"
              strokeDasharray="4 4"
            >
              <animate attributeName="stroke-dashoffset" from="16" to="0" dur="1.2s" repeatCount="indefinite" />
            </line>
            <line
              x1="225"
              y1="26"
              x2="366"
              y2="26"
              stroke="var(--blueprint-emerald,#10b981)"
              strokeWidth="1.5"
              strokeDasharray="4 4"
            >
              <animate attributeName="stroke-dashoffset" from="16" to="0" dur="1.2s" repeatCount="indefinite" />
            </line>

            <circle cx="42" cy="26" r="14" fill="var(--bg-surface)" stroke="var(--blueprint-cobalt,#2563eb)" strokeWidth="1.6" />
            <text x="42" y="30" textAnchor="middle" className="font-mono" fontSize="11" fontWeight="700" fill="var(--text-primary)">
              A
            </text>

            <circle cx="210" cy="26" r="15" fill="var(--bg-surface)" stroke="var(--blueprint-violet,#8b5cf6)" strokeWidth="1.6" />
            <text x="210" y="30" textAnchor="middle" className="font-mono" fontSize="10" fontWeight="700" fill="var(--text-primary)">
              TX
            </text>

            <circle cx="378" cy="26" r="14" fill="var(--bg-surface)" stroke="var(--blueprint-emerald,#10b981)" strokeWidth="1.6" />
            <text x="378" y="30" textAnchor="middle" className="font-mono" fontSize="11" fontWeight="700" fill="var(--text-primary)">
              B
            </text>

            <text x="42" y="51" textAnchor="middle" className="font-mono" fontSize="8.5" fill="var(--text-tertiary)">
              Bank A · Hold
            </text>
            <text x="210" y="51" textAnchor="middle" className="font-mono" fontSize="8.5" fill="var(--text-tertiary)">
              Recorded Outcome
            </text>
            <text x="378" y="51" textAnchor="middle" className="font-mono" fontSize="8.5" fill="var(--text-tertiary)">
              Bank B · Credit
            </text>
          </svg>
        </div>

        {/* Receipt + Cited Investigation */}
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div className="rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] p-4 space-y-3">
            <div className="flex items-center justify-between">
              <span
                key={`chk-${pulseCount}`}
                className="inline-flex h-8 w-8 items-center justify-center rounded-full bg-[var(--status-success-bg)] text-[var(--status-success-fg)]"
              >
                <AnimatedCheckmark size={18} />
              </span>
              <span className="rounded-full border border-[var(--status-success-border)] bg-[var(--status-success-bg)] px-2.5 py-0.5 text-[11px] font-mono font-semibold text-[var(--status-success-fg)]">
                <ScrambleText text="Completed" />
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
              <h3 className="font-display text-base font-bold tracking-tight text-[var(--text-primary)] m-0 leading-snug">
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
              <p className="text-[10px] font-mono text-[var(--text-tertiary)] m-0 flex items-center gap-1.5">
                <Activity className="w-3 h-3 text-[var(--blueprint-violet,#8b5cf6)]" aria-hidden="true" />
                <span>Illustrative report · no provider request</span>
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
    </SpotlightCard>
  );
}
