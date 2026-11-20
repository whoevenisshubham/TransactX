import { useState } from "react";
import { Amount, Button, EmptyState, formatDate, Icon, PageHeader, StatusBadge } from "../../components";
import { CopyValue } from "../../shared/CopyValue";
import type { Payment } from "./types";

export interface IncomingPaymentsProps {
  payments: Payment[];
  onRefresh: () => void;
}

export function IncomingPayments({ payments, onRefresh }: IncomingPaymentsProps) {
  const [filter, setFilter] = useState("ALL");
  const received = payments.filter((p) => p.direction === "RECEIVED");
  const filtered = received.filter(
    (payment) => filter === "ALL" || payment.state === filter
  );

  return (
    <>
      <PageHeader
        eyebrow="Incoming Feed"
        title="Payments received"
        description="Incoming payments in the latest 50 loaded account records. Refresh to fetch current observations."
        action={
          <Button variant="secondary" onClick={onRefresh}>
            <Icon name="activity" size={16} />
            Refresh
          </Button>
        }
      />

      {received.length === 0 ? (
        <EmptyState
          title="No incoming payments"
          description="Incoming payments will appear after they are recorded. Refresh to check for new activity."
        />
      ) : (
        <section className="transactions-section">
          <div className="transactions-toolbar">
            <span>
              {received.length} {received.length === 1 ? "payment" : "payments"} in recent feed
            </span>
            <span className="toolbar-note">
              Most recent first · Open a receipt for details
            </span>
          </div>

          <label className="merchant-status-filter">
            Payment status
            <select
              value={filter}
              onChange={(event) => setFilter(event.target.value)}
            >
              <option value="ALL">All outcomes</option>
              {[...new Set(received.map((payment) => payment.state))].map((state) => (
                <option key={state} value={state}>
                  {state.replaceAll("_", " ")}
                </option>
              ))}
            </select>
          </label>

          <div className="ops-table-scroll">
            <table className="ops-table merchant-receipt-table">
              <caption>
                {filtered.length} matching incoming payments in this loaded page
              </caption>
              <thead>
                <tr>
                  <th scope="col">Customer</th>
                  <th scope="col">Amount</th>
                  <th scope="col">Outcome</th>
                  <th scope="col">Initiated</th>
                  <th scope="col">Details</th>
                </tr>
              </thead>
              <tbody>
                {filtered.map((payment) => (
                  <tr key={payment.id}>
                    <td data-label="Customer">
                      <span className="receipt-customer">
                        <strong>{payment.senderName}</strong>
                        <small>{payment.senderPaymentIdentifier}</small>
                      </span>
                    </td>
                    <td data-label="Amount">
                      <Amount paise={payment.amountPaise} />
                    </td>
                    <td data-label="Outcome">
                      <StatusBadge state={payment.state} />
                    </td>
                    <td data-label="Recorded">{formatDate(payment.createdAt)}</td>
                    <td data-label="Details">
                      <details>
                        <summary>View receipt</summary>
                        <div className="merchant-table-receipt">
                          <span>{payment.note || "No note"}</span>
                          <span>
                            {payment.sourceBankCode ?? "Unknown bank"} →{" "}
                            {payment.destinationBankCode ?? "Unknown bank"}
                          </span>
                          <span className="merchant-account-number">{payment.id}</span>
                          <CopyValue value={payment.id} label="Copy reference" />
                        </div>
                      </details>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      )}
    </>
  );
}
