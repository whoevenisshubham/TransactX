import { DotSentinel, SpotlightCard } from "../components/effects";
import type { HealthSnapshot } from "../types";

export function HealthObservationCard({ targetId, snapshot }: { targetId: string; snapshot: HealthSnapshot | null }) {
  const measured = snapshot !== null && snapshot.sampleCount > 0;
  const old = measured && Date.now() - Date.parse(snapshot.computedAt) > 5 * 60 * 1000;
  const pct = measured ? Math.max(0, Math.min(100, snapshot.score * 100)) : 0;
  const filledSegments = Math.round(pct / 10);
  const accent = !snapshot || old || (measured && pct < 75) ? "amber" : measured ? "emerald" : "cobalt";
  const sentinelState = !snapshot ? "warning" : !measured ? "idle" : old ? "warning" : pct >= 85 ? "success" : "warning";

  return (
    <SpotlightCard as="article" accent={accent} className="health-observation-card shadow-[var(--shadow-subtle)]">
      <div className="health-observation-header">
        <h3 className="inline-flex items-center gap-2">
          <DotSentinel state={sentinelState} />
          {targetId}
        </h3>
        <span className={`console-pill ${old ? "console-pill-warning" : "console-pill-neutral"}`}>
          {!snapshot ? "Unavailable" : !measured ? "No samples" : old ? "Older observation" : "Recorded observation"}
        </span>
      </div>
      <strong className="health-observation-score font-display tabular-nums">
        {measured ? `${(snapshot.score * 100).toFixed(1)}%` : "Not evaluated"}
      </strong>
      <p className="ops-muted">
        {measured
          ? "Recorded rolling health score"
          : snapshot
          ? "No measured samples in this window. A zero-sample score does not establish health."
          : "The current observation could not be collected. This does not establish that the bank is down."}
      </p>
      {measured && (
        <>
          <div className="health-score-track" aria-hidden="true">
            <span style={{ width: `${pct}%` }} />
          </div>
          {/* Grafana-style 10-segment LCD gauge */}
          <div className="grid grid-cols-10 gap-1 pt-0.5" aria-hidden="true">
            {Array.from({ length: 10 }).map((_, idx) => {
              const lit = idx < filledSegments;
              const segColor =
                idx >= 8
                  ? "bg-emerald-500"
                  : idx >= 5
                  ? "bg-blue-500"
                  : "bg-amber-500";
              return (
                <span
                  key={idx}
                  className={`h-1.5 rounded-[2px] transition-opacity duration-300 ${
                    lit ? segColor : "bg-[var(--bg-surface-raised)] border border-[var(--border-hairline)]"
                  }`}
                />
              );
            })}
          </div>
          <dl className="health-observation-metrics">
            <div><dt>Availability</dt><dd>{(snapshot.availabilityScore * 100).toFixed(1)}%</dd></div>
            <div><dt>Success</dt><dd>{(snapshot.successScore * 100).toFixed(1)}%</dd></div>
            <div><dt>Samples</dt><dd>{snapshot.sampleCount}</dd></div>
            <div><dt>Latency penalty</dt><dd>{snapshot.latencyPenalty.toFixed(3)}</dd></div>
          </dl>
        </>
      )}
      {snapshot && (
        <p className="health-source-time">
          Computed {new Date(snapshot.computedAt).toLocaleString()} (device timezone)
        </p>
      )}
    </SpotlightCard>
  );
}
