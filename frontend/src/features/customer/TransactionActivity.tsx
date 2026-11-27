import { useState } from "react";
import { motion } from "motion/react";
import { Button, EmptyState, Icon, PageHeader, PaymentRow } from "../../components";
import { useAudioEngine } from "../../components/effects";
import type { Payment } from "../../types";
import type { TransactionActivityProps } from "./types";

export function TransactionActivity({ payments, onNavigate }: TransactionActivityProps) {
  const [filter, setFilter] = useState<"ALL" | "SENT" | "RECEIVED">("ALL");
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState("ALL");
  const [fromDate, setFromDate] = useState("");
  const [toDate, setToDate] = useState("");
  const { play } = useAudioEngine();

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
              {(
                [
                  { id: "ALL", label: "All", count: payments.length },
                  { id: "SENT", label: "Sent", count: sentCount },
                  { id: "RECEIVED", label: "Received", count: receivedCount },
                ] as const
              ).map((tab) => {
                const active = filter === tab.id;
                return (
                  <button
                    key={tab.id}
                    type="button"
                    className={`filter-tab relative overflow-hidden ${active ? "is-active" : ""}`}
                    onClick={() => {
                      play("tab");
                      setFilter(tab.id);
                    }}
                    role="tab"
                    aria-selected={active}
                  >
                    {active && (
                      <motion.span
                        layoutId="tx-filter-pill"
                        transition={{ type: "spring", stiffness: 380, damping: 32 }}
                        className="absolute inset-0 rounded-[inherit] bg-[var(--bg-surface)] shadow-[var(--shadow-subtle)]"
                        aria-hidden="true"
                      />
                    )}
                    <span className="relative z-10 inline-flex items-center gap-1.5">
                      {tab.label} <span className="filter-count">{tab.count}</span>
                    </span>
                  </button>
                );
              })}
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
