import { useCallback } from "react";
import { Button, InlineError, PageHeader } from "../components";
import { liveOpsClient } from "./client";
import { InvestigationJob } from "./InvestigationPanel";
import { useResource } from "./useResource";

export function InvestigationPage({ id, token, onNavigate }: { id: string; token: string; onNavigate: (path: string) => void }) {
  const linkedPaymentId = new URLSearchParams(window.location.search).get("paymentId");
  const validPaymentId = linkedPaymentId && /^[0-9a-f-]{36}$/i.test(linkedPaymentId) ? linkedPaymentId : null;
  return <div className="ops-page">
    <PageHeader eyebrow="Operations" title="Saved investigation" description={id} action={<Button variant="secondary" onClick={() => onNavigate("/console/worklist")}>Back to worklist</Button>} />
    {validPaymentId ? <LinkedInvestigation id={id} paymentId={validPaymentId} token={token} /> : <>
      <p className="ops-banner ops-banner-warning">This link has no valid payment context. The report remains available; payment citations cannot be opened here.</p>
      <InvestigationJob id={id} evidenceIds={[]} token={token} />
    </>}
  </div>;
}

function LinkedInvestigation({ id, paymentId, token }: { id: string; paymentId: string; token: string }) {
  const load = useCallback((signal: AbortSignal) => liveOpsClient.evidence(paymentId, token, signal), [paymentId, token]);
  const evidence = useResource(paymentId, load);
  const record = evidence.data;
  return <>
    {evidence.loading && <p role="status">Loading linked payment evidence…</p>}
    {evidence.error && <InlineError message={`Linked payment evidence is unavailable: ${evidence.error}`} />}
    <InvestigationJob id={id} snapshotId={record?.snapshotId} paymentId={record ? paymentId : undefined} evidenceIds={record ? [record.payment.evidenceId, ...record.operations.map((operation) => operation.evidenceId), ...record.transitions.map((transition) => transition.evidenceId)] : []} token={token} />
  </>;
}
