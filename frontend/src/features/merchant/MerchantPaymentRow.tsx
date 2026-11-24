import { useState } from "react";
import { Amount, Avatar, Icon, StatusBadge } from "../../components";
import { CopyValue } from "../../shared/CopyValue";
import { formatDate } from "../../components";
import type { Payment } from "../../types";

export interface MerchantPaymentRowProps {
  payment: Payment;
  expanded?: boolean;
}

export function MerchantPaymentRow({ payment, expanded = false }: MerchantPaymentRowProps) {
  const [isExpanded, setIsExpanded] = useState(expanded);

  const settlementLabel = payment.completedAt
    ? formatDate(payment.completedAt)
    : ["FAILED", "REVERSED"].includes(payment.state)
    ? "No completed settlement recorded"
    : "Not yet confirmed";

  return (
    <details
      className="merchant-payment-row merchant-receipt"
      open={isExpanded}
      onToggle={(event) => setIsExpanded(event.currentTarget.open)}
    >
      <summary>
        <Avatar name={payment.senderName} />
        <span className="payment-main">
          <strong>{payment.senderName}</strong>
          <small>{payment.senderPaymentIdentifier} · {formatDate(payment.createdAt)}</small>
        </span>
        <span className="payment-amount">
          <Amount paise={payment.amountPaise} />
          <StatusBadge state={payment.state} />
        </span>
        <Icon name="chevron" />
      </summary>
      <div className="merchant-payment-expanded">
        <div>
          <span className="section-kicker">Payment reference</span>
          <span className="merchant-full-reference">{payment.id}</span>
          <CopyValue value={payment.id} label="Copy reference" />
        </div>
        <div>
          <span className="section-kicker">Recorded route</span>
          <span>{payment.sourceBankCode ?? "Unknown bank"} → {payment.destinationBankCode ?? "Unknown bank"}</span>
        </div>
        <div>
          <span className="section-kicker">Settlement</span>
          <span>{settlementLabel}</span>
        </div>
        {payment.note && (
          <div>
            <span className="section-kicker">Note</span>
            <span>{payment.note}</span>
          </div>
        )}
        {payment.failureReason && (
          <p className="inline-error">{payment.failureReason}</p>
        )}
      </div>
    </details>
  );
}
