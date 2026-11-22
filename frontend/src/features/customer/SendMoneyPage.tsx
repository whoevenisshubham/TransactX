import { FormEvent, useRef, useState } from "react";
import { api, ApiError } from "../../api";
import {
  Amount,
  Avatar,
  Badge,
  Button,
  Icon,
  InlineError,
  PageHeader,
  paymentResultCopy,
} from "../../components";
import { parsePaise } from "../../money";
import type { Payment, Recipient } from "../../types";
import type { SendMoneyPageProps } from "./types";

export function SendMoneyPage({
  account,
  token,
  queue,
  onComplete,
  onQueueChanged,
  onNavigate,
}: SendMoneyPageProps) {
  const [stage, setStage] = useState<
    "form" | "confirm" | "processing" | "result" | "uncertain" | "queued"
  >("form");
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
  const lookupGeneration = useRef(0);

  const amountPaise = parsePaise(amount);
  const availableBalance = account?.balancePaise ?? 0;
  const isOverBalance = amountPaise !== null && amountPaise > availableBalance;

  // Resolve recipient by TransactX payment ID
  async function resolve(identifierToLookup?: string, event?: FormEvent) {
    event?.preventDefault();
    const query = (identifierToLookup ?? recipientInput).trim();
    if (!query) return;
    const currentGen = ++lookupGeneration.current;
    setLookupBusy(true);
    setError("");
    setRecipient(null);
    setIdempotencyKey(null);
    setClientRequestId(null);
    try {
      const resolved = await api.resolveRecipient(query, token);
      if (currentGen !== lookupGeneration.current) return;
      setRecipient(resolved);
      if (identifierToLookup) {
        setRecipientInput(identifierToLookup);
      }
    } catch (caught) {
      if (currentGen !== lookupGeneration.current) return;
      setError(caught instanceof Error ? caught.message : "Payment ID not found");
    } finally {
      if (currentGen === lookupGeneration.current) {
        setLookupBusy(false);
      }
    }
  }

  function handleQuickPreset(rupees: number | "Max") {
    if (rupees === "Max") {
      setAmount((availableBalance / 100).toFixed(2));
    } else {
      setAmount(rupees.toFixed(2));
    }
    setError("");
    setIdempotencyKey(null);
    setClientRequestId(null);
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
    // Preserve existing idempotency key and client request ID if already active/unresolved; otherwise generate fresh
    setIdempotencyKey((prev) => prev ?? crypto.randomUUID());
    setClientRequestId((prev) => prev ?? crypto.randomUUID());
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
        // Network failure / unreachable: safely save intent on device
        try {
          await queue.enqueue(payload, { clientRequestId: requestID, idempotencyKey: key });
          onQueueChanged();
          setStage("queued");
        } catch {
          setError("We couldn't save this payment on your device. Please try again.");
          setStage("form");
        }
      } else if (caught instanceof ApiError && caught.status >= 500) {
        // Server error or timeout: outcome is uncertain, never assume failure!
        setStage("uncertain");
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
    setStage("form");
  }

  if (stage === "confirm" && recipient && amountPaise !== null) {
    return (
      <ConfirmPayment
        recipient={recipient}
        amount={amountPaise}
        note={note}
        account={account}
        idempotencyKey={idempotencyKey}
        onBack={editPayment}
        onConfirm={submitPayment}
        busy={busy}
      />
    );
  }

  if (stage === "processing" && recipient && amountPaise !== null) {
    return <ProcessingPayment recipient={recipient} amount={amountPaise} />;
  }

  if (stage === "queued" && recipient && amountPaise !== null) {
    return (
      <QueuedPayment
        recipient={recipient}
        amount={amountPaise}
        onDone={() => onNavigate("home")}
      />
    );
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

      <div className="grid grid-cols-1 lg:grid-cols-12 gap-6 items-start">
        {/* Main 4-step progressive flow card */}
        <section className="lg:col-span-8 flex flex-col gap-6">
          <form className="pay-form" onSubmit={continueToConfirm}>
            {/* Step 1: Target Counterparty */}
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
                        setClientRequestId(null);
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
                  <small className="field-hint">
                    Enter the recipient's unique TransactX payment ID.
                  </small>
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

                {/* Frequent counterparties quick chips */}
                <div className="pt-2">
                  <span className="text-xs uppercase tracking-wider text-[var(--muted)] font-medium block mb-2">
                    Quick Counterparties
                  </span>
                  <div className="flex flex-wrap gap-2">
                    <button
                      type="button"
                      className="px-2.5 py-1 text-xs rounded border border-[var(--line)] bg-[var(--surface-soft)] hover:bg-[var(--line)] transition-colors text-[var(--foreground)]"
                      onClick={() => void resolve("store@fixture")}
                    >
                      store@fixture (Demo Store)
                    </button>
                    <button
                      type="button"
                      className="px-2.5 py-1 text-xs rounded border border-[var(--line)] bg-[var(--surface-soft)] hover:bg-[var(--line)] transition-colors text-[var(--foreground)]"
                      onClick={() => void resolve("bob@transactx")}
                    >
                      bob@transactx (Bob Demo)
                    </button>
                  </div>
                </div>
              </div>
            </div>

            {/* Step 2: Settlement Amount */}
            <div className="form-section">
              <div className="form-section-number">02</div>
              <div className="form-section-body">
                <label className="field field-large">
                  <span>Amount</span>
                  <div
                    className={`amount-input ${
                      isOverBalance ? "amount-input-error" : ""
                    }`.trim()}
                  >
                    <span>₹</span>
                    <input
                      value={amount}
                      onChange={(event) => {
                        setAmount(event.target.value);
                        setError("");
                        setIdempotencyKey(null);
                        setClientRequestId(null);
                      }}
                      inputMode="decimal"
                      placeholder="0.00"
                      aria-label="Amount in Indian rupees"
                    />
                  </div>
                  <small
                    className={`field-hint ${
                      isOverBalance ? "field-hint-error" : ""
                    }`}
                  >
                    {isOverBalance ? (
                      `Amount exceeds your available balance of `
                    ) : (
                      `Available to send: `
                    )}
                    <Amount paise={availableBalance} />
                  </small>
                </label>

                {/* Sub-unit integer precision display */}
                {amountPaise !== null && amountPaise > 0 && (
                  <div className="mt-2 flex items-center gap-2 text-xs font-mono text-[var(--muted)]">
                    <span className="w-1.5 h-1.5 rounded-full bg-[var(--reset-teal)]" />
                    <span>
                      {amountPaise.toLocaleString()} paise (zero floating-point drift · integer safe)
                    </span>
                  </div>
                )}

                {/* Quick preset amount buttons */}
                <div className="flex flex-wrap gap-2 mt-3">
                  {[50, 100, 150, 500, 1000].map((preset) => (
                    <button
                      key={preset}
                      type="button"
                      className="px-3 py-1 rounded text-xs font-medium border border-[var(--line)] bg-[var(--surface-soft)] hover:bg-[var(--line)] transition-colors"
                      onClick={() => handleQuickPreset(preset)}
                    >
                      ₹{preset}
                    </button>
                  ))}
                  <button
                    type="button"
                    className="px-3 py-1 rounded text-xs font-medium border border-[var(--line)] bg-[var(--surface-soft)] hover:bg-[var(--line)] text-[var(--reset-teal)] transition-colors"
                    onClick={() => handleQuickPreset("Max")}
                  >
                    Max
                  </button>
                </div>
              </div>
            </div>

            {/* Step 3: Memo Note */}
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

          {/* Saga Sequence Architectural asset */}
          <div className="p-4 rounded-lg border border-[var(--line)] bg-[var(--surface-soft)] text-xs text-[var(--muted)] space-y-2">
            <div className="flex items-center justify-between font-mono">
              <span className="uppercase tracking-wider">Under The Hood · Saga Flow</span>
              <span>2PC Atomic Consensus</span>
            </div>
            <div className="w-full overflow-x-auto py-2">
              <svg
                className="w-full max-w-xl mx-auto h-16 text-[var(--foreground)]"
                fill="none"
                viewBox="0 0 620 80"
                xmlns="http://www.w3.org/2000/svg"
              >
                <rect fill="currentColor" fillOpacity="0.05" height="40" rx="8" width="120" x="10" y="20" />
                <text className="font-mono text-[10px] font-semibold" fill="currentColor" textAnchor="middle" x="70" y="38">
                  1. Enclave Lock
                </text>
                <text className="font-mono text-[8px]" fill="currentColor" fillOpacity="0.6" textAnchor="middle" x="70" y="50">
                  Debit Staged
                </text>
                <path d="M130 40H170" stroke="currentColor" strokeDasharray="3 3" strokeOpacity="0.3" strokeWidth="1.5" />
                <polygon fill="currentColor" fillOpacity="0.5" points="172,40 167,37 167,43" />
                <rect fill="currentColor" fillOpacity="0.05" height="40" rx="8" width="125" x="175" y="20" />
                <text className="font-mono text-[10px] font-semibold" fill="currentColor" textAnchor="middle" x="237" y="38">
                  2. RPC Prepare
                </text>
                <text className="font-mono text-[8px]" fill="currentColor" fillOpacity="0.6" textAnchor="middle" x="237" y="50">
                  Counterparty ACK
                </text>
                <path d="M300 40H340" stroke="currentColor" strokeDasharray="3 3" strokeOpacity="0.3" strokeWidth="1.5" />
                <polygon fill="currentColor" fillOpacity="0.5" points="342,40 337,37 337,43" />
                <rect fill="#316bf3" fillOpacity="0.12" height="48" rx="8" width="125" x="345" y="16" />
                <text className="font-mono text-[10px] font-bold" fill="#0051d5" textAnchor="middle" x="407" y="37">
                  3. 2PC Commit
                </text>
                <text className="font-mono text-[8px] font-semibold" fill="#0051d5" textAnchor="middle" x="407" y="51">
                  Atomic Finality
                </text>
                <path d="M470 40H505" stroke="currentColor" strokeOpacity="0.3" strokeWidth="1.5" />
                <polygon fill="currentColor" fillOpacity="0.5" points="507,40 502,37 502,43" />
                <rect fill="currentColor" fillOpacity="0.05" height="40" rx="8" width="100" x="510" y="20" />
                <text className="font-mono text-[10px] font-semibold" fill="#166534" textAnchor="middle" x="560" y="38">
                  4. Merkle Leaf
                </text>
                <text className="font-mono text-[8px]" fill="currentColor" fillOpacity="0.6" textAnchor="middle" x="560" y="50">
                  Receipt Minted
                </text>
              </svg>
            </div>
          </div>
        </section>

        {/* Right Sidebar Guarantees & Funding Rail */}
        <aside className="lg:col-span-4 flex flex-col gap-4">
          <div className="p-4 rounded-xl border border-[var(--line)] bg-[var(--surface-soft)] space-y-3">
            <div className="flex items-center justify-between text-xs font-mono text-[var(--muted)]">
              <span className="uppercase tracking-wider">Source Funding Rail</span>
              <span className="text-[var(--reset-teal)] font-medium">Verified Active</span>
            </div>
            <div>
              <h4 className="font-semibold text-base">{account?.bankName ?? "Bank Station"}</h4>
              <p className="text-xs font-mono text-[var(--muted)]">
                {account?.accountNumber ?? "TX-CORE-001"} · {account?.bankCode ?? "BANK-A"}
              </p>
            </div>
            <div className="p-3 rounded-lg bg-[var(--surface)] border border-[var(--line)]">
              <span className="text-xs text-[var(--muted)] block">Available Balance</span>
              <Amount paise={availableBalance} prominent />
            </div>
          </div>

          <div className="p-4 rounded-xl border border-[var(--line)] bg-[var(--surface-soft)] space-y-3">
            <h4 className="text-xs uppercase tracking-wider font-mono text-[var(--muted)] font-semibold">
              Consensus Guarantees
            </h4>
            <div className="space-y-3 text-xs">
              <div className="flex items-start gap-2">
                <Icon name="check" size={16} />
                <div>
                  <strong className="block text-[var(--foreground)]">Zero Phantom Debits</strong>
                  <span className="text-[var(--muted)]">
                    Funds are finalized only when the destination ledger completes stage two.
                  </span>
                </div>
              </div>
              <div className="flex items-start gap-2">
                <Icon name="activity" size={16} />
                <div>
                  <strong className="block text-[var(--foreground)]">Automatic Outbox Recovery</strong>
                  <span className="text-[var(--muted)]">
                    Network dropouts are resolved by idempotent offline replay workers safely.
                  </span>
                </div>
              </div>
            </div>
          </div>
        </aside>
      </div>
    </>
  );
}

