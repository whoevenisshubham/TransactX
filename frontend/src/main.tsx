import { FormEvent, StrictMode, useEffect, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import { api, ApiError } from "./api";
import { Amount, Avatar, Badge, BrandMark, Button, EmptyState, Field, formatDate, Icon, InlineError, PageHeader, PaymentRow, paymentResultCopy, Skeleton, StatusBadge } from "./components";
import { parsePaise } from "./money";
import { OfflineIntentQueue, type OfflineIntent } from "./offlineQueue";
import { OfflineReplayWorker } from "./offlineReplay";
import { encodeQRSvg } from "./qr";
import type { Account, MerchantReceiveInfo, MerchantView, Payment, Recipient, User, View } from "./types";
import "./styles.css";

export { parsePaise } from "./money";

// ---------------------------------------------------------------------------
// Root App — role-based routing
// ---------------------------------------------------------------------------

function App() {
  const [token, setToken] = useState(() => sessionStorage.getItem("transactx.token"));
  const [authMode, setAuthMode] = useState<"login" | "register">("login");
  const [user, setUser] = useState<User | null>(null);
  const [roleLoading, setRoleLoading] = useState(false);

  // When a token arrives, fetch the user's role from the server.
  useEffect(() => {
    if (!token) { setUser(null); return; }
    let active = true;
    setRoleLoading(true);
    api.me(token).then((profile) => { if (active) { setUser(profile); setRoleLoading(false); } })
      .catch(() => { if (active) { sessionStorage.removeItem("transactx.token"); setToken(null); setUser(null); setRoleLoading(false); } });
    return () => { active = false; };
  }, [token]);

  function authenticated(nextToken: string) { sessionStorage.setItem("transactx.token", nextToken); setToken(nextToken); }
  function logout() { sessionStorage.removeItem("transactx.token"); setToken(null); setUser(null); }

  if (!token) return <AuthPage mode={authMode} onModeChange={setAuthMode} onAuthenticated={authenticated} />;
  if (roleLoading || !user) return <LoadingShell />;

  if (user.role === "MERCHANT") return <MerchantShell token={token} user={user} onLogout={logout} />;
  if (user.role === "CUSTOMER") return <CustomerShell token={token} onLogout={logout} />;
  // OPS_ADMIN or unknown roles — access denied screen
  return (
    <main className="center-state" style={{ flexDirection: "column", paddingTop: "10vh" }}>
      <p className="eyebrow">Access denied</p>
      <h1 style={{ fontSize: "1.8rem" }}>This area is not available</h1>
      <p style={{ color: "var(--muted)", maxWidth: 360, textAlign: "center" }}>
        Your account role does not have access to this product. Please contact your administrator.
      </p>
      <Button onClick={logout}>Log out</Button>
    </main>
  );
}// ---------------------------------------------------------------------------
// Auth page
// ---------------------------------------------------------------------------

function AuthPage({
  mode,
  onModeChange,
  onAuthenticated,
}: {
  mode: "login" | "register";
  onModeChange: (mode: "login" | "register") => void;
  onAuthenticated: (token: string) => void;
}) {
  return (
    <main className="auth-layout">
      <section className="auth-intro">
        <div className="brand-lockup">
          <BrandMark />
          <span>TransactX</span>
        </div>
        <div className="auth-intro-copy">
          <p className="eyebrow">A calmer way to move money</p>
          <h1>Payments with a clear line of sight.</h1>
          <p>Send money, follow every confirmation, and keep your financial activity precise.</p>
        </div>
        <div className="intro-foot">
          <span className="network-pulse" />
          <span>Secure account access</span>
          <span className="intro-separator">•</span>
          <span>INR payments</span>
          <span className="intro-separator">•</span>
          <span>Real-time settlement</span>
        </div>
      </section>
      <section className="auth-panel">
        <div className="auth-panel-top">
          <span className="mobile-brand">
            <BrandMark />
            TransactX
          </span>
          <span className="auth-context">
            <Badge tone="neutral">Customer account</Badge>
          </span>
        </div>
        <div className="auth-form-wrap">
          <div className="auth-tabs" role="tablist" aria-label="Sign in or register">
            <button
              type="button"
              className={mode === "login" ? "is-active" : ""}
              onClick={() => onModeChange("login")}
              role="tab"
              aria-selected={mode === "login"}
            >
              Sign in
            </button>
            <button
              type="button"
              className={mode === "register" ? "is-active" : ""}
              onClick={() => onModeChange("register")}
              role="tab"
              aria-selected={mode === "register"}
            >
              Create account
            </button>
          </div>
          {mode === "login" ? (
            <LoginForm onAuthenticated={onAuthenticated} />
          ) : (
            <RegisterForm onAuthenticated={onAuthenticated} />
          )}
        </div>
        <p className="auth-legal">
          By continuing, you agree to use TransactX as a simulated payment network.
        </p>
      </section>
    </main>
  );
}

function LoginForm({ onAuthenticated }: { onAuthenticated: (token: string) => void }) {
  const [identifier, setIdentifier] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!identifier.trim() || !password) return;
    setBusy(true);
    setError("");
    try {
      const result = await api.login(identifier.trim(), password);
      onAuthenticated(result.token);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "We couldn't sign you in.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="auth-form" onSubmit={submit}>
      <div className="form-heading">
        <p className="eyebrow">Welcome back</p>
        <h2>Sign in to continue</h2>
        <p>Access your balance and payment activity.</p>
      </div>
      <Field
        label="Phone or payment ID"
        value={identifier}
        onChange={setIdentifier}
        placeholder="you@transactx"
        autoComplete="username"
        required
      />
      <Field
        label="Password"
        value={password}
        onChange={setPassword}
        type="password"
        autoComplete="current-password"
        required
      />
      {error && <InlineError message={error} />}
      <Button type="submit" disabled={busy || !identifier.trim() || !password}>
        {busy ? "Signing in…" : "Sign in"}
        <Icon name="arrow" />
      </Button>
    </form>
  );
}

