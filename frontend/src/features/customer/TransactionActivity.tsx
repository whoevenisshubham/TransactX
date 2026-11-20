import { useState } from "react";
import { Button, EmptyState, Icon, PageHeader, PaymentRow } from "../../components";
import type { Payment } from "../../types";
import type { TransactionActivityProps } from "./types";

export function TransactionActivity({ payments, onNavigate }: TransactionActivityProps) {
  const [filter, setFilter] = useState<"ALL" | "SENT" | "RECEIVED">("ALL");
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState("ALL");
  const [fromDate, setFromDate] = useState("");
  const [toDate, setToDate] = useState("");

  const sentCount = payments.filter((p) => p.direction === "SENT").length;
  const receivedCount = payments.filter((p) => p.direction === "RECEIVED").length;

  const filtered = payments.filter((p) => {
    if (status !== "ALL" && p.state !== status) return false;
    const date = p.createdAt.slice(0, 10);
    if ((fromDate && date < fromDate) || (toDate && date > toDate)) return false;
    if (filter === "SENT" && p.direction !== "SENT") return false;
    if (filter === "RECEIVED" && p.direction !== "RECEIVED") return false;
    if (!search.trim()) return true;
    const q = search.trim().toLowerCase();
    const counterparty = (p.direction === "RECEIVED" ? p.senderName : p.receiverName).toLowerCase();
    const identifier = (
      p.direction === "RECEIVED" ? p.senderPaymentIdentifier : p.receiverPaymentIdentifier
    ).toLowerCase();
    const noteMatch = p.note ? p.note.toLowerCase().includes(q) : false;
    const idMatch = p.id.toLowerCase().includes(q);
    return counterparty.includes(q) || identifier.includes(q) || noteMatch || idMatch;
  });

  function clearFilters() {
    setFilter("ALL");
    setSearch("");
    setStatus("ALL");
    setFromDate("");
    setToDate("");
  }

  return (
    <>
      <PageHeader
        eyebrow="Activity"
        title="Transactions"
        description="Search and filter your latest 50 loaded payment records. This is not an all-time ledger search."
        action={
          <Button onClick={() => onNavigate("pay")}>
            <Icon name="send" />
            Send money
          </Button>
        }
      />

      {payments.length === 0 ? (
        <EmptyState
          title="No transactions yet"
          description="Your payments will appear here after your first transfer."
          action={
            <Button variant="secondary" onClick={() => onNavigate("pay")}>
              Send money
            </Button>
          }
        />
      ) : (
        <section className="transactions-section">
          <div className="transactions-filter-bar">
            <div className="filter-tabs" role="tablist" aria-label="Transaction filters">
              <button
                type="button"
                className={`filter-tab ${filter === "ALL" ? "is-active" : ""}`}
                onClick={() => setFilter("ALL")}
                role="tab"
                aria-selected={filter === "ALL"}
              >
                All <span className="filter-count">{payments.length}</span>
              </button>
              <button
                type="button"
                className={`filter-tab ${filter === "SENT" ? "is-active" : ""}`}
                onClick={() => setFilter("SENT")}
                role="tab"
                aria-selected={filter === "SENT"}
              >
                Sent <span className="filter-count">{sentCount}</span>
              </button>
              <button
                type="button"
                className={`filter-tab ${filter === "RECEIVED" ? "is-active" : ""}`}
                onClick={() => setFilter("RECEIVED")}
                role="tab"
                aria-selected={filter === "RECEIVED"}
              >
                Received <span className="filter-count">{receivedCount}</span>
              </button>
            </div>

            <div className="transactions-search">
              <span className="search-icon">
                <Icon name="activity" size={15} />
              </span>
              <input
                type="search"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="Search transactions…"
                aria-label="Search transactions"
              />
            </div>
          </div>

          <div className="activity-filters">
            <label>
              Status
              <select
                aria-label="Status"
                value={status}
                onChange={(event) => setStatus(event.target.value)}
              >
                <option value="ALL">All outcomes</option>
                {[...new Set(payments.map((payment) => payment.state))].map((state) => (
                  <option key={state} value={state}>
                    {state.replaceAll("_", " ")}
                  </option>
                ))}
              </select>
            </label>
            <label>
              From date (UTC)
              <input
                type="date"
                value={fromDate}
                onChange={(event) => setFromDate(event.target.value)}
              />
            </label>
            <label>
              To date (UTC)
              <input
                type="date"
                min={fromDate || undefined}
                value={toDate}
                onChange={(event) => setToDate(event.target.value)}
              />
            </label>
          </div>

          <div className="transactions-toolbar">
            <span>
              Showing {filtered.length} of {payments.length}{" "}
              {payments.length === 1 ? "transaction" : "transactions"}
            </span>
            <span className="toolbar-note">Sorted by most recent</span>
          </div>

          {filtered.length === 0 ? (
            <div className="empty-state" style={{ marginTop: "1rem" }}>
              <div className="empty-mark">
                <Icon name="activity" size={20} />
              </div>
              <h3>No matching transactions</h3>
              <p>Try searching with a different name, payment ID, or clearing your filters.</p>
              <Button variant="secondary" onClick={clearFilters}>
                Clear filters
              </Button>
            </div>
          ) : (
            <div className="payment-list payment-list-large">
              {filtered.map((payment) => (
                <PaymentRow
                  key={payment.id}
                  payment={payment}
                  onClick={() => onNavigate("details", payment.id)}
                />
              ))}
            </div>
          )}
        </section>
      )}
    </>
  );
}
