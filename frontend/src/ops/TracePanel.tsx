import { useCallback, useState } from "react";
import { Button, InlineError } from "../components";
import { DataTable } from "../design/OpsPrimitives";
import { liveOpsClient } from "./client";
import type { SourceStatus, Trace } from "./contracts";
import { useResource } from "./useResource";

export function TracePanel({ traceIds, sourceStatus, token }: { traceIds: string[]; sourceStatus: SourceStatus; token: string }) {
  const [selected, setSelected] = useState<string | null>(null);
  return <section aria-label="Trace context">
    <h2>Trace context</h2>
    <p>Trace source: {sourceStatus.replaceAll("_", " ")}. Traces describe service observations and do not prove settlement.</p>
    {traceIds.length === 0 && <p>No trace was collected for this snapshot.</p>}
    {traceIds.map((id) => <Button key={id} variant="quiet" onClick={() => setSelected(id)} aria-pressed={selected === id}>View trace {id}</Button>)}
    {selected && <TraceDetail key={selected} traceId={selected} token={token} />}
  </section>;
}

function TraceDetail({ traceId, token }: { traceId: string; token: string }) {
  const load = useCallback((signal: AbortSignal) => liveOpsClient.trace(traceId, token, signal), [traceId, token]);
  const resource = useResource(traceId, load);
  return <div>
    {resource.loading && <p role="status">Loading trace…</p>}
    {resource.error && <InlineError message={resource.error} />}
    {resource.data && <>
      <p>Collected {new Date(resource.data.collectedAt).toLocaleString()} · {resource.data.sourceStatus.replaceAll("_", " ")}</p>
      {resource.data.truncated && <p className="ops-banner ops-banner-warning">This trace was truncated to the server limit.</p>}
      {resource.data.spans.length === 0 ? <p>No spans were collected.</p> :
        <DataTable<Trace["spans"][number]> caption="Allowlisted service spans" rows={resource.data.spans} rowKey={(span) => span.spanId} columns={[
          { key: "service", heading: "Service", render: (span) => span.service },
          { key: "name", heading: "Span", render: (span) => span.name },
          { key: "time", heading: "Started", render: (span) => new Date(span.startedAt).toLocaleString() },
          { key: "status", heading: "Status", render: (span) => span.status },
          { key: "attributes", heading: "Safe attributes", render: (span) => <pre className="ops-mono">{JSON.stringify(span.attributes, null, 2)}</pre> },
        ]} />}
    </>}
  </div>;
}
