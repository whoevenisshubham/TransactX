import { useCallback, useEffect, useRef, useState } from "react";
import { Button, EmptyState, InlineError, PageHeader } from "../components";
import { DataTable, formatPaiseDecimal, OpsStatusBadge, Timeline } from "../design/OpsPrimitives";
import { liveOpsClient } from "./client";
import type { EvidenceOperation, EvidenceTransition, RecoveryAttempt } from "./contracts";
import { useResource } from "./useResource";
import { TracePanel } from "./TracePanel";
import { InvestigationPanel } from "./InvestigationPanel";
import { focusEvidence } from "./focusEvidence";

function time(value: string | null) {
  return value ? new Date(value).toLocaleString() : "Not observed";
}
function optional(value: string | null) {
  return value ?? "Unknown";
}
function bankMark(code: string | null) {
  return code === "BANK-A" ? "A" : code === "BANK-B" ? "B" : "·";
}

export function PaymentEvidencePage({
  paymentId,
  token,
  isAdmin,
  liveRevision,
  onNavigate,
}: {
  paymentId: string;
  token: string;
  isAdmin: boolean;
  liveRevision: number;
  onNavigate: (path: string) => void;
}) {
  const load = useCallback(
    (signal: AbortSignal) => liveOpsClient.evidence(paymentId, token, signal),
    [paymentId, token]
  );
  const detail = useResource(paymentId, load);
  const loadAttempts = useCallback(
    (signal: AbortSignal) => liveOpsClient.recoveryAttempts(paymentId, token, undefined, signal),
    [paymentId, token]
  );
  const attempts = useResource(paymentId, loadAttempts);

  const [reason, setReason] = useState("");
  const [recheckStatus, setRecheckStatus] = useState<string | null>(null);
  const [recheckError, setRecheckError] = useState<string | null>(null);
  const [rechecking, setRechecking] = useState(false);
  const [exportError, setExportError] = useState<string | null>(null);
  const [exportedAt, setExportedAt] = useState<string | null>(null);
  const [copyStatus, setCopyStatus] = useState("");
  const [mobileSection, setMobileSection] = useState<"journey" | "evidence" | "investigate">("journey");
  const recheckAttempt = useRef<{ paymentId: string; reason: string; key: string } | null>(null);

  useEffect(() => {
    if (!liveRevision || document.hidden) return;
    const timer = window.setTimeout(() => {
      detail.refresh();
      attempts.refresh();
    }, 350);
    return () => window.clearTimeout(timer);
  }, [liveRevision, detail.refresh, attempts.refresh]);

  useEffect(() => {
    const timer = window.setInterval(() => {
      if (!document.hidden) {
        detail.refresh();
        attempts.refresh();
      }
    }, 15000);
    return () => window.clearInterval(timer);
  }, [detail.refresh, attempts.refresh]);

  useEffect(() => {
    setReason("");
    setRecheckStatus(null);
    setRecheckError(null);
    setExportError(null);
    setExportedAt(null);
  }, [paymentId]);

  const evidence = detail.key === paymentId ? detail.data : null;
  const attemptPage = attempts.key === paymentId ? attempts.data : null;

  useEffect(() => {
    if (!evidence || !window.location.hash.startsWith("#evidence-")) return;
    try {
      const id = decodeURIComponent(window.location.hash.slice(1));
      window.setTimeout(() => {
        focusEvidence(id);
      }, 0);
    } catch {
      /* Ignore malformed link fragments. */
    }
  }, [evidence]);

  async function scheduleRecheck() {
    if (!isAdmin || rechecking) return;
    setRechecking(true);
    setRecheckError(null);
    setRecheckStatus(null);
    try {
      const normalizedReason = reason.trim();
      if (
        recheckAttempt.current?.paymentId !== paymentId ||
        recheckAttempt.current.reason !== normalizedReason
      ) {
        recheckAttempt.current = {
          paymentId,
          reason: normalizedReason,
          key: crypto.randomUUID(),
        };
      }
      const result = await liveOpsClient.recheck(
        paymentId,
        normalizedReason,
        recheckAttempt.current.key,
        token
      );
      recheckAttempt.current = null;
      setRecheckStatus(
        result.status === "scheduled"
          ? `Recheck scheduled at ${time(result.scheduledAt)}. Payment outcome is unchanged until new evidence arrives.`
          : "This payment is already terminal; no recheck was scheduled."
      );
      detail.refresh();
      attempts.refresh();
    } catch (error) {
      setRecheckError(
        error instanceof Error ? error.message : "Could not schedule the recheck."
      );
    } finally {
      setRechecking(false);
    }
  }

  async function exportEvidence() {
    setExportError(null);
    setExportedAt(null);
    try {
      const payload = await liveOpsClient.exportEvidence(paymentId, token);
      const blob = new Blob([JSON.stringify(payload, null, 2)], { type: "application/json" });
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = `transactx-evidence-${paymentId}.json`;
      document.body.append(anchor);
      anchor.click();
      anchor.remove();
      window.setTimeout(() => URL.revokeObjectURL(url), 1000);
      setExportedAt(new Date().toISOString());
    } catch (error) {
      setExportError(error instanceof Error ? error.message : "Could not export evidence.");
    }
  }

  function jumpToSection(target: "journey" | "evidence" | "investigate") {
    setMobileSection(target);
    const behavior = matchMedia("(prefers-reduced-motion: reduce)").matches ? "instant" : "smooth";
    if (target === "evidence") {
      const section = document.getElementById("journey-detailed-evidence");
      if (section instanceof HTMLDetailsElement) {
        section.open = true;
        section.querySelector("summary")?.focus();
        section.scrollIntoView({ block: "start", behavior });
      }
    } else if (target === "investigate") {
      const panel = document.getElementById("payment-investigation");
      panel?.focus({ preventScroll: true });
      panel?.scrollIntoView({ block: "start", behavior });
    } else {
      window.scrollTo({ top: 0, behavior });
    }
  }

  return (
    <div className="ops-page reset-journey">
      <PageHeader
        title="Payment journey"
        description="Outcome, bank observations and a cited investigation."
        action={
          <Button
            variant="secondary"
            onClick={() => {
              const from = window.history.state?.from;
              onNavigate(
                typeof from === "string" && from.startsWith("/console/worklist")
                  ? from
                  : "/console/worklist"
              );
            }}
          >
            Back to worklist
          </Button>
        }
      />

      {detail.loading && !evidence && <p role="status">Loading payment evidence…</p>}
      {detail.error && <InlineError message={detail.error} />}
      {detail.stale && evidence && (
        <p role="status" className="ops-banner ops-banner-warning">
          Showing the last collected evidence while refreshing. Collected {time(evidence.collectedAt)}.
        </p>
      )}
      {!evidence && !detail.loading && !detail.error && (
        <EmptyState title="Evidence unavailable" description="Try refreshing this payment." />
      )}

      {evidence && (
        <>
          <details className="journey-reference-details rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] px-4 py-3 text-xs">
            <summary className="cursor-pointer font-semibold text-[var(--text-primary)] select-none">
              Payment reference & snapshot
            </summary>
            <div className="journey-reference mt-3 flex flex-wrap items-center gap-2">
              <span className="ops-mono text-[var(--text-primary)]">{paymentId}</span>
              <button
                type="button"
                aria-label="Copy payment reference"
                className="rounded-md border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)] px-2.5 py-1 text-xs font-semibold text-[var(--text-primary)] hover:border-[var(--border-active)] cursor-pointer"
                onClick={() => {
                  navigator.clipboard.writeText(paymentId).then(
                    () => setCopyStatus("Reference copied"),
                    () => setCopyStatus("Copy unavailable. Select the reference to copy it.")
                  );
                }}
              >
                Copy
              </button>
              <span role="status" className="text-[var(--status-success-fg)]">
                {copyStatus}
              </span>
            </div>
            <div className="journey-meta mt-2 flex flex-wrap items-center justify-between gap-2 text-[var(--text-secondary)]">
              <p className="ops-muted m-0">Collected {time(evidence.collectedAt)}</p>
              <details>
                <summary className="cursor-pointer">Snapshot identity</summary>
                <span className="ops-mono text-[11px]">{evidence.snapshotId}</span>
              </details>
            </div>
          </details>

          {/* Mobile Segmented Switcher (< 768px) + Desktop/Tablet Section Jump Links */}
          <nav
            className="flex md:hidden rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)] p-1 gap-1"
            aria-label="Mobile journey switcher"
          >
            {(
              [
                { id: "journey", label: "Journey" },
                { id: "evidence", label: "Evidence" },
                { id: "investigate", label: "Investigate" },
              ] as const
            ).map((tab) => (
              <button
                key={tab.id}
                type="button"
                aria-pressed={mobileSection === tab.id}
                onClick={() => jumpToSection(tab.id)}
                className={`flex-1 rounded-lg px-2.5 py-2 text-xs font-semibold transition-colors cursor-pointer ${
                  mobileSection === tab.id
                    ? "bg-[var(--bg-surface)] text-[var(--text-primary)] shadow-[var(--shadow-subtle)]"
                    : "text-[var(--text-secondary)]"
                }`}
              >
                {tab.label}
              </button>
            ))}
          </nav>

          <nav
            className="journey-section-nav hidden md:flex flex-wrap items-center gap-3"
            aria-label="Payment journey sections"
          >
            <a
              className="button button-secondary journey-jump"
              href="#payment-investigation"
              onClick={(event) => {
                event.preventDefault();
                jumpToSection("investigate");
              }}
            >
              Ask about this payment ↓
            </a>
            <a
              className="ops-link text-xs"
              href="#journey-detailed-evidence"
              onClick={(event) => {
                event.preventDefault();
                jumpToSection("evidence");
              }}
            >
              Detailed evidence ↓
            </a>
          </nav>

          <div className="journey-layout">
            <div className="journey-body">
              <div className="journey-overview space-y-4">
                <section
                  className="journey-payment-banner grid grid-cols-1 md:grid-cols-2 gap-4 rounded-2xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] p-5 shadow-[var(--shadow-subtle)]"
                  id={`evidence-${evidence.payment.evidenceId}`}
                >
                  <div className="journey-payment-outcome space-y-2">
                    <p className="text-[11px] font-mono font-semibold uppercase tracking-wider text-[var(--accent-strong)] m-0">
                      Central payment
                    </p>
                    <p className="amount amount-prominent ops-mono text-3xl font-bold tracking-tight text-[var(--text-primary)] m-0">
                      {formatPaiseDecimal(evidence.payment.amountPaise, evidence.payment.currency)}
                    </p>
                    <div>
                      <OpsStatusBadge state={evidence.payment.state} />
                    </div>
                    <details className="pt-1 text-xs text-[var(--text-secondary)]">
                      <summary className="cursor-pointer font-medium">
                        Central evidence identity
                      </summary>
                      <p className="mt-1 mb-0.5">Created {time(evidence.payment.createdAt)}</p>
                      <span className="ops-mono text-[11px]">{evidence.payment.evidenceId}</span>
                      <p className="mt-0.5 mb-0">Last updated {time(evidence.payment.updatedAt)}</p>
                    </details>
                  </div>

                  <div className="journey-bank-route rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/60 p-4 space-y-3">
                    <h2 className="text-xs font-mono uppercase tracking-wider text-[var(--text-secondary)] m-0">
                      Original route
                    </h2>
                    <div className="journey-bank-stations flex items-center justify-between gap-2">
                      <div className="flex flex-col items-start min-w-0">
                        <span className="inline-flex h-7 w-7 items-center justify-center rounded-full border border-[var(--border-hairline)] bg-[var(--bg-surface)] font-mono text-xs font-bold text-[var(--text-primary)]">
                          {bankMark(evidence.route.sourceBankCode)}
                        </span>
                        <strong className="mt-1 text-sm font-semibold text-[var(--text-primary)] truncate">
                          {optional(evidence.route.sourceBankCode)}
                        </strong>
                        <small className="text-[11px] text-[var(--text-tertiary)]">
                          Source bank
                        </small>
                      </div>
                      <span className="journey-route-line font-mono text-[var(--text-tertiary)]" aria-hidden="true">
                        →
                      </span>
                      <div className="flex flex-col items-end min-w-0 text-right">
                        <span className="inline-flex h-7 w-7 items-center justify-center rounded-full border border-[var(--border-hairline)] bg-[var(--bg-surface)] font-mono text-xs font-bold text-[var(--text-primary)]">
                          {bankMark(evidence.route.destinationBankCode)}
                        </span>
                        <strong className="mt-1 text-sm font-semibold text-[var(--text-primary)] truncate">
                          {optional(evidence.route.destinationBankCode)}
                        </strong>
                        <small className="text-[11px] text-[var(--text-tertiary)]">
                          Destination bank
                        </small>
                      </div>
                    </div>
                    <details className="text-xs text-[var(--text-secondary)]">
                      <summary className="cursor-pointer font-medium">Route provenance</summary>
                      <p className="mt-1 mb-0.5">
                        Execution target:{" "}
                        <span className="ops-mono">{optional(evidence.route.executionTargetId)}</span>
                      </p>
                      <small>
                        Selected {time(evidence.route.selectedAt)} · Source:{" "}
                        {evidence.route.status.replaceAll("_", " ")}
                      </small>
                    </details>
                  </div>
                </section>

                {(evidence.payment.state === "PENDING_RECONCILIATION" ||
                  evidence.payment.state === "BANK_SETTLED_CENTRAL_PENDING") && (
                  <p className="ops-banner ops-banner-warning" role="status">
                    This payment has no safely established final result. An unavailable or unknown
                    bank observation does not prove failure.
                  </p>
                )}

                {evidence.warnings.length > 0 && (
                  <section aria-label="Evidence warnings" className="space-y-2">
                    <h2>Evidence limits</h2>
                    {evidence.warnings.map((warning) => (
                      <p key={warning.code} className="ops-banner ops-banner-warning">
                        {warning.message}
                      </p>
                    ))}
                  </section>
                )}

                <section
                  className="journey-observations rounded-2xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] p-5 space-y-3"
                  aria-label="Independent source observations"
                >
                  <h2>Independent observations</h2>
                  <ul className="grid grid-cols-1 sm:grid-cols-2 gap-3 list-none p-0 m-0">
                    {evidence.sources.map((source) => (
                      <li
                        key={source.name}
                        className="rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/50 px-4 py-3.5 space-y-1.5"
                      >
                        <div className="flex items-center justify-between gap-3 text-xs">
                          <strong className="font-semibold text-[var(--text-primary)] min-w-0 truncate">
                            {source.name}
                          </strong>
                          <span className="console-pill console-pill-neutral shrink-0">
                            {source.status.replaceAll("_", " ")}
                          </span>
                        </div>
                        <p className="text-xs text-[var(--text-tertiary)] m-0 ops-mono">
                          {source.observedAt ? `Observed ${time(source.observedAt)}` : "Not observed"}
                        </p>
                        {source.errorCode && (
                          <p className="observation-error m-0">{source.errorCode}</p>
                        )}
                      </li>
                    ))}
                  </ul>
                </section>
              </div>

              <details
                className="journey-details rounded-2xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] p-5"
                id="journey-detailed-evidence"
              >
                <summary className="cursor-pointer font-semibold text-sm text-[var(--text-primary)] select-none">
                  Detailed evidence{" "}
                  <span className="ml-2 text-xs font-normal text-[var(--text-tertiary)]">
                    Transitions, bank operations & recovery
                  </span>
                </summary>
                <div className="journey-detail-content mt-5 space-y-6">
                  <Timeline<EvidenceTransition>
                    title="State transitions"
                    entries={evidence.transitions}
                    entryKey={(entry) => entry.id}
                    entryId={(entry) => `evidence-${entry.evidenceId}`}
                    when={(entry) => entry.occurredAt}
                    heading={(entry) => `${entry.fromState} → ${entry.toState}`}
                    detail={(entry) => (
                      <details>
                        <summary className="cursor-pointer">
                          Sequence {entry.sequence} · Evidence identity
                        </summary>
                        <span className="ops-mono">{entry.evidenceId}</span>
                      </details>
                    )}
                  />
                  {evidence.transitionsTruncated && (
                    <p className="ops-banner ops-banner-warning">
                      Only the first 200 transitions were returned.
                    </p>
                  )}

                  <section>
                    <h2>Bank operations</h2>
                    {evidence.operations.length === 0 ? (
                      <p className="ops-muted">
                        No bank operations were collected in this snapshot.
                      </p>
                    ) : (
                      <DataTable<EvidenceOperation>
                        caption="Original bank operations and observed participant status"
                        rows={evidence.operations}
                        rowKey={(operation) => operation.operationId}
                        rowId={(operation) => `evidence-${operation.evidenceId}`}
                        columns={[
                          {
                            key: "id",
                            heading: "Operation",
                            render: (operation) => (
                              <details>
                                <summary className="cursor-pointer">Reference</summary>
                                <span className="ops-mono">{operation.operationId}</span>
                              </details>
                            ),
                          },
                          {
                            key: "kind",
                            heading: "Type / bank",
                            render: (operation) =>
                              `${operation.operationType} · ${operation.bankCode}`,
                          },
                          {
                            key: "central",
                            heading: "Central",
                            render: (operation) => operation.centralStatus,
                          },
                          {
                            key: "participant",
                            heading: "Participant",
                            render: (operation) => operation.participantStatus ?? "Unknown",
                          },
                          {
                            key: "presence",
                            heading: "Presence",
                            render: (operation) => operation.presence.replaceAll("_", " "),
                          },
                          {
                            key: "observed",
                            heading: "Observed",
                            render: (operation) => (
                              <span className="ops-mono text-xs">
                                {time(operation.lastObservedAt)}
                              </span>
                            ),
                          },
                        ]}
                      />
                    )}
                  </section>

                  <section className="space-y-2">
                    <h2>Recovery</h2>
                    <p className="text-xs text-[var(--text-secondary)]">
                      {evidence.recovery.status.replaceAll("_", " ")} ·{" "}
                      <span className="ops-mono tabular-nums">
                        {evidence.recovery.attemptCount}
                      </span>{" "}
                      attempts
                    </p>
                    <p className="text-xs text-[var(--text-secondary)]">
                      Next attempt: {time(evidence.recovery.nextAttemptAt)}
                    </p>
                    {evidence.recovery.reasonCode && (
                      <p className="text-xs text-[var(--text-secondary)]">
                        Reason: <span className="ops-mono">{evidence.recovery.reasonCode}</span>
                      </p>
                    )}
                    {attempts.error && (
                      <InlineError message={`Attempt history: ${attempts.error}`} />
                    )}
                    {attemptPage && (
                      <DataTable<RecoveryAttempt>
                        caption="Recorded recovery attempts"
                        rows={attemptPage.items}
                        rowKey={(attempt) => attempt.attemptId}
                        columns={[
                          {
                            key: "started",
                            heading: "Started",
                            render: (attempt) => (
                              <span className="ops-mono text-xs">{time(attempt.startedAt)}</span>
                            ),
                          },
                          {
                            key: "result",
                            heading: "Result",
                            render: (attempt) => attempt.result,
                          },
                          {
                            key: "from",
                            heading: "State before",
                            render: (attempt) => attempt.stateBefore,
                          },
                          {
                            key: "after",
                            heading: "State after",
                            render: (attempt) => attempt.stateAfter ?? "Unknown",
                          },
                          {
                            key: "reason",
                            heading: "Reason",
                            render: (attempt) => attempt.reasonCode ?? "—",
                          },
                        ]}
                      />
                    )}
                    {attemptPage?.nextCursor && (
                      <p className="ops-muted">
                        More attempts are available from the API. This view shows the first page.
                      </p>
                    )}
                  </section>

                  <section className="space-y-1.5 text-xs text-[var(--text-secondary)]">
                    <h2>Related checks and telemetry</h2>
                    <p>
                      Reconciliation: {evidence.reconciliation.sourceStatus.replaceAll("_", " ")};{" "}
                      {evidence.reconciliation.discrepancyCount === null
                        ? "count unavailable"
                        : `${evidence.reconciliation.discrepancyCount} recorded discrepancies`}
                      .
                    </p>
                    {evidence.reconciliation.scopeMismatch && (
                      <p className="ops-banner ops-banner-warning">
                        The available reconciliation scope does not match this payment.
                      </p>
                    )}
                    <p>
                      Integrity: {evidence.integrity.sourceStatus.replaceAll("_", " ")};{" "}
                      {evidence.integrity.violationCount === null
                        ? "count unavailable"
                        : `${evidence.integrity.violationCount} global violations`}
                      .
                    </p>
                    <p className="ops-muted">
                      Checks and traces provide context; they do not establish this payment’s
                      financial outcome.
                    </p>
                  </section>

                  <TracePanel
                    traceIds={evidence.traces.traceIds}
                    sourceStatus={evidence.traces.sourceStatus}
                    token={token}
                  />

                  {isAdmin && (
                    <section className="rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)] p-4 space-y-3">
                      <h2>Schedule a recheck</h2>
                      <p className="text-xs text-[var(--text-secondary)]">
                        This asks the worker to recheck later. It does not settle money in this
                        browser request.
                      </p>
                      <label className="field">
                        Reason{" "}
                        <input
                          value={reason}
                          onChange={(event) => setReason(event.target.value)}
                          maxLength={200}
                          minLength={1}
                        />
                      </label>
                      <Button disabled={rechecking || !reason.trim()} onClick={scheduleRecheck}>
                        {rechecking ? "Scheduling…" : "Schedule recheck"}
                      </Button>
                      {recheckStatus && <p role="status">{recheckStatus}</p>}
                      {recheckError && <InlineError message={recheckError} />}
                    </section>
                  )}

                  <section className="space-y-2">
                    <h2>Export redacted evidence</h2>
                    <Button variant="secondary" onClick={exportEvidence}>
                      Download server export
                    </Button>
                    {exportedAt && (
                      <p role="status" className="text-xs text-[var(--status-success-fg)]">
                        Export received {time(exportedAt)}. The downloaded file contains the server
                        response.
                      </p>
                    )}
                    {exportError && <InlineError message={exportError} />}
                  </section>
                </div>
              </details>
            </div>

            <aside
              className="journey-rail"
              id="payment-investigation"
              tabIndex={-1}
              aria-label="Investigation workspace"
            >
              <p className="text-[11px] font-mono font-semibold uppercase tracking-wider text-[var(--accent-strong)] m-0 mb-2">
                Read-only investigation
              </p>
              <InvestigationPanel
                paymentId={paymentId}
                snapshotId={evidence.snapshotId}
                evidenceIds={[
                  evidence.payment.evidenceId,
                  ...evidence.operations.map((operation) => operation.evidenceId),
                  ...evidence.transitions.map((transition) => transition.evidenceId),
                ]}
                token={token}
                onNavigate={onNavigate}
              />
            </aside>
          </div>
        </>
      )}
    </div>
  );
}
