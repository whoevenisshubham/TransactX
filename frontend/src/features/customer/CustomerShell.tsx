import { useEffect, useRef, useState } from "react";
import { api, ApiError } from "../../api";
import { Avatar, BrandMark, Button, Icon, InlineError } from "../../components";
import { LoadingShell } from "../../shared/ShellStates";
import { ThemeSelect } from "../../theme";
import { OfflineIntentQueue, type OfflineIntent } from "../../offlineQueue";
import { OfflineReplayWorker } from "../../offlineReplay";
import type { Account, Payment, User, View } from "../../types";
import { CustomerDashboard } from "./CustomerDashboard";
import { SendMoneyPage } from "./SendMoneyPage";
import { TransactionActivity } from "./TransactionActivity";
import { ReceiptDrawer } from "./ReceiptDrawer";
import { OfflineQueuePanel } from "./OfflineQueuePanel";
import type { CustomerShellProps } from "./types";

export function CustomerShell({ token, onLogout }: CustomerShellProps) {
  const [user, setUser] = useState<User | null>(null);
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [payments, setPayments] = useState<Payment[]>([]);
  const [queue, setQueue] = useState<OfflineIntentQueue | null>(null);
  const [queueIntents, setQueueIntents] = useState<OfflineIntent[]>([]);
  const [online, setOnline] = useState(() => navigator.onLine);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [view, setView] = useState<View>(readView());
  const [selectedPaymentID, setSelectedPaymentID] = useState<string | null>(readPaymentID());
  const replayRef = useRef<() => Promise<void>>(() => Promise.resolve());

  useEffect(() => {
    let active = true;
    Promise.all([api.me(token), api.accounts(token), api.payments(token)])
      .then(([profile, accountList, paymentList]) => {
        if (active) {
          setQueue(new OfflineIntentQueue(undefined, profile.id));
          setUser(profile);
          setAccounts(accountList);
          setPayments(paymentList);
          setLoading(false);
        }
      })
      .catch((caught) => {
        if (!active) return;
        if (caught instanceof ApiError && caught.status === 401) onLogout();
        else {
          setError(caught instanceof Error ? caught.message : "We couldn't load your account.");
          setLoading(false);
        }
      });
    return () => {
      active = false;
    };
  }, [token, onLogout]);

  async function refreshQueue(scopedQueue = queue) {
    if (!scopedQueue) return;
    const states: OfflineIntent["state"][] = [
      "QUEUED",
      "SYNCING",
      "SYNCED",
      "RETRYABLE",
      "FAILED",
    ];
    const records = (
      await Promise.all(states.map((state) => scopedQueue.listByState(state)))
    ).flat();
    setQueueIntents(records.sort((left, right) => right.updatedAt.localeCompare(left.updatedAt)));
  }

  useEffect(() => {
    if (!queue) return;
    let active = true;
    const worker = new OfflineReplayWorker(queue, {
      createPayment: async (payload, idempotencyKey, clientRequestId) => {
        const response = await api.createPaymentWithStatus(
          payload,
          token,
          idempotencyKey,
          clientRequestId
        );
        return { payment: response.data, status: response.status };
      },
      getPayment: (paymentID) => api.payment(paymentID, token),
    });

    const replay = async () => {
      const results = await worker.run();
      if (active && results.length > 0) {
        await refreshQueue(queue);
        if (results.some((r) => r.outcome === "SYNCED" || r.outcome === "FAILED")) {
          api.payments(token).then(setPayments).catch(() => undefined);
          api.accounts(token).then(setAccounts).catch(() => undefined);
        }
      }
    };
    replayRef.current = replay;

    const handleOnline = () => {
      setOnline(true);
      void replay();
    };
    const handleOffline = () => setOnline(false);

    window.addEventListener("online", handleOnline);
    window.addEventListener("offline", handleOffline);
    void replay();

    // Periodic sweep for retryable intents whose nextAttemptAt becomes due while page remains open
    const sweepInterval = window.setInterval(() => {
      if (navigator.onLine) {
        void replay();
      }
    }, 15000);

    return () => {
      active = false;
      worker.stop();
      window.clearInterval(sweepInterval);
      window.removeEventListener("online", handleOnline);
      window.removeEventListener("offline", handleOffline);
      void queue.close();
    };
  }, [queue, token]);

  useEffect(() => {
    const handlePopState = () => {
      setView(readView());
      setSelectedPaymentID(readPaymentID());
    };
    window.addEventListener("popstate", handlePopState);
    return () => window.removeEventListener("popstate", handlePopState);
  }, []);

  function navigate(nextView: View, paymentID?: string) {
    const path = paymentID
      ? `/transactions/${paymentID}`
      : nextView === "home"
      ? "/"
      : `/${nextView}`;
    window.history.pushState({}, "", path);
    setView(nextView);
    setSelectedPaymentID(paymentID ?? null);
    window.scrollTo({ top: 0, behavior: "smooth" });
  }

  function refreshPayments() {
    api.payments(token).then(setPayments).catch(() => undefined);
    api.accounts(token).then(setAccounts).catch(() => undefined);
  }

  function completedPayment(payment: Payment) {
    refreshPayments();
    navigate("details", payment.id);
  }

  async function retryIntent(clientRequestId: string) {
    if (!queue) return;
    await queue.retryFailed(clientRequestId);
    const worker = new OfflineReplayWorker(queue, {
      createPayment: async (payload, idempotencyKey, requestId) => {
        const response = await api.createPaymentWithStatus(
          payload,
          token,
          idempotencyKey,
          requestId
        );
        return { payment: response.data, status: response.status };
      },
      getPayment: (paymentID) => api.payment(paymentID, token),
    });
    await worker.run();
    await refreshQueue(queue);
    refreshPayments();
  }

  function queueChanged() {
    void refreshQueue();
    if (navigator.onLine) {
      void replayRef.current();
    }
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

  return (
    <div className="product-shell customer-shell reset-workspace reset-personal reset-expanded">
      <Sidebar view={view} user={user} onNavigate={navigate} onLogout={onLogout} />
      <main className="main-content">
        <MobileHeader user={user} onLogout={onLogout} />
        <div className="content-wrap">
          <div className="workspace-topbar">
            <span>
              Personal /{" "}
              {view === "home"
                ? "Overview"
                : view === "pay"
                ? "Send money"
                : view === "details"
                ? "Receipt"
                : "Activity"}
            </span>
            <NetworkStatus online={online} />
          </div>

          {view === "home" && (
            <CustomerDashboard
              user={user}
              account={account}
              payments={payments}
              onNavigate={navigate}
              onExploreDemo={() => {
                onLogout();
                window.location.assign("/demo");
              }}
            />
          )}

          {view === "pay" && queue && (
            <SendMoneyPage
              account={account}
              token={token}
              queue={queue}
              onComplete={completedPayment}
              onQueueChanged={queueChanged}
              onNavigate={navigate}
            />
          )}

          {view === "transactions" && (
            <TransactionActivity payments={payments} onNavigate={navigate} />
          )}

          {view === "details" && selectedPaymentID && (
            <ReceiptDrawer
              paymentID={selectedPaymentID}
              token={token}
              onNavigate={navigate}
            />
          )}

          <OfflineQueuePanel intents={queueIntents} onRetry={retryIntent} />
        </div>
      </main>
      <MobileNav view={view} onNavigate={navigate} />
    </div>
  );
}

function Sidebar({
  view,
  user,
  onNavigate,
  onLogout,
}: {
  view: View;
  user: User | null;
  onNavigate: (view: View) => void;
  onLogout: () => void;
}) {
  return (
    <aside className="sidebar">
      <div className="brand-lockup">
        <BrandMark />
        <span>TransactX</span>
        <span className="customer-tag">Personal</span>
      </div>
      <div className="sidebar-rule" />
      <nav aria-label="Primary navigation">
        <NavItem
          view="home"
          active={view === "home"}
          label="Overview"
          icon="home"
          onClick={onNavigate}
        />
        <NavItem
          view="pay"
          active={view === "pay"}
          label="Pay"
          icon="send"
          onClick={onNavigate}
        />
        <NavItem
          view="transactions"
          active={view === "transactions" || view === "details"}
          label="Transactions"
          icon="activity"
          onClick={onNavigate}
        />
      </nav>
      <div className="sidebar-bottom">
        <ThemeSelect />
        <div className="user-chip">
          <Avatar name={user?.name ?? "Customer"} />
          <span>
            <strong>{user?.name ?? "Customer"}</strong>
            <small>{user?.paymentIdentifier ?? ""}</small>
          </span>
        </div>
        <button
          type="button"
          className="logout-button"
          onClick={onLogout}
          aria-label="Log out of TransactX"
        >
          Log out
        </button>
      </div>
    </aside>
  );
}

function NavItem({
  view,
  active,
  label,
  icon,
  onClick,
}: {
  view: View;
  active: boolean;
  label: string;
  icon: "home" | "send" | "activity";
  onClick: (view: View) => void;
}) {
  return (
    <button
      type="button"
      className={`nav-item ${active ? "is-active" : ""}`}
      onClick={() => onClick(view)}
      aria-current={active ? "page" : undefined}
    >
      <Icon name={icon} />
      <span>{label}</span>
    </button>
  );
}

function MobileHeader({
  user,
  onLogout,
}: {
  user: User | null;
  onLogout: () => void;
}) {
  return (
    <header className="mobile-header">
      <div className="mobile-brand">
        <BrandMark />
        <span>TransactX</span>
        <span className="customer-tag">Personal</span>
      </div>
      <ThemeSelect />
      <button
        type="button"
        className="mobile-user"
        onClick={onLogout}
        aria-label={`Log out ${user?.name ?? ""}`}
        title="Log out"
      >
        <Avatar name={user?.name ?? "Customer"} />
      </button>
    </header>
  );
}

function MobileNav({
  view,
  onNavigate,
}: {
  view: View;
  onNavigate: (view: View) => void;
}) {
  return (
    <nav className="mobile-nav" aria-label="Mobile navigation">
      <button
        type="button"
        className={`mobile-nav-item ${view === "home" ? "is-active" : ""}`}
        onClick={() => onNavigate("home")}
        aria-current={view === "home" ? "page" : undefined}
      >
        <Icon name="home" size={18} />
        <span>Overview</span>
      </button>
      <button
        type="button"
        className={`mobile-nav-item ${view === "pay" ? "is-active" : ""}`}
        onClick={() => onNavigate("pay")}
        aria-current={view === "pay" ? "page" : undefined}
      >
        <Icon name="send" size={18} />
        <span>Pay</span>
      </button>
      <button
        type="button"
        className={`mobile-nav-item ${
          view === "transactions" || view === "details" ? "is-active" : ""
        }`}
        onClick={() => onNavigate("transactions")}
        aria-current={view === "transactions" || view === "details" ? "page" : undefined}
      >
        <Icon name="activity" size={18} />
        <span>Activity</span>
      </button>
    </nav>
  );
}

function NetworkStatus({ online }: { online: boolean }) {
  return (
    <div
      className={`network-status ${online ? "network-online" : "network-offline"}`}
      role="status"
      aria-live="polite"
    >
      <span className="status-dot" />
      {online ? "ONLINE" : "OFFLINE"}
      <small>
        {online
          ? "Network connection active · Replay verifies reachability."
          : "Offline · Payment intents will be saved locally on this device."}
      </small>
    </div>
  );
}

function readView(): View {
  const path = window.location.pathname;
  if (path.startsWith("/pay")) return "pay";
  if (path.startsWith("/transactions/")) return "details";
  if (path.startsWith("/transactions")) return "transactions";
  return "home";
}

function readPaymentID(): string | null {
  const match = window.location.pathname.match(/^\/transactions\/([^/]+)/);
  return match?.[1] ?? null;
}
