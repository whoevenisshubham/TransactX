import { useCallback, useEffect, useRef, useState } from "react";
import { Button, Card, EmptyState, InlineError, PageHeader } from "../components";
import { DataTable, formatPaiseDecimal, OpsStatusBadge, Timeline } from "../design/OpsPrimitives";
import { liveOpsClient } from "./client";
import type { EvidenceOperation, EvidenceSource, EvidenceTransition, RecoveryAttempt } from "./contracts";
import { useResource } from "./useResource";
import { TracePanel } from "./TracePanel";
import { InvestigationPanel } from "./InvestigationPanel";

function time(value: string | null) { return value ? new Date(value).toLocaleString() : "Not observed"; }
function optional(value: string | null) { return value ?? "Unknown"; }

export function PaymentEvidencePage({ paymentId, token, isAdmin, liveRevision, onNavigate }: {
  paymentId: string; token: string; isAdmin: boolean; liveRevision: number;
  onNavigate: (path: string) => void;
}) {
  const load = useCallback((signal: AbortSignal) => liveOpsClient.evidence(paymentId, token, signal), [paymentId, token]);
  const detail = useResource(paymentId, load);
  const loadAttempts = useCallback((signal: AbortSignal) => liveOpsClient.recoveryAttempts(paymentId, token, undefined, signal), [paymentId, token]);
  const attempts = useResource(paymentId, loadAttempts);
  const [reason, setReason] = useState("");
  const [recheckStatus, setRecheckStatus] = useState<string | null>(null);
  const [recheckError, setRecheckError] = useState<string | null>(null);
  const [rechecking, setRechecking] = useState(false);
  const [exportError, setExportError] = useState<string | null>(null);
  const [exportedAt, setExportedAt] = useState<string | null>(null);
  const recheckAttempt = useRef<{ paymentId: string; reason: string; key: string } | null>(null);

  useEffect(() => {
    if (!liveRevision || document.hidden) return;
    const timer = window.setTimeout(() => { detail.refresh(); attempts.refresh(); }, 350);
    return () => window.clearTimeout(timer);
  }, [liveRevision, detail.refresh, attempts.refresh]);
  useEffect(() => {
    const timer = window.setInterval(() => {
      if (!document.hidden) { detail.refresh(); attempts.refresh(); }
    }, 15000);
    return () => window.clearInterval(timer);
  }, [detail.refresh, attempts.refresh]);
  useEffect(() => {
    setReason(""); setRecheckStatus(null); setRecheckError(null); setExportError(null); setExportedAt(null);
  }, [paymentId]);

  const evidence = detail.key === paymentId ? detail.data : null;
  const attemptPage = attempts.key === paymentId ? attempts.data : null;
  useEffect(() => {
    if (!evidence || !window.location.hash.startsWith("#evidence-")) return;
    try {
      const id = decodeURIComponent(window.location.hash.slice(1));
      window.setTimeout(() => document.getElementById(id)?.scrollIntoView({ block: "center" }), 0);
    } catch { /* Ignore malformed link fragments. */ }
  }, [evidence]);

  async function scheduleRecheck() {
    if (!isAdmin || rechecking) return;
    setRechecking(true); setRecheckError(null); setRecheckStatus(null);
    try {
      const normalizedReason = reason.trim();
      if (recheckAttempt.current?.paymentId !== paymentId || recheckAttempt.current.reason !== normalizedReason) {
        recheckAttempt.current = { paymentId, reason: normalizedReason, key: crypto.randomUUID() };
      }
      const result = await liveOpsClient.recheck(paymentId, normalizedReason, recheckAttempt.current.key, token);
      recheckAttempt.current = null;
      setRecheckStatus(result.status === "scheduled" ? `Recheck scheduled at ${time(result.scheduledAt)}. Payment outcome is unchanged until new evidence arrives.` : "This payment is already terminal; no recheck was scheduled.");
      detail.refresh(); attempts.refresh();
    } catch (error) {
      setRecheckError(error instanceof Error ? error.message : "Could not schedule the recheck.");
    } finally { setRechecking(false); }
  }

  async function exportEvidence() {
    setExportError(null); setExportedAt(null);
    try {
      const payload = await liveOpsClient.exportEvidence(paymentId, token);
      const blob = new Blob([JSON.stringify(payload, null, 2)], { type: "application/json" });
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url; anchor.download = `transactx-evidence-${paymentId}.json`;
      document.body.append(anchor); anchor.click(); anchor.remove();
      window.setTimeout(() => URL.revokeObjectURL(url), 1000);
      setExportedAt(new Date().toISOString());
    } catch (error) {
      setExportError(error instanceof Error ? error.message : "Could not export evidence.");
    }
  }

  return <div className="ops-page">
    <PageHeader eyebrow="Payment evidence" title="Payment journey" description={paymentId} action={<Button variant="secondary" onClick={() => {
      const from = window.history.state?.from;
      onNavigate(typeof from === "string" && from.startsWith("/console/worklist") ? from : "/console/worklist");
    }}>Back to worklist</Button>} />
    {detail.loading && !evidence && <p role="status">Loading payment evidence…</p>}
    {detail.error && <InlineError message={detail.error} />}
    {detail.stale && evidence && <p role="status" className="ops-banner ops-banner-warning">Showing the last collected evidence while refreshing. Collected {time(evidence.collectedAt)}.</p>}
    {!evidence && !detail.loading && !detail.error && <EmptyState title="Evidence unavailable" description="Try refreshing this payment." />}
    {evidence && <>
      <p className="ops-muted">Collected {time(evidence.collectedAt)} · Snapshot <span className="ops-mono">{evidence.snapshotId}</span></p>
      <div className="ops-evidence-grid">
        <Card variant="bordered" id={`evidence-${evidence.payment.evidenceId}`}><h2>Central payment</h2>
          <p><OpsStatusBadge state={evidence.payment.state} /></p>
          <p className="amount amount-prominent">{formatPaiseDecimal(evidence.payment.amountPaise, evidence.payment.currency)}</p>
          <p>Created {time(evidence.payment.createdAt)}</p>
          <p>Last updated {time(evidence.payment.updatedAt)}</p>
          <p className="ops-muted">Central evidence ID: <span className="ops-mono">{evidence.payment.evidenceId}</span></p>
        </Card>
        <Card variant="bordered"><h2>Original route</h2>
          <p>{optional(evidence.route.sourceBankCode)} → {optional(evidence.route.destinationBankCode)}</p>
          <p>Execution target: <span className="ops-mono">{optional(evidence.route.executionTargetId)}</span></p>
          <p>Selected {time(evidence.route.selectedAt)}</p>
          <p>Source: {evidence.route.status.replaceAll("_", " ")}</p>
        </Card>
      </div>
      {(evidence.payment.state === "PENDING_RECONCILIATION" || evidence.payment.state === "BANK_SETTLED_CENTRAL_PENDING") &&
        <p className="ops-banner ops-banner-warning" role="status">This payment has no safely established final result. An unavailable or unknown bank observation does not prove failure.</p>}
      {evidence.warnings.length > 0 && <section aria-label="Evidence warnings"><h2>Evidence limits</h2>{evidence.warnings.map((warning) => <p key={warning.code} className="ops-banner ops-banner-warning">{warning.message}</p>)}</section>}
      <DataTable<EvidenceSource> caption="Source availability and observation time" rows={evidence.sources} rowKey={(source) => source.name} columns={[
        { key: "source", heading: "Source", render: (source) => source.name },
        { key: "status", heading: "Availability", render: (source) => source.status.replaceAll("_", " ") },
        { key: "observed", heading: "Observed", render: (source) => time(source.observedAt) },
        { key: "error", heading: "Error code", render: (source) => source.errorCode ?? "—" },
      ]} />
      <Timeline<EvidenceTransition> title="State transitions" entries={evidence.transitions} entryKey={(entry) => entry.id} entryId={(entry) => `evidence-${entry.evidenceId}`} when={(entry) => entry.occurredAt} heading={(entry) => `${entry.fromState} → ${entry.toState}`} detail={(entry) => <span className="ops-mono">Sequence {entry.sequence} · {entry.evidenceId}</span>} />
      {evidence.transitionsTruncated && <p className="ops-banner ops-banner-warning">Only the first 200 transitions were returned.</p>}
      <h2>Bank operations</h2>
      {evidence.operations.length === 0 ? <p>No bank operations were collected in this snapshot.</p> : <DataTable<EvidenceOperation> caption="Original bank operations and observed participant status" rows={evidence.operations} rowKey={(operation) => operation.operationId} rowId={(operation) => `evidence-${operation.evidenceId}`} columns={[
        { key: "id", heading: "Operation", render: (operation) => <span className="ops-mono">{operation.operationId}</span> },
        { key: "kind", heading: "Type / bank", render: (operation) => `${operation.operationType} · ${operation.bankCode}` },
        { key: "central", heading: "Central", render: (operation) => operation.centralStatus },
        { key: "participant", heading: "Participant", render: (operation) => operation.participantStatus ?? "Unknown" },
        { key: "presence", heading: "Presence", render: (operation) => operation.presence.replaceAll("_", " ") },
        { key: "observed", heading: "Observed", render: (operation) => time(operation.lastObservedAt) },
      ]} />}
      <section><h2>Recovery</h2>
        <p>{evidence.recovery.status.replaceAll("_", " ")} · {evidence.recovery.attemptCount} attempts</p>
        <p>Next attempt: {time(evidence.recovery.nextAttemptAt)}</p>
        {evidence.recovery.reasonCode && <p>Reason: {evidence.recovery.reasonCode}</p>}
        {attempts.error && <InlineError message={`Attempt history: ${attempts.error}`} />}
        {attemptPage && <DataTable<RecoveryAttempt> caption="Recorded recovery attempts" rows={attemptPage.items} rowKey={(attempt) => attempt.attemptId} columns={[
          { key: "started", heading: "Started", render: (attempt) => time(attempt.startedAt) },
          { key: "result", heading: "Result", render: (attempt) => attempt.result },
          { key: "from", heading: "State before", render: (attempt) => attempt.stateBefore },
          { key: "after", heading: "State after", render: (attempt) => attempt.stateAfter ?? "Unknown" },
          { key: "reason", heading: "Reason", render: (attempt) => attempt.reasonCode ?? "—" },
        ]} />}
        {attemptPage?.nextCursor && <p className="ops-muted">More attempts are available from the API. This view shows the first page.</p>}
      </section>
      <section><h2>Related checks and telemetry</h2>
        <p>Reconciliation: {evidence.reconciliation.sourceStatus.replaceAll("_", " ")}; {evidence.reconciliation.discrepancyCount === null ? "count unavailable" : `${evidence.reconciliation.discrepancyCount} recorded discrepancies`}.</p>
        {evidence.reconciliation.scopeMismatch && <p className="ops-banner ops-banner-warning">The available reconciliation scope does not match this payment.</p>}
        <p>Integrity: {evidence.integrity.sourceStatus.replaceAll("_", " ")}; {evidence.integrity.violationCount === null ? "count unavailable" : `${evidence.integrity.violationCount} global violations`}.</p>
        <p className="ops-muted">Checks and traces provide context; they do not establish this payment’s financial outcome.</p>
      </section>
      <TracePanel traceIds={evidence.traces.traceIds} sourceStatus={evidence.traces.sourceStatus} token={token} />
      <InvestigationPanel paymentId={paymentId} snapshotId={evidence.snapshotId} evidenceIds={[evidence.payment.evidenceId, ...evidence.operations.map((operation) => operation.evidenceId), ...evidence.transitions.map((transition) => transition.evidenceId)]} token={token} onNavigate={onNavigate} />
      {isAdmin && <section><h2>Schedule a recheck</h2>
        <p>This asks the worker to recheck later. It does not settle money in this browser request.</p>
        <label className="field">Reason <input value={reason} onChange={(event) => setReason(event.target.value)} maxLength={200} minLength={1} /></label>
        <Button disabled={rechecking || !reason.trim()} onClick={scheduleRecheck}>{rechecking ? "Scheduling…" : "Schedule recheck"}</Button>
        {recheckStatus && <p role="status">{recheckStatus}</p>}
        {recheckError && <InlineError message={recheckError} />}
      </section>}
      <section><h2>Export redacted evidence</h2>
        <Button variant="secondary" onClick={exportEvidence}>Download server export</Button>
        {exportedAt && <p role="status">Export received {time(exportedAt)}. The downloaded file contains the server response.</p>}
        {exportError && <InlineError message={exportError} />}
      </section>
    </>}
  </div>;
}
