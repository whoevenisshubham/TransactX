import { useEffect, useRef, useState } from "react";
import { api, ApiError } from "../../api";
import { Avatar, BrandMark, Button, InlineError } from "../../components";
import { ThemeSelect } from "../../theme";
import { LoadingShell } from "../../shared/ShellStates";
import { MerchantDashboard } from "./MerchantDashboard";
import { ReceiveStation } from "./ReceiveStation";
import { IncomingPayments } from "./IncomingPayments";
import { SettlementOverview } from "./SettlementOverview";
import { MerchantSearch } from "./MerchantSearch";
import type { Account, MerchantView, Payment, User } from "./types";

export interface MerchantShellProps {
  token: string;
  user: User;
  onLogout: () => void;
}

export function MerchantShell({ token, user, onLogout }: MerchantShellProps) {
  const [view, setView] = useState<MerchantView>(readMerchantView());
  const [payments, setPayments] = useState<Payment[]>([]);
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [isOnline, setIsOnline] = useState(() =>
    typeof navigator !== "undefined" ? navigator.onLine : true
  );
  const refreshGeneration = useRef(0);

  useEffect(() => {
    let active = true;
    Promise.all([api.accounts(token), api.payments(token)])
      .then(([accountList, paymentList]) => {
        if (active) {
          setAccounts(accountList);
          setPayments(paymentList);
          setLoading(false);
        }
      })
      .catch((caught) => {
        if (!active) return;
        if (caught instanceof ApiError && caught.status === 401) {
          onLogout();
        } else {
          setError(
            caught instanceof Error ? caught.message : "We couldn't load your merchant account."
          );
          setLoading(false);
        }
      });
    return () => {
      active = false;
    };
  }, [token, onLogout]);

  useEffect(() => {
    const handlePop = () => setView(readMerchantView());
    const handleOnline = () => setIsOnline(true);
    const handleOffline = () => setIsOnline(false);

    window.addEventListener("popstate", handlePop);
    window.addEventListener("online", handleOnline);
    window.addEventListener("offline", handleOffline);

    return () => {
      window.removeEventListener("popstate", handlePop);
      window.removeEventListener("online", handleOnline);
      window.removeEventListener("offline", handleOffline);
    };
  }, []);

  function navigate(nextView: MerchantView) {
    const path = merchantViewPath(nextView);
    window.history.pushState({}, "", path);
    setView(nextView);
    window.scrollTo({ top: 0, behavior: "smooth" });
  }

  function refreshPayments() {
    if (!navigator.onLine) return;
    const currentGen = ++refreshGeneration.current;
    api.payments(token)
      .then((p) => {
        if (currentGen === refreshGeneration.current) setPayments(p);
      })
      .catch(() => undefined);
    api.accounts(token)
      .then((acc) => {
        if (currentGen === refreshGeneration.current) setAccounts(acc);
      })
      .catch(() => undefined);
  }

  if (loading) return <LoadingShell />;
  if (error) {
    return (
      <main className="center-state">
        <InlineError message={error} />
        <Button onClick={() => window.location.reload()}>Try again</Button>
      </main>
    );
  }

  const account = accounts[0];

  const viewTitle =
    view === "m-home"
      ? "Dashboard"
      : view === "m-receive"
      ? "Receive"
      : view === "m-incoming"
      ? "Incoming"
      : view === "m-settlement"
      ? "Settlement"
      : "Search";

  return (
    <div className="product-shell merchant-shell reset-workspace reset-merchant reset-expanded">
      <MerchantSidebar
        view={view}
        user={user}
        onNavigate={navigate}
        onLogout={onLogout}
      />
      <main className="main-content">
        <MerchantMobileHeader user={user} onLogout={onLogout} />
        <div className="content-wrap">
          <div className="workspace-topbar">
            <span>Merchant / {viewTitle}</span>
            <span>
              {!isOnline ? (
                <span style={{ color: "var(--warning-strong, #b45309)" }}>
                  ● Offline (cached observations)
                </span>
              ) : (
                "Simulated payment network"
              )}
            </span>
          </div>

          {!isOnline && (
            <div
              style={{
                background: "var(--warning-pale, #fffbeb)",
                color: "var(--warning-strong, #92400e)",
                border: "1px solid var(--warning-line, #fef3c7)",
                borderRadius: "8px",
                padding: "10px 16px",
                fontSize: "13px",
                marginBottom: "16px",
                display: "flex",
                alignItems: "center",
                gap: "8px",
              }}
              role="status"
            >
              <span>⚠</span>
              <span>
                Offline mode — displaying cached ledger observations. Automatic refresh paused until connection is restored.
              </span>
            </div>
          )}

          {view === "m-home" && (
            <MerchantDashboard
              user={user}
              account={account}
              payments={payments}
              onNavigate={navigate}
              onRefresh={refreshPayments}
            />
          )}
          {view === "m-receive" && <ReceiveStation token={token} />}
          {view === "m-incoming" && (
            <IncomingPayments payments={payments} onRefresh={refreshPayments} />
          )}
          {view === "m-settlement" && (
            <SettlementOverview payments={payments} onRefresh={refreshPayments} />
          )}
          {view === "m-search" && <MerchantSearch payments={payments} />}
        </div>
      </main>
      <MerchantMobileNav view={view} onNavigate={navigate} />
    </div>
  );
}

