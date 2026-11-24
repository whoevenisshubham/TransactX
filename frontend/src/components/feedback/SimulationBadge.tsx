import React from "react";
import { AlertCircle } from "lucide-react";

export function SimulationBadge({ className = "" }: { className?: string }) {
  return (
    <div
      role="status"
      aria-label="Simulation Environment Indicator"
      className={`inline-flex items-center gap-2 px-3 py-1 rounded-full text-xs font-medium border border-[var(--status-warning-border)] bg-[var(--status-warning-bg)] text-[var(--status-warning-fg)] select-none shadow-sm ${className}`}
    >
      <span className="relative flex h-2 w-2">
        <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-[var(--status-warning-fg)] opacity-75"></span>
        <span className="relative inline-flex rounded-full h-2 w-2 bg-[var(--status-warning-fg)]"></span>
      </span>
      <span className="font-semibold tracking-wide">SIMULATED ENVIRONMENT</span>
      <span className="hidden sm:inline text-[var(--status-warning-fg)] font-medium">· NO REAL FUNDS AT RISK</span>
    </div>
  );
}
