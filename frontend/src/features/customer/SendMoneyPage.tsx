import { FormEvent, useEffect, useRef, useState } from "react";
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
import {
  ActiveReservationConflictError,
  classifyPaymentOutcome,
  computePayloadHash,
  StorageUnavailableError,
  type DurableUnresolvedIntent,
  type DurableUnresolvedStore,
} from "../../unresolvedStore";
import type { SendMoneyPageProps } from "./types";

export type UnresolvedIntent = DurableUnresolvedIntent;

function isDurableIntent(val: unknown): val is UnresolvedIntent {
  return (
    val !== null &&
    typeof val === "object" &&
    "ownerUserId" in val &&
    typeof (val as Record<string, unknown>).ownerUserId === "string" &&
    "clientRequestId" in val
  );
}

export function SendMoneyPage({
  account,
  token,
  queue,
  onComplete,
  onPaymentCompleted,
  onQueueChanged,
  onNavigate,
  unresolvedStore,
  unresolvedIntent: initialUnresolvedIntent,
  storeStatus,
}: SendMoneyPageProps) {
  const [stage, setStage] = useState<
    "form" | "confirm" | "processing" | "result" | "uncertain" | "queued"
  >("form");
  const [recipientInput, setRecipientInput] = useState("");
  const [recipient, setRecipient] = useState<Recipient | null>(null);
  const [reviewedRecipient, setReviewedRecipient] = useState<Recipient | null>(null);
  const [amount, setAmount] = useState("");
  const [reviewedAmountPaise, setReviewedAmountPaise] = useState<number | null>(null);
  const [note, setNote] = useState("");
  const [reviewedNote, setReviewedNote] = useState<string>("");
  const [lookupBusy, setLookupBusy] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [payment, setPayment] = useState<Payment | null>(null);
  const [idempotencyKey, setIdempotencyKey] = useState<string | null>(null);
  const [clientRequestId, setClientRequestId] = useState<string | null>(null);
  const [unresolvedIntent, setUnresolvedIntent] = useState<DurableUnresolvedIntent | null>(
    initialUnresolvedIntent ?? null
  );
  const [statusChecking, setStatusChecking] = useState(false);
  const [statusCheckError, setStatusCheckError] = useState("");
  const submitLock = useRef(false);
  const lookupGeneration = useRef(0);

  useEffect(() => {
    if (initialUnresolvedIntent) {
      setUnresolvedIntent(initialUnresolvedIntent);
      setRecipient(initialUnresolvedIntent.recipient);
      setRecipientInput(initialUnresolvedIntent.recipient.paymentIdentifier);
      setAmount(
        initialUnresolvedIntent.amount || (initialUnresolvedIntent.amountPaise / 100).toFixed(2)
      );
      setNote(initialUnresolvedIntent.note);
      setIdempotencyKey(initialUnresolvedIntent.idempotencyKey);
      setClientRequestId(initialUnresolvedIntent.clientRequestId);
      setStage("uncertain");
    } else if (unresolvedStore) {
      void unresolvedStore.getActive().then((active) => {
        if (active) {
          setUnresolvedIntent(active);
          setRecipient(active.recipient);
          setRecipientInput(active.recipient.paymentIdentifier);
          setAmount(active.amount || (active.amountPaise / 100).toFixed(2));
          setNote(active.note);
          setIdempotencyKey(active.idempotencyKey);
          setClientRequestId(active.clientRequestId);
          setStage("uncertain");
        }
      });
    }
  }, [initialUnresolvedIntent, unresolvedStore]);

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
    setReviewedRecipient(null);
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
    setReviewedAmountPaise(null);
    setIdempotencyKey(null);
    setClientRequestId(null);
  }

  function continueToConfirm(event: FormEvent) {
    event.preventDefault();
    lookupGeneration.current++;
    if (unresolvedIntent) {
      setStage("uncertain");
      return;
    }
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
    setReviewedRecipient(recipient);
    setReviewedAmountPaise(amountPaise);
    setReviewedNote(note);
    // Preserve existing idempotency key and client request ID if already active/unresolved; otherwise generate fresh
    setIdempotencyKey((prev) => prev ?? crypto.randomUUID());
    setClientRequestId((prev) => prev ?? crypto.randomUUID());
    setStage("confirm");
  }

  async function submitPayment(rawOverride?: unknown) {
    const overrideIntent = isDurableIntent(rawOverride) ? rawOverride : undefined;
    const targetRecipient = overrideIntent?.recipient ?? reviewedRecipient ?? recipient;
    const targetAmountPaise = overrideIntent?.amountPaise ?? reviewedAmountPaise ?? amountPaise;
    const targetNote = overrideIntent?.note ?? reviewedNote ?? note;
    const key = overrideIntent?.idempotencyKey ?? idempotencyKey ?? crypto.randomUUID();
    const requestID = overrideIntent?.clientRequestId ?? clientRequestId ?? crypto.randomUUID();

    if (!targetRecipient || targetAmountPaise === null || submitLock.current) return;
    submitLock.current = true;
    const payload = {
      recipient: targetRecipient.paymentIdentifier,
      amountPaise: targetAmountPaise,
      currency: "INR",
      note: targetNote.trim() || undefined,
    };
    setIdempotencyKey(key);
    setClientRequestId(requestID);
    setError("");

    // Situation 1: Known offline before dispatch -> enqueue into local replay queue
    if (!navigator.onLine && !overrideIntent) {
      submitLock.current = false;
      setBusy(false);
      try {
        await queue.enqueue(payload, { clientRequestId: requestID, idempotencyKey: key });
        onQueueChanged();
        setStage("queued");
        return;
      } catch {
        setError("We couldn't save this payment on your device. Please try again.");
        setStage("form");
        return;
      }
    }

    setStage("processing");
    setBusy(true);

    const now = new Date().toISOString();
    let payloadHash = "";
    try {
      payloadHash = await computePayloadHash(payload);
    } catch {
      payloadHash = "hash-calc-failed";
    }

    const baseIntent: DurableUnresolvedIntent = overrideIntent
      ? {
          ...overrideIntent,
          updatedAt: now,
          state: "DISPATCHING",
        }
      : {
          ownerUserId:
            unresolvedStore?.getOwnerUserId() ??
            (account?.accountNumber ? `user-${account.accountNumber}` : "authenticated-user"),
          clientRequestId: requestID,
          idempotencyKey: key,
          recipient: targetRecipient,
          amountPaise: targetAmountPaise,
          amount: amount || (targetAmountPaise / 100).toFixed(2),
          currency: "INR",
          note: targetNote,
          payloadHash,
          createdAt: now,
          updatedAt: now,
          state: "DISPATCHING",
        };

    // Pre-dispatch reservation: MUST reserve in IndexedDB before network dispatch (Fail-closed storage gate)
    if (!unresolvedStore) {
      submitLock.current = false;
      setBusy(false);
      setError("Payment dispatch blocked: Durable storage is not initialized.");
      setStage("form");
      return;
    }

    try {
      await unresolvedStore.reserveActiveIntent(baseIntent);
      setUnresolvedIntent(baseIntent);
    } catch (reserveErr) {
      submitLock.current = false;
      setBusy(false);
      if (reserveErr instanceof ActiveReservationConflictError) {
        setError(
          "Another payment is currently unresolved or processing for this account. Cross-tab concurrency locked."
        );
        setStage("uncertain");
        return;
      }
      if (reserveErr instanceof StorageUnavailableError) {
        setError(
          "Payment dispatch blocked: Durable browser storage (IndexedDB) is unavailable or disabled. To prevent duplicate charges, payments cannot be submitted without durable persistence."
        );
        setStage("form");
        return;
      }
      setError(
        reserveErr instanceof Error ? reserveErr.message : "Failed to safely persist payment reservation."
      );
      setStage("form");
      return;
    }

    // Now dispatch to backend via createPaymentWithStatus
    try {
      const response = await api.createPaymentWithStatus(payload, token, key, requestID);
      const outcome = classifyPaymentOutcome(response.data);

      if (outcome === "TERMINAL_COMPLETED") {
        await unresolvedStore.resolveTerminal(requestID, "RESOLVED_COMPLETED", response.data.id);
        setUnresolvedIntent(null);
        setPayment(response.data);
        onPaymentCompleted?.(response.data);
        setStage("result");
      } else if (outcome === "TERMINAL_FAILED") {
        await unresolvedStore.resolveTerminal(requestID, "RESOLVED_FAILED", response.data.id);
        setUnresolvedIntent(null);
        setError(response.data.failureReason || "Payment failed.");
        setStage("form");
      } else if (outcome === "TERMINAL_REVERSED") {
        await unresolvedStore.resolveTerminal(requestID, "RESOLVED_REVERSED", response.data.id);
        setUnresolvedIntent(null);
        setError("Payment was reversed.");
        setStage("form");
      } else {
        // NON_TERMINAL_PROCESSING or UNKNOWN_STATE: Keep active reservation locked!
        await unresolvedStore.updateProgress(requestID, "PROCESSING", response.data.id, response.status);
        setUnresolvedIntent({
          ...baseIntent,
          state: "PROCESSING",
          paymentId: response.data.id,
          lastHttpStatus: response.status,
        });
        setStage("uncertain");
      }
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 0) {
        // Situation 2: Post-dispatch network failure / drop
        // Request may have reached the server! Keep active reservation, set state UNCERTAIN!
        // DO NOT enqueue into OfflineIntentQueue!
        await unresolvedStore.updateProgress(requestID, "UNCERTAIN", undefined, 0, caught.message);
        setUnresolvedIntent({
          ...baseIntent,
          state: "UNCERTAIN",
          lastHttpStatus: 0,
          lastError: caught.message,
        });
        setStage("uncertain");
      } else if (caught instanceof ApiError && caught.status >= 500) {
        // Server error or timeout: outcome uncertain!
        await unresolvedStore.updateProgress(
          requestID,
          "UNCERTAIN",
          undefined,
          caught.status,
          caught.message
        );
        setUnresolvedIntent({
          ...baseIntent,
          state: "UNCERTAIN",
          lastHttpStatus: caught.status,
          lastError: caught.message,
        });
        setStage("uncertain");
      } else if (caught instanceof ApiError && caught.status === 409) {
        // Conflict on idempotency key with mismatched payload
        setError("Payment conflict: Idempotency key already used with different payment parameters.");
        setStage("uncertain");
      } else if (caught instanceof ApiError && (caught.status === 401 || caught.status === 403)) {
        // Authentication or authorization failure: preserve unresolved intent and active reservation!
        await unresolvedStore.updateProgress(
          requestID,
          "UNCERTAIN",
          undefined,
          caught.status,
          caught.message
        );
        setUnresolvedIntent({
          ...baseIntent,
          state: "UNCERTAIN",
          lastHttpStatus: caught.status,
          lastError: caught.message,
        });
        setError(caught.status === 401 ? "Session expired during payment dispatch. Please sign in again." : "Payment access forbidden.");
        setStage("uncertain");
      } else {
        // Definitive client rejection (400, 422) where no payment was created or accepted by backend
        if (unresolvedStore) {
          await unresolvedStore.resolveTerminal(requestID, "RESOLVED_FAILED");
        }
        setError(paymentError(caught));
        setUnresolvedIntent(null);
        setIdempotencyKey(null);
        setClientRequestId(null);
        setStage("form");
      }
    } finally {
      submitLock.current = false;
      setBusy(false);
    }
  }

  async function checkUncertainStatus() {
    const activeKey = unresolvedIntent?.idempotencyKey ?? idempotencyKey;
    const activeRequestId = unresolvedIntent?.clientRequestId ?? clientRequestId;
    if (!activeKey || !activeRequestId || !unresolvedStore) return;
    setStatusChecking(true);
    setStatusCheckError("");
    try {
      const existing = await api.paymentByIdempotencyKey(activeKey, token);
      const outcome = classifyPaymentOutcome(existing);

      if (outcome === "TERMINAL_COMPLETED") {
        await unresolvedStore.resolveTerminal(activeRequestId, "RESOLVED_COMPLETED", existing.id);
        setUnresolvedIntent(null);
        setPayment(existing);
        onPaymentCompleted?.(existing);
        setStage("result");
      } else if (outcome === "TERMINAL_FAILED") {
        await unresolvedStore.resolveTerminal(activeRequestId, "RESOLVED_FAILED", existing.id);
        setUnresolvedIntent(null);
        setError(existing.failureReason || "Payment failed.");
        setStage("form");
      } else if (outcome === "TERMINAL_REVERSED") {
        await unresolvedStore.resolveTerminal(activeRequestId, "RESOLVED_REVERSED", existing.id);
        setUnresolvedIntent(null);
        setError("Payment was reversed.");
        setStage("form");
      } else {
        // Non-terminal state (e.g. PROCESSING, PENDING_RECONCILIATION, COMMITTED, etc.)
        await unresolvedStore.updateProgress(activeRequestId, "PROCESSING", existing.id, 200);
        setStatusCheckError(
          `Payment is currently in intermediate state "${existing.state}". Settlement is ongoing; transaction remains safely locked.`
        );
      }
    } catch (caught) {
      const now = new Date().toISOString();
      if (caught instanceof ApiError && caught.status === 404) {
        setStatusCheckError(
          "Payment not yet found on the ledger. It may still be processing in flight. Your transaction identity is preserved and safe to retry."
        );
        await unresolvedStore.updateProgress(activeRequestId, "UNCERTAIN", undefined, 404);
      } else {
        const errorMsg =
          caught instanceof Error ? caught.message : "Unable to verify payment status.";
        setStatusCheckError(errorMsg);
        await unresolvedStore.updateProgress(
          activeRequestId,
          "UNCERTAIN",
          undefined,
          caught instanceof ApiError ? caught.status : undefined,
          errorMsg
        );
      }
    } finally {
      setStatusChecking(false);
    }
  }

  function retryUncertain() {
    if (unresolvedIntent) {
      void submitPayment(unresolvedIntent);
    } else {
      void submitPayment();
    }
  }

  function editPayment() {
    setReviewedRecipient(null);
    setReviewedAmountPaise(null);
    setReviewedNote("");
    setStage("form");
  }

  if (stage === "confirm" && (reviewedRecipient ?? recipient) && (reviewedAmountPaise ?? amountPaise) !== null) {
    const targetRecipient = reviewedRecipient ?? recipient!;
    const targetAmount = reviewedAmountPaise ?? amountPaise!;
    const targetNote = reviewedNote || note;
    return (
      <ConfirmPayment
        recipient={targetRecipient}
        amount={targetAmount}
        note={targetNote}
        account={account}
        idempotencyKey={idempotencyKey}
        onBack={editPayment}
        onConfirm={() => void submitPayment()}
        busy={busy}
      />
    );
  }

  if (stage === "processing" && (recipient || unresolvedIntent) && (amountPaise !== null || unresolvedIntent)) {
    const displayRecipient = unresolvedIntent?.recipient ?? recipient!;
    const displayAmount = unresolvedIntent?.amountPaise ?? amountPaise!;
    return <ProcessingPayment recipient={displayRecipient} amount={displayAmount} />;
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
    return (
      <UncertainPayment
        intent={unresolvedIntent}
        onRetry={retryUncertain}
        onCheckStatus={() => void checkUncertainStatus()}
        onViewActivity={() => onNavigate("transactions")}
        onReturnToForm={() => setStage("form")}
        onReturnToOverview={() => onNavigate("home")}
        checkingStatus={statusChecking}
        statusError={statusCheckError || error}
      />
    );
  }

  if (stage === "result" && payment) {
    return (
      <PaymentResult
        payment={payment}
        token={token}
        onPayment={setPayment}
        onView={() => {
          onPaymentCompleted?.(payment);
          onComplete(payment);
        }}
        onDone={() => {
          onPaymentCompleted?.(payment);
          onNavigate("home");
        }}
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
            {unresolvedIntent && (
              <div
                className="p-4 rounded-lg border border-[var(--reset-gold)] bg-[var(--surface-soft)] text-xs flex flex-col gap-2"
                role="alert"
                data-testid="unresolved-intent-banner"
              >
                <div className="flex items-center gap-2 font-semibold text-[var(--reset-gold)] text-sm">
                  <Icon name="activity" size={16} />
                  <span>Unresolved Payment Intent Locked</span>
                </div>
                {unresolvedIntent.persistenceFailed && (
                  <div className="p-2 rounded bg-amber-500/10 border border-amber-500/20 text-amber-200">
                    Storage fallback active: Local storage was restricted; transaction identity is preserved in memory.
                  </div>
                )}
                <p className="text-[var(--muted)]">
                  A transfer of <Amount paise={unresolvedIntent.amountPaise} /> to{" "}
                  <strong>{unresolvedIntent.recipient.name}</strong> (
                  {unresolvedIntent.recipient.paymentIdentifier}) was initiated with idempotency key{" "}
                  <code className="font-mono text-xs">{unresolvedIntent.idempotencyKey.slice(0, 8)}…</code>{" "}
                  but the server returned an uncertain outcome. To guarantee zero duplicate debits,
                  form editing is frozen to preserve this transaction's cryptographic identity.
                </p>
                <div className="flex flex-wrap gap-2 pt-1">
                  <Button size="sm" onClick={() => setStage("uncertain")}>
                    Return to status check
                  </Button>
                  <Button size="sm" variant="secondary" onClick={() => onNavigate("home")}>
                    Return to overview
                  </Button>
                </div>
              </div>
            )}

            {/* Step 1: Target Counterparty */}
            <div className="form-section">
              <div className="form-section-number">01</div>
              <div className="form-section-body">
                <label className="field field-large">
                  <span>Recipient payment ID</span>
                  <div className="input-with-action">
                    <input
                      value={recipientInput}
                      disabled={lookupBusy || Boolean(unresolvedIntent)}
                      onChange={(event) => {
                        if (unresolvedIntent) return;
                        setRecipientInput(event.target.value);
                        setRecipient(null);
                        setReviewedRecipient(null);
                        setError("");
                        setIdempotencyKey(null);
                        setClientRequestId(null);
                      }}
                      onBlur={() => {
                        if (recipientInput && !recipient && !unresolvedIntent) void resolve();
                      }}
                      placeholder="name@transactx"
                      autoComplete="off"
                      aria-label="Recipient TransactX payment ID"
                    />
                    <button
                      type="button"
                      className="input-action"
                      onClick={() => void resolve()}
                      disabled={lookupBusy || !recipientInput.trim() || Boolean(unresolvedIntent)}
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
                      disabled={Boolean(unresolvedIntent)}
                      className="px-2.5 py-1 text-xs rounded border border-[var(--line)] bg-[var(--surface-soft)] hover:bg-[var(--line)] transition-colors text-[var(--foreground)] disabled:opacity-50"
                      onClick={() => void resolve("store@fixture")}
                    >
                      store@fixture (Demo Store)
                    </button>
                    <button
                      type="button"
                      disabled={Boolean(unresolvedIntent)}
                      className="px-2.5 py-1 text-xs rounded border border-[var(--line)] bg-[var(--surface-soft)] hover:bg-[var(--line)] transition-colors text-[var(--foreground)] disabled:opacity-50"
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
                      disabled={Boolean(unresolvedIntent)}
                      onChange={(event) => {
                        if (unresolvedIntent) return;
                        setAmount(event.target.value);
                        setReviewedAmountPaise(null);
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
                      disabled={Boolean(unresolvedIntent)}
                      className="px-3 py-1 rounded text-xs font-medium border border-[var(--line)] bg-[var(--surface-soft)] hover:bg-[var(--line)] transition-colors disabled:opacity-50"
                      onClick={() => handleQuickPreset(preset)}
                    >
                      ₹{preset}
                    </button>
                  ))}
                  <button
                    type="button"
                    disabled={Boolean(unresolvedIntent)}
                    className="px-3 py-1 rounded text-xs font-medium border border-[var(--line)] bg-[var(--surface-soft)] hover:bg-[var(--line)] text-[var(--reset-teal)] transition-colors disabled:opacity-50"
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
                    disabled={Boolean(unresolvedIntent)}
                    onChange={(event) => {
                      if (unresolvedIntent) return;
                      setNote(event.target.value);
                      setReviewedNote("");
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
                disabled={Boolean(unresolvedIntent) || !recipient || amountPaise === null || isOverBalance || storeStatus === "loading"}
              >
                {storeStatus === "loading" ? "Initializing storage…" : "Review payment"} <Icon name="arrow" />
              </Button>
            </div>
          </form>

          {/* Saga Sequence Architectural asset */}
          <div className="p-4 rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/60 text-xs text-[var(--text-secondary)] space-y-2">
            <div className="flex items-center justify-between font-mono">
              <span className="font-semibold text-[var(--text-primary)]">Payment settlement flow</span>
              <span>Two-phase commit</span>
            </div>
            <div className="w-full overflow-x-auto py-1">
              <svg
                className="w-full max-w-xl mx-auto h-16"
                fill="none"
                viewBox="0 0 640 80"
                xmlns="http://www.w3.org/2000/svg"
                role="img"
                aria-label="Four-stage settlement flow: Intent Staged, Debit Bank A, Credit Bank B, Authoritative Finality"
              >
                <rect fill="var(--bg-surface)" stroke="var(--border-hairline)" height="44" rx="8" width="130" x="8" y="18" />
                <text className="font-mono text-[10px] font-semibold" fill="var(--text-primary)" textAnchor="middle" x="73" y="37">
                  1. Intent Staged
                </text>
                <text className="font-mono text-[8.5px]" fill="var(--text-secondary)" textAnchor="middle" x="73" y="51">
                  Client UUIDv4
                </text>
                <path d="M138 40H166" stroke="var(--border-active)" strokeDasharray="3 3" strokeWidth="1.5" />
                <polygon fill="var(--text-tertiary)" points="168,40 163,37 163,43" />

                <rect fill="var(--bg-surface)" stroke="var(--border-hairline)" height="44" rx="8" width="130" x="170" y="18" />
                <text className="font-mono text-[10px] font-semibold" fill="var(--text-primary)" textAnchor="middle" x="235" y="37">
                  2. Debit / Bank A
                </text>
                <text className="font-mono text-[8.5px]" fill="var(--text-secondary)" textAnchor="middle" x="235" y="51">
                  Isolated Hold
                </text>
                <path d="M300 40H328" stroke="var(--border-active)" strokeDasharray="3 3" strokeWidth="1.5" />
                <polygon fill="var(--text-tertiary)" points="330,40 325,37 325,43" />

                <rect fill="var(--status-info-bg)" stroke="var(--accent-strong)" height="46" rx="8" width="135" x="332" y="17" />
                <text className="font-mono text-[10px] font-bold" fill="var(--accent-strong)" textAnchor="middle" x="399" y="37">
                  3. Credit / Bank B
                </text>
                <text className="font-mono text-[8.5px] font-semibold" fill="var(--accent-strong)" textAnchor="middle" x="399" y="51">
                  2PC Prepare / Commit
                </text>
                <path d="M467 40H495" stroke="var(--border-active)" strokeWidth="1.5" />
                <polygon fill="var(--text-tertiary)" points="497,40 492,37 492,43" />

                <rect fill="var(--status-success-bg)" stroke="var(--status-success-border)" height="44" rx="8" width="132" x="499" y="18" />
                <text className="font-mono text-[10px] font-semibold" fill="var(--status-success-fg)" textAnchor="middle" x="565" y="37">
                  4. Authoritative Finality
                </text>
                <text className="font-mono text-[8.5px]" fill="var(--status-success-fg)" textAnchor="middle" x="565" y="51">
                  Evidence Recorded
                </text>
              </svg>
            </div>
          </div>
        </section>

        {/* Right Sidebar Guarantees & Funding Rail */}
        <aside className="lg:col-span-4 flex flex-col gap-4">
          <div className="p-4 rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] space-y-3">
            <div className="flex items-center justify-between text-xs font-mono text-[var(--text-secondary)]">
              <span className="font-semibold">Source funding rail</span>
              <span className="text-[var(--status-success-fg)] font-medium">Verified active</span>
            </div>
            <div>
              <h4 className="font-semibold text-base text-[var(--text-primary)]">{account?.bankName ?? "Bank Station"}</h4>
              <p className="text-xs font-mono text-[var(--text-tertiary)]">
                {account?.accountNumber ?? "TX-CORE-001"} · {account?.bankCode ?? "BANK-A"}
              </p>
            </div>
            <div className="px-3.5 py-3 rounded-lg bg-[var(--bg-surface-raised)] border border-[var(--border-hairline)]">
              <span className="text-xs text-[var(--text-secondary)] block">Available Balance</span>
              <span className="mt-1 block font-mono text-[clamp(1.35rem,2vw,1.75rem)] font-bold tracking-tight tabular-nums text-[var(--text-primary)]">
                <Amount paise={availableBalance} />
              </span>
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
  intent,
  onRetry,
  onCheckStatus,
  onViewActivity,
  onReturnToForm,
  onReturnToOverview,
  checkingStatus,
  statusError,
}: {
  intent: UnresolvedIntent | null;
  onRetry: () => void;
  onCheckStatus: () => void;
  onViewActivity: () => void;
  onReturnToForm: () => void;
  onReturnToOverview?: () => void;
  checkingStatus: boolean;
  statusError: string;
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
      {intent?.persistenceFailed && (
        <div className="p-2 rounded bg-amber-500/10 border border-amber-500/20 text-amber-200 text-xs mb-3">
          Storage fallback active: Local storage was restricted; transaction identity is preserved in memory.
        </div>
      )}
      {intent && (
        <div className="mini-payment">
          <Avatar name={intent.recipient.name} />
          <span>
            <strong>{intent.recipient.name}</strong>
            <small>
              <Amount paise={intent.amountPaise} /> · Key:{" "}
              <code className="font-mono text-xs">{intent.idempotencyKey.slice(0, 8)}…</code>
            </small>
          </span>
        </div>
      )}
      {statusError && <InlineError message={statusError} />}
      <div className="confirm-actions flex-wrap">
        <Button variant="secondary" onClick={onCheckStatus} disabled={checkingStatus}>
          {checkingStatus ? "Checking status…" : "Check status"}
        </Button>
        <Button onClick={onRetry} disabled={checkingStatus}>
          Try again safely
        </Button>
        <Button variant="secondary" onClick={onViewActivity}>
          View activity
        </Button>
        <Button variant="quiet" onClick={onReturnToForm}>
          Review intent details
        </Button>
        {onReturnToOverview && (
          <Button variant="secondary" onClick={onReturnToOverview}>
            Return to overview
          </Button>
        )}
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