function MerchantSidebar({
  view,
  user,
  onNavigate,
  onLogout,
}: {
  view: MerchantView;
  user: User;
  onNavigate: (v: MerchantView) => void;
  onLogout: () => void;
}) {
  return (
    <aside className="sidebar merchant-sidebar">
      <div className="brand-lockup">
        <BrandMark />
        <span>TransactX</span>
        <span className="merchant-badge">Merchant</span>
      </div>
      <div className="sidebar-rule" />
      <nav aria-label="Merchant navigation">
        <MerchantNavItem
          view="m-home"
          active={view === "m-home"}
          label="Dashboard"
          icon="home"
          onClick={onNavigate}
        />
        <MerchantNavItem
          view="m-receive"
          active={view === "m-receive"}
          label="Receive / QR"
          icon="qr"
          onClick={onNavigate}
        />
        <MerchantNavItem
          view="m-incoming"
          active={view === "m-incoming"}
          label="Incoming"
          icon="activity"
          onClick={onNavigate}
        />
        <MerchantNavItem
          view="m-settlement"
          active={view === "m-settlement"}
          label="Settlement"
          icon="check"
          onClick={onNavigate}
        />
        <MerchantNavItem
          view="m-search"
          active={view === "m-search"}
          label="Search"
          icon="search"
          onClick={onNavigate}
        />
      </nav>
      <div className="sidebar-bottom">
        <ThemeSelect />
        <div className="user-chip">
          <Avatar name={user.name} />
          <span>
            <strong>{user.name}</strong>
            <small>{user.paymentIdentifier}</small>
          </span>
        </div>
        <button type="button" className="logout-button" onClick={onLogout}>
          Log out
        </button>
      </div>
    </aside>
  );
}

function MerchantNavItem({
  view,
  active,
  label,
  icon,
  onClick,
}: {
  view: MerchantView;
  active: boolean;
  label: string;
  icon: "home" | "qr" | "activity" | "check" | "search";
  onClick: (v: MerchantView) => void;
}) {
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

function MerchantMobileNav({
  view,
  onNavigate,
}: {
  view: MerchantView;
  onNavigate: (v: MerchantView) => void;
}) {
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

function MerchantIcon({
  name,
  size = 18,
}: {
  name: "home" | "qr" | "activity" | "check" | "search";
  size?: number;
}) {
  const paths: Record<string, React.ReactNode> = {
    home: (
      <>
        <path d="m3 9 6-5 6 5" />
        <path d="M5 8v7h8V8M8 15v-4h2v4" />
      </>
    ),
    activity: (
      <>
        <path d="M3 12h3l2-5 3 10 2-5h3" />
        <path d="M3 4h12M3 20h12" />
      </>
    ),
    check: <path d="m4 9 3 3 6-7" />,
    qr: (
      <>
        <rect x="3" y="3" width="6" height="6" rx="1" />
        <rect x="9" y="3" width="6" height="6" rx="1" />
        <rect x="3" y="9" width="6" height="6" rx="1" />
        <path d="M9 12h1m2 0h1M9 15h1m2-3v3m2 0h1" />
      </>
    ),
    search: (
      <>
        <circle cx="8" cy="8" r="5" />
        <path d="m15 15-3.5-3.5" />
      </>
    ),
  };
  return (
    <svg
      aria-hidden="true"
      className="icon"
      width={size}
      height={size}
      viewBox="0 0 18 18"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      {paths[name]}
    </svg>
  );
}

function MerchantMobileHeader({
  user,
  onLogout,
}: {
  user: User;
  onLogout: () => void;
}) {
  return (
    <header className="mobile-header merchant-mobile-header">
      <div className="mobile-brand">
        <BrandMark />
        <span>TransactX</span>
        <span className="merchant-badge-mobile">Merchant</span>
      </div>
      <ThemeSelect />
      <button
        type="button"
        className="mobile-user"
        onClick={onLogout}
        aria-label={`Log out ${user.name}`}
      >
        <Avatar name={user.name} size="sm" />
      </button>
    </header>
  );
}

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
    case "m-receive":
      return "/merchant/receive";
    case "m-incoming":
      return "/merchant/incoming";
    case "m-settlement":
      return "/merchant/settlement";
    case "m-search":
      return "/merchant/search";
    default:
      return "/merchant";
  }
}
