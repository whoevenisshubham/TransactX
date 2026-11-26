import React, { useEffect, useState } from "react";
import { api } from "../../api";
import { Button, InlineError, PageHeader, Skeleton } from "../../components";
import type { ActivityEvent } from "./types";
import { ConsoleIcon } from "./ConsoleIcon";
import { formatIso } from "./format";

export function ActivityView({ token, liveRevision }: { token: string; liveRevision: number }) {
  const [events, setEvents] = useState<ActivityEvent[]>([]);
  const [nextOffset, setNextOffset] = useState<number | undefined>();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  function loadEvents(offset = 0, append = false) {
    setLoading(true);
    setError("");
    api.opsActivity(token, 50, offset)
      .then((page) => {
        setEvents((current) => (append ? [...current, ...(page.items ?? [])] : page.items ?? []));
        setNextOffset(page.nextOffset);
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : "Failed to load operational activity.");
      })
      .finally(() => setLoading(false));
  }

  useEffect(() => {
    loadEvents(0, false);
  }, [token, liveRevision]);

  return (
    <>
      <PageHeader
        eyebrow="Audit Feed"
        title="Operational Activity Log"
        description="Durable circuit, chaos, reconciliation, integrity, and routing facts in deterministic time order."
        action={
          <Button variant="secondary" onClick={() => loadEvents(0, false)}>
            <ConsoleIcon name="refresh" size={14} />
            Refresh
          </Button>
        }
      />

      {error && <InlineError message={error} />}

      {loading && events.length === 0 ? (
        <ConsoleSkeleton />
      ) : events.length === 0 ? (
        <div className="console-card">
          <span className="console-card-kicker">Activity Log</span>
          <p className="console-meta-text">No durable operational activity has been recorded.</p>
        </div>
      ) : (
        <section className="section-block">
          <div className="console-table-wrap" tabIndex={0} role="region" aria-label="Operational evidence table">
            <table className="console-table">
              <thead>
                <tr>
                  <th>Time</th>
                  <th>Category</th>
                  <th>Event</th>
                  <th>Target</th>
                  <th>Summary</th>
                  <th>Severity</th>
                </tr>
              </thead>
              <tbody>
                {events.map((ev) => (
                  <tr key={ev.id}>
                    <td className="ops-mono tabular-nums text-xs whitespace-nowrap">{formatIso(ev.occurredAt)}</td>
                    <td>
                      <span className="console-pill console-pill-neutral">{ev.category}</span>
                    </td>
                    <td>
                      <span className="font-medium text-[var(--text-primary)]">{ev.title}</span>
                      <div className="ops-mono text-[11px] text-[var(--text-tertiary)]">{ev.eventType}</div>
                    </td>
                    <td className="ops-mono font-semibold">{ev.targetId || "—"}</td>
                    <td>{ev.summary}</td>
                    <td>
                      <span
                        className={`console-pill ${
                          ev.severity === "ERROR"
                            ? "console-pill-error"
                            : ev.severity === "WARNING"
                            ? "console-pill-warning"
                            : "console-pill-success"
                        }`}
                      >
                        {ev.severity}
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      )}
      {nextOffset !== undefined && (
        <Button variant="secondary" disabled={loading} onClick={() => loadEvents(nextOffset, true)}>
          {loading ? "Loading..." : "Load more"}
        </Button>
      )}
    </>
  );
}

function ConsoleSkeleton() {
  return (
    <div style={{ display: "grid", gap: "1rem" }}>
      <Skeleton className="skeleton-heading" />
      <div className="console-grid-4">
        <Skeleton className="skeleton-block" />
        <Skeleton className="skeleton-block" />
        <Skeleton className="skeleton-block" />
        <Skeleton className="skeleton-block" />
      </div>
    </div>
  );
}