function RegisterForm({ onAuthenticated }: { onAuthenticated: (token: string) => void }) {
  const [form, setForm] = useState({
    name: "",
    phone: "",
    paymentIdentifier: "",
    password: "",
    role: "CUSTOMER",
  });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  function update(field: keyof typeof form, value: string) {
    setForm((current) => ({ ...current, [field]: value }));
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!form.name.trim() || !form.phone.trim() || !form.paymentIdentifier.trim()) {
      setError("Please fill in all required fields.");
      return;
    }
    if (form.password.length < 8) {
      setError("Password must be at least 8 characters long.");
      return;
    }
    setBusy(true);
    setError("");
    try {
      await api.register(form);
      const result = await api.login(form.paymentIdentifier.trim(), form.password);
      onAuthenticated(result.token);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "We couldn't create your account.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="auth-form auth-form-register" onSubmit={submit}>
      <div className="form-heading">
        <p className="eyebrow">Start with TransactX</p>
        <h2>Create your account</h2>
        <p>A simple account for clear, deliberate payments.</p>
      </div>
      <div className="form-grid">
        <Field
          label="Full name"
          value={form.name}
          onChange={(value) => update("name", value)}
          autoComplete="name"
          placeholder="Aarav Sharma"
          required
        />
        <Field
          label="Phone"
          value={form.phone}
          onChange={(value) => update("phone", value)}
          autoComplete="tel"
          placeholder="+91 98765 43210"
          required
        />
        <Field
          className="form-grid-wide"
          label="Payment ID"
          value={form.paymentIdentifier}
          onChange={(value) => update("paymentIdentifier", value)}
          placeholder="aarav@transactx"
          autoComplete="username"
          hint="Your unique address on TransactX."
          required
        />
        <Field
          className="form-grid-wide"
          label="Password"
          value={form.password}
          onChange={(value) => update("password", value)}
          type="password"
          autoComplete="new-password"
          minLength={8}
          hint="Must be at least 8 characters."
          required
        />
      </div>
      {error && <InlineError message={error} />}
      <Button
        type="submit"
        disabled={
          busy ||
          !form.name.trim() ||
          !form.phone.trim() ||
          !form.paymentIdentifier.trim() ||
          form.password.length < 8
        }
      >
        {busy ? "Creating account…" : "Create account"}
        <Icon name="arrow" />
      </Button>
    </form>
  );
}

// ---------------------------------------------------------------------------
// Customer Shell
// ---------------------------------------------------------------------------

