import { useCallback, useEffect, useState, type FormEvent } from "react";
import { Button, EmptyState, InlineError, PageHeader } from "../components";
import { DataTable, formatPaiseDecimal, OpsStatusBadge } from "../design/OpsPrimitives";
import { liveOpsClient } from "./client";
import type { WorklistFilters, WorklistItem } from "./contracts";
import { useResource } from "./useResource";
import { isoToLocalInput, localInputToIso, parseWorklistFilters, paymentStates, recoveryStatuses, worklistSearch } from "./worklistFilters";

export function WorklistPage({ token, liveRevision, onNavigate }: {
  token: string; liveRevision: number; onNavigate: (path: string) => void;
}) {
  const [locationState, setLocationState] = useState(() => parseWorklistFilters(window.location.search));
  const [draft, setDraft] = useState<WorklistFilters>(locationState.filters);
  const filters = locationState.filters;
  const key = worklistSearch(filters);
  const load = useCallback((signal: AbortSignal) => liveOpsClient.worklist(filters, token, signal), [key, token]);
  const resource = useResource(key, load);

  useEffect(() => {
    const onPop = () => setLocationState(parseWorklistFilters(window.location.search));
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, []);
  useEffect(() => setDraft(filters), [key]);
  useEffect(() => {
    if (!liveRevision || document.hidden) return;
    const timer = window.setTimeout(resource.refresh, 350);
    return () => window.clearTimeout(timer);
  }, [liveRevision, resource.refresh]);
  useEffect(() => {
    const timer = window.setInterval(() => { if (!document.hidden) resource.refresh(); }, 15000);
    return () => window.clearInterval(timer);
  }, [resource.refresh]);

  function navigateFilters(next: WorklistFilters) {
    const nextSearch = worklistSearch(next);
    window.history.pushState({}, "", `/console/worklist${nextSearch}`);
    setLocationState(parseWorklistFilters(nextSearch));
  }
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    navigateFilters({ ...draft, cursor: undefined });
  }

  const page = resource.key === key ? resource.data : null;
  const error = resource.key === key ? resource.error : null;
  return <div className="ops-page">
    <PageHeader eyebrow="Operations" title="Recovery worklist" description="Find payments that need a closer look. Each row reflects the last projection update; payment details fetch central truth separately." action={<Button variant="secondary" onClick={resource.refresh}>Refresh</Button>} />
    <form className="ops-controls" onSubmit={submit} aria-label="Worklist filters">
      <label>Payment state
        <select value={draft.state ?? ""} onChange={(event) => setDraft({ ...draft, state: event.target.value || undefined })}>
          <option value="">Attention by default</option>
          {paymentStates.map((state) => <option key={state} value={state}>{state.replaceAll("_", " ")}</option>)}
        </select>
      </label>
      <label>Recovery status
        <select value={draft.recoveryStatus ?? ""} onChange={(event) => setDraft({ ...draft, recoveryStatus: event.target.value as WorklistFilters["recoveryStatus"] || undefined })}>
          <option value="">Any</option>
          {recoveryStatuses.map((status) => <option key={status} value={status}>{status.replaceAll("_", " ")}</option>)}
        </select>
      </label>
      <label>Bank code
        <input value={draft.bankCode ?? ""} onChange={(event) => setDraft({ ...draft, bankCode: event.target.value.toUpperCase() || undefined })} maxLength={20} placeholder="BANK-A" />
      </label>
      <label>Exact payment UUID
        <input value={draft.paymentId ?? ""} onChange={(event) => setDraft({ ...draft, paymentId: event.target.value || undefined })} placeholder="UUID" />
      </label>
      <label>Created after
        <input type="datetime-local" value={isoToLocalInput(draft.createdFrom)} onChange={(event) => setDraft({ ...draft, createdFrom: localInputToIso(event.target.value) })} />
      </label>
      <label>Created before
        <input type="datetime-local" value={isoToLocalInput(draft.createdTo)} onChange={(event) => setDraft({ ...draft, createdTo: localInputToIso(event.target.value) })} />
      </label>
      <Button type="submit">Apply filters</Button>
      <Button type="button" variant="quiet" onClick={() => { setDraft({}); navigateFilters({}); }}>Clear</Button>
    </form>
    {locationState.warning && <p className="ops-banner ops-banner-warning" role="status">{locationState.warning}</p>}
    {page?.projection.status !== "ready" && page && <p className="ops-banner ops-banner-warning" role="status">Worklist projection is {page.projection.status}. Open a payment for authoritative details.</p>}
    {resource.stale && page && <p className="ops-banner ops-banner-warning" role="status">Showing an older page while refreshing. Last fetched {resource.fetchedAt ? new Date(resource.fetchedAt).toLocaleString() : "earlier"}.</p>}
    {error && <InlineError message={error} />}
    {resource.loading && !page && <p role="status">Loading worklist…</p>}
    {!resource.loading && !page && !error && <p role="status">No worklist data is available.</p>}
    {page && <>
      <p className="ops-muted" role="status">Showing {page.items.length} payments on this page · Projection as of {new Date(page.asOf).toLocaleString()}</p>
      {page.items.length === 0 ? <EmptyState title="No matching payments" description="Try another filter or return to the attention view." /> :
        <DataTable<WorklistItem> caption="Payments on the loaded worklist page" rowKey={(item) => item.paymentId} rows={page.items} columns={[
          { key: "payment", heading: "Payment", render: (item) => <a className="ops-link ops-mono" href={`/console/payments/${item.paymentId}`} onClick={(event) => { event.preventDefault(); onNavigate(`/console/payments/${item.paymentId}`); }}>{item.paymentId}</a> },
          { key: "state", heading: "State", render: (item) => <OpsStatusBadge state={item.state} /> },
          { key: "amount", heading: "Amount", render: (item) => formatPaiseDecimal(item.amountPaise, item.currency) },
          { key: "banks", heading: "Banks", render: (item) => `${item.sourceBankCode ?? "Unknown"} → ${item.destinationBankCode ?? "Unknown"}` },
          { key: "recovery", heading: "Recovery", render: (item) => <>{item.recoveryStatus.replaceAll("_", " ")} · {item.attemptCount} attempts</> },
          { key: "age", heading: "Age", render: (item) => `${Math.floor(item.ageMs / 60000)} min` },
        ]} />}
      <div className="ops-controls">
        {filters.cursor && <Button variant="secondary" onClick={() => window.history.back()}>Previous page</Button>}
        {page.nextCursor && <Button variant="secondary" onClick={() => navigateFilters({ ...filters, cursor: page.nextCursor! })}>Next page</Button>}
      </div>
    </>}
  </div>;
}
