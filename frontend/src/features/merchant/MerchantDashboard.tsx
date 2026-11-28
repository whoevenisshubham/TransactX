import { useEffect, useState } from "react";
import { Amount, BrandMark, Button, EmptyState, Icon, PageHeader } from "../../components";
import { DotSentinel, useAudioEngine } from "../../components/effects";
import { CopyValue } from "../../shared/CopyValue";
import { firstName } from "../../shared/presentation";
import { formatPaiseDecimal } from "../../design/OpsPrimitives";
import { encodeQRSvg } from "../../qr";
import { downloadQR } from "../../merchant/downloadQR";
import { MerchantPaymentRow } from "./MerchantPaymentRow";
import type { Account, MerchantView, Payment, User } from "./types";
import { toast } from "sonner";

export interface MerchantDashboardProps {
  user: User;
  account?: Account;
  payments: Payment[];
  onNavigate: (view: MerchantView) => void;
  onRefresh: () => void;
}

export function MerchantDashboard({
  user,
  account,
  payments,
  onNavigate,
  onRefresh,
}: MerchantDashboardProps) {
  const received = payments.filter((payment) => payment.direction === "RECEIVED");
  const completed = received.filter((payment) => payment.state === "COMPLETED");
  const pending = received.filter(
    (payment) => !["COMPLETED", "FAILED", "REVERSED"].includes(payment.state)
  );
  const total = completed
    .reduce((sum, payment) => sum + BigInt(payment.amountPaise), 0n)
    .toString();

  return (
    <>
      <PageHeader
        eyebrow="YOUR RECEIVING DESK"
        title={`Welcome, ${firstName(user.name)}`}
        description="A place for every incoming payment."
        action={
          <Button onClick={() => onNavigate("m-receive")}>
            <Icon name="qr" />
            Receive payment
          </Button>
        }
      />

      <div className="merchant-operating-layout">
        <div className="merchant-ledger-desk">
          <section
            className="merchant-position"
            aria-label="Merchant account and loaded activity"
          >
            <div>
              <p className="section-kicker">Account position</p>
              {account ? (
                <Amount paise={account.balancePaise} prominent />
              ) : (
                <p>Unavailable</p>
              )}
              <span>
                {account?.bankName ?? "Bank identity unavailable"} · {account?.status ?? "Unavailable"}
              </span>
              <small>
                {account?.accountNumber ?? "Account unavailable"} · Simulated INR
              </small>
            </div>
            <span className="inline-flex items-center gap-1.5 self-start rounded-full border border-[var(--status-success-border)] bg-[var(--status-success-bg)] px-2.5 py-1 text-[11px] font-mono font-semibold text-[var(--status-success-fg)] shrink-0">
              Receiving
            </span>
          </section>

          <div className="merchant-receipt-totals">
            <div>
              <span>Completed receipts</span>
              <strong className="font-mono tabular-nums">{formatPaiseDecimal(total)}</strong>
              <small>
                {completed.length} completed{" "}
                {completed.length === 1 ? "payment" : "payments"} in this page
              </small>
            </div>
            <div>
              <span>Awaiting confirmation</span>
              <strong className="font-mono tabular-nums">{pending.length.toString().padStart(2, "0")}</strong>
              <button
                type="button"
                className="text-link"
                onClick={() => onNavigate("m-settlement")}
              >
                Check settlement <Icon name="arrow" />
              </button>
            </div>
          </div>

          <section className="merchant-incoming-desk">
            <div className="section-heading">
              <div>
                <p className="eyebrow">INCOMING ACTIVITY</p>
                <h2>Recent received payments</h2>
              </div>
              <button type="button" className="text-link" onClick={onRefresh}>
                Refresh <Icon name="activity" />
              </button>
            </div>
            <p className="merchant-scope">
              Based on the latest {payments.length} loaded account records. Showing up to 5 incoming payments.
            </p>
            {received.length === 0 ? (
              <EmptyState
                title="No payments received yet"
                description="Share your payment ID or QR to receive a simulated payment."
              />
            ) : (
              received
                .slice(0, 5)
                .map((payment) => (
                  <MerchantPaymentRow key={payment.id} payment={payment} />
                ))
            )}
            <button
              type="button"
              className="merchant-all-incoming"
              onClick={() => onNavigate("m-incoming")}
            >
              View all loaded incoming payments <Icon name="arrow" />
            </button>
          </section>
        </div>

        <DashboardReceiveStation
          user={user}
          active={account?.status === "ACTIVE"}
          onNavigate={onNavigate}
        />
      </div>
    </>
  );
}