function CustomerShell({ token, onLogout }: { token: string; onLogout: () => void }) {
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
    const states: OfflineIntent["state"][] = ["QUEUED", "SYNCING", "SYNCED", "RETRYABLE", "FAILED"];
    const records = (await Promise.all(states.map((state) => scopedQueue.listByState(state)))).flat();
    setQueueIntents(records.sort((left, right) => right.updatedAt.localeCompare(left.updatedAt)));
  }

  useEffect(() => {
    if (!queue) return;
    let active = true;
    const worker = new OfflineReplayWorker(queue, {
      createPayment: async (payload, idempotencyKey, clientRequestId) => {
        const response = await api.createPaymentWithStatus(payload, token, idempotencyKey, clientRequestId);
        return { payment: response.data, status: response.status };
      },
      getPayment: (paymentID) => api.payment(paymentID, token),
    });
    const replay = async () => {
      await worker.run();
      if (active) {
        await refreshQueue(queue);
        api.payments(token).then(setPayments).catch(() => undefined);
      }
    };
    const handleOnline = () => {
      setOnline(true);
      void replay();
    };
    const handleOffline = () => setOnline(false);
    window.addEventListener("online", handleOnline);
    window.addEventListener("offline", handleOffline);
    void replay();
    return () => {
      active = false;
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
    const path = paymentID ? `/transactions/${paymentID}` : nextView === "home" ? "/" : `/${nextView}`;
    window.history.pushState({}, "", path);
    setView(nextView);
    setSelectedPaymentID(paymentID ?? null);
    window.scrollTo({ top: 0, behavior: "smooth" });
  }

  function refreshPayments() {
    api.payments(token).then(setPayments).catch(() => undefined);
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
        const response = await api.createPaymentWithStatus(payload, token, idempotencyKey, requestId);
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
    <div className="product-shell">
      <Sidebar view={view} user={user} onNavigate={navigate} onLogout={onLogout} />
      <main className="main-content">
        <MobileHeader user={user} onLogout={onLogout} />
        <div className="content-wrap">
          <NetworkStatus online={online} />
          {view === "home" && (
            <HomeView user={user} account={account} payments={payments} onNavigate={navigate} />
          )}
          {view === "pay" && queue && (
            <PayView
              account={account}
              token={token}
              queue={queue}
              onComplete={completedPayment}
              onQueueChanged={queueChanged}
              onNavigate={navigate}
            />
          )}
          {view === "transactions" && (
            <TransactionsView payments={payments} onNavigate={navigate} />
          )}
          {view === "details" && selectedPaymentID && (
            <DetailsView paymentID={selectedPaymentID} token={token} onNavigate={navigate} />
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
        <NavItem view="home" active={view === "home"} label="Overview" icon="home" onClick={onNavigate} />
        <NavItem view="pay" active={view === "pay"} label="Pay" icon="send" onClick={onNavigate} />
        <NavItem
          view="transactions"
          active={view === "transactions" || view === "details"}
          label="Transactions"
          icon="activity"
          onClick={onNavigate}
        />
      </nav>
      <div className="sidebar-bottom">
        <div className="user-chip">
          <Avatar name={user?.name ?? "Customer"} />
          <span>
            <strong>{user?.name ?? "Customer"}</strong>
            <small>{user?.paymentIdentifier ?? ""}</small>
          </span>
        </div>
        <button type="button" className="logout-button" onClick={onLogout} aria-label="Log out of TransactX">
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

function MobileHeader({ user, onLogout }: { user: User | null; onLogout: () => void }) {
  return (
    <header className="mobile-header">
      <div className="mobile-brand">
        <BrandMark />
        <span>TransactX</span>
        <span className="customer-tag">Personal</span>
      </div>
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

function MobileNav({ view, onNavigate }: { view: View; onNavigate: (view: View) => void }) {
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
        className={`mobile-nav-item ${view === "transactions" || view === "details" ? "is-active" : ""}`}
        onClick={() => onNavigate("transactions")}
        aria-current={view === "transactions" || view === "details" ? "page" : undefined}
      >
        <Icon name="activity" size={18} />
        <span>Activity</span>
      </button>
    </nav>
  );
}

function HomeView({
  user,
  account,
  payments,
  onNavigate,
}: {
  user: User | null;
  account?: Account;
  payments: Payment[];
  onNavigate: (view: View, paymentID?: string) => void;
}) {
  const activeStatus = account?.status === "ACTIVE";
  return (
    <>
      <PageHeader
        eyebrow="Personal account"
        title={`Good day, ${firstName(user?.name)}`}
        description="Your TransactX balance and recent activity."
        action={
          <Button onClick={() => onNavigate("pay")}>
            <Icon name="send" />
            Send money
          </Button>
        }
      />
      <section className="overview-grid">
        <div className="balance-panel">
          <div>
            <div className="section-kicker">Available balance</div>
            <Amount paise={account?.balancePaise ?? 0} prominent />
          </div>
          <div className="balance-foot">
            <span title="Account number">{account?.accountNumber ?? "Account unavailable"}</span>
            <span className="account-status">
              <span className={`status-dot ${activeStatus ? "status-success-dot" : "status-neutral-dot"}`} />
              {activeStatus ? "Active account" : account?.status ?? "Unavailable"}
            </span>
          </div>
        </div>
        <div className="account-note">
          <span className="note-index">01</span>
          <div>
            <strong>Ready when you are</strong>
            <p>
              Send money directly to any TransactX payment ID. You review the verified recipient, amount, and
              optional note before anything is submitted.
            </p>
            <button type="button" className="text-link" onClick={() => onNavigate("pay")}>
              Start a payment <Icon name="arrow" size={15} />
            </button>
          </div>
        </div>
      </section>
      <section className="section-block">
        <div className="section-heading">
          <div>
            <p className="eyebrow">Activity</p>
            <h2>Recent transactions</h2>
          </div>
          {payments.length > 0 && (
            <button type="button" className="text-link" onClick={() => onNavigate("transactions")}>
              View all ({payments.length}) <Icon name="arrow" size={15} />
            </button>
          )}
        </div>
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
          <div className="payment-list">
            {payments.slice(0, 5).map((payment) => (
              <PaymentRow
                key={payment.id}
                payment={payment}
                onClick={() => onNavigate("details", payment.id)}
              />
            ))}
          </div>
        )}
      </section>
      <section className="account-summary">
        <div>
          <p className="eyebrow">Account details</p>
          <h2>Your TransactX identity</h2>
        </div>
        <div className="summary-detail">
          <span>Payment ID</span>
          <strong>{user?.paymentIdentifier ?? "—"}</strong>
        </div>
        <div className="summary-detail">
          <span>Account number</span>
          <strong>{account?.accountNumber ?? "—"}</strong>
        </div>
      </section>
    </>
  );
}

function PayView({
  account,
  token,
  queue,
  onComplete,
  onQueueChanged,
  onNavigate,
}: {
  account?: Account;
  token: string;
  queue: OfflineIntentQueue;
  onComplete: (payment: Payment) => void;
  onQueueChanged: () => void;
  onNavigate: (view: View) => void;
}) {
  const [stage, setStage] = useState<"form" | "confirm" | "processing" | "result" | "uncertain" | "queued">("form");
  const [recipientInput, setRecipientInput] = useState("");
  const [recipient, setRecipient] = useState<Recipient | null>(null);
  const [amount, setAmount] = useState("");
  const [note, setNote] = useState("");
  const [lookupBusy, setLookupBusy] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [payment, setPayment] = useState<Payment | null>(null);
  const [idempotencyKey, setIdempotencyKey] = useState<string | null>(null);
  const [clientRequestId, setClientRequestId] = useState<string | null>(null);
  const submitLock = useRef(false);

  const amountPaise = parsePaise(amount);
  const availableBalance = account?.balancePaise ?? 0;
  const isOverBalance = amountPaise !== null && amountPaise > availableBalance;

  async function resolve(event?: FormEvent) {
    event?.preventDefault();
    if (!recipientInput.trim()) return;
    setLookupBusy(true);
    setError("");
    setRecipient(null);
    setIdempotencyKey(null);
    setClientRequestId(null);
    try {
      setRecipient(await api.resolveRecipient(recipientInput.trim(), token));
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "Payment ID not found");
    } finally {
      setLookupBusy(false);
    }
  }

  function continueToConfirm(event: FormEvent) {
    event.preventDefault();
    if (!recipient) {
      void resolve();
      return;
    }
    if (amountPaise === null) {
      setError("Enter a valid amount with up to two decimal places.");
      return;
    }
    if (amountPaise > availableBalance) {
      setError("Your balance is too low for this payment.");
      return;
    }
    if (note.trim().length > 280) {
      setError("Keep your note under 280 characters.");
      return;
    }
    setError("");
    setIdempotencyKey(crypto.randomUUID());
    setClientRequestId(crypto.randomUUID());
    setStage("confirm");
  }

  async function submitPayment() {
    if (!recipient || amountPaise === null || submitLock.current) return;
    submitLock.current = true;
    const key = idempotencyKey ?? crypto.randomUUID();
    const requestID = clientRequestId ?? crypto.randomUUID();
    const payload = {
      recipient: recipient.paymentIdentifier,
      amountPaise,
      currency: "INR",
      note: note.trim() || undefined,
    };
    setIdempotencyKey(key);
    setClientRequestId(requestID);
    setStage("processing");
    setBusy(true);
    setError("");
    try {
      const result = await api.createPayment(payload, token, key, requestID);
      setPayment(result);
      setStage("result");
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 0) {
        try {
          await queue.enqueue(payload, { clientRequestId: requestID, idempotencyKey: key });
          onQueueChanged();
          setStage("queued");
        } catch {
          setError("We couldn't save this payment on your device. Please try again.");
          setStage("form");
        }
      } else {
        setError(paymentError(caught));
        setIdempotencyKey(null);
        setClientRequestId(null);
        setStage("form");
      }
    } finally {
      submitLock.current = false;
      setBusy(false);
    }
  }

  function editPayment() {
    setIdempotencyKey(null);
    setClientRequestId(null);
    setStage("form");
  }

  if (stage === "confirm") {
    return (
      <ConfirmPayment
        recipient={recipient!}
        amount={amountPaise!}
        note={note}
        onBack={editPayment}
        onConfirm={submitPayment}
        busy={busy}
      />
    );
  }

  if (stage === "processing") {
    return <ProcessingPayment recipient={recipient!} amount={amountPaise!} />;
  }

  if (stage === "queued") {
    return <QueuedPayment recipient={recipient!} amount={amountPaise!} onDone={() => onNavigate("home")} />;
  }

  if (stage === "uncertain") {
    return <UncertainPayment onRetry={submitPayment} onEdit={editPayment} />;
  }

  if (stage === "result" && payment) {
    return (
      <PaymentResult
        payment={payment}
        token={token}
        onPayment={setPayment}
        onView={() => onComplete(payment)}
        onDone={() => onNavigate("home")}
      />
    );
  }

  return (
    <>
      <PageHeader
        eyebrow="New transfer"
        title="Send money"
        description="Verify recipient identity and enter amount before confirming."
      />
      <form className="pay-form" onSubmit={continueToConfirm}>
        <div className="form-section">
          <div className="form-section-number">01</div>
          <div className="form-section-body">
            <label className="field field-large">
              <span>Recipient payment ID</span>
              <div className="input-with-action">
                <input
                  value={recipientInput}
                  onChange={(event) => {
                    setRecipientInput(event.target.value);
                    setRecipient(null);
                    setError("");
                    setIdempotencyKey(null);
                  }}
                  onBlur={() => {
                    if (recipientInput && !recipient) void resolve();
                  }}
                  placeholder="name@transactx"
                  autoComplete="off"
                  aria-label="Recipient TransactX payment ID"
                />
                <button
                  type="button"
                  className="input-action"
                  onClick={() => void resolve()}
                  disabled={lookupBusy || !recipientInput.trim()}
                >
                  {lookupBusy ? "Checking…" : "Find"}
                </button>
              </div>
              <small className="field-hint">Enter the recipient's unique TransactX payment ID.</small>
            </label>
            {recipient && (
              <div className="recipient-confirmed">
                <Avatar name={recipient.name} />
                <span>
                  <strong>{recipient.name}</strong>
                  <small>{recipient.paymentIdentifier}</small>
                </span>
                <span className="badge badge-success">
                  <Icon name="check" size={14} /> Verified
                </span>
              </div>
            )}
          </div>
        </div>

        <div className="form-section">
          <div className="form-section-number">02</div>
          <div className="form-section-body">
            <label className="field field-large">
              <span>Amount</span>
              <div className={`amount-input ${isOverBalance ? "amount-input-error" : ""}`.trim()}>
                <span>₹</span>
                <input
                  value={amount}
                  onChange={(event) => {
                    setAmount(event.target.value);
                    setError("");
                    setIdempotencyKey(null);
                  }}
                  inputMode="decimal"
                  placeholder="0.00"
                  aria-label="Amount in Indian rupees"
                />
              </div>
              <small className={`field-hint ${isOverBalance ? "field-hint-error" : ""}`}>
                {isOverBalance ? (
                  `Amount exceeds your available balance of `
                ) : (
                  `Available to send: `
                )}
                <Amount paise={availableBalance} />
              </small>
            </label>
          </div>
        </div>

        <div className="form-section">
          <div className="form-section-number">03</div>
          <div className="form-section-body">
            <label className="field field-large">
              <span>
                Note <small className="optional-label">Optional</small>
              </span>
              <input
                value={note}
                maxLength={280}
                onChange={(event) => {
                  setNote(event.target.value);
                  setIdempotencyKey(null);
                }}
                placeholder="What is this payment for?"
                aria-label="Optional note"
              />
              <small className="field-hint">
                {note.length}/280 characters · Included in transaction details.
              </small>
            </label>
          </div>
        </div>

        {error && <InlineError message={error} />}

        <div className="pay-actions">
          <Button type="button" variant="quiet" onClick={() => onNavigate("home")}>
            Cancel
          </Button>
          <Button
            type="submit"
            disabled={!recipient || amountPaise === null || isOverBalance}
          >
            Review payment <Icon name="arrow" />
          </Button>
        </div>
      </form>
    </>
  );
}

function ConfirmPayment({
  recipient,
  amount,
  note,
  onBack,
  onConfirm,
  busy,
}: {
  recipient: Recipient;
  amount: number;
  note: string;
  onBack: () => void;
  onConfirm: () => void;
  busy: boolean;
}) {
  return (
    <div className="focused-state">
      <button type="button" className="back-link" onClick={onBack}>
        <Icon name="arrow" size={15} />
        Back to payment
      </button>
      <div className="confirm-heading">
        <p className="eyebrow">Review transfer</p>
        <h1>Confirm payment details</h1>
        <p>Ensure the recipient and amount are correct before proceeding.</p>
      </div>
      <div className="confirm-sheet">
        <div className="confirm-recipient">
          <Avatar name={recipient.name} />
          <span>
            <small>Sending to</small>
            <strong>{recipient.name}</strong>
            <span>{recipient.paymentIdentifier}</span>
          </span>
        </div>
        <div className="confirm-amount">
          <small>Total amount</small>
          <Amount paise={amount} prominent />
        </div>
        {note.trim() && (
          <div className="confirm-message">
            <small>Note</small>
            <strong>{note.trim()}</strong>
          </div>
        )}
        <div className="confirm-note">
          <span>
            <Icon name="check" size={15} />
            Verified recipient
          </span>
          <span>
            <Icon name="check" size={15} />
            INR settlement
          </span>
          <span>
            <Icon name="check" size={15} />
            Zero network fee
          </span>
        </div>
      </div>
      <div className="confirm-actions">
        <Button variant="secondary" onClick={onBack}>
          Edit payment
        </Button>
        <Button onClick={onConfirm} disabled={busy}>
          {busy ? "Confirming…" : "Confirm payment"}
          <Icon name="arrow" />
        </Button>
      </div>
    </div>
  );
}

function QueuedPayment({
  recipient,
  amount,
  onDone,
}: {
  recipient: Recipient;
  amount: number;
  onDone: () => void;
}) {
  return (
    <div className="focused-state centered-state">
      <div className="result-mark result-pending">
        <Icon name="activity" size={24} />
      </div>
      <p className="eyebrow">Stored locally</p>
      <h1>Payment queued for replay</h1>
      <p className="state-description">
        We could not reach the TransactX network. Your payment intent has been safely saved on your device and will be
        automatically submitted once connectivity is restored.
      </p>
      <div className="mini-payment">
        <Avatar name={recipient.name} />
        <span>
          <strong>{recipient.name}</strong>
          <small>
            <Amount paise={amount} />
          </small>
        </span>
      </div>
      <div className="confirm-actions">
        <Button onClick={onDone}>Back to overview</Button>
      </div>
    </div>
  );
}

function ProcessingPayment({ recipient, amount }: { recipient: Recipient; amount: number }) {
  return (
    <div className="focused-state centered-state">
      <div className="processing-mark">
        <span />
        <span />
        <span />
      </div>
      <p className="eyebrow">Payment network</p>
      <h1>Confirming your payment</h1>
      <p className="state-description">
        We're routing your transfer through the payment rails. Please keep this window open for a moment.
      </p>
      <div className="mini-payment">
        <Avatar name={recipient.name} />
        <span>
          <strong>{recipient.name}</strong>
          <small>
            <Amount paise={amount} />
          </small>
        </span>
      </div>
    </div>
  );
}

function UncertainPayment({ onRetry, onEdit }: { onRetry: () => void; onEdit: () => void }) {
  return (
    <div className="focused-state centered-state">
      <div className="result-mark result-pending">
        <Icon name="activity" size={24} />
      </div>
      <p className="eyebrow">Payment status</p>
      <h1>We couldn't confirm the response</h1>
      <p className="state-description">
        Your transfer may still be processing on the ledger. We preserved your payment request ID and idempotency key
        so you can safely check status or retry without duplicate debit.
      </p>
      <div className="confirm-actions">
        <Button variant="secondary" onClick={onEdit}>
          Edit payment
        </Button>
        <Button onClick={onRetry}>Try again safely</Button>
      </div>
    </div>
  );
}

function PaymentResult({
  payment,
  token,
  onPayment,
  onView,
  onDone,
}: {
  payment: Payment;
  token: string;
  onPayment: (payment: Payment) => void;
  onView: () => void;
  onDone: () => void;
}) {
  const copy = paymentResultCopy(payment);
  const [refreshing, setRefreshing] = useState(false);
  const counterparty = payment.direction === "RECEIVED" ? payment.senderName : payment.receiverName;

  async function refresh() {
    setRefreshing(true);
    try {
      onPayment(await api.payment(payment.id, token));
    } finally {
      setRefreshing(false);
    }
  }

  const isPending =
    payment.state !== "COMPLETED" && payment.state !== "FAILED" && payment.state !== "REVERSED";

  return (
    <div className="focused-state centered-state">
      <div className={`result-mark result-${copy.tone}`}>
        <Icon
          name={copy.tone === "success" ? "check" : copy.tone === "error" ? "close" : "activity"}
          size={24}
        />
      </div>
      <p className="eyebrow">Payment update</p>
      <h1>{copy.title}</h1>
      <p className="state-description">{copy.description}</p>
      <Amount paise={payment.amountPaise} prominent />
      <div className="result-recipient">
        {payment.direction === "RECEIVED" ? "From" : "To"} <strong>{counterparty}</strong>
        <span>
          {payment.direction === "RECEIVED"
            ? payment.senderPaymentIdentifier
            : payment.receiverPaymentIdentifier}
        </span>
      </div>
      {payment.note && <p className="result-note">"{payment.note}"</p>}
      <div className="confirm-actions">
        {isPending && (
          <Button variant="secondary" onClick={() => void refresh()} disabled={refreshing}>
            {refreshing ? "Checking…" : "Check status"}
          </Button>
        )}
        <Button variant="secondary" onClick={onDone}>
          Back to overview
        </Button>
        <Button onClick={onView}>
          View transaction <Icon name="arrow" />
        </Button>
      </div>
    </div>
  );
}

function TransactionsView({
  payments,
  onNavigate,
}: {
  payments: Payment[];
  onNavigate: (view: View, paymentID?: string) => void;
}) {
  const [filter, setFilter] = useState<"ALL" | "SENT" | "RECEIVED">("ALL");
  const [search, setSearch] = useState("");

  const sentCount = payments.filter((p) => p.direction === "SENT").length;
  const receivedCount = payments.filter((p) => p.direction === "RECEIVED").length;

  const filtered = payments.filter((p) => {
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

  return (
    <>
      <PageHeader
        eyebrow="Activity"
        title="Transactions"
        description="A complete, auditable ledger of all your transfers."
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
          <div className="transactions-toolbar">
            <span>
              Showing {filtered.length} of {payments.length} {payments.length === 1 ? "transaction" : "transactions"}
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
              <Button
                variant="secondary"
                onClick={() => {
                  setFilter("ALL");
                  setSearch("");
                }}
              >
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

function DetailsView({
  paymentID,
  token,
  onNavigate,
}: {
  paymentID: string;
  token: string;
  onNavigate: (view: View) => void;
}) {
  const [payment, setPayment] = useState<Payment | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    setLoading(true);
    api
      .payment(paymentID, token)
      .then(setPayment)
      .catch((caught) => setError(caught instanceof Error ? caught.message : "Transaction not found"))
      .finally(() => setLoading(false));
  }, [paymentID, token]);

  if (loading) return <DetailsSkeleton />;
  if (error || !payment) {
    return (
      <div className="center-state">
        <InlineError message={error || "Transaction not found"} />
        <Button variant="secondary" onClick={() => onNavigate("transactions")}>
          Back to transactions
        </Button>
      </div>
    );
  }

  const copy = paymentResultCopy(payment);
  const isReceived = payment.direction === "RECEIVED";
  const recipientLabel = isReceived ? "Sender" : "Recipient";
  const counterparty = isReceived ? payment.senderName : payment.receiverName;
  const identifier = isReceived ? payment.senderPaymentIdentifier : payment.receiverPaymentIdentifier;
  const sign = isReceived ? "+ " : "− ";

  return (
    <div className="details-page">
      <button type="button" className="back-link" onClick={() => onNavigate("transactions")}>
        <Icon name="arrow" size={15} />
        Back to transactions
      </button>
      <div className="details-heading">
        <div>
          <p className="eyebrow">Audit record</p>
          <h1>Payment details</h1>
        </div>
        <StatusBadge state={payment.state} />
      </div>
      <section className="details-hero">
        <div>
          <span className="detail-label">Amount</span>
          <Amount paise={payment.amountPaise} sign={sign} prominent />
          <p className="detail-state">{copy.description}</p>
        </div>
        <div className="details-recipient">
          <Avatar name={counterparty} />
          <span>
            <small>{recipientLabel}</small>
            <strong>{counterparty}</strong>
            <span>{identifier}</span>
          </span>
        </div>
      </section>
      <section className="record-section">
        <p className="eyebrow">Record</p>
        <div className="record-list">
          <Record label="Status">
            <StatusBadge state={payment.state} />
          </Record>
          <Record label="Direction" value={isReceived ? "Received (+)" : "Sent (−)"} />
          <Record label="Initiated" value={formatDate(payment.createdAt)} />
          <Record
            label="Settled"
            value={payment.completedAt ? formatDate(payment.completedAt) : "Pending network settlement"}
          />
          {payment.durationMs !== undefined && <Record label="Duration" value={`${payment.durationMs} ms`} />}
          {payment.sourceBankName && (
            <Record
              label="Source bank"
              value={`${payment.sourceBankName}${payment.sourceBankCode ? ` (${payment.sourceBankCode})` : ""}`}
            />
          )}
          {payment.destinationBankName && (
            <Record
              label="Destination bank"
              value={`${payment.destinationBankName}${payment.destinationBankCode ? ` (${payment.destinationBankCode})` : ""}`}
            />
          )}
          {payment.note && <Record label="Note" value={payment.note} />}
          <Record label="Network origin" value={payment.origin} />
          <Record label="Payment reference" value={payment.id} mono />
        </div>
      </section>
      {payment.failureReason && (
        <div className="detail-alert" role="alert">
          <Icon name="alert" size={17} />
          <span>{payment.failureReason}</span>
        </div>
      )}
    </div>
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

function OfflineQueuePanel({
  intents,
  onRetry,
}: {
  intents: OfflineIntent[];
  onRetry: (clientRequestId: string) => Promise<void>;
}) {
  if (intents.length === 0) return null;
  return (
    <section className="offline-queue-panel">
      <div className="section-heading">
        <div>
          <p className="eyebrow">Local sync</p>
          <h2>Offline payment intents</h2>
        </div>
        <span className="toolbar-note">{intents.length} retained on device</span>
      </div>
      <div className="offline-intent-list">
        {intents.map((intent) => (
          <div className="offline-intent-row" key={intent.clientRequestId}>
            <div>
              <strong>{intent.payload.recipient}</strong>
              <small>
                <Amount paise={intent.payload.amountPaise} /> · {formatDate(intent.createdAt)}
              </small>
            </div>
            <div className="offline-intent-status">
              <StatusBadge
                state={intent.replay?.resolution === "PENDING" ? "PENDING" : intent.state}
              />
              {intent.state === "FAILED" && (
                <Button variant="secondary" onClick={() => void onRetry(intent.clientRequestId)}>
                  Retry safely
                </Button>
              )}
            </div>
          </div>
        ))}
      </div>
    </section>
  );
}

function Record({
  label,
  value,
  mono,
  children,
}: {
  label: string;
  value?: string;
  mono?: boolean;
  children?: React.ReactNode;
}) {
  return (
    <div className="record-row">
      <span>{label}</span>
      {children ?? <strong className={mono ? "mono" : ""}>{value}</strong>}
    </div>
  );
}

function LoadingShell() {
  return (
    <div className="product-shell">
      <aside className="sidebar">
        <div className="brand-lockup">
          <BrandMark />
          <span>TransactX</span>
          <span className="customer-tag">Personal</span>
        </div>
      </aside>
      <main className="main-content">
        <div className="content-wrap">
          <Skeleton className="skeleton-heading" />
          <div className="skeleton-block" />
          <div className="skeleton-block skeleton-short" />
        </div>
      </main>
    </div>
  );
}

function DetailsSkeleton() {
  return (
    <div className="details-page">
      <Skeleton className="skeleton-heading" />
      <div className="skeleton-block" />
      <div className="skeleton-block skeleton-short" />
    </div>
  );
}

// ---------------------------------------------------------------------------
// Merchant Shell
// ---------------------------------------------------------------------------

function MerchantShell({ token, user, onLogout }: { token: string; user: User; onLogout: () => void }) {
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
        <div className="user-chip">
          <Avatar name={user.name} />
          <span><strong>{user.name}</strong><small>{user.paymentIdentifier}</small></span>
        </div>
        <button className="logout-button" onClick={onLogout}>Log out</button>
      </div>
    </aside>
  );
}

function MerchantNavItem({ view, active, label, icon, onClick }: { view: MerchantView; active: boolean; label: string; icon: "home" | "qr" | "activity" | "check" | "search"; onClick: (v: MerchantView) => void }) {
  return (
    <button className={`nav-item ${active ? "is-active" : ""}`} onClick={() => onClick(view)}>
      <MerchantIcon name={icon} />
      <span>{label}</span>
    </button>
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
      <div className="mobile-brand"><BrandMark /><span>TransactX</span><span className="merchant-badge-mobile">Merchant</span></div>
      <button className="mobile-user" onClick={onLogout} aria-label={`Log out ${user.name}`}><Avatar name={user.name} /></button>
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
      <PageHeader eyebrow="Merchant" title={`Welcome, ${firstName(user.name)}`} description="Your merchant account at a glance." action={<Button onClick={() => onNavigate("m-receive")}><MerchantIcon name="qr" size={16} />Receive payment</Button>} />
      <section className="overview-grid">
        <div className="balance-panel">
          <div className="section-kicker">Available balance</div>
          <Amount paise={account?.balancePaise ?? 0} prominent />
          <div className="balance-foot">
            <span>{account?.accountNumber ?? "Account unavailable"}</span>
            <span className="account-status"><span className="status-dot" />{account?.status === "ACTIVE" ? "Active" : account?.status ?? "Unavailable"}</span>
          </div>
        </div>
        <div className="account-note merchant-stat-panel">
          <span className="note-index">01</span>
          <div>
            <strong>Today's activity</strong>
            {completedToday.length === 0
              ? <p>No completed payments received today.</p>
              : <p><strong style={{ color: "var(--accent)", fontSize: "1.4rem" }}>{completedToday.length}</strong> payment{completedToday.length !== 1 ? "s" : ""} completed today.</p>
            }
            <button className="text-link" onClick={() => onNavigate("m-incoming")}>View incoming <Icon name="arrow" size={15} /></button>
          </div>
        </div>
      </section>

      <section className="section-block">
        <div className="section-heading">
          <div><p className="eyebrow">Incoming</p><h2>Recent payments received</h2></div>
          <div style={{ display: "flex", gap: "0.5rem" }}>
            <button className="text-link" onClick={onRefresh}>Refresh</button>
            {received.length > 0 && <button className="text-link" onClick={() => onNavigate("m-incoming")}>View all <Icon name="arrow" size={15} /></button>}
          </div>
        </div>
        {received.length === 0
          ? <EmptyState title="No payments received yet" description="Payments sent to your merchant account will appear here." action={<Button variant="secondary" onClick={() => onNavigate("m-receive")}>Show receive QR</Button>} />
          : <div className="payment-list">{received.slice(0, 5).map((p) => <MerchantPaymentRow key={p.id} payment={p} />)}</div>
        }
      </section>

      <section className="account-summary">
        <div><p className="eyebrow">Account details</p><h2>Your merchant identity</h2></div>
        <div className="summary-detail"><span>Payment ID</span><strong>{user.paymentIdentifier}</strong></div>
        <div className="summary-detail"><span>Account number</span><strong>{account?.accountNumber ?? "—"}</strong></div>
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
  // qrSvg is populated async once `info` is available.
  const [qrSvg, setQrSvg] = useState("");

  useEffect(() => {
    let active = true;
    api.merchantReceiveInfo(token)
      .then((data) => {
        if (!active) return;
        setInfo(data);
        // Encode the real QR code immediately after info is available.
        return encodeQRSvg(data.paymentIdentifier).then((svg) => {
          if (active) setQrSvg(svg);
        });
      })
      .catch((caught) => { if (active) { setError(caught instanceof Error ? caught.message : "Could not load receive information."); } })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [token]);

  if (loading) return <DetailsSkeleton />;
  if (error || !info) return (
    <div className="center-state">
      <InlineError message={error || "Receive information unavailable."} />
    </div>
  );

  if (info.accountStatus !== "ACTIVE") return (
    <div className="center-state">
      <div className="result-mark result-pending"><Icon name="alert" size={24} /></div>
      <h1 style={{ fontSize: "1.6rem" }}>Account inactive</h1>
      <p style={{ color: "var(--muted)" }}>Your account is not currently active. Contact support for assistance.</p>
    </div>
  );

  return (
    <>
      <PageHeader eyebrow="Receive" title="Accept payments" description="Share your QR code or payment ID with customers." />
      <div className="merchant-receive-layout">
        <div className="merchant-qr-card">
          <div className="merchant-qr-label">
            <p className="eyebrow">Scan to pay</p>
            <p style={{ color: "var(--muted)", fontSize: "0.82rem", marginBottom: "1.5rem" }}>
              Ask your customer to scan this code with their TransactX app.
            </p>
          </div>
          {qrSvg
            ? <div className="merchant-qr-frame" aria-label={`QR code for payment to ${info.paymentIdentifier}`}
                dangerouslySetInnerHTML={{ __html: qrSvg }} />
            : <div className="merchant-qr-frame" style={{ width: 168, height: 168, display: "flex", alignItems: "center", justifyContent: "center", color: "var(--muted)", fontSize: "0.78rem" }}>Generating…</div>
          }
          <div className="merchant-qr-id">
            <span className="eyebrow">Payment ID</span>
            <strong className="merchant-pid">{info.paymentIdentifier}</strong>
          </div>
          <button className="text-link" style={{ justifySelf: "center", marginTop: "0.5rem" }}
            onClick={() => void navigator.clipboard?.writeText(info.paymentIdentifier)}>
            Copy payment ID <Icon name="arrow" size={15} />
          </button>
        </div>
        <div className="merchant-receive-note">
          <span className="note-index">!</span>
          <div>
            <strong>How receiving works</strong>
            <p>When a customer pays your payment ID, the funds are transferred via the TransactX network. Settlement is server-authoritative — the outcome will appear in your incoming payments feed once confirmed.</p>
            <p style={{ marginTop: "0.75rem" }}>Payment ID: <strong style={{ fontFamily: "DM Mono, monospace", fontSize: "0.78rem" }}>{info.paymentIdentifier}</strong></p>
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
      <PageHeader eyebrow="Incoming" title="Payments received" description="All payments sent to your merchant account." action={<Button variant="secondary" onClick={onRefresh}><Icon name="activity" size={16} />Refresh</Button>} />
      {received.length === 0
        ? <EmptyState title="No incoming payments" description="Payments received from customers will appear here once they are processed." />
        : (
          <section className="transactions-section">
            <div className="transactions-toolbar">
              <span>{received.length} {received.length === 1 ? "payment" : "payments"} received</span>
              <span className="toolbar-note">Most recent first</span>
            </div>
            <div className="payment-list payment-list-large">
              {received.map((p) => <MerchantPaymentRow key={p.id} payment={p} expanded />)}
            </div>
          </section>
        )
      }
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
      <PageHeader eyebrow="Settlement" title="Settlement status" description="Track the settlement state of payments received." action={<Button variant="secondary" onClick={onRefresh}><Icon name="activity" size={16} />Refresh</Button>} />

      {received.length === 0
        ? <EmptyState title="No incoming payments" description="Settlement status will appear here once payments are received." />
        : (
          <div className="merchant-settlement-layout">
            <SettlementGroup title="Completed" count={completed.length} tone="success" payments={completed} description="Fully settled and confirmed." />
            <SettlementGroup title="Pending confirmation" count={pending.length} tone="warning" payments={pending} description="Payment is being processed. Outcome is not yet confirmed — do not consider this settled." />
            <SettlementGroup title="Failed / Reversed" count={failed.length} tone="error" payments={failed} description="Payment did not complete. No funds were transferred." />
            {other.length > 0 && <SettlementGroup title="Other" count={other.length} tone="neutral" payments={other} description="Status is currently unknown." />}
          </div>
        )
      }
    </>
  );
}

function SettlementGroup({ title, count, tone, payments, description }: { title: string; count: number; tone: "success" | "warning" | "error" | "neutral"; payments: Payment[]; description: string }) {
  const [open, setOpen] = useState(tone === "success" || tone === "warning");

  return (
    <div className={`settlement-group settlement-group-${tone}`}>
      <button className="settlement-group-header" onClick={() => setOpen((o) => !o)} aria-expanded={open}>
        <span className="settlement-group-title">
          <span className={`status-dot status-${tone}-dot`} />
          {title}
          <span className="settlement-count">{count}</span>
        </span>
        <span style={{ color: "var(--muted)", fontSize: "0.72rem" }}>{description}</span>
        <Icon name="chevron" size={16} />
      </button>
      {open && (
        <div className="settlement-group-body">
          {payments.length === 0
            ? <p className="settlement-empty">No payments in this category.</p>
            : payments.map((p) => <MerchantPaymentRow key={p.id} payment={p} expanded />)
          }
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

  return (
    <>
      <PageHeader eyebrow="Search" title="Transaction search" description="Search your incoming payment history. Results are bounded to recent payments." />
      <div className="merchant-search-toolbar">
        <label className="field merchant-search-field">
          <span>Search by sender or note</span>
          <input
            id="merchant-search-input"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Name, payment ID, or note…"
            autoComplete="off"
          />
        </label>
        <label className="field merchant-filter-field">
          <span>Filter by status</span>
          <select id="merchant-state-filter" value={stateFilter} onChange={(e) => setStateFilter(e.target.value)} className="merchant-select">
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
      {filtered.length === 0
        ? <EmptyState title="No results" description={query.trim() || stateFilter !== "all" ? "Try a different search term or status filter." : "No incoming payments found."} />
        : (
          <section className="transactions-section">
            <div className="transactions-toolbar">
              <span>{filtered.length} {filtered.length === 1 ? "result" : "results"}</span>
              <span className="toolbar-note">{received.length} total incoming</span>
            </div>
            <div className="payment-list payment-list-large">
              {filtered.map((p) => <MerchantPaymentRow key={p.id} payment={p} expanded />)}
            </div>
          </section>
        )
      }
    </>
  );
}

// ---------------------------------------------------------------------------
// Shared Merchant payment row (safe — no internal IDs)
// ---------------------------------------------------------------------------

function MerchantPaymentRow({ payment, expanded = false }: { payment: Payment; expanded?: boolean }) {
  const stateText = payment.state === "COMPLETED" ? "Completed"
    : payment.state === "FAILED" ? "Not completed"
    : payment.state === "REVERSED" ? "Reversed"
    : ["PROCESSING", "PENDING_RECONCILIATION", "BANK_SETTLED_CENTRAL_PENDING"].includes(payment.state) ? "Confirming…"
    : payment.state.replace(/_/g, " ").toLowerCase();

  return (
    <div className="merchant-payment-row">
      <Avatar name={payment.senderName} />
      <span className="payment-main">
        <strong>{payment.senderName}</strong>
        <small>{payment.senderPaymentIdentifier} · {formatDate(payment.createdAt)}</small>
        {expanded && payment.note && <small className="merchant-payment-note">"{payment.note}"</small>}
      </span>
      <span className="payment-amount">
        <Amount paise={payment.amountPaise} sign="+ " />
        <small><StatusBadge state={payment.state} /></small>
      </span>
      {expanded && (
        <div className="merchant-payment-expanded">
          <span className="merchant-state-pill" data-state={payment.state}>{stateText}</span>
          {payment.completedAt && <small className="merchant-completed-at">Settled {formatDate(payment.completedAt)}</small>}
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

function firstName(name?: string) { return name?.trim().split(" ")[0] || "there"; }
function readView(): View { const path = window.location.pathname; if (path.startsWith("/pay")) return "pay"; if (path.startsWith("/transactions/")) return "details"; if (path.startsWith("/transactions")) return "transactions"; return "home"; }
function readPaymentID() { const match = window.location.pathname.match(/^\/transactions\/([^/]+)/); return match?.[1] ?? null; }
function paymentError(caught: unknown) { if (!(caught instanceof ApiError)) return "We couldn't complete the payment. Please try again."; if (caught.code === "INSUFFICIENT_FUNDS") return "Your balance is too low for this payment."; if (caught.code === "RECIPIENT_NOT_FOUND") return "That payment ID could not be found."; if (caught.code === "BANK_UNAVAILABLE") return "Payments are temporarily unavailable. Try again shortly."; return "We couldn't complete the payment. Please try again."; }
function isToday(dateStr: string): boolean { const d = new Date(dateStr); const now = new Date(); return d.getFullYear() === now.getFullYear() && d.getMonth() === now.getMonth() && d.getDate() === now.getDate(); }

createRoot(document.getElementById("root")!).render(<StrictMode><App /></StrictMode>);
