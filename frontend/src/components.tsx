import { useState, type ButtonHTMLAttributes, type ReactNode } from "react";
import type { Payment, View } from "./types";

export function Icon({ name, size = 18 }: { name: "home" | "send" | "activity" | "arrow" | "chevron" | "check" | "alert" | "close"; size?: number }) {
  const paths: Record<string, ReactNode> = {
    home: <><path d="m3 9 6-5 6 5" /><path d="M5 8v7h8V8M8 15v-4h2v4" /></>,
    send: <><path d="m3 14 12-8-4 12-2-5-6-1Z" /><path d="m9 13 6-7" /></>,
    activity: <><path d="M3 12h3l2-5 3 10 2-5h3" /><path d="M3 4h12M3 20h12" /></>,
    arrow: <><path d="M4 12h11" /><path d="m11 7 5 5-5 5" /></>,
    chevron: <path d="m6 9 3 3 3-3" />,
    check: <path d="m4 9 3 3 6-7" />,
    alert: <><path d="M9 3 2.5 15h13L9 3Z" /><path d="M9 8v3M9 13h.01" /></>,
    close: <><path d="m5 5 8 8M13 5l-8 8" /></>,
  };
  return <svg aria-hidden="true" className="icon" width={size} height={size} viewBox="0 0 18 18" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round">{paths[name]}</svg>;
}

export function BrandMark() { return <span className="brand-mark" aria-hidden="true"><span /><span /><span /></span>; }
export function Button({ variant = "primary", children, className = "", ...props }: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: "primary" | "secondary" | "quiet" | "danger"; className?: string }) { return <button className={`button button-${variant} ${className}`} {...props}>{children}</button>; }
export function PageHeader({ eyebrow, title, description, action }: { eyebrow?: string; title: string; description?: string; action?: ReactNode }) { return <div className="page-header"><div>{eyebrow && <p className="eyebrow">{eyebrow}</p>}<h1>{title}</h1>{description && <p className="page-description">{description}</p>}</div>{action}</div>; }

export function StatusBadge({ state }: { state: string }) {
  const normalized = state.toLowerCase();
  let label = state.replaceAll("_", " ").toLowerCase();
  let tone: "success" | "error" | "warning" | "neutral" = "neutral";

  switch (state) {
    case "COMPLETED":
      label = "Completed";
      tone = "success";
      break;
    case "FAILED":
      label = "Failed";
      tone = "error";
      break;
    case "REVERSED":
      label = "Reversed";
      tone = "error";
      break;
    case "PROCESSING":
      label = "Processing";
      tone = "warning";
      break;
    case "PENDING_RECONCILIATION":
      label = "Pending Confirmation";
      tone = "warning";
      break;
    case "BANK_SETTLED_CENTRAL_PENDING":
      label = "Syncing Central";
      tone = "warning";
      break;
    case "OFFLINE_CAPTURED":
      label = "Captured Offline";
      tone = "neutral";
      break;
    case "QUEUED":
      label = "Queued";
      tone = "neutral";
      break;
    case "SYNCING":
      label = "Syncing";
      tone = "warning";
      break;
    case "RETRYABLE":
      label = "Retryable";
      tone = "warning";
      break;
    case "SYNCED":
      label = "Synced";
      tone = "neutral";
      break;
    case "REPLAY_FAILED":
      label = "Replay Failed";
      tone = "error";
      break;
    default:
      if (normalized.includes("pending")) tone = "warning";
  }

  return <span className={`status-badge status-${tone}`}><span className="status-dot" />{label}</span>;
}

export function formatPaise(paise: number) {
  if (!Number.isSafeInteger(paise) || paise < 0) return "₹0.00";
  const value = BigInt(paise);
  const whole = (value / 100n).toString().replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  const fraction = (value % 100n).toString().padStart(2, "0");
  return `₹${whole}.${fraction}`;
}
export function Amount({ paise, sign = "", prominent = false }: { paise: number; sign?: string; prominent?: boolean }) { return <span className={`amount ${prominent ? "amount-prominent" : ""}`}>{sign}{formatPaise(paise)}</span>; }
export function Avatar({ name }: { name: string }) { const initials = name.split(" ").map((part) => part[0]).join("").slice(0, 2).toUpperCase(); return <span className="avatar" aria-hidden="true">{initials || "TX"}</span>; }
export function Skeleton({ className = "" }: { className?: string }) { return <span className={`skeleton ${className}`} aria-hidden="true" />; }
export function EmptyState({ title, description, action }: { title: string; description: string; action?: ReactNode }) { return <div className="empty-state"><span className="empty-mark"><Icon name="activity" size={20} /></span><h3>{title}</h3><p>{description}</p>{action}</div>; }

export function PaymentRow({ payment, onClick }: { payment: Payment; onClick: () => void }) {
  const completed = payment.state === "COMPLETED";
  const counterparty = payment.direction === "RECEIVED" ? payment.senderName : payment.receiverName;
  const sign = payment.direction === "RECEIVED" ? "+ " : "− ";
  const dirClass = payment.direction === "RECEIVED" ? "payment-received" : "payment-sent";
  return <button className={`payment-row ${dirClass}`} onClick={onClick}><Avatar name={counterparty} /><span className="payment-main"><strong>{counterparty}</strong><small>{payment.direction === "RECEIVED" ? "Received" : "Sent"} · {formatDate(payment.createdAt)}</small></span><span className={`payment-amount ${completed ? "" : "payment-muted"}`}><Amount paise={payment.amountPaise} sign={sign} /><small><StatusBadge state={payment.state} /></small></span><Icon name="chevron" size={16} /></button>;
}
export function formatDate(value: string) { return new Intl.DateTimeFormat("en-IN", { day: "2-digit", month: "short", year: "numeric", hour: "2-digit", minute: "2-digit" }).format(new Date(value)); }
export function formatShortDate(value: string) { return new Intl.DateTimeFormat("en-IN", { day: "2-digit", month: "short" }).format(new Date(value)); }
export function CopyButton({ text, label = "Copy" }: { text: string; label?: string }) {
  const [copied, setCopied] = useState(false);
  function handleCopy() {
    if (!text) return;
    void navigator.clipboard?.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  }
  return (
    <button type="button" className="copy-button" onClick={handleCopy} title={`Copy ${text}`}>
      {copied ? "Copied!" : label}
    </button>
  );
}

export function paymentResultCopy(payment: Payment) {
  if (payment.state === "COMPLETED") return { title: "Payment complete", description: "Your payment was processed and settled successfully.", tone: "success" as const };
  if (payment.state === "FAILED" || payment.state === "REVERSED") return { title: "Payment not completed", description: payment.failureReason ?? "The payment could not be completed.", tone: "error" as const };
  if (payment.state === "PROCESSING") return { title: "Payment processing", description: "The network is processing this payment. Outcome is not confirmed yet.", tone: "pending" as const };
  if (payment.state === "PENDING_RECONCILIATION") return { title: "Pending confirmation", description: "Network verification in progress. Money may have moved downstream; status will update upon status resolution.", tone: "pending" as const };
  if (payment.state === "BANK_SETTLED_CENTRAL_PENDING") return { title: "Syncing ledger", description: "Bank transfer completed successfully. Central ledger records are being updated.", tone: "pending" as const };
  return { title: "Status unconfirmed", description: "The outcome is not yet known. Do not initiate a duplicate payment attempt.", tone: "pending" as const };
}
export function navLabel(view: View) { return view === "home" ? "Overview" : view === "pay" ? "Pay" : "Transactions"; }
