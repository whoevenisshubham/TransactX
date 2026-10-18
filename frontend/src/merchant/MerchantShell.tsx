import { useEffect, useState } from "react";
import { api, ApiError } from "../api";
import { Amount, Avatar, BrandMark, Button, EmptyState, formatDate, Icon, InlineError, PageHeader, Skeleton, StatusBadge } from "../components";
import { encodeQRSvg } from "../qr";
import type { Account, MerchantReceiveInfo, MerchantView, Payment, User } from "../types";
import { ThemeSelect } from "../theme";
import { LoadingShell, DetailsSkeleton } from "../shared/ShellStates";
import { firstName } from "../shared/presentation";

export function MerchantShell({ token, user, onLogout }: { token: string; user: User; onLogout: () => void }) {
  const [view, setView] = useState<MerchantView>(readMerchantView());
  const [payments, setPayments] = useState<Payment[]>([]);
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    let active = true;
    Promise.all([api.accounts(token), api.payments(token)])
      .then(([accountList, paymentList]) => {
        if (active) { setAccounts(accountList); setPayments(paymentList); setLoading(false); }
      })
      .catch((caught) => {
        if (!active) return;
        if (caught instanceof ApiError && caught.status === 401) onLogout();
        else { setError(caught instanceof Error ? caught.message : "We couldn't load your merchant account."); setLoading(false); }
      });
    return () => { active = false; };
  }, [token, onLogout]);

  useEffect(() => {
    const handle = () => setView(readMerchantView());
    window.addEventListener("popstate", handle);
    return () => window.removeEventListener("popstate", handle);
  }, []);

  function navigate(nextView: MerchantView) {
    const path = merchantViewPath(nextView);
    window.history.pushState({}, "", path);
    setView(nextView);
    window.scrollTo({ top: 0, behavior: "smooth" });
  }

  function refreshPayments() { api.payments(token).then(setPayments).catch(() => undefined); }

  if (loading) return <LoadingShell />;
  if (error) return <main className="center-state"><InlineError message={error} /><Button onClick={() => window.location.reload()}>Try again</Button></main>;

  const account = accounts[0];

  return (
    <div className="product-shell merchant-shell">
      <MerchantSidebar view={view} user={user} onNavigate={navigate} onLogout={onLogout} />
      <main className="main-content">
        <MerchantMobileHeader user={user} onLogout={onLogout} />
        <div className="content-wrap">
          {view === "m-home" && <MerchantDashboardView user={user} account={account} payments={payments} onNavigate={navigate} onRefresh={refreshPayments} />}
          {view === "m-receive" && <MerchantReceiveView token={token} />}
          {view === "m-incoming" && <MerchantIncomingView payments={payments} onRefresh={refreshPayments} />}
          {view === "m-settlement" && <MerchantSettlementView payments={payments} onRefresh={refreshPayments} />}
          {view === "m-search" && <MerchantSearchView payments={payments} />}
        </div>
      </main>
      <MerchantMobileNav view={view} onNavigate={navigate} />
    </div>
  );
}

function MerchantSidebar({ view, user, onNavigate, onLogout }: { view: MerchantView; user: User; onNavigate: (v: MerchantView) => void; onLogout: () => void }) {
  return (
    <aside className="sidebar merchant-sidebar">
      <div className="brand-lockup">
        <BrandMark />
        <span>TransactX</span>
        <span className="merchant-badge">Merchant</span>
      </div>
      <div className="sidebar-rule" />
      <nav aria-label="Merchant navigation">
        <MerchantNavItem view="m-home" active={view === "m-home"} label="Dashboard" icon="home" onClick={onNavigate} />
        <MerchantNavItem view="m-receive" active={view === "m-receive"} label="Receive / QR" icon="qr" onClick={onNavigate} />
        <MerchantNavItem view="m-incoming" active={view === "m-incoming"} label="Incoming" icon="activity" onClick={onNavigate} />
        <MerchantNavItem view="m-settlement" active={view === "m-settlement"} label="Settlement" icon="check" onClick={onNavigate} />
        <MerchantNavItem view="m-search" active={view === "m-search"} label="Search" icon="search" onClick={onNavigate} />
      </nav>
      <div className="sidebar-bottom">
        <ThemeSelect />
        <div className="user-chip">
          <Avatar name={user.name} />
          <span><strong>{user.name}</strong><small>{user.paymentIdentifier}</small></span>
        </div>
        <button type="button" className="logout-button" onClick={onLogout}>Log out</button>
      </div>
    </aside>
  );
}

