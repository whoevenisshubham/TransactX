import React, { useEffect, useState } from "react";
import { ArrowRight, Play, RotateCcw, AlertTriangle, ShieldCheck } from "lucide-react";
import { NetworkCanvas, SpotlightCard, useAudioEngine, type NetworkSimulationMode } from "../../components/effects";

export const narrative = [
  {
    title: "An intent is saved.",
    label: "Original request",
    state: "Recorded",
    canvasMode: "healthy" as NetworkSimulationMode,
    a: "Hold pending",
    b: "Not yet contacted",
    outcome: "Awaiting observations",
    copy: "One payment identity follows the request, including a retry. A saved intent is not settled money.",
  },
  {
    title: "A reply goes missing.",
    label: "Independent observations",
    state: "Unknown outcome",
    canvasMode: "wire-drop" as NetworkSimulationMode,
    a: "Hold reported",
    b: "200 OK dropped on wire",
    outcome: "Pending reconciliation",
    copy: "A bank may have accepted an operation even when its reply is lost. The system checks the original operation; it does not invent a failure.",
  },
  {
    title: "The facts come together.",
    label: "Collected evidence",
    state: "Verified in this example",
    canvasMode: "recovering" as NetworkSimulationMode,
    a: "Source confirmed",
    b: "Credit confirmed",
    outcome: "Completed with evidence",
    copy: "After the participant outcome is established, the central record completes. The explanation cites what was actually collected.",
  },
];

const STRATEGY_LAB = [
  {
    id: "blind-retry",
    name: "1. Blind Retry",
    verdict: "UNSAFE · DOUBLE DEBIT",
    tone: "danger",
    summary: "Retrying without idempotency creates a second transfer while the first credit succeeded silently.",
    stateMap: [
      { label: "Bank A Debits", val: "₹200.00 (2× charged)" },
      { label: "Bank B Credits", val: "₹200.00 (duplicated)" },
      { label: "Audit Trail", val: "Divergent ledger" },
    ],
  },
  {
    id: "premature-fail",
    name: "2. Premature Failure",
    verdict: "INCONSISTENT · GHOST FUNDS",
    tone: "danger",
    summary: "Marking timeout as FAILED refunds sender immediately while recipient bank actually settled ₹100.",
    stateMap: [
      { label: "Sender App", val: "Refunded (+₹100)" },
      { label: "Bank B Schema", val: "Credited (+₹100)" },
      { label: "SQL Sum Check", val: "Conservation violation" },
    ],
  },
  {
    id: "premature-success",
    name: "3. Optimistic Fake Receipt",
    verdict: "MISLEADING RECEIPT",
    tone: "warning",
    summary: "Showing COMPLETED before verifying participant storage violates financial truth.",
    stateMap: [
      { label: "Customer UI", val: "Faked Completed" },
      { label: "Bank B State", val: "Unknown / Unverified" },
      { label: "Compliance", val: "Evidence mismatch" },
    ],
  },
  {
    id: "transactx",
    name: "4. TransactX Protocol",
    verdict: "AUTHORITATIVE RECOVERY",
    tone: "success",
    summary: "Quarantines into PENDING_RECONCILIATION, acquires fenced lease, probes original ID, and converges safely.",
    stateMap: [
      { label: "Central Status", val: "PENDING_RECONCILIATION -> COMPLETED" },
      { label: "Lease Fencing", val: "HeartbeatLease monotonic epoch" },
      { label: "SQL Sum Delta", val: "0 duplicate debits" },
    ],
  },
];

