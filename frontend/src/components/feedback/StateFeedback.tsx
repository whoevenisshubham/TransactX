import React from "react";
import { Loader2, AlertTriangle, Inbox } from "lucide-react";
import { Button } from "../ui/button";

export function LoadingState({
  title = "Loading data...",
  description = "Fetching the latest records from the transaction orchestrator.",
  className = "",
}: {
  title?: string;
  description?: string;
  className?: string;
}) {
  return (
    <div
      role="status"
      aria-live="polite"
      className={`flex flex-col items-center justify-center p-12 text-center rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] ${className}`}
    >
      <Loader2 className="w-8 h-8 animate-spin text-[var(--action-accent-bg)] mb-4" />
      <h3 className="text-sm font-semibold text-[var(--text-primary)] mb-1">{title}</h3>
      <p className="text-xs text-[var(--text-secondary)] max-w-sm">{description}</p>
    </div>
  );
}

export function EmptyState({
  title = "No records found",
  description = "There are no matching items in this view.",
  actionLabel,
  onAction,
  icon,
  className = "",
}: {
  title?: string;
  description?: string;
  actionLabel?: string;
  onAction?: () => void;
  icon?: React.ReactNode;
  className?: string;
}) {
  return (
    <div
      role="region"
      aria-label="Empty state"
      className={`flex flex-col items-center justify-center p-12 text-center rounded-xl border border-dashed border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/30 ${className}`}
    >
      <div className="w-10 h-10 rounded-full bg-[var(--bg-surface-raised)] flex items-center justify-center text-[var(--text-secondary)] mb-4">
        {icon ?? <Inbox className="w-5 h-5" />}
      </div>
      <h3 className="text-sm font-semibold text-[var(--text-primary)] mb-1">{title}</h3>
      <p className="text-xs text-[var(--text-secondary)] max-w-sm mb-4">{description}</p>
      {actionLabel && onAction && (
        <Button variant="secondary" size="sm" onClick={onAction}>
          {actionLabel}
        </Button>
      )}
    </div>
  );
}

export function ErrorState({
  title = "Failed to load records",
  description = "An unexpected error occurred while communicating with the cluster.",
  onRetry,
  className = "",
}: {
  title?: string;
  description?: string;
  onRetry?: () => void;
  className?: string;
}) {
  return (
    <div
      role="alert"
      className={`flex flex-col items-center justify-center p-12 text-center rounded-xl border border-[var(--status-danger-border)] bg-[var(--status-danger-bg)]/30 ${className}`}
    >
      <div className="w-10 h-10 rounded-full bg-[var(--status-danger-bg)] flex items-center justify-center text-[var(--status-danger-fg)] mb-4">
        <AlertTriangle className="w-5 h-5" />
      </div>
      <h3 className="text-sm font-semibold text-[var(--status-danger-fg)] mb-1">{title}</h3>
      <p className="text-xs text-[var(--text-secondary)] max-w-sm mb-4">{description}</p>
      {onRetry && (
        <Button variant="secondary" size="sm" onClick={onRetry}>
          Try Again
        </Button>
      )}
    </div>
  );
}
