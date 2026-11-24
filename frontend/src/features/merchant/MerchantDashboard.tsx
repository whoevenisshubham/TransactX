import { useEffect, useState } from "react";
import { Amount, BrandMark, Button, EmptyState, Icon, PageHeader } from "../../components";
import { CopyValue } from "../../shared/CopyValue";
import { firstName } from "../../shared/presentation";
import { formatPaiseDecimal } from "../../design/OpsPrimitives";
import { encodeQRSvg } from "../../qr";
import { downloadQR } from "../../merchant/downloadQR";
import { MerchantPaymentRow } from "./MerchantPaymentRow";
import type { Account, MerchantView, Payment, User } from "./types";

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
            <div className="merchant-position-mark" aria-hidden="true">
              <span>↙</span>
              <BrandMark />
            </div>
          </section>

          <div className="merchant-receipt-totals">
            <div>
              <span>Completed receipts</span>
              <strong>{formatPaiseDecimal(total)}</strong>
              <small>
                {completed.length} completed{" "}
                {completed.length === 1 ? "payment" : "payments"} in this page
              </small>
            </div>
            <div>
              <span>Awaiting confirmation</span>
              <strong>{pending.length.toString().padStart(2, "0")}</strong>
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
    <aside className="merchant-receive-station" aria-label="Receive station">
      <div className="receive-station-top">
        <BrandMark />
        <span>RECEIVE STATION</span>
      </div>
      <h2>
        Open for <br />receiving.
      </h2>
      <p>
        One identity to share.<br />Every receipt in its place.
      </p>

      {active ? (
        <>
          <div
            className="merchant-qr-frame station-qr"
            role="img"
            aria-label={`QR code containing payment ID ${user.paymentIdentifier}`}
          >
            {qr ? (
              <span dangerouslySetInnerHTML={{ __html: qr }} />
            ) : (
              <span>{error || "Preparing QR…"}</span>
            )}
          </div>
          <strong className="station-merchant-name">{user.name}</strong>
          <span className="station-identifier">{user.paymentIdentifier}</span>
          <CopyValue value={user.paymentIdentifier} label="Copy merchant ID" />
          <Button
            variant="secondary"
            disabled={!qr}
            onClick={() =>
              void downloadQR(qr, "png").catch(() =>
                setError("Download unavailable. Open receive QR to try again.")
              )
            }
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
