import React from "react";

export function SimulationBadge({ className = "" }: { className?: string }) {
  return (
    <div
      role="status"
      aria-label="Simulation Environment Indicator"
      className={`inline-flex items-center gap-2 px-3 py-1 rounded-full text-xs font-medium border border-[var(--status-warning-border)] bg-[var(--status-warning-bg)] text-[var(--status-warning-fg)] select-none whitespace-nowrap shrink-0 shadow-[var(--shadow-subtle)] ${className}`}
    >
      <span className="relative flex h-2 w-2 shrink-0" aria-hidden="true">
        <span className="animate-ping motion-reduce:animate-none absolute inline-flex h-full w-full rounded-full bg-[var(--status-warning-fg)] opacity-60"></span>
        <span className="relative inline-flex rounded-full h-2 w-2 bg-[var(--status-warning-fg)]"></span>
      </span>
      <span className="font-semibold tracking-wide font-mono text-[11px]">SIMULATED ENVIRONMENT</span>
      <span className="hidden md:inline text-[var(--status-warning-fg)] font-medium font-mono text-[11px]">
        · NO REAL FUNDS
      </span>
    </div>
  );
}