function MerchantNavItem({ view, active, label, icon, onClick }: { view: MerchantView; active: boolean; label: string; icon: "home" | "qr" | "activity" | "check" | "search"; onClick: (v: MerchantView) => void }) {
  return (
    <button
      type="button"
      className={`nav-item ${active ? "is-active" : ""}`}
      aria-current={active ? "page" : undefined}
      onClick={() => onClick(view)}
    >
      <MerchantIcon name={icon} />
      <span>{label}</span>
    </button>
  );
}

function MerchantMobileNav({ view, onNavigate }: { view: MerchantView; onNavigate: (v: MerchantView) => void }) {
  return (
    <nav className="mobile-nav merchant-mobile-nav" aria-label="Mobile merchant navigation">
      <button
        type="button"
        className={`mobile-nav-item ${view === "m-home" ? "is-active" : ""}`}
        aria-current={view === "m-home" ? "page" : undefined}
        onClick={() => onNavigate("m-home")}
      >
        <MerchantIcon name="home" size={16} />
        <span>Dashboard</span>
      </button>
      <button
        type="button"
        className={`mobile-nav-item ${view === "m-receive" ? "is-active" : ""}`}
        aria-current={view === "m-receive" ? "page" : undefined}
        onClick={() => onNavigate("m-receive")}
      >
        <MerchantIcon name="qr" size={16} />
        <span>Receive</span>
      </button>
      <button
        type="button"
        className={`mobile-nav-item ${view === "m-incoming" ? "is-active" : ""}`}
        aria-current={view === "m-incoming" ? "page" : undefined}
        onClick={() => onNavigate("m-incoming")}
      >
        <MerchantIcon name="activity" size={16} />
        <span>Incoming</span>
      </button>
      <button
        type="button"
        className={`mobile-nav-item ${view === "m-settlement" ? "is-active" : ""}`}
        aria-current={view === "m-settlement" ? "page" : undefined}
        onClick={() => onNavigate("m-settlement")}
      >
        <MerchantIcon name="check" size={16} />
        <span>Settlement</span>
      </button>
      <button
        type="button"
        className={`mobile-nav-item ${view === "m-search" ? "is-active" : ""}`}
        aria-current={view === "m-search" ? "page" : undefined}
        onClick={() => onNavigate("m-search")}
      >
        <MerchantIcon name="search" size={16} />
        <span>Search</span>
      </button>
    </nav>
  );
}

function MerchantIcon({ name, size = 18 }: { name: "home" | "qr" | "activity" | "check" | "search"; size?: number }) {
  const paths: Record<string, React.ReactNode> = {
    home: <><path d="m3 9 6-5 6 5" /><path d="M5 8v7h8V8M8 15v-4h2v4" /></>,
    activity: <><path d="M3 12h3l2-5 3 10 2-5h3" /><path d="M3 4h12M3 20h12" /></>,
    check: <path d="m4 9 3 3 6-7" />,
    qr: <><rect x="3" y="3" width="6" height="6" rx="1" /><rect x="9" y="3" width="6" height="6" rx="1" /><rect x="3" y="9" width="6" height="6" rx="1" /><path d="M9 12h1m2 0h1M9 15h1m2-3v3m2 0h1" /></>,
    search: <><circle cx="8" cy="8" r="5" /><path d="m15 15-3.5-3.5" /></>,
  };
  return <svg aria-hidden="true" className="icon" width={size} height={size} viewBox="0 0 18 18" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round">{paths[name]}</svg>;
}

