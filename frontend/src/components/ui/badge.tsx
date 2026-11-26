import React from "react";

export type BadgeVariant = "neutral" | "success" | "warning" | "danger" | "info";

export interface BadgeProps extends React.HTMLAttributes<HTMLSpanElement> {
  variant?: BadgeVariant;
  dot?: boolean;
}

export function Badge({
  className = "",
  variant = "neutral",
  dot = false,
  children,
  ...props
}: BadgeProps) {
  const variants: Record<BadgeVariant, { container: string; dot: string }> = {
    neutral: {
      container:
        "bg-[var(--bg-surface-raised)] text-[var(--text-secondary)] border-[var(--border-hairline)]",
      dot: "bg-[var(--text-tertiary)]",
    },
    success: {
      container:
        "bg-[var(--status-success-bg)] text-[var(--status-success-fg)] border-[var(--status-success-border)]",
      dot: "bg-[var(--status-success-fg)]",
    },
    warning: {
      container:
        "bg-[var(--status-warning-bg)] text-[var(--status-warning-fg)] border-[var(--status-warning-border)]",
      dot: "bg-[var(--status-warning-fg)]",
    },
    danger: {
      container:
        "bg-[var(--status-danger-bg)] text-[var(--status-danger-fg)] border-[var(--status-danger-border)]",
      dot: "bg-[var(--status-danger-fg)]",
    },
    info: {
      container:
        "bg-[var(--status-info-bg)] text-[var(--status-info-fg)] border-[var(--status-info-border)]",
      dot: "bg-[var(--status-info-fg)]",
    },
  };

  return (
    <span
      className={`inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium border font-mono tracking-tight whitespace-nowrap select-none shrink-0 ${variants[variant].container} ${className}`}
      {...props}
    >
      {dot && <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${variants[variant].dot}`} />}
      {children}
    </span>
  );
}

export function StatusBadge({
  status,
  className = "",
}: {
  status: string;
  className?: string;
}) {
  const normalized = status.toUpperCase();

  let variant: BadgeVariant = "neutral";
  if (["COMPLETED", "COMMITTED", "SYNCED", "CLOSED", "HEALTHY", "RESOLVED"].includes(normalized)) {
    variant = "success";
  } else if (
    ["PROCESSING", "VALIDATING", "ROUTING", "SYNCING", "QUEUED", "PENDING", "HALF_OPEN", "SCHEDULED"].includes(
      normalized
    )
  ) {
    variant = "info";
  } else if (
    ["RECOVERING", "HOLD_ACTIVE", "DEFERRED", "PENDING_RECONCILIATION", "BANK_SETTLED_CENTRAL_PENDING"].includes(
      normalized
    )
  ) {
    variant = "warning";
  } else if (["FAILED", "REVERSED", "REPLAY_FAILED", "OPEN", "NEEDS_ATTENTION", "UNHEALTHY"].includes(normalized)) {
    variant = "danger";
  }

  return (
    <Badge variant={variant} dot className={className}>
      {normalized}
    </Badge>
  );
}
