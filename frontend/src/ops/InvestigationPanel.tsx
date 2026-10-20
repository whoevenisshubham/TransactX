import { useCallback, useEffect, useRef, useState } from "react";
import { Button, InlineError } from "../components";
import { DataTable } from "../design/OpsPrimitives";
import { liveOpsClient } from "./client";
import type { Investigation } from "./contracts";
import { useResource } from "./useResource";
import { focusEvidence } from "./focusEvidence";

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

  const composer = <div className="investigator-composer">
    <p>Questions stay scoped to this payment. Reports explain collected evidence; they cannot change settlement.</p>
    <div className="ops-question-chips" role="group" aria-label="Investigation starters">{["Why is this payment pending?", "What evidence is missing?", "What did each bank report?"].map((suggestion) => <button type="button" key={suggestion} onClick={() => setQuestion(suggestion)}>{suggestion}</button>)}</div>
    <label className="field">Question
      <textarea aria-label="Question" value={question} onChange={(event) => setQuestion(event.target.value)} maxLength={1000} rows={3} placeholder="What evidence explains the current pending state?" />
    </label>
    <Button disabled={submitting || !question.trim() || (jobId !== null && submittedQuestion === question.trim())} onClick={submit}>{submitting ? "Starting…" : "Start investigation"}</Button>
    {error && <InlineError message={error} />}
  </div>;
  return <section aria-label="Payment investigation">
    <div className="investigator-heading"><span aria-hidden="true">✳</span><div><p className="section-kicker">EVIDENCE, EXPLAINED</p><h2>Investigate this payment</h2></div></div>
    {jobId ? <>
      <p className="investigation-submitted-question">{submittedQuestion}</p>
      <Button variant="quiet" onClick={() => onNavigate(`/console/investigations/${jobId}?paymentId=${encodeURIComponent(paymentId)}`)}>Open saved investigation ↗</Button>
      <InvestigationJob key={jobId} id={jobId} snapshotId={snapshotId} evidenceIds={evidenceIds} token={token} />
      <details className="investigator-new-question"><summary>Ask another scoped question</summary>{composer}</details>
    </> : composer}
  </section>;
}