function DashboardReceiveStation({
  user,
  active,
  onNavigate,
}: {
  user: User;
  active: boolean;
  onNavigate: (view: MerchantView) => void;
}) {
  const [qr, setQR] = useState("");
  const [error, setError] = useState("");
  const { play } = useAudioEngine();

  useEffect(() => {
    let current = true;
    encodeQRSvg(user.paymentIdentifier)
      .then((svg) => {
        if (current) setQR(svg);
      })
      .catch(() => {
        if (current) setError("QR unavailable. Your payment ID is still available.");
      });
    return () => {
      current = false;
    };
  }, [user.paymentIdentifier]);

  return (
    <aside className="merchant-receive-station relative overflow-hidden" aria-label="Receive station">
      <div className="receive-station-top">
        <BrandMark />
        <span className="inline-flex items-center gap-2">
          <DotSentinel state={active ? "success" : "warning"} />
          <span>RECEIVE STATION</span>
        </span>
      </div>
      <h2>
        Open for <br />receiving.
      </h2>
      <p>
        One identity to share.<br />Every receipt in its place.
      </p>

      {active ? (
        <>
          <div className="relative mx-auto w-fit p-1 rounded-2xl border border-[var(--border-hairline)] shadow-[var(--shadow-raised)] bg-white">
            <span aria-hidden="true" className="pointer-events-none absolute -top-1 -left-1 h-3 w-3 border-t-2 border-l-2 border-[var(--blueprint-cobalt,#2563eb)] rounded-tl" />
            <span aria-hidden="true" className="pointer-events-none absolute -top-1 -right-1 h-3 w-3 border-t-2 border-r-2 border-[var(--blueprint-cobalt,#2563eb)] rounded-tr" />
            <span aria-hidden="true" className="pointer-events-none absolute -bottom-1 -left-1 h-3 w-3 border-b-2 border-l-2 border-[var(--blueprint-cobalt,#2563eb)] rounded-bl" />
            <span aria-hidden="true" className="pointer-events-none absolute -bottom-1 -right-1 h-3 w-3 border-b-2 border-r-2 border-[var(--blueprint-cobalt,#2563eb)] rounded-br" />
            <div
              className="merchant-qr-frame station-qr bg-white text-[#0f172a] p-3 rounded-xl !m-0"
              role="img"
              aria-label={`QR code containing payment ID ${user.paymentIdentifier}`}
            >
              {qr ? (
                <span dangerouslySetInnerHTML={{ __html: qr }} />
              ) : (
                <span>{error || "Preparing QR…"}</span>
              )}
            </div>
          </div>
          <strong className="station-merchant-name">{user.name}</strong>
          <span className="station-identifier">{user.paymentIdentifier}</span>
          <CopyValue value={user.paymentIdentifier} label="Copy merchant ID" />
          <Button
            variant="secondary"
            disabled={!qr}
            onClick={() => {
              play("packet");
              void downloadQR(qr, "png")
                .then(() => toast.success("Merchant QR PNG downloaded"))
                .catch(() => setError("Download unavailable. Open receive QR to try again."));
            }}
          >
            Download QR <Icon name="arrow" />
          </Button>
        </>
      ) : (
        <p>Receiving unavailable while the account is inactive.</p>
      )}

      {error && <p role="status">{error}</p>}
      <small>
        TransactX identifier only.<br />Not a UPI or bank-app QR.
      </small>
      <button
        type="button"
        className="text-link"
        onClick={() => onNavigate("m-receive")}
      >
        Open receive details <Icon name="arrow" />
      </button>
    </aside>
  );
}
