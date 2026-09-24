import type { ButtonHTMLAttributes, HTMLAttributes, InputHTMLAttributes, ReactNode } from "react";
import type { Payment, View } from "./types";

export type IconName = "home" | "send" | "activity" | "arrow" | "chevron" | "check" | "alert" | "close";

export function Icon({ name, size = 18 }: { name: IconName; size?: number }) {
  const paths: Record<IconName, ReactNode> = {
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

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: "primary" | "secondary" | "quiet" | "danger";
  size?: "sm" | "md" | "lg";
  className?: string;
}

export function Button({ variant = "primary", size = "md", children, className = "", ...props }: ButtonProps) {
  const sizeClass = size === "sm" ? "button-sm" : size === "lg" ? "button-lg" : "";
  return <button className={`button button-${variant} ${sizeClass} ${className}`.trim()} {...props}>{children}</button>;
}

export interface IconButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  icon: IconName;
  label: string;
  variant?: "quiet" | "secondary" | "primary";
  size?: number;
  className?: string;
}

export function IconButton({ icon, label, variant = "quiet", size = 18, className = "", disabled, ...props }: IconButtonProps) {
  return (
    <button className={`icon-button icon-button-${variant} ${className}`.trim()} aria-label={label} title={label} disabled={disabled} {...props}>
      <Icon name={icon} size={size} />
    </button>
  );
}

export interface BadgeProps {
  children: ReactNode;
  tone?: "neutral" | "success" | "warning" | "error" | "accent";
  className?: string;
}

export function Badge({ children, tone = "neutral", className = "" }: BadgeProps) {
  return <span className={`badge badge-${tone} ${className}`.trim()}>{children}</span>;
}

export function StatusBadge({ state }: { state: string }) {
  const normalized = state.toLowerCase();
  const label = normalized === "completed" ? "Completed" : normalized === "failed" ? "Not completed" : normalized === "pending_reconciliation" || normalized === "bank_settled_central_pending" ? "Confirming" : normalized === "processing" ? "Processing" : state.replaceAll("_", " ").toLowerCase();
  const tone = normalized === "completed" ? "success" : normalized === "failed" || normalized === "reversed" ? "error" : normalized.includes("pending") || normalized === "processing" ? "warning" : "neutral";
  return <span className={`status-badge status-${tone}`}><span className="status-dot" />{label}</span>;
}

export interface CardProps extends HTMLAttributes<HTMLDivElement> {
  variant?: "default" | "soft" | "bordered" | "elevated";
  className?: string;
  children?: ReactNode;
}

export function Card({ variant = "default", children, className = "", ...props }: CardProps) {
  return <div className={`card card-${variant} ${className}`.trim()} {...props}>{children}</div>;
}

export function PageHeader({ eyebrow, title, description, action, className = "" }: { eyebrow?: string; title: string; description?: string; action?: ReactNode; className?: string }) {
  return (
    <div className={`page-header ${className}`.trim()}>
      <div>
        {eyebrow && <p className="eyebrow">{eyebrow}</p>}
        <h1>{title}</h1>
        {description && <p className="page-description">{description}</p>}
      </div>
      {action}
    </div>
  );
}

export function formatPaise(paise: number) {
  if (!Number.isSafeInteger(paise) || paise < 0) return "₹0.00";
  const value = BigInt(paise);
  const whole = (value / 100n).toString().replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  const fraction = (value % 100n).toString().padStart(2, "0");
  return `₹${whole}.${fraction}`;
}

export function Amount({ paise, sign = "", prominent = false }: { paise: number; sign?: string; prominent?: boolean }) {
  return <span className={`amount ${prominent ? "amount-prominent" : ""}`.trim()}>{sign}{formatPaise(paise)}</span>;
}

export function Avatar({ name, size = "md", className = "" }: { name: string; size?: "sm" | "md" | "lg"; className?: string }) {
  const initials = name.split(" ").map((part) => part[0]).join("").slice(0, 2).toUpperCase();
  const sizeClass = size === "sm" ? "avatar-sm" : size === "lg" ? "avatar-lg" : "";
  return <span className={`avatar ${sizeClass} ${className}`.trim()} aria-hidden="true">{initials || "TX"}</span>;
}

export function Skeleton({ variant = "block", className = "" }: { variant?: "block" | "text" | "circle"; className?: string }) {
  const variantClass = variant === "circle" ? "skeleton-circle" : variant === "text" ? "skeleton-text" : "";
  return <span className={`skeleton ${variantClass} ${className}`.trim()} aria-hidden="true" />;
}

export function EmptyState({ title, description, action, icon = "activity" }: { title: string; description: string; action?: ReactNode; icon?: IconName }) {
  return (
    <div className="empty-state">
      <span className="empty-mark"><Icon name={icon} size={20} /></span>
      <h3>{title}</h3>
      <p>{description}</p>
      {action}
    </div>
  );
}

export function InlineError({ message, className = "" }: { message: string; className?: string }) {
  return (
    <p className={`inline-error ${className}`.trim()} role="alert">
      <Icon name="alert" size={16} />
      {message}
    </p>
  );
}

export interface FieldProps extends Omit<InputHTMLAttributes<HTMLInputElement>, "onChange" | "value"> {
  label: string;
  value: string;
  onChange: (value: string) => void;
  hint?: string;
  error?: string;
  className?: string;
}

export function Field({ label, value, onChange, className = "", hint, error, ...props }: FieldProps) {
  return (
    <label className={`field ${className}`.trim()}>
      <span>{label}</span>
      <input value={value} onChange={(event) => onChange(event.target.value)} aria-invalid={error ? "true" : undefined} {...props} />
      {hint && !error && <span className="field-hint">{hint}</span>}
      {error && <span className="field-error" role="alert">{error}</span>}
    </label>
  );
}

export function Divider({ spacing = "md", className = "" }: { spacing?: "sm" | "md" | "lg"; className?: string }) {
  return <hr className={`divider divider-${spacing} ${className}`.trim()} aria-hidden="true" />;
}

export function PaymentRow({ payment, onClick }: { payment: Payment; onClick: () => void }) {
  const completed = payment.state === "COMPLETED";
  const counterparty = payment.direction === "RECEIVED" ? payment.senderName : payment.receiverName;
  const sign = payment.direction === "RECEIVED" ? "+ " : "− ";
  return (
    <button type="button" className="payment-row" onClick={onClick}>
      <Avatar name={counterparty} />
      <span className="payment-main">
        <strong>{counterparty}</strong>
        <small>{payment.direction === "RECEIVED" ? "Received from" : "Sent to"} · {formatDate(payment.createdAt)}</small>
      </span>
      <span className={`payment-amount ${completed ? "" : "payment-muted"} ${payment.direction === "RECEIVED" ? "payment-received" : ""}`.trim()}>
        <Amount paise={payment.amountPaise} sign={sign} />
        <small><StatusBadge state={payment.state} /></small>
      </span>
      <Icon name="chevron" size={16} />
    </button>
  );
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
  if (payment.state === "PROCESSING") return { title: "Payment being processed", description: "The payment is still being processed. It has not been marked complete yet.", tone: "pending" as const };
  return { title: "Payment still being confirmed", description: "The outcome is not known yet. This payment is not marked complete; check its status again later.", tone: "pending" as const };
}

export function navLabel(view: View) {
  return view === "home" ? "Overview" : view === "pay" ? "Pay" : "Transactions";
}
