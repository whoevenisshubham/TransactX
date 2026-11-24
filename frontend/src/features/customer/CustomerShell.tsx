import { useEffect, useRef, useState } from "react";
import { api, ApiError } from "../../api";
import { Amount, Avatar, BrandMark, Button, Icon, InlineError } from "../../components";
import { LoadingShell } from "../../shared/ShellStates";
import { ThemeSelect } from "../../theme";
import { OfflineIntentQueue, type OfflineIntent } from "../../offlineQueue";
import { OfflineReplayWorker } from "../../offlineReplay";
import { DurableUnresolvedStore, type DurableUnresolvedIntent } from "../../unresolvedStore";
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
  const [unresolvedStore, setUnresolvedStore] = useState<DurableUnresolvedStore | null>(null);
  const [storeStatus, setStoreStatus] = useState<"uninitialized" | "loading" | "ready" | "failed">("uninitialized");
  const [activeUnresolvedIntent, setActiveUnresolvedIntent] = useState<DurableUnresolvedIntent | null>(null);
  const replayRef = useRef<() => Promise<void>>(() => Promise.resolve());

  useEffect(() => {
    if (!user) {
      setUnresolvedStore(null);
      setActiveUnresolvedIntent(null);
      setStoreStatus("uninitialized");
      return;
    }
    setStoreStatus("loading");
    const store = new DurableUnresolvedStore(user.id);
    setUnresolvedStore(store);

    let active = true;
    const loadActiveIntent = async () => {
      try {
        const intent = await store.getActive();
        if (active) {
          setActiveUnresolvedIntent(intent);
          setStoreStatus("ready");
        }
      } catch {
        if (active) {
          setStoreStatus("failed");
        }
      }
    };

    void loadActiveIntent();

    const unsubscribe = store.onChanged(() => {
      void loadActiveIntent();
    });

    return () => {
      active = false;
      unsubscribe();
      store.close();
    };
  }, [user]);

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

  const refreshGenRef = useRef(0);

  useEffect(() => {
    if (!queue) return;
    let active = true;
    let scheduleTimeoutId: number | null = null;

    const clearScheduledTimer = () => {
      if (scheduleTimeoutId !== null) {
        window.clearTimeout(scheduleTimeoutId);
        scheduleTimeoutId = null;
      }
    };

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

    const scheduleNextReplay = async () => {
      clearScheduledTimer();
      if (!active || !navigator.onLine) return;

      try {
        const queued = await queue.listByState("QUEUED");
        const retryable = await queue.listByState("RETRYABLE");

        if (queued.length > 0) {
          void runReplayAndSchedule();
          return;
        }

        if (retryable.length === 0) return;

        const now = Date.now();
        let earliestDueTime = Infinity;

        for (const item of retryable) {
          const nextAttempt = item.retry?.nextAttemptAt;
          if (!nextAttempt) {
            earliestDueTime = 0;
            break;
          }
          const time = new Date(nextAttempt).getTime();
          if (isNaN(time) || time <= now) {
            earliestDueTime = 0;
            break;
          }
          if (time < earliestDueTime) {
            earliestDueTime = time;
          }
        }

        if (earliestDueTime === 0) {
          void runReplayAndSchedule();
          return;
        }

        if (earliestDueTime < Infinity) {
          const delay = Math.max(0, earliestDueTime - now);
          scheduleTimeoutId = window.setTimeout(() => {
            scheduleTimeoutId = null;
            void runReplayAndSchedule();
          }, delay);
        }
      } catch {
        // queue closed or storage error
      }
    };

    const runReplayAndSchedule = async () => {
      if (!active || !navigator.onLine) return;
      const results = await worker.run();
      if (active) {
        if (results.length > 0) {
          await refreshQueue(queue);
          if (results.some((r) => r.outcome === "SYNCED" || r.outcome === "FAILED")) {
            refreshPayments();
          }
        }
        await scheduleNextReplay();
      }
    };

    replayRef.current = runReplayAndSchedule;

    const handleOnline = () => {
      setOnline(true);
      void runReplayAndSchedule();
    };
    const handleOffline = () => {
      setOnline(false);
      clearScheduledTimer();
    };

    window.addEventListener("online", handleOnline);
    window.addEventListener("offline", handleOffline);
    void runReplayAndSchedule();

    return () => {
      active = false;
      clearScheduledTimer();
      worker.stop();
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
    const currentGen = ++refreshGenRef.current;
    api.payments(token)
      .then((data) => {
        if (currentGen === refreshGenRef.current) {
          setPayments(data);
        }
      })
      .catch(() => undefined);
    api.accounts(token)
      .then((data) => {
        if (currentGen === refreshGenRef.current) {
          setAccounts(data);
        }
      })
      .catch(() => undefined);
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

          {activeUnresolvedIntent && (
            <div
              className="p-4 mb-4 rounded-lg border border-[var(--reset-gold)] bg-[var(--surface-soft)] text-xs flex flex-wrap items-center justify-between gap-3"
              role="alert"
              data-testid="global-unresolved-banner"
            >
              <div className="flex items-center gap-2 font-medium">
                <Icon name="activity" size={16} />
                <span>
                  Unresolved transfer pending: <Amount paise={activeUnresolvedIntent.amountPaise} /> to{" "}
                  <strong>{activeUnresolvedIntent.recipient.name}</strong> (Idempotency key:{" "}
                  <code className="font-mono text-xs">{activeUnresolvedIntent.idempotencyKey.slice(0, 8)}…</code>)
                </span>
              </div>
              <Button size="sm" onClick={() => navigate("pay")}>
                Resolve transfer
              </Button>
            </div>
          )}

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
              unresolvedStore={unresolvedStore ?? undefined}
              unresolvedIntent={activeUnresolvedIntent}
              storeStatus={storeStatus}
              onComplete={completedPayment}
              onPaymentCompleted={refreshPayments}
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