function MerchantMobileHeader({ user, onLogout }: { user: User; onLogout: () => void }) {
  return (
    <header className="mobile-header merchant-mobile-header">
      <div className="mobile-brand">
        <BrandMark />
        <span>TransactX</span>
        <span className="merchant-badge-mobile">Merchant</span>
      </div>
      <button type="button" className="mobile-user" onClick={onLogout} aria-label={`Log out ${user.name}`}>
        <Avatar name={user.name} size="sm" />
      </button>
    </header>
  );
}

// ---------------------------------------------------------------------------
// Merchant Dashboard View
// ---------------------------------------------------------------------------

function MerchantDashboardView({ user, account, payments, onNavigate, onRefresh }: { user: User; account?: Account; payments: Payment[]; onNavigate: (v: MerchantView) => void; onRefresh: () => void }) {
  const received = payments.filter((p) => p.direction === "RECEIVED");
  const completedToday = received.filter((p) => p.state === "COMPLETED" && isToday(p.createdAt));

  return (
    <>
      <PageHeader
        eyebrow="Merchant Operations"
        title={`Welcome, ${firstName(user.name)}`}
        description="Monitor account balance and recent incoming payment activity."
        action={
          <Button onClick={() => onNavigate("m-receive")}>
            <MerchantIcon name="qr" size={16} />
            Receive payment
          </Button>
        }
      />
      <section className="overview-grid">
        <div className="balance-panel">
          <div className="section-kicker">Available balance</div>
          <Amount paise={account?.balancePaise ?? 0} prominent />
          <div className="balance-foot">
            <span className="account-number-mono">{account?.accountNumber ?? "Account unavailable"}</span>
            <span className="account-status">
              <span className={`status-dot ${account?.status === "ACTIVE" ? "status-success-dot" : "status-warning-dot"}`} />
              {account?.status === "ACTIVE" ? "Active" : account?.status ?? "Unavailable"}
            </span>
          </div>
        </div>
        <div className="account-note merchant-stat-panel">
          <span className="note-index">01</span>
          <div>
            <strong>Recent completed activity</strong>
            {completedToday.length === 0 ? (
              <p>No completed payments in today's recent feed.</p>
            ) : (
              <p>
                <strong style={{ color: "var(--accent)", fontSize: "1.35rem", display: "inline-block", marginRight: "0.35rem" }}>
                  {completedToday.length}
                </strong>
                completed payment{completedToday.length !== 1 ? "s" : ""} in recent feed today.
              </p>
            )}
            <button type="button" className="text-link" onClick={() => onNavigate("m-incoming")}>
              View incoming feed <Icon name="arrow" size={15} />
            </button>
          </div>
        </div>
      </section>

      <section className="section-block">
        <div className="section-heading">
          <div>
            <p className="eyebrow">Incoming activity</p>
            <h2>Recent received payments</h2>
          </div>
          <div style={{ display: "flex", gap: "0.75rem", alignItems: "center" }}>
            <button type="button" className="text-link" onClick={onRefresh}>Refresh</button>
            {received.length > 0 && (
              <button type="button" className="text-link" onClick={() => onNavigate("m-incoming")}>
                View all ({received.length}) <Icon name="arrow" size={15} />
              </button>
            )}
          </div>
        </div>
        {received.length === 0 ? (
          <EmptyState
            title="No payments received yet"
            description="Payments sent to your merchant payment ID will appear here once processed."
            action={<Button variant="secondary" onClick={() => onNavigate("m-receive")}>Show receive QR</Button>}
          />
        ) : (
          <div className="payment-list">
            {received.slice(0, 5).map((p) => (
              <MerchantPaymentRow key={p.id} payment={p} />
            ))}
          </div>
        )}
      </section>

      <section className="account-summary">
        <div>
          <p className="eyebrow">Merchant credentials</p>
          <h2>Your merchant identity</h2>
        </div>
        <div className="summary-detail">
          <span>Payment identifier</span>
          <strong className="mono-code">{user.paymentIdentifier}</strong>
        </div>
        <div className="summary-detail">
          <span>Account number</span>
          <strong className="mono-code">{account?.accountNumber ?? "—"}</strong>
        </div>
        <div className="summary-detail">
          <span>Account status</span>
          <strong>{account?.status ?? "—"}</strong>
        </div>
      </section>
    </>
  );
}

