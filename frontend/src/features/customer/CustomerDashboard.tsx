import { useState } from "react";
import { Amount, BrandMark, Button, EmptyState, Icon, PageHeader, PaymentRow } from "../../components";
import { CopyValue } from "../../shared/CopyValue";
import { firstName } from "../../shared/presentation";
import type { CustomerDashboardProps } from "./types";

export function CustomerDashboard({
  user,
  account,
  payments,
  onNavigate,
  onExploreDemo,
}: CustomerDashboardProps) {
  const [masked, setMasked] = useState(false);
  const [activityFilter, setActivityFilter] = useState<"ALL" | "SENT" | "RECEIVED">("ALL");
  const [activityQuery, setActivityQuery] = useState("");

  const activeStatus = account?.status === "ACTIVE";
  const confirming = payments.filter(
    (payment) => !["COMPLETED", "FAILED", "REVERSED"].includes(payment.state)
  ).length;

  const visible = payments.filter((payment) => {
    const matchesFilter =
      activityFilter === "ALL" || payment.direction === activityFilter;
    const query = activityQuery.toLowerCase().trim();
    const matchesQuery =
      !query ||
      `${payment.senderName} ${payment.receiverName} ${payment.note ?? ""}`
        .toLowerCase()
        .includes(query);
    return matchesFilter && matchesQuery;
  });

  return (
    <>
      <PageHeader
        eyebrow="YOUR EVERYDAY ACCOUNT"
        title={`Good day, ${firstName(user?.name)}`}
        description="Make your next move."
      />

      <div className="personal-account-layout">
        {/* Available Balance Card */}
        <section className="personal-account-document" aria-label="Account balance">
          <div className="account-document-head">
            <span>
              <BrandMark /> PERSONAL ACCOUNT
            </span>
            <span className="account-status">
              <span
                className={`status-dot ${
                  activeStatus ? "status-success-dot" : "status-neutral-dot"
                }`}
              />
              {activeStatus ? "Active account" : account?.status ?? "Unavailable"}
            </span>
          </div>

          <div className="personal-balance-line">
            <div>
              <span className="section-kicker">Available balance</span>
              {!account ? (
                <p>Balance unavailable</p>
              ) : masked ? (
                <p className="masked-balance" aria-label="Balance hidden">
                  ••••••
                </p>
              ) : (
                <Amount paise={account.balancePaise} prominent />
              )}
            </div>
            <button
              type="button"
              className="balance-visibility !border !border-white/25 !rounded-full !px-3 !py-1.5 !bg-white/10 hover:!bg-white/15 !text-xs !font-medium !text-white transition-colors cursor-pointer shrink-0"
              aria-pressed={masked}
              onClick={() => setMasked((value) => !value)}
            >
              {masked ? "Show balance" : "Hide balance"}
            </button>
          </div>

          <div className="personal-account-action">
            <span>Simulated INR · No real money</span>
            <Button onClick={() => onNavigate("pay")}>
              Send money <Icon name="arrow" />
            </Button>
          </div>

          <div className="personal-account-imprint">
            <span className="customer-bank-identity">
              {account?.bankName ? (
                <>
                  {account.bankName}
                  {account.bankCode ? ` · ${account.bankCode}` : ""}
                </>
              ) : (
                "Bank identity unavailable"
              )}
            </span>
            <span>{account?.accountNumber ?? "Account unavailable"}</span>
          </div>
        </section>

        {/* Identity & Receive Card */}
        <section className="personal-receive-note rounded-2xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] p-6" aria-label="Your payment identity">
          <span className="personal-note-symbol" aria-hidden="true">
            ↙
          </span>
          <p className="section-kicker">MONEY CAN COME YOUR WAY, TOO.</p>
          <h2>
            Your identity.
            <br />
            Ready to share.
          </h2>
          <strong className="personal-payment-id font-mono tabular-nums">
            {user?.paymentIdentifier ?? "Unavailable"}
          </strong>
          {user && <CopyValue value={user.paymentIdentifier} label="Copy payment ID" />}
          <p>Receive from another TransactX account using this payment ID.</p>
        </section>
      </div>

      {/* Zero balance helper for exploration */}
      {account?.balancePaise === 0 && (
        <aside className="zero-balance-note">
          <strong>Your available balance is ₹0.</strong>
          <p>
            To try payments, explore a funded shared demo. Private accounts and shared demos
            stay separate.
          </p>
          <button type="button" onClick={onExploreDemo} className="text-link">
            Log out and explore shared demos <Icon name="arrow" />
          </button>
        </aside>
      )}

      {/* Recent Movements Section */}
      <section className="personal-activity-desk">
        <div className="section-heading">
          <div>
            <p className="eyebrow">THE LATEST MOVEMENTS</p>
            <h2>Recent transactions</h2>
          </div>
          <button
            type="button"
            className="text-link"
            onClick={() => onNavigate("transactions")}
          >
            Open activity <Icon name="arrow" />
          </button>
        </div>

        <div className="personal-activity-tools">
          <div role="group" aria-label="Recent transaction direction">
            <button
              type="button"
              aria-pressed={activityFilter === "ALL"}
              onClick={() => setActivityFilter("ALL")}
            >
              All activity
            </button>
            <button
              type="button"
              aria-pressed={activityFilter === "SENT"}
              onClick={() => setActivityFilter("SENT")}
            >
              Sent
            </button>
            <button
              type="button"
              aria-pressed={activityFilter === "RECEIVED"}
              onClick={() => setActivityFilter("RECEIVED")}
            >
              Received
            </button>
          </div>
          <label className="activity-search">
            <span>Find in loaded activity</span>
            <input
              value={activityQuery}
              onChange={(event) => setActivityQuery(event.target.value)}
              placeholder="Name or note"
            />
          </label>
        </div>

        <div className="activity-scope">
          <span>Latest {payments.length} loaded records · showing up to 5</span>
          <span>{confirming} awaiting confirmation in this page</span>
        </div>

        {visible.length === 0 ? (
          <EmptyState
            title={payments.length ? "No matching activity" : "No transactions yet"}
            description={
              payments.length
                ? "Try another name or direction within the loaded records."
                : "Your payments will appear here after your first transfer."
            }
          />
        ) : (
          <div className="payment-list">
            {visible.slice(0, 5).map((payment) => (
              <PaymentRow
                key={payment.id}
                payment={payment}
                onClick={() => onNavigate("details", payment.id)}
              />
            ))}
          </div>
        )}
      </section>

      <div className="personal-footnote">
        <Icon name="check" />
        <p>
          <strong>Review first. Send once.</strong> Missing confirmation? Check the original
          payment before starting another.
        </p>
      </div>
    </>
  );
}
