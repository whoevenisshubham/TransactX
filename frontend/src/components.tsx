import type { ButtonHTMLAttributes, ReactNode } from "react";
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

export function BrandMark() {
  return <span className="brand-mark" aria-hidden="true"><span /><span /><span /></span>;
}

export function Button({ variant = "primary", children, className = "", ...props }: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: "primary" | "secondary" | "quiet" | "danger"; className?: string }) {
  return <button className={`button button-${variant} ${className}`} {...props}>{children}</button>;
}

export function PageHeader({ eyebrow, title, description, action }: { eyebrow?: string; title: string; description?: string; action?: ReactNode }) {
  return <div className="page-header"><div>{eyebrow && <p className="eyebrow">{eyebrow}</p>}<h1>{title}</h1>{description && <p className="page-description">{description}</p>}</div>{action}</div>;
}

export function StatusBadge({ state }: { state: string }) {
  const normalized = state.toLowerCase();
  const label = normalized === "completed" ? "Completed" : normalized === "failed" ? "Not completed" : normalized === "pending_reconciliation" || normalized === "bank_settled_central_pending" ? "Confirming" : normalized === "processing" ? "Processing" : state.replaceAll("_", " ").toLowerCase();
  const tone = normalized === "completed" ? "success" : normalized === "failed" || normalized === "reversed" ? "error" : normalized.includes("pending") || normalized === "processing" ? "warning" : "neutral";
  return <span className={`status-badge status-${tone}`}><span className="status-dot" />{label}</span>;
}

export function Amount({ paise, sign = "", prominent = false }: { paise: number; sign?: string; prominent?: boolean }) {
  return <span className={`amount ${prominent ? "amount-prominent" : ""}`}>{sign}₹{(paise / 100).toLocaleString("en-IN", { minimumFractionDigits: 2, maximumFractionDigits: 2 })}</span>;
}

export function Avatar({ name }: { name: string }) {
  const initials = name.split(" ").map((part) => part[0]).join("").slice(0, 2).toUpperCase();
  return <span className="avatar" aria-hidden="true">{initials || "TX"}</span>;
}

export function Skeleton({ className = "" }: { className?: string }) { return <span className={`skeleton ${className}`} aria-hidden="true" />; }

export function EmptyState({ title, description, action }: { title: string; description: string; action?: ReactNode }) {
  return <div className="empty-state"><span className="empty-mark"><Icon name="activity" size={20} /></span><h3>{title}</h3><p>{description}</p>{action}</div>;
}

export function PaymentRow({ payment, onClick }: { payment: Payment; onClick: () => void }) {
  const completed = payment.state === "COMPLETED";
  return <button className="payment-row" onClick={onClick}><Avatar name={payment.counterpartyName} /><span className="payment-main"><strong>{payment.counterpartyName}</strong><small>{formatDate(payment.createdAt)}</small></span><span className={`payment-amount ${completed ? "" : "payment-muted"}`}><Amount paise={payment.amountPaise} sign="− " /><small><StatusBadge state={payment.state} /></small></span><Icon name="chevron" size={16} /></button>;
}

export function formatDate(value: string) {
  return new Intl.DateTimeFormat("en-IN", { day: "2-digit", month: "short", year: "numeric", hour: "2-digit", minute: "2-digit" }).format(new Date(value));
}

export function formatShortDate(value: string) {
  return new Intl.DateTimeFormat("en-IN", { day: "2-digit", month: "short" }).format(new Date(value));
}

export function paymentResultCopy(payment: Payment) {
  if (payment.state === "COMPLETED") return { title: "Payment complete", description: "Your payment has been completed.", tone: "success" as const };
  if (payment.state === "FAILED" || payment.state === "REVERSED") return { title: "Payment not completed", description: payment.failureReason ?? "The payment could not be completed.", tone: "error" as const };
  return { title: "Payment received", description: "Final confirmation is still in progress. You can safely check the transaction again later.", tone: "pending" as const };
}

export function navLabel(view: View) { return view === "home" ? "Overview" : view === "pay" ? "Pay" : "Transactions"; }