export function TwoPhaseNarrative() {
  const [step, setStep] = useState(1);
  const [strategyIdx, setStrategyIdx] = useState(3);
  const [autoPlay, setAutoPlay] = useState(false);
  const { play } = useAudioEngine();
  const moment = narrative[step];
  const strategy = STRATEGY_LAB[strategyIdx];

  useEffect(() => {
    if (!autoPlay) return;
    const id = window.setInterval(() => {
      setStep((s) => {
        const next = (s + 1) % narrative.length;
        if (next === 1) play("error");
        else if (next === 2) play("success");
        else play("packet");
        return next;
      });
    }, 2400);
    return () => window.clearInterval(id);
  }, [autoPlay, play]);

  return (
    <section id="how-it-works" className="border-t border-[var(--border-hairline)] bg-[var(--bg-canvas)]">
      <div className="mx-auto max-w-7xl px-4 py-14 sm:px-6 lg:px-8 lg:py-20 space-y-12">
        <div className="flex flex-col md:flex-row md:items-end justify-between gap-4">
          <div className="max-w-2xl space-y-2">
            <p className="text-xs font-mono font-semibold text-[var(--blueprint-cobalt,#2563eb)] m-0">
              A payment is more than a response.
            </p>
            <h2 className="font-display text-3xl font-bold tracking-tight text-[var(--text-primary)] sm:text-4xl m-0">
              When the connection breaks,
              <br />
              keep the story intact.
            </h2>
          </div>

          <div className="flex items-center gap-2 shrink-0">
            <button
              type="button"
              onClick={() => {
                play("tab");
                setAutoPlay((a) => !a);
              }}
              className="inline-flex items-center gap-1.5 rounded-lg border border-[var(--border-hairline)] bg-[var(--bg-surface)] px-3 py-2 text-xs font-mono font-semibold text-[var(--text-primary)] hover:border-[var(--border-active)] cursor-pointer"
            >
              <Play className="w-3.5 h-3.5 text-[var(--blueprint-emerald,#10b981)]" aria-hidden="true" />
              <span>{autoPlay ? "Pause walkthrough" : "Play sequence"}</span>
            </button>
            <button
              type="button"
              onClick={() => {
                setAutoPlay(false);
                setStep(0);
                setStrategyIdx(3);
                play("tick");
              }}
              aria-label="Reset failure simulator"
              className="inline-flex items-center gap-1.5 rounded-lg border border-[var(--border-hairline)] bg-[var(--bg-surface)] px-3 py-2 text-xs font-mono text-[var(--text-secondary)] hover:text-[var(--text-primary)] cursor-pointer"
            >
              <RotateCcw className="w-3.5 h-3.5" aria-hidden="true" />
              <span>Reset</span>
            </button>
          </div>
        </div>

        {/* Synchronized Stepper + Live Network Canvas */}
        <div className="grid grid-cols-1 gap-8 lg:grid-cols-12 lg:items-start">
          {/* Left Step Controls */}
          <div className="lg:col-span-5 space-y-3">
            <span className="inline-block text-[11px] font-mono uppercase tracking-wider text-[var(--text-tertiary)]">
              Illustrative scenario · not live observations
            </span>

            <div className="space-y-2">
              {narrative.map((item, index) => {
                const active = step === index;
                return (
                  <button
                    key={item.title}
                    type="button"
                    aria-pressed={active}
                    onClick={() => {
                      setAutoPlay(false);
                      setStep(index);
                      if (index === 1) play("error");
                      else if (index === 2) play("success");
                      else play("tab");
                    }}
                    className={`w-full flex items-center justify-between gap-3 rounded-xl border px-4 py-3.5 text-left transition-all cursor-pointer focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--border-focus)] ${
                      active
                        ? "border-[var(--action-accent-bg)] bg-[var(--bg-surface)] text-[var(--text-primary)] shadow-[var(--shadow-subtle)]"
                        : "border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/50 text-[var(--text-secondary)] hover:bg-[var(--bg-surface)] hover:text-[var(--text-primary)]"
                    }`}
                  >
                    <span className="flex items-center gap-3 min-w-0">
                      <span
                        className={`font-mono text-xs font-bold ${
                          active ? "text-[var(--accent-strong)]" : "text-[var(--text-tertiary)]"
                        }`}
                      >
                        0{index + 1}
                      </span>
                      <strong className="text-sm font-semibold truncate">{item.title}</strong>
                    </span>
                    <ArrowRight
                      className={`w-4 h-4 shrink-0 transition-transform ${
                        active ? "text-[var(--action-accent-bg)] translate-x-0.5" : "text-[var(--text-tertiary)]"
                      }`}
                      aria-hidden="true"
                    />
                  </button>
                );
              })}
            </div>

            <p className="pt-2 text-sm leading-relaxed text-[var(--text-secondary)] m-0">
              {moment.copy}
            </p>
          </div>

          {/* Right Live Stage */}
          <SpotlightCard
            tilt={false}
            data-cursor="Wire Stage"
            className="lg:col-span-7 rounded-2xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] p-6 shadow-[var(--shadow-subtle)]"
          >
            <div className="relative z-10 space-y-5" aria-live="polite">
              <div className="flex flex-wrap items-center justify-between gap-2 border-b border-[var(--border-hairline)] pb-4">
                <span className="text-xs font-mono font-semibold uppercase tracking-wider text-[var(--text-secondary)]">
                  {moment.label}
                </span>
                <span
                  className={`rounded-full border px-3 py-1 text-xs font-mono font-semibold ${
                    step === 1
                      ? "border-[var(--status-danger-border)] bg-[var(--status-danger-bg)] text-[var(--status-danger-fg)]"
                      : step === 2
                      ? "border-[var(--status-success-border)] bg-[var(--status-success-bg)] text-[var(--status-success-fg)]"
                      : "border-[var(--border-hairline)] bg-[var(--bg-surface-raised)] text-[var(--text-primary)]"
                  }`}
                >
                  {moment.state}
                </span>
              </div>

              <div className="rounded-xl border border-[var(--border-hairline)] tx-blueprint-grid overflow-hidden">
                <NetworkCanvas mode={moment.canvasMode} height={210} />
              </div>

              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <div className="rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/60 p-3.5">
                  <div className="font-mono text-[11px] uppercase text-[var(--text-tertiary)]">Bank A</div>
                  <div className="mt-0.5 text-sm font-semibold text-[var(--text-primary)]">{moment.a}</div>
                </div>
                <div className="rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/60 p-3.5">
                  <div className="font-mono text-[11px] uppercase text-[var(--text-tertiary)]">Bank B</div>
                  <div className="mt-0.5 text-sm font-semibold text-[var(--text-primary)]">{moment.b}</div>
                </div>
              </div>

              <div className="rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/40 p-4 flex items-center justify-between gap-3">
                <div>
                  <div className="text-[11px] font-mono uppercase tracking-wider text-[var(--text-tertiary)]">
                    Central payment outcome
                  </div>
                  <div className="text-base font-bold text-[var(--text-primary)]">{moment.outcome}</div>
                </div>
                <span className="font-mono text-xs text-[var(--text-tertiary)]">
                  Bank observations & central state stay distinct
                </span>
              </div>
            </div>
          </SpotlightCard>
        </div>

        {/* 4-Strategy Wire-Drop Comparison Matrix */}
        <div className="rounded-2xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] p-5 sm:p-6 space-y-5">
          <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 border-b border-[var(--border-hairline)] pb-4">
            <div>
              <span className="font-mono text-xs font-semibold text-[var(--blueprint-cobalt,#2563eb)]">
                Drop the wire lab
              </span>
              <h3 className="font-display text-lg font-bold text-[var(--text-primary)] m-0">
                Compare naive failure handling vs. TransactX reconciliation
              </h3>
            </div>
            <span
              className={`inline-flex items-center gap-1.5 rounded-full border px-3 py-1 font-mono text-[11px] font-semibold ${
                strategy.tone === "success"
                  ? "border-[var(--status-success-border)] bg-[var(--status-success-bg)] text-[var(--status-success-fg)]"
                  : strategy.tone === "warning"
                  ? "border-[var(--status-warning-border)] bg-[var(--status-warning-bg)] text-[var(--status-warning-fg)]"
                  : "border-[var(--status-danger-border)] bg-[var(--status-danger-bg)] text-[var(--status-danger-fg)]"
              }`}
            >
              {strategy.tone === "success" ? (
                <ShieldCheck className="w-3.5 h-3.5" aria-hidden="true" />
              ) : (
                <AlertTriangle className="w-3.5 h-3.5" aria-hidden="true" />
              )}
              <span>{strategy.verdict}</span>
            </span>
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-2">
            {STRATEGY_LAB.map((item, idx) => {
              const active = idx === strategyIdx;
              return (
                <button
                  key={item.id}
                  type="button"
                  onClick={() => {
                    setStrategyIdx(idx);
                    play(item.tone === "success" ? "success" : "error");
                  }}
                  className={`rounded-xl border px-3.5 py-2.5 text-left text-xs font-mono font-semibold transition-all cursor-pointer ${
                    active
                      ? "border-[var(--action-accent-bg)] bg-[var(--status-info-bg)]/50 text-[var(--text-primary)]"
                      : "border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/50 text-[var(--text-secondary)] hover:text-[var(--text-primary)]"
                  }`}
                >
                  {item.name}
                </button>
              );
            })}
          </div>

          <p className="text-sm text-[var(--text-secondary)] m-0">{strategy.summary}</p>

          <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
            {strategy.stateMap.map((entry) => (
              <div
                key={entry.label}
                className="rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/50 p-3.5"
              >
                <div className="font-mono text-[11px] text-[var(--text-tertiary)]">{entry.label}</div>
                <div className="mt-1 font-mono text-xs font-bold text-[var(--text-primary)]">{entry.val}</div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </section>
  );
}