function ConfirmPayment({
  recipient,
  amount,
  note,
  account,
  idempotencyKey,
  onBack,
  onConfirm,
  busy,
}: {
  recipient: Recipient;
  amount: number;
  note: string;
  account?: { bankName?: string; bankCode?: string; accountNumber?: string };
  idempotencyKey: string | null;
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

        {/* Route and Idempotency Trust Footnote */}
        {idempotencyKey && (
          <div className="pt-2 border-t border-[var(--line)] text-xs font-mono text-[var(--muted)] flex justify-between items-center">
            <span>Idempotency Key:</span>
            <code className="text-[var(--foreground)]">{idempotencyKey.slice(0, 18)}…</code>
          </div>
        )}
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

function ProcessingPayment({
  recipient,
  amount,
}: {
  recipient: Recipient;
  amount: number;
}) {
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
        We could not reach the TransactX network. Your payment intent has been safely saved on
        your device and will be automatically submitted once connectivity is restored.
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

function UncertainPayment({
  onRetry,
  onEdit,
}: {
  onRetry: () => void;
  onEdit: () => void;
}) {
  return (
    <div className="focused-state centered-state">
      <div className="result-mark result-pending">
        <Icon name="activity" size={24} />
      </div>
      <p className="eyebrow">Payment status</p>
      <h1>We couldn't confirm the response</h1>
      <p className="state-description">
        Your transfer may still be processing on the ledger. We preserved your payment request ID
        and idempotency key so you can safely check status or retry without duplicate debit.
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
  const counterparty =
    payment.direction === "RECEIVED" ? payment.senderName : payment.receiverName;

  async function refresh() {
    setRefreshing(true);
    try {
      onPayment(await api.payment(payment.id, token));
    } finally {
      setRefreshing(false);
    }
  }

  const isPending =
    payment.state !== "COMPLETED" &&
    payment.state !== "FAILED" &&
    payment.state !== "REVERSED";

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

function paymentError(caught: unknown) {
  if (!(caught instanceof ApiError)) {
    return "We couldn't complete the payment. Please try again.";
  }
  if (caught.code === "INSUFFICIENT_FUNDS") {
    return "Your balance is too low for this payment.";
  }
  if (caught.code === "RECIPIENT_NOT_FOUND") {
    return "That payment ID could not be found.";
  }
  if (caught.code === "BANK_UNAVAILABLE") {
    return "Payments are temporarily unavailable. Try again shortly.";
  }
  return "We couldn't complete the payment. Please try again.";
}
