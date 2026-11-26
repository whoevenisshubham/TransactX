import React, { useState } from "react";
import { ArrowRight } from "lucide-react";

export const narrative = [
  {
    title: "An intent is saved.",
    label: "Original request",
    state: "Recorded",
    copy: "One payment identity follows the request, including a retry. A saved intent is not settled money.",
    a: "Not yet observed",
    b: "Not yet observed",
    outcome: "Awaiting observations",
  },
  {
    title: "A reply goes missing.",
    label: "Independent observations",
    state: "Unknown outcome",
    copy: "A bank may have accepted an operation even when its reply is lost. The system checks the original operation; it does not invent a failure.",
    a: "Hold reported",
    b: "Reply unavailable",
    outcome: "Pending reconciliation",
  },
  {
    title: "The facts come together.",
    label: "Collected evidence",
    state: "Verified in this example",
    copy: "After the participant outcome is established, the central record completes. The explanation cites what was actually collected.",
    a: "Source confirmed",
    b: "Credit confirmed",
    outcome: "Completed with evidence",
  },
];

export function TwoPhaseNarrative() {
  const [step, setStep] = useState(1);
  const moment = narrative[step];

  return (
    <section id="how-it-works" className="border-t border-[var(--border-hairline)] bg-[var(--bg-canvas)]">
      <div className="mx-auto max-w-7xl px-4 py-14 sm:px-6 lg:px-8 lg:py-20 space-y-10">
        <div className="max-w-2xl space-y-2">
          <p className="text-xs font-mono font-semibold text-[var(--action-accent-bg)] m-0">
            A payment is more than a response.
          </p>
          <h2 className="text-3xl font-bold tracking-tight text-[var(--text-primary)] sm:text-4xl m-0">
            When the connection breaks,
            <br />
            keep the story intact.
          </h2>
        </div>

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
                    onClick={() => setStep(index)}
                    className={`w-full flex items-center justify-between gap-3 rounded-xl border px-4 py-3.5 text-left transition-colors cursor-pointer focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--border-focus)] ${
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
                      className={`w-4 h-4 shrink-0 ${
                        active ? "text-[var(--action-accent-bg)]" : "text-[var(--text-tertiary)]"
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
          <div
            className="lg:col-span-7 rounded-2xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] p-6 shadow-[var(--shadow-subtle)] space-y-6"
            aria-live="polite"
          >
            <div className="flex flex-wrap items-center justify-between gap-2 border-b border-[var(--border-hairline)] pb-4">
              <span className="text-xs font-mono font-semibold uppercase tracking-wider text-[var(--text-secondary)]">
                {moment.label}
              </span>
              <span className="rounded-full border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)] px-3 py-1 text-xs font-mono font-semibold text-[var(--text-primary)]">
                {moment.state}
              </span>
            </div>

            <div className="grid grid-cols-1 gap-4 sm:grid-cols-3 items-center">
              <div className="rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/60 p-4 space-y-1">
                <div className="flex items-center gap-2">
                  <span className="inline-flex h-6 w-6 items-center justify-center rounded-full bg-[var(--bg-surface)] border border-[var(--border-hairline)] font-mono text-xs font-bold text-[var(--text-primary)]">
                    A
                  </span>
                  <strong className="text-sm text-[var(--text-primary)]">Bank A</strong>
                </div>
                <small className="block text-xs font-mono text-[var(--text-secondary)]">{moment.a}</small>
              </div>

              <div className="flex justify-center text-[var(--text-tertiary)]" aria-hidden="true">
                <ArrowRight className="w-5 h-5" />
              </div>

              <div className="rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/60 p-4 space-y-1">
                <div className="flex items-center gap-2">
                  <span className="inline-flex h-6 w-6 items-center justify-center rounded-full bg-[var(--bg-surface)] border border-[var(--border-hairline)] font-mono text-xs font-bold text-[var(--text-primary)]">
                    B
                  </span>
                  <strong className="text-sm text-[var(--text-primary)]">Bank B</strong>
                </div>
                <small className="block text-xs font-mono text-[var(--text-secondary)]">{moment.b}</small>
              </div>
            </div>

            <div className="rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)] p-4 space-y-1">
              <span className="text-[11px] font-mono uppercase tracking-wider text-[var(--text-tertiary)]">
                Central payment
              </span>
              <strong className="block text-base font-bold text-[var(--text-primary)]">
                {moment.outcome}
              </strong>
              <small className="block text-xs text-[var(--text-secondary)]">
                Bank observations and the authoritative outcome are distinct.
              </small>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}