// ---------------------------------------------------------------------------
// Merchant Receive / QR View
// ---------------------------------------------------------------------------

function MerchantReceiveView({ token }: { token: string }) {
  const [info, setInfo] = useState<MerchantReceiveInfo | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [qrSvg, setQrSvg] = useState("");
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    let active = true;
    api.merchantReceiveInfo(token)
      .then((data) => {
        if (!active) return;
        setInfo(data);
        return encodeQRSvg(data.paymentIdentifier).then((svg) => {
          if (active) setQrSvg(svg);
        });
      })
      .catch((caught) => {
        if (active) setError(caught instanceof Error ? caught.message : "Could not load receive information.");
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => { active = false; };
  }, [token]);

  function handleCopy() {
    if (!info?.paymentIdentifier) return;
    if (navigator.clipboard) {
      void navigator.clipboard.writeText(info.paymentIdentifier);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  }

  if (loading) return <DetailsSkeleton />;
  if (error || !info) {
    return (
      <div className="center-state">
        <InlineError message={error || "Receive information unavailable."} />
      </div>
    );
  }

  if (info.accountStatus !== "ACTIVE") {
    return (
      <div className="center-state">
        <div className="result-mark result-pending"><Icon name="alert" size={24} /></div>
        <h1 style={{ fontSize: "1.6rem" }}>Account inactive</h1>
        <p style={{ color: "var(--muted)" }}>Your merchant account is not currently active. Contact support for assistance.</p>
      </div>
    );
  }

  return (
    <>
      <PageHeader
        eyebrow="Receive Payments"
        title="Accept payments"
        description="Share your QR code or payment identifier to accept payments from customers on TransactX."
      />
      <div className="merchant-receive-layout">
        <div className="merchant-qr-card">
          <div className="merchant-qr-label">
            <span className="eyebrow">Scan to pay</span>
            <p className="merchant-qr-hint">
              Ask your customer to scan this QR code using their TransactX app.
            </p>
          </div>
          {qrSvg ? (
            <div
              className="merchant-qr-frame"
              aria-label={`QR code for payment to ${info.paymentIdentifier}`}
              dangerouslySetInnerHTML={{ __html: qrSvg }}
            />
          ) : (
            <div className="merchant-qr-frame merchant-qr-placeholder">
              <Skeleton className="skeleton-block" />
              <span>Generating QR code…</span>
            </div>
          )}
          <div className="merchant-qr-id">
            <span className="eyebrow">Payment ID</span>
            <strong className="merchant-pid">{info.paymentIdentifier}</strong>
          </div>
          <button
            type="button"
            className={`button button-secondary button-sm merchant-copy-btn ${copied ? "is-copied" : ""}`}
            onClick={handleCopy}
            aria-live="polite"
          >
            {copied ? (
              <>
                <Icon name="check" size={14} />
                <span>Copied to clipboard</span>
              </>
            ) : (
              <>
                <span>Copy payment ID</span>
                <Icon name="arrow" size={14} />
              </>
            )}
          </button>
        </div>

        <div className="merchant-receive-instructions">
          <div className="merchant-receive-note">
            <span className="note-index">01</span>
            <div>
              <strong>Verified recipient</strong>
              <p>Customers can scan your QR code or enter your payment identifier in their TransactX app. Your merchant identity is verified automatically before payment confirmation.</p>
            </div>
          </div>
          <div className="merchant-receive-note">
            <span className="note-index">02</span>
            <div>
              <strong>Server-authoritative settlement</strong>
              <p>Transactions are coordinated across participant bank ledgers. Once processed, the outcome will appear in your incoming payments feed once confirmed.</p>
            </div>
          </div>
          <div className="merchant-receive-note">
            <span className="note-index">03</span>
            <div>
              <strong>Account verification</strong>
              <p>Active merchant account. Associated account number: <strong className="mono-code">{info.accountNumber}</strong>.</p>
            </div>
          </div>
        </div>
      </div>
    </>
  );
}

// ---------------------------------------------------------------------------
// Merchant Incoming Payments View
// ---------------------------------------------------------------------------

function MerchantIncomingView({ payments, onRefresh }: { payments: Payment[]; onRefresh: () => void }) {
  const received = payments.filter((p) => p.direction === "RECEIVED");

  return (
    <>
      <PageHeader
        eyebrow="Incoming Feed"
        title="Payments received"
        description="Chronological feed of incoming payments sent to your merchant account."
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
          description="Incoming payments from customers will appear here in real time as they are processed."
        />
      ) : (
        <section className="transactions-section">
          <div className="transactions-toolbar">
            <span>{received.length} {received.length === 1 ? "payment" : "payments"} in recent feed</span>
            <span className="toolbar-note">Most recent first · Click row to expand details</span>
          </div>
          <div className="payment-list payment-list-large">
            {received.map((p) => (
              <MerchantPaymentRow key={p.id} payment={p} expanded />
            ))}
          </div>
        </section>
      )}
    </>
  );
}

