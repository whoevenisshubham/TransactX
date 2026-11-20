import { useState } from "react";
import { Button, EmptyState, Icon, PageHeader } from "../../components";
import { MerchantPaymentRow } from "./MerchantPaymentRow";
import type { Payment } from "./types";

export interface MerchantSearchProps {
  payments: Payment[];
}

export function MerchantSearch({ payments }: MerchantSearchProps) {
  const [query, setQuery] = useState("");
  const [stateFilter, setStateFilter] = useState("all");

  const received = payments.filter((p) => p.direction === "RECEIVED");

  const filtered = received.filter((p) => {
    const q = query.toLowerCase();
    const matchQuery =
      query.trim() === "" ||
      p.senderName.toLowerCase().includes(q) ||
      p.senderPaymentIdentifier.toLowerCase().includes(q) ||
      (p.note ?? "").toLowerCase().includes(q) ||
      p.id.toLowerCase().includes(q);
    const matchState = stateFilter === "all" || p.state === stateFilter;
    return matchQuery && matchState;
  });

  const hasFilter = query.trim() !== "" || stateFilter !== "all";

  function handleReset() {
    setQuery("");
    setStateFilter("all");
  }

  return (
    <>
      <PageHeader
        eyebrow="Search"
        title="Transaction search"
        description="Search incoming payments within the latest 50 loaded account records by customer, reference, payment ID or note."
      />

      <div className="merchant-search-toolbar">
        <label className="field merchant-search-field">
          <span>Search by sender or note</span>
          <div className="merchant-search-input-wrap">
            <input
              id="merchant-search-input"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Name, payment ID, or note…"
              autoComplete="off"
            />
            {query && (
              <button
                type="button"
                className="merchant-search-clear"
                onClick={() => setQuery("")}
                aria-label="Clear query"
              >
                <Icon name="close" size={14} />
              </button>
            )}
          </div>
        </label>

        <label className="field merchant-filter-field">
          <span>Filter by status</span>
          <select
            id="merchant-state-filter"
            value={stateFilter}
            onChange={(e) => setStateFilter(e.target.value)}
            className="merchant-select"
          >
            <option value="all">All statuses</option>
            <option value="COMPLETED">Completed</option>
            <option value="PROCESSING">Processing</option>
            <option value="PENDING_RECONCILIATION">Pending confirmation</option>
            <option value="BANK_SETTLED_CENTRAL_PENDING">Confirming</option>
            <option value="FAILED">Failed</option>
            <option value="REVERSED">Reversed</option>
          </select>
        </label>
      </div>

      {filtered.length === 0 ? (
        <EmptyState
          title="No results found"
          description={
            hasFilter
              ? "No incoming payments match your search criteria. Try a different query or reset filters."
              : "No incoming payments found in your recent feed."
          }
          action={
            hasFilter ? (
              <Button variant="secondary" onClick={handleReset}>
                Reset search
              </Button>
            ) : undefined
          }
        />
      ) : (
        <section className="transactions-section">
          <div className="transactions-toolbar">
            <span>
              {filtered.length} {filtered.length === 1 ? "result" : "results"} found
            </span>
            <span className="toolbar-note">
              {received.length} total in recent feed · Bounded search
            </span>
          </div>
          <div className="payment-list payment-list-large">
            {filtered.map((p) => (
              <MerchantPaymentRow key={p.id} payment={p} expanded />
            ))}
          </div>
        </section>
      )}
    </>
  );
}
