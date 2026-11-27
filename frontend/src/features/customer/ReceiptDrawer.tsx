import { useEffect, useState } from "react";
import { api } from "../../api";
import {
  Amount,
  Avatar,
  Button,
  formatDate,
  Icon,
  InlineError,
  paymentResultCopy,
  StatusBadge,
} from "../../components";
import { DotSentinel, SpotlightCard } from "../../components/effects";
import { DetailsSkeleton } from "../../shared/ShellStates";
import { CopyValue } from "../../shared/CopyValue";
import type { Payment } from "../../types";
import type { ReceiptDrawerProps } from "./types";

export function ReceiptDrawer({
  paymentID,
  token,
  onNavigate,
}: ReceiptDrawerProps) {
  const [payment, setPayment] = useState<Payment | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    let active = true;
    setLoading(true);
    setError("");
    api
      .payment(paymentID, token)
      .then((p) => {
        if (active) setPayment(p);
      })
      .catch((caught) => {
        if (active) setError(caught instanceof Error ? caught.message : "Transaction not found");
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [paymentID, token]);

  if (loading) return <DetailsSkeleton />;

  if (error || !payment) {
    return (
      <div className="center-state">
        <InlineError message={error || "Transaction not found"} />
        <Button variant="secondary" onClick={() => onNavigate("transactions")}>
          Back to transactions
        </Button>
      </div>
    );
  }

  const copy = paymentResultCopy(payment);
  const isReceived = payment.direction === "RECEIVED";
  const recipientLabel = isReceived ? "Sender" : "Recipient";
  const counterparty = isReceived ? payment.senderName : payment.receiverName;
  const identifier = isReceived
    ? payment.senderPaymentIdentifier
    : payment.receiverPaymentIdentifier;
  const sign = isReceived ? "+ " : "− ";

  return (
    <div className="details-page">
      <button
        type="button"
        className="back-link"
        onClick={() => onNavigate("transactions")}
      >
        <Icon name="arrow" size={15} />
        Back to transactions
      </button>

      <div className="details-heading">
        <div>
          <p className="eyebrow">Audit record</p>
          <h1>Payment details</h1>
        </div>
        <StatusBadge state={payment.state} />
      </div>

      <SpotlightCard
        as="section"
        accent={
          payment.state === "COMPLETED"
            ? "emerald"
            : payment.state === "FAILED" || payment.state === "REVERSED"
            ? "crimson"
            : "cobalt"
        }
        className="details-hero"
      >
        <div>
          <span className="detail-label flex items-center gap-2">
            <DotSentinel
              state={
                payment.state === "COMPLETED"
                  ? "success"
                  : payment.state === "FAILED" || payment.state === "REVERSED"
                  ? "error"
                  : "warning"
              }
            />
            Amount
          </span>
          <Amount paise={payment.amountPaise} sign={sign} prominent />
          <p className="detail-state">{copy.description}</p>
        </div>
        <div className="details-recipient">
          <Avatar name={counterparty} />
          <span>
            <small>{recipientLabel}</small>
            <strong>{counterparty}</strong>
            <span>{identifier}</span>
          </span>
        </div>
      </SpotlightCard>

      <section className="record-section">
        <p className="eyebrow">Record</p>
        <div className="record-list">
          <Record label="Status">
            <StatusBadge state={payment.state} />
          </Record>
          <Record label="Direction" value={isReceived ? "Received (+)" : "Sent (−)"} />
          <Record label="Initiated" value={formatDate(payment.createdAt)} />
          <Record
            label="Settled"
            value={
              payment.completedAt
                ? formatDate(payment.completedAt)
                : ["FAILED", "REVERSED"].includes(payment.state)
                ? "No completed settlement recorded"
                : "Pending network settlement"
            }
          />
          {payment.durationMs !== undefined && (
            <Record label="Duration" value={`${payment.durationMs} ms`} />
          )}
          {payment.sourceBankName && (
            <Record
              label="Source bank"
              value={`${payment.sourceBankName}${
                payment.sourceBankCode ? ` (${payment.sourceBankCode})` : ""
              }`}
            />
          )}
          {payment.destinationBankName && (
            <Record
              label="Destination bank"
              value={`${payment.destinationBankName}${
                payment.destinationBankCode ? ` (${payment.destinationBankCode})` : ""
              }`}
            />
          )}
          {payment.note && <Record label="Note" value={payment.note} />}
          <Record label="Network origin" value={payment.origin} />
          <Record label="Payment reference" mono>
            <span className="receipt-reference font-mono tabular-nums break-all">
              {payment.id}
              <CopyValue value={payment.id} label="Copy payment reference" />
            </span>
          </Record>
        </div>
      </section>

      {payment.failureReason && (
        <div className="detail-alert" role="alert">
          <Icon name="alert" size={17} />
          <span>{payment.failureReason}</span>
        </div>
      )}
    </div>
  );
}

function Record({
  label,
  value,
  mono,
  children,
}: {
  label: string;
  value?: string;
  mono?: boolean;
  children?: React.ReactNode;
}) {
  return (
    <div className="record-row">
      <span>{label}</span>
      {children ?? <strong className={mono ? "mono" : ""}>{value}</strong>}
    </div>
  );
}