// ---------------------------------------------------------------------------
// Merchant Settlement Status View
// ---------------------------------------------------------------------------

function MerchantSettlementView({ payments, onRefresh }: { payments: Payment[]; onRefresh: () => void }) {
  const received = payments.filter((p) => p.direction === "RECEIVED");
  const completed = received.filter((p) => p.state === "COMPLETED");
  const pending = received.filter((p) => ["PROCESSING", "PENDING_RECONCILIATION", "BANK_SETTLED_CENTRAL_PENDING"].includes(p.state));
  const failed = received.filter((p) => ["FAILED", "REVERSED"].includes(p.state));
  const other = received.filter((p) => !["COMPLETED", "PROCESSING", "PENDING_RECONCILIATION", "BANK_SETTLED_CENTRAL_PENDING", "FAILED", "REVERSED"].includes(p.state));

  return (
    <>
      <PageHeader
        eyebrow="Settlement"
        title="Settlement status"
        description="Track the settlement state of payments received."
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
          description="Settlement status will appear here once payments are received."
        />
      ) : (
        <div className="merchant-settlement-layout">
          <SettlementGroup
            title="Completed"
            count={completed.length}
            tone="success"
            payments={completed}
            description="Payments with a confirmed completed state."
          />
          <SettlementGroup
            title="Pending confirmation"
            count={pending.length}
            tone="warning"
            payments={pending}
            description="Payment is being processed. Outcome is not yet confirmed — do not consider this settled."
          />
          <SettlementGroup
            title="Failed / Reversed"
            count={failed.length}
            tone="error"
            payments={failed}
            description="Payment did not reach a completed state. Review the payment details for the current outcome."
          />
          {other.length > 0 && (
            <SettlementGroup
              title="Other"
              count={other.length}
              tone="neutral"
              payments={other}
              description="Status is currently unclassified."
            />
          )}
        </div>
      )}
    </>
  );
}

