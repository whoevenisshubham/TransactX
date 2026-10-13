import { useCallback, useEffect, useRef, useState } from "react";
import { Button, InlineError } from "../components";
import { DataTable } from "../design/OpsPrimitives";
import { liveOpsClient } from "./client";
import type { Investigation } from "./contracts";
import { useResource } from "./useResource";

export function InvestigationPanel({ paymentId, snapshotId, evidenceIds, token, onNavigate }: {
  paymentId: string; snapshotId: string; evidenceIds: string[]; token: string; onNavigate: (path: string) => void;
}) {
  const [question, setQuestion] = useState("");
  const [jobId, setJobId] = useState<string | null>(null);
  const [submittedQuestion, setSubmittedQuestion] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const attempt = useRef<{ paymentId: string; question: string; key: string } | null>(null);
  const inFlight = useRef(false);

  async function submit() {
    const normalized = question.trim();
    if (!normalized || normalized.length > 1000 || inFlight.current) return;
    if (jobId && submittedQuestion === normalized) return;
    inFlight.current = true; setSubmitting(true); setError(null);
    try {
      if (attempt.current?.paymentId !== paymentId || attempt.current.question !== normalized) {
        attempt.current = { paymentId, question: normalized, key: crypto.randomUUID() };
      }
      const accepted = await liveOpsClient.createInvestigation(paymentId, normalized, attempt.current.key, token);
      setJobId(accepted.investigationId);
      setSubmittedQuestion(normalized);
      attempt.current = null;
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "Could not start an investigation.");
    } finally { inFlight.current = false; setSubmitting(false); }
  }

  return <section aria-label="Payment investigation">
    <h2>Investigate this payment</h2>
    <p>Questions stay scoped to this payment. Reports explain collected evidence; they cannot change settlement.</p>
    <label className="field">Question
      <textarea value={question} onChange={(event) => setQuestion(event.target.value)} maxLength={1000} rows={3} placeholder="What evidence explains the current pending state?" />
    </label>
    <Button disabled={submitting || !question.trim() || (jobId !== null && submittedQuestion === question.trim())} onClick={submit}>{submitting ? "Starting…" : "Start investigation"}</Button>
    {error && <InlineError message={error} />}
    {jobId && <>
      <Button variant="quiet" onClick={() => onNavigate(`/console/investigations/${jobId}?paymentId=${encodeURIComponent(paymentId)}`)}>Open saved investigation</Button>
      <InvestigationJob key={jobId} id={jobId} snapshotId={snapshotId} evidenceIds={evidenceIds} token={token} />
    </>}
  </section>;
}

function EvidenceCitation({ id, available }: { id: string; available: boolean }) {
  return available
    ? <button type="button" className="ops-citation" onClick={() => document.getElementById(`evidence-${id}`)?.scrollIntoView({ behavior: "smooth", block: "center" })}>View evidence {id}</button>
    : <span className="ops-muted">Evidence {id} is not in this snapshot</span>;
}

