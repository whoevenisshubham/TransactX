import { useEffect, useRef, useState } from "react";
import { api, ApiError } from "../../api";
import { Button, InlineError } from "../../components";
import { WorkspaceShell } from "../../components/layout/WorkspaceShell";
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

  const navGroups = [
    {
      id: "merchant-primary",
      items: [
        {
          id: "m-home",
          label: "Dashboard",
          active: view === "m-home",
          icon: <MerchantIcon name="home" />,
          onSelect: () => navigate("m-home"),
        },
        {
          id: "m-receive",
          label: "Receive / QR",
          active: view === "m-receive",
          icon: <MerchantIcon name="qr" />,
          onSelect: () => navigate("m-receive"),
        },
        {
          id: "m-incoming",
          label: "Incoming",
          active: view === "m-incoming",
          icon: <MerchantIcon name="activity" />,
          onSelect: () => navigate("m-incoming"),
        },
        {
          id: "m-settlement",
          label: "Settlement",
          active: view === "m-settlement",
          icon: <MerchantIcon name="check" />,
          onSelect: () => navigate("m-settlement"),
        },
        {
          id: "m-search",
          label: "Search",
          active: view === "m-search",
          icon: <MerchantIcon name="search" />,
          onSelect: () => navigate("m-search"),
        },
      ],
    },
  ];

  const bottomNavItems = [
    {
      id: "m-home",
      label: "Dashboard",
      active: view === "m-home",
      icon: <MerchantIcon name="home" size={16} />,
      onSelect: () => navigate("m-home"),
    },
    {
      id: "m-receive",
      label: "Receive",
      active: view === "m-receive",
      icon: <MerchantIcon name="qr" size={16} />,
      onSelect: () => navigate("m-receive"),
    },
    {
      id: "m-incoming",
      label: "Incoming",
      active: view === "m-incoming",
      icon: <MerchantIcon name="activity" size={16} />,
      onSelect: () => navigate("m-incoming"),
    },
    {
      id: "m-settlement",
      label: "Settlement",
      active: view === "m-settlement",
      icon: <MerchantIcon name="check" size={16} />,
      onSelect: () => navigate("m-settlement"),
    },
    {
      id: "m-search",
      label: "Search",
      active: view === "m-search",
      icon: <MerchantIcon name="search" size={16} />,
      onSelect: () => navigate("m-search"),
    },
  ];

  return (
    <WorkspaceShell
      role="merchant"
      roleLabel="Merchant"
      roleBadgeClassName="merchant-badge"
      shellClassName="merchant-shell reset-workspace reset-merchant reset-expanded"
      desktopNavAriaLabel="Merchant navigation"
      mobileDialogLabel="Merchant navigation"
      menuButtonLabel="Open navigation"
      defaultDesktopCollapsed={false}
      navGroups={navGroups}
      bottomNavItems={bottomNavItems}
      user={{ name: user.name, secondary: user.paymentIdentifier }}
      onLogout={onLogout}
    >
      <div className="workspace-topbar">
        <span>Merchant / {viewTitle}</span>
        <span>
          {!isOnline ? (
            <span style={{ color: "var(--warning-strong)" }}>
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
            background: "var(--warning-pale)",
            color: "var(--warning-strong)",
            border: "1px solid var(--warning-line)",
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
    </WorkspaceShell>
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
      className="icon shrink-0"
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
