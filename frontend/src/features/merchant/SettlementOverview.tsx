import { useState } from "react";
import { AnimatePresence, motion } from "motion/react";
import { Button, EmptyState, Icon, PageHeader } from "../../components";
import { DotSentinel, useAudioEngine } from "../../components/effects";
import { MerchantPaymentRow } from "./MerchantPaymentRow";
import type { Payment, SettlementTone } from "./types";

export interface SettlementOverviewProps {
  payments: Payment[];
  onRefresh: () => void;
}

export function SettlementOverview({ payments, onRefresh }: SettlementOverviewProps) {
  const received = payments.filter((p) => p.direction === "RECEIVED");
  const completed = received.filter((p) => p.state === "COMPLETED");
  const pending = received.filter((p) =>
    ["PROCESSING", "PENDING_RECONCILIATION", "BANK_SETTLED_CENTRAL_PENDING"].includes(p.state)
  );
  const failed = received.filter((p) => ["FAILED", "REVERSED"].includes(p.state));
  const other = received.filter(
    (p) =>
      ![
        "COMPLETED",
        "PROCESSING",
        "PENDING_RECONCILIATION",
        "BANK_SETTLED_CENTRAL_PENDING",
        "FAILED",
        "REVERSED",
      ].includes(p.state)
  );

  return (
    <>
      <PageHeader
        eyebrow="Settlement"
        title="Settlement status"
        description="Outcomes in the latest 50 loaded records. These are payment states, not a separate payout schedule."
        action={
          <Button variant="secondary" onClick={onRefresh} disabled={typeof navigator !== "undefined" && !navigator.onLine}>
            <Icon name="activity" size={16} />
            Refresh
          </Button>
        }
      />

      {received.length === 0 ? (
        <EmptyState
          title="No incoming payments"
          description="Settlement status will appear here once payments are received."
        />
      ) : (
        <div className="merchant-settlement-layout space-y-4">
          <SettlementGroup
            title="Completed"
            count={completed.length}
            tone="success"
            payments={completed}
            description="Payments with a confirmed completed state."
          />
          <SettlementGroup
            title="Pending confirmation"
            count={pending.length}
            tone="warning"
            payments={pending}
            description="Payment is being processed. Outcome is not yet confirmed — do not consider this settled."
          />
          <SettlementGroup
            title="Failed / Reversed"
            count={failed.length}
            tone="error"
            payments={failed}
            description="Payment did not reach a completed state. Review the payment details for the current outcome."
          />
          {other.length > 0 && (
            <SettlementGroup
              title="Other"
              count={other.length}
              tone="neutral"
              payments={other}
              description="Status is currently unclassified."
            />
          )}
        </div>
      )}
    </>
  );
}

function SettlementGroup({
  title,
  count,
  tone,
  payments,
  description,
}: {
  title: string;
  count: number;
  tone: SettlementTone;
  payments: Payment[];
  description: string;
}) {
  const [open, setOpen] = useState(tone === "success" || tone === "warning");
  const { play } = useAudioEngine();
  const sentinelState =
    tone === "success" ? "success" : tone === "warning" ? "warning" : tone === "error" ? "error" : "idle";

  return (
    <div className={`settlement-group settlement-group-${tone} rounded-2xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] overflow-hidden transition-colors hover:border-[var(--border-active)]`}>
      <button
        type="button"
        className="settlement-group-header w-full text-left"
        onClick={() => {
          play("tab");
          setOpen((o) => !o);
        }}
        aria-expanded={open}
      >
        <span className="settlement-group-title font-semibold text-[var(--text-primary)] inline-flex items-center gap-2">
          <DotSentinel state={sentinelState} />
          {title}
          <span className="settlement-count font-mono tabular-nums">{count}</span>
        </span>
        <span className="settlement-description">{description}</span>
        <span className={`inline-flex transition-transform duration-200 ${open ? "rotate-90" : ""}`}>
          <Icon name="chevron" size={16} />
        </span>
      </button>
      <AnimatePresence initial={false}>
        {open && (
          <motion.div
            initial={{ opacity: 0.96, y: -4 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0.96, y: -4 }}
            transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
            className="settlement-group-body"
          >
            {payments.length === 0 ? (
              <p className="settlement-empty">No payments in this category.</p>
            ) : (
              payments.map((p) => (
                <MerchantPaymentRow key={p.id} payment={p} expanded />
              ))
            )}
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}