function EvidenceCitation({ id, available, label }: { id: string; available: boolean; label: string }) {
  return available
    ? <button type="button" className="ops-citation" title={id} onClick={() => {
      focusEvidence(`evidence-${id}`);
    }}>{label}</button>
    : <span className="ops-muted" title={id}>{label} · Not in this snapshot</span>;
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
  const citationIds = [...new Set([...(report?.facts ?? []), ...(report?.inferences ?? [])].flatMap(item => item.evidenceIds))];
  const citationLabel = (id: string) => `${id.startsWith("operation:") ? "Bank operation" : id.startsWith("transition:") ? "State transition" : id.startsWith("payment:") ? "Payment record" : "Evidence"} [${citationIds.indexOf(id) + 1}]`;
  return <div className="ops-investigation">

    {job.loading && <p role="status">Loading investigation…</p>}
    {job.error && <InlineError message={job.error} />}
    {job.data && <p role="status">Status: {job.data.status} · Mode: {(report?.mode ?? job.data.mode) === "evidence_only" ? "Evidence summary" : "LLM investigation"}</p>}
    {status === "queued" || status === "running" ? <p>Evidence gathering is in progress. This view will refresh.</p> : null}
    {job.data?.error && <InlineError message={job.data.error.message} />}
    {job.data && (report?.mode ?? job.data.mode) === "evidence_only" && <p className="ops-muted">This is a report from collected records, not a Gemini-generated answer. {job.data.usage.model ? "See execution details for recorded model usage." : "This report contains no accepted model answer; a provider attempt may still have occurred."}</p>}
    {report && <div className="investigation-report">
      {effectiveSnapshotId && report.snapshotId !== effectiveSnapshotId && <p className="ops-banner ops-banner-warning" role="status">This report uses an older evidence snapshot. Refresh or start a new investigation for the current payment view.</p>}
      <p>Coverage: {report.evidenceCoverage.replaceAll("_", " ")} · Collected {new Date(report.collectedAt).toLocaleString()}</p>
      <div className="investigation-conclusion"><p className="eyebrow">Conclusion from collected evidence</p><p><strong>{report.summary}</strong></p></div>
      <h3>Facts</h3>
      {report.facts.length === 0 && <p>No grounded facts were returned.</p>}
      <ul>{report.facts.map((fact, index) => <li key={index}>
        {fact.text} {fact.contradiction && <strong>Contradictory evidence</strong>}
        <div className="ops-citations">{fact.evidenceIds.map((evidenceId) => paymentId && available.has(evidenceId)
          ? <a className="ops-citation" title={evidenceId} key={evidenceId} href={`/console/payments/${paymentId}#evidence-${encodeURIComponent(evidenceId)}`}>{citationLabel(evidenceId)}</a>
          : <EvidenceCitation key={evidenceId} id={evidenceId} label={citationLabel(evidenceId)} available={available.has(evidenceId)} />)}</div>
      </li>)}</ul>
      <h3>Interpretation</h3>
      {report.inferences.length === 0 && <p>No inference was included.</p>}
      <ul>{report.inferences.map((inference, index) => <li key={index}>
        <strong>Inference:</strong> {inference.text} {inference.contradiction && <strong>Contradictory evidence</strong>}
        <div className="ops-citations">{inference.evidenceIds.map((evidenceId) => paymentId && available.has(evidenceId)
          ? <a className="ops-citation" title={evidenceId} key={evidenceId} href={`/console/payments/${paymentId}#evidence-${encodeURIComponent(evidenceId)}`}>{citationLabel(evidenceId)}</a>
          : <EvidenceCitation key={evidenceId} id={evidenceId} label={citationLabel(evidenceId)} available={available.has(evidenceId)} />)}</div>
      </li>)}</ul>
      <h3>Suggested next checks</h3>
      {report.recommendations.length === 0 && <p>No further check was suggested.</p>}
      <ul>{report.recommendations.map((recommendation, index) => <li key={index}>{recommendation.text}
        {recommendation.runbookChunkIds.length > 0 && <span className="ops-muted"> · Runbook references: {recommendation.runbookChunkIds.join(", ")}</span>}
      </li>)}</ul>
      {report.missingEvidence.length > 0 && <p className="ops-banner ops-banner-warning">Missing evidence: {report.missingEvidence.join(", ")}</p>}
    </div>}
    <details className="investigation-identity"><summary>Saved report identity</summary><p className="ops-mono">Investigation {id}</p></details>
    {job.data && <details><summary>Report execution details</summary>
      <p>Prompt version: {job.data.usage.promptVersion || "Not recorded"}</p>
      <p>Model: {job.data.usage.model ?? "Not recorded in this report"}</p>
      <p>Input tokens: {job.data.usage.inputTokens ?? "Not reported"} · Output tokens: {job.data.usage.outputTokens ?? "Not reported"}</p>
    </details>}
    {job.data && job.data.toolTimeline.length > 0 && <details className="investigation-tool-details"><summary>Collected sources and tool timeline</summary><DataTable<Investigation["toolTimeline"][number]> caption="Investigator tool timeline" rows={job.data.toolTimeline} rowKey={(step) => `${step.toolName}:${step.startedAt}`} columns={[
      { key: "tool", heading: "Tool", render: (step) => step.toolName },
      { key: "time", heading: "Started", render: (step) => new Date(step.startedAt).toLocaleString() },
      { key: "status", heading: "Result", render: (step) => step.status },
      { key: "evidence", heading: "Evidence IDs", render: (step) => step.evidenceIds.join(", ") || "—" },
    ]} /></details>}
  </div>;
}