export function InvestigationJob({ id, snapshotId, evidenceIds, token, paymentId }: {
  id: string; snapshotId?: string; evidenceIds: string[]; token: string; paymentId?: string;
}) {
  const load = useCallback((signal: AbortSignal) => liveOpsClient.investigation(id, token, signal), [id, token]);
  const job = useResource(id, load);
  const status = job.data?.status;
  useEffect(() => {
    if (status !== "queued" && status !== "running") return;
    const timer = window.setInterval(() => { if (!document.hidden) job.refresh(); }, 3000);
    return () => window.clearInterval(timer);
  }, [status, job.refresh]);

  const retainedEvidenceIds = job.data?.snapshot
    ? [
        job.data.snapshot.payment.evidenceId,
        ...job.data.snapshot.operations.map((op) => op.evidenceId),
        ...job.data.snapshot.transitions.map((tr) => tr.evidenceId),
      ]
    : [];
  const available = new Set([...evidenceIds, ...retainedEvidenceIds]);
  const effectiveSnapshotId = snapshotId ?? job.data?.snapshot?.snapshotId;
  const report = job.data?.report;
  return <div className="ops-investigation">
    <p className="ops-mono">Investigation {id}</p>
    {job.loading && <p role="status">Loading investigation…</p>}
    {job.error && <InlineError message={job.error} />}
    {job.data && <p role="status">Status: {job.data.status} · Mode: {job.data.mode === "evidence_only" ? "Evidence summary" : "LLM investigation"}</p>}
    {status === "queued" || status === "running" ? <p>Evidence gathering is in progress. This view will refresh.</p> : null}
    {job.data?.error && <InlineError message={job.data.error.message} />}
    {job.data && <details><summary>Report execution details</summary>
      <p>Prompt version: {job.data.usage.promptVersion}</p>
      <p>Model: {job.data.usage.model ?? "No model call recorded"}</p>
      <p>Input tokens: {job.data.usage.inputTokens ?? "Not reported"} · Output tokens: {job.data.usage.outputTokens ?? "Not reported"}</p>
    </details>}
    {report && <>
      {effectiveSnapshotId && report.snapshotId !== effectiveSnapshotId && <p className="ops-banner ops-banner-warning" role="status">This report uses an older evidence snapshot. Refresh or start a new investigation for the current payment view.</p>}
      <p>Coverage: {report.evidenceCoverage.replaceAll("_", " ")} · Collected {new Date(report.collectedAt).toLocaleString()}</p>
      <p><strong>{report.summary}</strong></p>
      <h3>Facts</h3>
      {report.facts.length === 0 && <p>No grounded facts were returned.</p>}
      <ul>{report.facts.map((fact, index) => <li key={index}>
        {fact.text} {fact.contradiction && <strong>Contradictory evidence</strong>}
        <div className="ops-citations">{fact.evidenceIds.map((evidenceId) => paymentId && available.has(evidenceId)
          ? <a className="ops-citation" key={evidenceId} href={`/console/payments/${paymentId}#evidence-${encodeURIComponent(evidenceId)}`}>View evidence {evidenceId}</a>
          : <EvidenceCitation key={evidenceId} id={evidenceId} available={available.has(evidenceId)} />)}</div>
      </li>)}</ul>
      <h3>Interpretation</h3>
      {report.inferences.length === 0 && <p>No inference was included.</p>}
      <ul>{report.inferences.map((inference, index) => <li key={index}>
        <strong>Inference:</strong> {inference.text} {inference.contradiction && <strong>Contradictory evidence</strong>}
        <div className="ops-citations">{inference.evidenceIds.map((evidenceId) => paymentId && available.has(evidenceId)
          ? <a className="ops-citation" key={evidenceId} href={`/console/payments/${paymentId}#evidence-${encodeURIComponent(evidenceId)}`}>View evidence {evidenceId}</a>
          : <EvidenceCitation key={evidenceId} id={evidenceId} available={available.has(evidenceId)} />)}</div>
      </li>)}</ul>
      <h3>Suggested next checks</h3>
      {report.recommendations.length === 0 && <p>No further check was suggested.</p>}
      <ul>{report.recommendations.map((recommendation, index) => <li key={index}>{recommendation.text}
        {recommendation.runbookChunkIds.length > 0 && <span className="ops-muted"> · Runbook references: {recommendation.runbookChunkIds.join(", ")}</span>}
      </li>)}</ul>
      {report.missingEvidence.length > 0 && <p className="ops-banner ops-banner-warning">Missing evidence: {report.missingEvidence.join(", ")}</p>}
    </>}
    {job.data && job.data.toolTimeline.length > 0 && <DataTable<Investigation["toolTimeline"][number]> caption="Investigator tool timeline" rows={job.data.toolTimeline} rowKey={(step) => `${step.toolName}:${step.startedAt}`} columns={[
      { key: "tool", heading: "Tool", render: (step) => step.toolName },
      { key: "time", heading: "Started", render: (step) => new Date(step.startedAt).toLocaleString() },
      { key: "status", heading: "Result", render: (step) => step.status },
      { key: "evidence", heading: "Evidence IDs", render: (step) => step.evidenceIds.join(", ") || "—" },
    ]} />}
  </div>;
}