function SettlementGroup({ title, count, tone, payments, description }: { title: string; count: number; tone: "success" | "warning" | "error" | "neutral"; payments: Payment[]; description: string }) {
  const [open, setOpen] = useState(tone === "success" || tone === "warning");

  return (
    <div className={`settlement-group settlement-group-${tone}`}>
      <button
        type="button"
        className="settlement-group-header"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
      >
        <span className="settlement-group-title">
          <span className={`status-dot status-${tone}-dot`} />
          {title}
          <span className="settlement-count">{count}</span>
        </span>
        <span className="settlement-description">{description}</span>
        <Icon name="chevron" size={16} />
      </button>
      {open && (
        <div className="settlement-group-body">
          {payments.length === 0 ? (
            <p className="settlement-empty">No payments in this category.</p>
          ) : (
            payments.map((p) => <MerchantPaymentRow key={p.id} payment={p} expanded />)
          )}
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Merchant Transaction Search View
// ---------------------------------------------------------------------------

function MerchantSearchView({ payments }: { payments: Payment[] }) {
  const [query, setQuery] = useState("");
  const [stateFilter, setStateFilter] = useState("all");

  const received = payments.filter((p) => p.direction === "RECEIVED");

  const filtered = received.filter((p) => {
    const matchQuery = query.trim() === "" ||
      p.senderName.toLowerCase().includes(query.toLowerCase()) ||
      p.senderPaymentIdentifier.toLowerCase().includes(query.toLowerCase()) ||
      (p.note ?? "").toLowerCase().includes(query.toLowerCase());
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
        description="Filter and locate incoming payments in your recent feed by customer name, identifier, or note."
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
          action={hasFilter ? <Button variant="secondary" onClick={handleReset}>Reset search</Button> : undefined}
        />
      ) : (
        <section className="transactions-section">
          <div className="transactions-toolbar">
            <span>{filtered.length} {filtered.length === 1 ? "result" : "results"} found</span>
            <span className="toolbar-note">{received.length} total in recent feed · Bounded search</span>
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

// ---------------------------------------------------------------------------
// Shared Merchant payment row (safe — no internal IDs)
// ---------------------------------------------------------------------------

function MerchantPaymentRow({ payment, expanded = false }: { payment: Payment; expanded?: boolean }) {
  const [isExpanded, setIsExpanded] = useState(expanded);

  const stateText = payment.state === "COMPLETED" ? "Completed"
    : payment.state === "FAILED" ? "Not completed"
    : payment.state === "REVERSED" ? "Reversed"
    : ["PROCESSING", "PENDING_RECONCILIATION", "BANK_SETTLED_CENTRAL_PENDING"].includes(payment.state) ? "Confirming…"
    : payment.state.replace(/_/g, " ").toLowerCase();

  return (
    <div
      className={`merchant-payment-row ${isExpanded ? "merchant-payment-is-expanded" : ""}`}
      onClick={() => setIsExpanded((prev) => !prev)}
      role="button"
      tabIndex={0}
      aria-expanded={isExpanded}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          setIsExpanded((prev) => !prev);
        }
      }}
    >
      <Avatar name={payment.senderName} />
      <span className="payment-main">
        <strong>{payment.senderName}</strong>
        <small>{payment.senderPaymentIdentifier} · {formatDate(payment.createdAt)}</small>
        {payment.note && <small className="merchant-payment-note">"{payment.note}"</small>}
      </span>
      <span className="payment-amount payment-received">
        <Amount paise={payment.amountPaise} sign="+ " />
        <small><StatusBadge state={payment.state} /></small>
      </span>
      {isExpanded && (
        <div className="merchant-payment-expanded">
          <span className="merchant-state-pill" data-state={payment.state}>
            <span className={`status-dot status-${payment.state === "COMPLETED" ? "success" : ["FAILED", "REVERSED"].includes(payment.state) ? "error" : "warning"}-dot`} />
            {stateText}
          </span>
          {payment.completedAt && (
            <small className="merchant-completed-at">Settled {formatDate(payment.completedAt)}</small>
          )}
          {payment.failureReason && (
            <small className="merchant-failure-note">Reason: {payment.failureReason}</small>
          )}
          <span className="merchant-ref">Ref: {payment.id.slice(0, 8)}</span>
        </div>
      )}
    </div>
  );
}

// (QR encoding is handled by src/qr.ts using the `qrcode` npm package)

// ---------------------------------------------------------------------------
// Routing helpers for merchant views
// ---------------------------------------------------------------------------

function readMerchantView(): MerchantView {
  const p = window.location.pathname;
  if (p.startsWith("/merchant/receive")) return "m-receive";
  if (p.startsWith("/merchant/incoming")) return "m-incoming";
  if (p.startsWith("/merchant/settlement")) return "m-settlement";
  if (p.startsWith("/merchant/search")) return "m-search";
  return "m-home";
}

function merchantViewPath(view: MerchantView): string {
  switch (view) {
    case "m-receive": return "/merchant/receive";
    case "m-incoming": return "/merchant/incoming";
    case "m-settlement": return "/merchant/settlement";
    case "m-search": return "/merchant/search";
    default: return "/merchant";
  }
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

function isToday(dateStr: string): boolean { const d = new Date(dateStr); const now = new Date(); return d.getFullYear() === now.getFullYear() && d.getMonth() === now.getMonth() && d.getDate() === now.getDate(); }

