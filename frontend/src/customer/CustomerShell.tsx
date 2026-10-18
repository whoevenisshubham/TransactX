import { FormEvent, useEffect, useRef, useState } from "react";
import { api, ApiError } from "../api";
import { Amount, Avatar, Badge, BrandMark, Button, EmptyState, Field, formatDate, Icon, InlineError, PageHeader, PaymentRow, paymentResultCopy, Skeleton, StatusBadge } from "../components";
import { parsePaise } from "../money";
import { OfflineIntentQueue, type OfflineIntent } from "../offlineQueue";
import { OfflineReplayWorker } from "../offlineReplay";
import type { Account, Payment, Recipient, User, View } from "../types";
import { ThemeSelect } from "../theme";
import { LoadingShell, DetailsSkeleton } from "../shared/ShellStates";
import { CopyValue } from "../shared/CopyValue";
import { firstName } from "../shared/presentation";

export function CustomerShell({ token, onLogout }: { token: string; onLogout: () => void }) {
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
    <div className="product-shell customer-shell">
      <Sidebar view={view} user={user} onNavigate={navigate} onLogout={onLogout} />
      <main className="main-content">
        <MobileHeader user={user} onLogout={onLogout} />
        <div className="content-wrap">
          <div className="workspace-topbar"><span>Personal / {view === "home" ? "Overview" : view === "pay" ? "Send money" : view === "details" ? "Receipt" : "Activity"}</span><NetworkStatus online={online} /></div>
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
        <ThemeSelect />
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
  const [masked, setMasked] = useState(false);
  const activeStatus = account?.status === "ACTIVE";
  const confirming = payments.filter(payment => !["COMPLETED", "FAILED", "REVERSED"].includes(payment.state)).length;
  return <>
    <PageHeader eyebrow="Personal workspace" title={`Good day, ${firstName(user?.name)}`} description="Your money, with the full picture." action={<Button onClick={() => onNavigate("pay")}><Icon name="send" />Send money</Button>} />
    <div className="customer-overview-grid">
      <section className="customer-balance" aria-label="Account balance"><div className="customer-card-top"><span className="section-kicker">Available balance</span><button type="button" className="balance-visibility" aria-pressed={masked} onClick={() => setMasked(value => !value)}>{masked ? "Show balance" : "Hide balance"}</button></div>{!account ? <p className="balance-unavailable">Balance unavailable</p> : masked ? <p className="masked-balance" aria-label="Balance hidden">••••••</p> : <Amount paise={account.balancePaise} prominent />}<div className="customer-account-foot"><span className="account-status"><span className={`status-dot ${activeStatus ? "status-success-dot" : "status-neutral-dot"}`} />{activeStatus ? "Active account" : account?.status ?? "Unavailable"}</span><span className="customer-account-number">{account?.accountNumber ?? "Account unavailable"}</span></div><p className="customer-bank-identity">{account?.bankName ? <>{account.bankName}{account.bankCode ? ` · ${account.bankCode}` : ""}</> : "Bank identity unavailable"}</p><p className="simulation-caption">Simulated INR · No real money</p></section>
      <section className="customer-identity" aria-label="Your payment identity"><div className="customer-card-top"><span className="section-kicker">Your payment ID</span><Icon name="qr" /></div><h2>{user?.paymentIdentifier ?? "Unavailable"}</h2><p>Share this ID to receive a payment from another TransactX account.</p>{user && <CopyValue value={user.paymentIdentifier} label="Copy payment ID" />}<div className="customer-identity-foot"><Avatar name={user?.name ?? "Customer"} /><span>{user?.name ?? "Customer"}<small>Personal account</small></span></div></section>
    </div>
    <section className="customer-activity-card"><div className="section-heading"><div><p className="eyebrow">Your latest activity</p><h2>Recent transactions</h2></div><button type="button" className="text-link" onClick={() => onNavigate("transactions")}>Open activity <Icon name="arrow" size={16} /></button></div><div className="activity-scope"><span>Latest {payments.length} loaded records · showing up to 5</span><span>{confirming} awaiting confirmation in this page</span></div>{payments.length === 0 ? <EmptyState title="No transactions yet" description="Your payments will appear here after your first transfer." action={<Button variant="secondary" onClick={() => onNavigate("pay")}>Send money</Button>} /> : <div className="payment-list">{payments.slice(0,5).map(payment => <PaymentRow key={payment.id} payment={payment} onClick={() => onNavigate("details",payment.id)} />)}</div>}</section>
    <section className="customer-guidance"><span className="role-icon"><Icon name="check" /></span><div><h2>Review first. Send once.</h2><p>Verify the recipient and exact amount before sending. If a confirmation is missing, check the original payment before starting another.</p></div><button type="button" className="text-link" onClick={() => onNavigate("pay")}>Start a payment <Icon name="arrow" /></button></section>
  </>;
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
  const [status, setStatus] = useState("ALL");
  const [fromDate, setFromDate] = useState("");
  const [toDate, setToDate] = useState("");

  const sentCount = payments.filter((p) => p.direction === "SENT").length;
  const receivedCount = payments.filter((p) => p.direction === "RECEIVED").length;

  const filtered = payments.filter((p) => {
    if (status !== "ALL" && p.state !== status) return false;
    const date = p.createdAt.slice(0, 10);
    if (fromDate && date < fromDate || toDate && date > toDate) return false;
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
          <div className="activity-filters"><label>Status<select aria-label="Status" value={status} onChange={event => setStatus(event.target.value)}><option value="ALL">All outcomes</option>{[...new Set(payments.map(payment => payment.state))].map(state => <option key={state} value={state}>{state.replaceAll("_", " ")}</option>)}</select></label><label>From date (UTC)<input type="date" value={fromDate} onChange={event => setFromDate(event.target.value)} /></label><label>To date (UTC)<input type="date" min={fromDate || undefined} value={toDate} onChange={event => setToDate(event.target.value)} /></label></div>
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
                  setSearch(""); setStatus("ALL"); setFromDate(""); setToDate("");
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
            value={payment.completedAt ? formatDate(payment.completedAt) : ["FAILED", "REVERSED"].includes(payment.state) ? "No completed settlement recorded" : "Pending network settlement"}
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
          <Record label="Payment reference" mono><span className="receipt-reference">{payment.id}<CopyValue value={payment.id} label="Copy payment reference" /></span></Record>
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


function readView(): View { const path = window.location.pathname; if (path.startsWith("/pay")) return "pay"; if (path.startsWith("/transactions/")) return "details"; if (path.startsWith("/transactions")) return "transactions"; return "home"; }
function readPaymentID() { const match = window.location.pathname.match(/^\/transactions\/([^/]+)/); return match?.[1] ?? null; }
function paymentError(caught: unknown) { if (!(caught instanceof ApiError)) return "We couldn't complete the payment. Please try again."; if (caught.code === "INSUFFICIENT_FUNDS") return "Your balance is too low for this payment."; if (caught.code === "RECIPIENT_NOT_FOUND") return "That payment ID could not be found."; if (caught.code === "BANK_UNAVAILABLE") return "Payments are temporarily unavailable. Try again shortly."; return "We couldn't complete the payment. Please try again."; }
