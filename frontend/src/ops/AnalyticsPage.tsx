import { useCallback, useEffect, useState } from "react";
import { Button, EmptyState, InlineError, PageHeader } from "../components";
import { DataTable } from "../design/OpsPrimitives";
import { liveOpsClient } from "./client";
import type { AnalyticsWindow } from "./contracts";
import { useResource } from "./useResource";
import { AnalyticsCharts } from "./AnalyticsCharts";

function rate(value: number | null): string { return value === null ? "Unavailable" : `${(value * 100).toFixed(1)}%`; }
function metric(value: number | null, unit: string): string { return value === null ? "Unavailable" : `${value.toFixed(1)} ${unit}`; }
function utc(value: string): string { return new Date(value).toISOString().replace("T", " ").replace(".000Z", " UTC"); }

export function AnalyticsPage({ token, liveRevision }: { token: string; liveRevision: number }) {
  const [cursor, setCursor] = useState<string | undefined>(() => new URLSearchParams(window.location.search).get("cursor") || undefined);
  const load = useCallback((signal: AbortSignal) => liveOpsClient.analytics(token, cursor, signal), [token, cursor]);
  const resource = useResource(cursor ?? "first", load);
  useEffect(() => {
    const onPop = () => setCursor(new URLSearchParams(window.location.search).get("cursor") || undefined);
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, []);
  useEffect(() => {
    if (!liveRevision || document.hidden) return;
    const timer = window.setTimeout(resource.refresh, 350);
    return () => window.clearTimeout(timer);
  }, [liveRevision, resource.refresh]);
  function navigate(next: string | undefined) {
    window.history.pushState({}, "", `/console/analytics${next ? `?cursor=${encodeURIComponent(next)}` : ""}`);
    setCursor(next);
  }
  const page = resource.key === (cursor ?? "first") ? resource.data : null;
  return <div className="ops-page">
    <PageHeader eyebrow="Operations" title="Operational analytics" description="Windowed observations and advisory anomaly signals. These measurements never authorize routing or settlement." action={<Button variant="secondary" onClick={resource.refresh}>Refresh</Button>} />
    {resource.loading && !page && <p role="status">Loading analytics…</p>}
    {resource.error && <InlineError message={resource.error} />}
    {resource.stale && page && <p className="ops-banner ops-banner-warning" role="status">Showing older analytics while refreshing.</p>}
    {page && <>
      <p className="ops-muted">Window data as of {utc(page.asOf)} · {page.items.length} windows on this page</p>
      <p className="analytics-provenance"><strong>Statistical baseline is the default.</strong> Experimental model signals are advisory. A missing score or alert does not prove a healthy system; calibration attestation is not supplied by this response.</p>
      {page.items.length > 0 && <AnalyticsCharts items={page.items} />}
      {page.items.length === 0 ? <EmptyState title="No analytics windows" description="No measured operational window is available for this page." /> :
        <DataTable<AnalyticsWindow> caption="Measured operational windows with a table alternative to charts" rows={page.items} rowKey={(item) => item.id} columns={[
          { key: "entity", heading: "Entity", render: (item) => `${item.entityType.replaceAll("_", " ")} · ${item.entityId}` },
          { key: "window", heading: "Window (UTC)", render: (item) => <>{utc(item.from)} → {utc(item.to)}</> },
          { key: "sample", heading: "Samples", render: (item) => item.sampleCount },
          { key: "rates", heading: "Timeout / failure", render: (item) => `${rate(item.features.timeoutRate)} / ${rate(item.features.failureRate)}` },
          { key: "latency", heading: "P95 latency", render: (item) => metric(item.features.p95LatencyMs, "ms") },
          { key: "model", heading: "Advisory model", render: (item) => <>{item.model.status.replaceAll("_", " ")} · {item.model.version ?? "no version"}{item.model.score === null ? "" : ` · score ${item.model.score.toFixed(3)}`}</> },
          { key: "baseline", heading: "Baseline", render: (item) => item.baselineAlert?.message ?? "No baseline alert recorded" },
        ]} />}
      <div className="ops-controls">
        {cursor && <Button variant="secondary" onClick={() => navigate(undefined)}>First page</Button>}
        {page.nextCursor && <Button variant="secondary" onClick={() => navigate(page.nextCursor!)}>Next page</Button>}
      </div>
    </>}
  </div>;
}
